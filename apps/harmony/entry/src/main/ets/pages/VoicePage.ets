// 语音听写页（PRD §4.5）：大「按住说话」按钮——DOWN=press / UP·CANCEL=release，
// 只认自己按下的 pointerId（KeyCap 同款模式，防多指串扰）；中间结果实时显示区 +
// 最终结果区（发送失败可手动重发）+ 权限拒绝引导态（文案 + 去设置开启）。
// 非连接态按钮禁用并提示。四态齐全：初始化/识别中（加载）、已上屏（成功）、
// 失败原因 + 重发/去设置（失败）、未识别到内容（空）。按钮带无障碍属性（HDS 硬约束 11），
// 播报提示随按住态动态切换（按住说话 ⇄ 松开结束）。
// 按压动效（motion.md §一 圆钮跟手缩放）：Down 走 animateTo 缩至 0.96，Up/Cancel 走
// curves.springMotion 弹簧回弹——双事件驱动可中断（stateStyles 无过程驱动，禁用）。
import { curves } from '@kit.ArkUI';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import { VoiceModel, VoicePhase } from '../common/voice/VoiceModel';
import { pageResStr } from './Res';

const BTN_SIZE_VP: number = 120; // 按住说话主按钮直径
const BTN_PRESS_SCALE: number = 0.96; // 按下跟手缩放档（motion.md §一 参考值）
const RESULT_MAX_LINES: number = 6;
const RESULT_MAX_HEIGHT_VP: number = 260;

// 页面文案键全集（aboutToAppear 一次性解析为本地化字符串，build 内查表）。
const TEXT_KEYS: string[] = [
  'voice_btn_label', 'voice_btn_a11y', 'voice_hold_hint',
  'voice_a11y_hold_to_speak', 'voice_a11y_release_to_end',
  'voice_need_connect', 'voice_phase_initializing', 'voice_phase_listening',
  'voice_phase_recognizing', 'voice_phase_sending', 'voice_phase_done', 'voice_empty_hint',
  'voice_err_mic_permission', 'voice_open_settings', 'voice_err_busy',
  'voice_err_create_failed', 'voice_err_start_failed', 'voice_err_max_audio',
  'voice_err_engine', 'voice_err_recognize', 'voice_err_unknown', 'voice_err_send_failed',
  'voice_resend',
];

@ComponentV2
export struct VoicePage {
  @Require @Param voice: VoiceModel;
  @Require @Param conn: ConnectionManager;
  @Local private pressed: boolean = false;
  @Local private pressScale: number = 1; // 按住键跟手缩放（层3 物理动效：按下形变/弹簧回弹）
  @Local private pointerId: number = -1; // 只认自己按下的指针
  private texts: Map<string, string> = new Map();

  aboutToAppear(): void {
    for (const k of TEXT_KEYS) {
      this.texts.set(k, pageResStr(this.getUIContext(), k));
    }
  }

  aboutToDisappear(): void {
    // 兜底释放：切 Tab 由 MainControlPage.cancelSession 负责，这里覆盖页面被移除的路径。
    this.voice.cancelSession();
  }

  private str(key: string): string {
    const v: string | undefined = this.texts.get(key);
    return v !== undefined ? v : key;
  }

  private connected(): boolean {
    return this.conn.snapshot.state === ConnState.CONNECTED;
  }

  // 状态行文案键：错误态显示错误键，会话态显示阶段键，空闲显示持机提示。
  private statusKey(): string {
    if (this.voice.phase === VoicePhase.ERROR) {
      return this.voice.errorKey.length > 0 ? this.voice.errorKey : 'voice_err_unknown';
    }
    switch (this.voice.phase) {
      case VoicePhase.INITIALIZING:
        return 'voice_phase_initializing';
      case VoicePhase.LISTENING:
        return 'voice_phase_listening';
      case VoicePhase.RECOGNIZING:
        return 'voice_phase_recognizing';
      case VoicePhase.SENDING:
        return 'voice_phase_sending';
      case VoicePhase.DONE:
        return 'voice_phase_done';
      default:
        return 'voice_hold_hint';
    }
  }

  private isErrorPhase(): boolean {
    return this.voice.phase === VoicePhase.ERROR;
  }

  // 按住期间（含初始化与等待最终结果）按钮保持点亮。
  private holding(): boolean {
    return this.voice.phase === VoicePhase.INITIALIZING
      || this.voice.phase === VoicePhase.LISTENING
      || this.voice.phase === VoicePhase.RECOGNIZING
      || this.pressed;
  }

