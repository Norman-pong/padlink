// 语音听写 ViewModel（PRD §4.5）：权限 → AsrSession → 最终结果发 TEXT 的状态机。
// 快照字段全部 @Trace 驱动 UI；phase 状态：Idle → Initializing →（Listening ⇄ Recognizing）
// → Sending → Done / Error。松手 finish 后等最终结果（isLast==true）才发送，空文本不发；
// 发送失败保留 finalText 供 resend()（Transport.sendReliable 吞错只计数，用 tcpSendFail
// 计数差判失败，不改 Transport）。权限被拒不抛异常：Error 态 + 引导文案键，去设置走
// requestPermissionOnSetting（request-user-authorization-second 现查）。
import { abilityAccessCtrl, bundleManager } from '@kit.AbilityKit';
import type { common, Permissions } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { privacyManager } from '@kit.AppGalleryKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import type { ConnectionManager } from '../net/ConnectionManager';
import { newText } from '../proto/Packet';
import { AsrSession } from './AsrSession';
import { asrErrorKey } from './AsrConfig';

const TAG: string = 'PadLink.Voice';
const DOMAIN: number = 0x5050;
// SDK 的 Permissions 为字面量联合类型，须以字面量声明（string 不能赋值）。
const MIC_PERMISSION: Permissions = 'ohos.permission.MICROPHONE';

// 会话阶段常量（readonly 常量类，同 ConnState 风格）。
export class VoicePhase {
  public static readonly IDLE: number = 0;
  public static readonly INITIALIZING: number = 1; // 权限申请 + createEngine 中
  public static readonly LISTENING: number = 2; // startListening 成功，尚无结果
  public static readonly RECOGNIZING: number = 3; // 中间结果已到 / 松手等待最终结果
  public static readonly SENDING: number = 4; // 最终结果发送中
  public static readonly DONE: number = 5; // 已上屏（或空结果收口）
  public static readonly ERROR: number = 6; // errorKey 标明原因（权限/忙碌/发送失败…）
}

@ObservedV2
export class VoiceModel {
  @Trace phase: number = VoicePhase.IDLE;
  @Trace interimText: string = ''; // 中间结果（实时显示，非累计拍为“当前最优”）
  @Trace finalText: string = ''; // 最终结果（累计），发送失败时保留供重发
  @Trace errorKey: string = ''; // 文案资源键（UI 用 pageResStr 解析为本地化文案）
  @Trace sendFailed: boolean = false; // true 时 UI 展示重发按钮

  private conn: ConnectionManager | null = null;
  private context: common.UIAbilityContext | null = null;
  private asr: AsrSession | null = null;
  // 会话代次：press/cancel 递增，使在途异步（权限弹窗/createEngine）回调失效。
  private gen: number = 0;
  private finishWhenReady: boolean = false; // begin 未完成时已松手
  private awaitingFinal: boolean = false; // 已松手/已判停，等待最终结果
  private accumulated: string = ''; // 子句最终结果累计
  private sent: boolean = false; // 本轮是否已成功上屏（防 isLast+onComplete 双发）

  // Index 装配：连接管理器与 UIAbility 上下文（权限弹窗需要）。
  public attach(conn: ConnectionManager, context: common.UIAbilityContext): void {
    this.conn = conn;
    this.context = context;
    const model: VoiceModel = this;
    const session: AsrSession = new AsrSession();
    session.onStarted = (): void => {
      // startListening 成功；phase 由 press() 在 begin 返回后置位
    };
    session.onResult = (text: string, isFinal: boolean, isLast: boolean): void => {
      model.handleResult(text, isFinal, isLast);
    };
    session.onComplete = (): void => {
      model.handleComplete();
    };
    session.onError = (errKey: string, code: number, message: string): void => {
      model.handleError(errKey, code);
    };
    this.asr = session;
  }

  // 按下：权限检查 → 申请 → begin → LISTENING。被拒/未连接/引擎失败一律落 ERROR 态，不抛异常。
  public async press(): Promise<void> {
    if (this.phase !== VoicePhase.IDLE && this.phase !== VoicePhase.DONE
      && this.phase !== VoicePhase.ERROR) {
      return; // 会话进行中，忽略重复按下
    }
    const myGen: number = ++this.gen;
    this.resetSession();
    if (this.conn === null || !this.conn.isConnected()) {
      this.phase = VoicePhase.ERROR;
      this.errorKey = 'voice_need_connect';
      return;
    }
    this.phase = VoicePhase.INITIALIZING;
    const granted: boolean = await this.ensureMicPermission();
    if (myGen !== this.gen) {
      return; // 等弹窗期间已被 cancel
    }
    if (!granted) {
      this.phase = VoicePhase.ERROR;
      this.errorKey = 'voice_err_mic_permission'; // UI 分支展示「去设置开启」引导
      return;
    }
    const session: AsrSession | null = this.asr;
    if (session === null) {
      return;
    }
    try {
      await session.begin();
    } catch (err) {
      const e = err as BusinessError;
      if (myGen !== this.gen) {
        return;
      }
      hilog.error(DOMAIN, TAG, 'asr begin fail %{public}d %{public}s', e.code, e.message);
      this.phase = VoicePhase.ERROR;
      this.errorKey = asrErrorKey(e.code);
      return;
    }
    if (myGen !== this.gen) {
      session.cancel();
      return;
    }
    this.phase = VoicePhase.LISTENING;
    if (this.finishWhenReady) { // 按压瞬间即松手：就绪后立即结束
      this.finishWhenReady = false;
      this.awaitingFinal = true;
      this.phase = VoicePhase.RECOGNIZING;
      session.finish();
    }
  }

  // 松手：主动 finish，等最终结果（isLast）到达后 finalize → 发送。
  public release(): void {
    if (this.phase === VoicePhase.INITIALIZING) {
      this.finishWhenReady = true;
      return;
    }
    if (this.phase !== VoicePhase.LISTENING && this.phase !== VoicePhase.RECOGNIZING) {
      return;
    }
    const session: AsrSession | null = this.asr;
    if (session === null) {
      return;
    }
    this.awaitingFinal = true;
    this.phase = VoicePhase.RECOGNIZING; // 状态行保持「识别中」直到最终结果
    session.finish();
  }

  // 取消当前会话（切 Tab / 离开页面 / 三指切换打断按住）：丢在途结果，麦克风立即释放。
  // DONE/ERROR 态（已上屏内容、权限引导、发送失败重发位）不被抹掉。
  public cancelSession(): void {
    this.gen++;
    this.finishWhenReady = false;
    this.awaitingFinal = false;
    const session: AsrSession | null = this.asr;
    if (session !== null) {
      session.cancel();
    }
    this.interimText = '';
    if (this.phase === VoicePhase.LISTENING || this.phase === VoicePhase.RECOGNIZING
      || this.phase === VoicePhase.SENDING || this.phase === VoicePhase.INITIALIZING) {
      this.phase = VoicePhase.IDLE;
    }
  }

  // 重发上屏（发送失败后 finalText 保留，按钮触发）。
  public resend(): void {
    if (this.accumulated.length === 0 || this.sent) {
      return;
    }
    this.sendText();
  }