  // 大按钮触摸：DOWN 登记自己的 pointerId 并 press；UP/CANCEL 匹配后 release。
  // 缩放动效（motion.md §一）：Down 跟手缩至 0.96；Up/Cancel springMotion 弹簧回弹——
  // animateTo 弹簧可中断（按住即停、抬手即弹，无「播完才响应」）。
  private handleTouch(ev: TouchEvent): void {
    if (ev.type === TouchType.Down) {
      if (this.pointerId === -1 && ev.changedTouches.length > 0) {
        this.pointerId = ev.changedTouches[0].id;
        this.pressed = true;
        this.getUIContext().animateTo({
          duration: 120,
          curve: Curve.EaseOut
        }, () => {
          this.pressScale = BTN_PRESS_SCALE;
        });
        this.voice.press();
      }
      return;
    }
    if (ev.type === TouchType.Up || ev.type === TouchType.Cancel) {
      let matched: boolean = false;
      for (let i = 0; i < ev.changedTouches.length; i++) {
        if (ev.changedTouches[i].id === this.pointerId) {
          matched = true;
        }
      }
      if (matched) {
        this.pointerId = -1;
        this.pressed = false;
        this.getUIContext().animateTo({
          curve: curves.springMotion(0.42, 0.72)
        }, () => {
          this.pressScale = 1;
        });
        this.voice.release();
      }
    }
  }

  build() {
    Column({ space: 12 }) {
      // 结果展示区：中间结果（次级色，实时刷新）/ 最终结果（主卡）/ 空结果提示。
      Scroll() {
        Column({ space: 8 }) {
          if (this.voice.interimText.length > 0) {
            Text(this.voice.interimText)
              .fontSize($r('sys.float.ohos_id_text_size_body2'))
              .fontColor($r('sys.color.font_secondary'))
              .width('100%')
          }
          if (this.voice.finalText.length > 0) {
            Text(this.voice.finalText)
              .fontSize($r('sys.float.ohos_id_text_size_body1'))
              .fontColor($r('sys.color.font_primary'))
              .width('100%')
              .padding(12)
              .borderRadius(12)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
          }
          if (this.voice.phase === VoicePhase.DONE && this.voice.finalText.length === 0) {
            Text(this.str('voice_empty_hint'))
              .fontSize($r('sys.float.ohos_id_text_size_body2'))
              .fontColor($r('sys.color.font_tertiary'))
          }
        }
        .width('100%')
        .alignItems(HorizontalAlign.Start)
      }
      .width('100%')
      .constraintSize({ minHeight: 80, maxHeight: RESULT_MAX_HEIGHT_VP })
      .align(Alignment.Top)
      .layoutWeight(1)

      // 状态行（识别中 / 已上屏 / 失败原因），错误态用警示色。
      Text(this.str(this.statusKey()))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor(this.isErrorPhase()
          ? $r('sys.color.warning') : $r('sys.color.font_secondary'))
        .textAlign(TextAlign.Center)
        .width('100%')
        .maxLines(2)

      // 错误动作行：权限拒绝 → 去设置开启；发送失败 → 重发（finalText 已保留）。
      Row({ space: 12 }) {
        if (this.isErrorPhase() && this.voice.errorKey === 'voice_err_mic_permission') {
          Button(this.str('voice_open_settings'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .onClick(() => {
              this.voice.openPermissionSettings();
            })
        }
        if (this.voice.sendFailed) {
          Button(this.str('voice_resend'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .onClick(() => {
              this.voice.resend();
            })
        }
      }

      // 提示行：未连接给出原因，连接中提示操作方式。
      Text(this.connected() ? this.str('voice_hold_hint') : this.str('voice_need_connect'))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_tertiary'))
        .textAlign(TextAlign.Center)
        .width('100%')

      // 大「按住说话」按钮：非连接态禁用（enabled 关断触摸）并降透明度。
      Text(this.str('voice_btn_label'))
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor(this.holding()
          ? $r('sys.color.font_on_primary') : $r('sys.color.font_primary'))
        .textAlign(TextAlign.Center)
        .width(BTN_SIZE_VP)
        .height(BTN_SIZE_VP)
        .borderRadius(BTN_SIZE_VP / 2)
        .backgroundColor(this.holding()
          ? $r('sys.color.comp_background_emphasize') : $r('sys.color.comp_background_tertiary'))
        .opacity(this.connected() ? 1 : 0.4)
        .enabled(this.connected())
        .scale({ x: this.pressScale, y: this.pressScale })
        .onTouch((ev: TouchEvent) => {
          this.handleTouch(ev);
        })
        .accessibilityText(this.str('voice_btn_a11y'))
        .accessibilityLevel('yes')
        // 动态操作提示（D12）：空闲播「按住说话」，按住中播「松开结束」。
        .accessibilityDescription(this.holding()
          ? this.str('voice_a11y_release_to_end') : this.str('voice_a11y_hold_to_speak'))
        .margin({ bottom: 16 })
    }
    .width('100%')
    .height('100%')
    .padding({ left: 24, right: 24, top: 12 })
    .alignItems(HorizontalAlign.Center)
  }
}