  // 权限引导态的「去设置开启」：直接拉起本应用的权限设置弹框（request-user-authorization-second）。
  public async openPermissionSettings(): Promise<void> {
    const ctx: common.UIAbilityContext | null = this.context;
    if (ctx === null) {
      return;
    }
    try {
      const res: Array<abilityAccessCtrl.GrantStatus> =
        await abilityAccessCtrl.createAtManager().requestPermissionOnSetting(ctx, [MIC_PERMISSION]);
      if (res.length > 0 && res[0] === abilityAccessCtrl.GrantStatus.PERMISSION_GRANTED
        && this.phase === VoicePhase.ERROR && this.errorKey === 'voice_err_mic_permission') {
        this.phase = VoicePhase.IDLE;
        this.errorKey = '';
      }
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'requestPermissionOnSetting fail %{public}d %{public}s', e.code, e.message);
    }
  }

  // 页面最终离开时释放引擎（AsrSession.shutdown 幂等；再次 press 自动重建）。
  public dispose(): void {
    this.gen++;
    const session: AsrSession | null = this.asr;
    if (session !== null) {
      session.shutdown();
    }
    this.phase = VoicePhase.IDLE;
    this.interimText = '';
  }

  // ---- 内部 ----

  private resetSession(): void {
    this.interimText = '';
    this.finalText = '';
    this.errorKey = '';
    this.sendFailed = false;
    this.accumulated = '';
    this.sent = false;
    this.awaitingFinal = false;
    this.finishWhenReady = false;
  }

  // 中间/最终结果同走 onResult：isFinal=false 实时刷新中间区；isFinal=true 累计并按
  // isLast 判定发送（hms-ai-speechrecognizer SpeechRecognitionResult 字段语义）。
  private handleResult(text: string, isFinal: boolean, isLast: boolean): void {
    const active: boolean = this.phase === VoicePhase.LISTENING
      || this.phase === VoicePhase.RECOGNIZING;
    if (!active && !this.awaitingFinal) {
      return; // 旧代次/已取消会话的迟到回调
    }
    if (!isFinal) {
      this.phase = VoicePhase.RECOGNIZING;
      this.interimText = this.accumulated + text;
      return;
    }
    this.accumulated += text;
    this.finalText = this.accumulated;
    this.interimText = '';
    if (isLast) {
      this.finalize();
    }
  }

  // onComplete 兜底：finish 后 isLast 结果异常缺失时按当前累计结果收口（sent 防双发）。
  private handleComplete(): void {
    if (!this.awaitingFinal) {
      return;
    }
    this.finalize();
  }

  private handleError(errKey: string, code: number): void {
    if (code === 1002200004 || code === 1002200005) {
      // finish/cancel 时无进行中任务：松手/取消的正常伴随回包，静默收口不算错。
      if (this.awaitingFinal) {
        this.finalize();
      }
      return;
    }
    if (code === 1002200002) {
      return; // 重复 start：会话已在进行中，忽略
    }
    this.awaitingFinal = false;
    this.phase = VoicePhase.ERROR;
    this.errorKey = errKey; // 1002200012 → voice_err_mic_permission（UI 引导态）
  }

  // 会话收口：空文本不发（PRD 口径）；有文本进发送流程。
  private finalize(): void {
    this.awaitingFinal = false;
    if (this.sent) {
      return;
    }
    if (this.accumulated.length === 0) {
      this.phase = VoicePhase.DONE; // UI 按 finalText 为空显示「未识别到内容」
      return;
    }
    this.sendText();
  }

  // 发送 TEXT（TCP 可靠通道）。sendReliable 内部吞错仅计数（既有口径），以 tcpSendFail
  // 计数差判定成败：失败保留 finalText 并置 sendFailed 供手动重发。
  private sendText(): void {
    const conn: ConnectionManager | null = this.conn;
    if (conn === null || !conn.isConnected()) {
      this.sendFailed = true;
      this.phase = VoicePhase.ERROR;
      this.errorKey = 'voice_err_send_failed';
      return;
    }
    this.phase = VoicePhase.SENDING;
    const failBefore: number = conn.transport.stats.tcpSendFail;
    conn.transport.sendReliable(newText(conn.transport.nextSeq(), this.accumulated))
      .then(() => {
        if (conn.transport.stats.tcpSendFail > failBefore) {
          this.sendFailed = true;
          this.phase = VoicePhase.ERROR;
          this.errorKey = 'voice_err_send_failed';
        } else {
          this.sent = true;
          this.phase = VoicePhase.DONE;
        }
      })
      .catch((err: BusinessError) => {
        this.sendFailed = true;
        this.phase = VoicePhase.ERROR;
        this.errorKey = 'voice_err_send_failed';
        hilog.warn(DOMAIN, TAG, 'text send fail %{public}d %{public}s', err.code, err.message);
      });
  }

  // AGC 隐私托管门控（store-privacy）：未签署隐私协议时系统拒绝权限申请，须先拉起标准化
  // 隐私弹窗征得同意。result 语义：1=同意完整模式、0=未同意。服务不可达（模拟器/侧载未托管）
  // 放行——隐私管理服务不支持模拟器，不应阻断开发链路；上架渠道包托管必达、门控必生效。
  private async ensurePrivacyConsented(ctx: common.UIAbilityContext): Promise<boolean> {
    let results: privacyManager.AppPrivacyResult[];
    try {
      results = privacyManager.getAppPrivacyResult();
    } catch (err) {
      hilog.info(DOMAIN, TAG, 'privacy mgmt unavailable, fail-open');
      return true;
    }
    if (results.length === 0) {
      return true;
    }
    const agreed: boolean =
      results.every((r: privacyManager.AppPrivacyResult): boolean => r.result === 1);
    if (agreed) {
      return true;
    }
    try {
      const consent: privacyManager.ConsentResult =
        await privacyManager.requestAppPrivacyConsent(ctx);
      const after: privacyManager.AppPrivacyResult[] = consent.results;
      return after.length > 0
        && after.every((r: privacyManager.AppPrivacyResult): boolean => r.result === 1);
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'requestAppPrivacyConsent fail %{public}d %{public}s', e.code, e.message);
      return false;
    }
  }

  // 权限检查 → 隐私签署门控 → 必要时弹窗申请。已授权直接过；被拒/异常返回 false（不抛异常）。
  private async ensureMicPermission(): Promise<boolean> {
    const ctx: common.UIAbilityContext | null = this.context;
    if (ctx === null) {
      return false;
    }
    const atManager: abilityAccessCtrl.AtManager = abilityAccessCtrl.createAtManager();
    try {
      const flags: number = bundleManager.BundleFlag.GET_BUNDLE_INFO_WITH_APPLICATION
        | bundleManager.BundleFlag.GET_BUNDLE_INFO_WITH_REQUESTED_PERMISSION;
      const info: bundleManager.BundleInfo = bundleManager.getBundleInfoForSelfSync(flags);
      const status: abilityAccessCtrl.GrantStatus =
        atManager.checkAccessTokenSync(info.appInfo.accessTokenId, MIC_PERMISSION);
      if (status === abilityAccessCtrl.GrantStatus.PERMISSION_GRANTED) {
        return true;
      }
    } catch (err) {
      hilog.warn(DOMAIN, TAG, 'checkAccessToken fail, fallback to request');
    }
    if (!(await this.ensurePrivacyConsented(ctx))) {
      return false; // 未同意隐私政策：系统必拒权限申请，不再弹权限框
    }
    try {
      const res = await atManager.requestPermissionsFromUser(ctx, [MIC_PERMISSION]);
      return res.authResults.length > 0 && res.authResults[0] === 0;
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'requestPermissionsFromUser fail %{public}d %{public}s', e.code, e.message);
      return false;
    }
  }
}
