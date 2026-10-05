// 配对 ViewModel（FR-0 M2）：发现 → 选择主机 → 无 token 连接 → PAIR_REQ 发起 → 码校验 →
// PAIR_OK 验签绑定 token → CONNECTED。状态机 Idle/Discovering/ConnectingHost/PairingPending/
// Verifying/Paired/Failed；PAIR_NAK reason 文案走 PairingCore 资源键 + string.json。
// 设备名取 @ohos.deviceInfo（js-apis-device-info，API6+）：marketName 优先、productModel 兜底，
// 发送前经 sanitizeDeviceName 截到 ≤64 字节（daemon MaxNameLen）。
import { deviceInfo } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ConnState } from '../net/ConnectionManager';
import type { ConnectionManager } from '../net/ConnectionManager';
import { Discoverer, DiscoverPolicy } from '../net/Discoverer';
import type { DiscoveredHost } from '../net/DiscoverCore';
import { hexToBytes, bytesToHex } from '../net/HexCodec';
import type { SettingsModel } from './SettingsModel';
import { HostInfo } from './SettingsModel';
import { HostsFullError } from './HostsStore';
import { nakReasonKey, validatePairCode, parsePairNakReason, pairOkToken,
  buildPairReqInitJson, buildPairReqCodeBytes, sanitizeDeviceName } from './PairingCore';
import { PacketType } from '../proto/PacketType';
import { ResError } from '../ResError';
import { newRaw } from '../proto/Packet';
import type { Packet } from '../proto/Packet';
import { utf8Encode } from '../proto/Utf8';
import { verifyHmac } from '../proto/Auth';
import { CryptoHmacProvider } from '../net/CryptoHmac';

const TAG: string = 'PadLink.Pairing';
const DOMAIN: number = 0x5050;
// daemon 单端口约定：发现广播与 TCP/UDP 数据面同端口（PROTOCOL.md §1）。
const DEFAULT_PORT: number = 53021;
// PAIR_OK/PAIR_NAK 应答超时：覆盖 daemon 侧校验与回包（本机局域网 RTT ≪ 1s，取 4s 容忍抖动）。
const PAIR_REPLY_TIMEOUT_MS: number = 4000;

// 配对流程状态（readonly 常量类，同 ConnState 风格）。
export class PairPhase {
  // 空闲/发现结果展示（列表或空态）。
  public static readonly IDLE: number = 0;
  // 发现中（UDP 广播窗口）。
  public static readonly DISCOVERING: number = 1;
  // 正在连接选中主机（TCP + 无 token HELLO）。
  public static readonly CONNECTING: number = 2;
  // 等待用户输入确认码（PAIR_REQ 已发起，daemon 主机侧展示确认码）。
  public static readonly PENDING_CODE: number = 3;
  // 码已提交，等待 PAIR_OK/PAIR_NAK。
  public static readonly VERIFYING: number = 4;
  // 配对成功（token 已绑定，短暂停留后回主控页）。
  public static readonly PAIRED: number = 5;
  // 失败（errorText/errorKey 给原因，可重试）。
  public static readonly FAILED: number = 6;
}

// UI 可观察快照：字段仅由 PairingModel 写入（页面只读）。
@ObservedV2
export class PairingSnapshot {
  @Trace phase: number = PairPhase.IDLE;
  // 发现结果（整体重赋值触发刷新，项内容不可变）。
  @Trace hosts: DiscoveredHost[] = [];
  // 选中主机展示名（name 为空时用 ip）。
  @Trace hostLabel: string = '';
  // 快速连接入口标签（''=无 lastHost 档案）。
  @Trace quickLabel: string = '';
  // 动态错误文案（连接/发现/回包异常等运行时原因）。
  @Trace errorText: string = '';
  // 静态错误文案资源键（PAIR_NAK 映射；页面 getStringByNameSync 取）。
  @Trace errorKey: string = '';
  // errorKey 的格式化参数（%1$s 按序替换；无参时为空数组）。
  @Trace errorParams: string[] = [];
  // 补充提示资源键（如快连失败时的自动重连引导；''= 无）。
  @Trace errorHintKey: string = '';
  // 确认码输入镜像（4 格回显 + 输满自动提交）。
  @Trace code: string = '';
}

export class PairingModel {
  public readonly snapshot: PairingSnapshot = new PairingSnapshot();
  // 配对/快连成功回调（页面借此短暂停留后返回主控页；PairingModel 非 @ObservedV2，不用 @Monitor 深路径）。
  public onPaired: (() => void) | null = null;
  // 配对成功但档案保存被拒（本机列表满 4 台）回调：页面 toast 提示先删除；连接本身不受影响。
  public onHostsFull: (() => void) | null = null;

  private conn: ConnectionManager;
  private settings: SettingsModel;
  private discoverer: Discoverer = new Discoverer();
  private selected: DiscoveredHost | null = null;
  private pairWaiter: ((pkt: Packet | null) => void) | null = null;
  private deviceNameCache: string | null = null;

  constructor(conn: ConnectionManager, settings: SettingsModel) {
    this.conn = conn;
    this.settings = settings;
    // PAIR_*/DISCOVER_RESP 经 Transport→ConnectionManager 路由至此（连接态由 CM 处理，配对态在此消费）
    conn.onPairPacket = (pkt: Packet) => {
      this.onPairPacket(pkt);
    };
  }

  // ---- 页面生命周期 ----

  // 进入发现页：刷新快速连接入口；空闲态自动发起一次发现。
  public onPageEnter(): void {
    this.refreshQuickLabel();
    // 上一趟配对残留的 PAIRED（成功视图已在弹出前展示过）按全新进入处理：清结果重扫，
    // 否则已配对用户再进发现页只会看到定格的成功视图，无法发起新配对。
    if (this.snapshot.phase === PairPhase.PAIRED) {
      this.snapshot.phase = PairPhase.IDLE;
      this.snapshot.hosts = [];
    }
    if (this.snapshot.phase === PairPhase.IDLE && this.snapshot.hosts.length === 0) {
      this.startDiscovery();
    }
  }

  // 离开发现页：配对进行中则取消会话（PAIRED/已完成连接不回收，回主控页即用）。
  public onPageLeave(): void {
    const p: number = this.snapshot.phase;
    if (p === PairPhase.DISCOVERING || p === PairPhase.CONNECTING
      || p === PairPhase.PENDING_CODE || p === PairPhase.VERIFYING) {
      this.cancel();
    }
  }

  // ---- 流程方法 ----

  // UDP 广播发现（窗口内收集全部应答，按 IP 去重）。
  // conn 浮层态只在断开态（IDLE/LOST）下挂 DISCOVERING：已连接/重连中的会话不被发现窗口
  // 改写（CONNECTED 被踩成 IDLE 会让主控页误判离线、RECONNECTING 被踩会掐断重连链）；
  // 回收时只认仍是 DISCOVERING 的态（防覆盖窗口内由其他路径完成的连接态迁移）。
  public async startDiscovery(): Promise<void> {
    if (this.snapshot.phase === PairPhase.DISCOVERING) {
      return;
    }
    this.resetError();
    this.snapshot.phase = PairPhase.DISCOVERING;
    const s: number = this.conn.snapshot.state;
    if (s === ConnState.IDLE || s === ConnState.LOST || s === ConnState.DISCOVERING) {
      this.conn.snapshot.state = ConnState.DISCOVERING; // 浮层展示（发现窗口由本模型驱动）
    }
    try {
      const hosts: DiscoveredHost[] = await this.discoverer.discover(DiscoverPolicy.WINDOW_MS, DEFAULT_PORT);
      this.snapshot.hosts = hosts;
      this.snapshot.phase = PairPhase.IDLE;
      if (this.conn.snapshot.state === ConnState.DISCOVERING) {
        this.conn.snapshot.state = ConnState.IDLE;
      }
    } catch (err) {
      this.snapshot.hosts = [];
      this.setKeyError('pair_discover_failed_fmt', [(err as Error).message]);
      this.snapshot.phase = PairPhase.FAILED;
      if (this.conn.snapshot.state === ConnState.DISCOVERING) {
        this.conn.snapshot.state = ConnState.IDLE;
      }
    }
  }

  // 选中主机：无 token 连接（HELLO 握手）→ 自动发起配对。
  public async selectHost(host: DiscoveredHost): Promise<void> {
    this.resetError();
    this.selected = host;
    this.snapshot.hostLabel = host.name.length > 0 ? host.name : host.ip;
    this.snapshot.code = '';
    this.snapshot.phase = PairPhase.CONNECTING;
    try {
      await this.conn.connectForPairing(host.ip, host.port, DEFAULT_PORT);
    } catch (err) {
      this.setConnError(err as Error);
      this.snapshot.phase = PairPhase.FAILED;
      return;
    }
    await this.startPairing();
  }

  // 发起（或重新发起）配对：PAIR_REQ JSON（成功无回包，确认码在主机屏幕展示）。
  public async startPairing(): Promise<void> {
    if (this.selected === null || this.snapshot.phase === PairPhase.PENDING_CODE) {
      return;
    }
    this.resetError();
    this.snapshot.code = '';
    this.snapshot.phase = PairPhase.PENDING_CODE;
    const json: string = buildPairReqInitJson(this.deviceName());
    const bytes: Uint8Array = utf8Encode(json);
    const pkt: Packet = newRaw(PacketType.PAIR_REQ, this.conn.transport.nextSeq(), bytes.buffer as ArrayBuffer);
    // 发送失败不上抛（sendUnsealed 同 sendReliable 口径）；会话已死时由提交码的超时/NAK 路径自愈
    await this.conn.transport.sendUnsealed(pkt);
  }

  // 重新发起按钮：会话可能已死（daemon 重启/断连），直接重建连接再发起，幂等自愈。
  public restartPairing(): void {
    if (this.selected === null) {
      return;
    }
    const host: DiscoveredHost = this.selected;
    this.conn.disconnect(); // 丢弃旧会话（回 IDLE）再重连
    this.selectHost(host);
  }

  // 确认码输入镜像（页面 TextInput onChange 调用；inputFilter 已限数字）。
  public setCode(code: string): void {
    this.snapshot.code = code;
  }

  // 提交确认码：PAIR_REQ 4B ASCII → PAIR_OK（新 token 封签，verifyHmac 自证）/ PAIR_NAK（文案可重输）。
  public async submitCode(code: string): Promise<void> {
    if (this.snapshot.phase === PairPhase.VERIFYING) {
      return;
    }
    if (!validatePairCode(code)) {
      this.snapshot.errorText = '';
      this.snapshot.errorKey = 'pair_code_invalid';
      return;
    }
    const bytes = buildPairReqCodeBytes(code);
    if (bytes === null) {
      return; // 与 validatePairCode 双保险，不可达
    }
    this.resetError();
    this.snapshot.phase = PairPhase.VERIFYING;
    const pkt: Packet = newRaw(PacketType.PAIR_REQ, this.conn.transport.nextSeq(), bytes.buffer as ArrayBuffer);
    this.conn.transport.sendUnsealed(pkt);
    const reply: Packet | null = await this.awaitPairReply(PAIR_REPLY_TIMEOUT_MS);
    if (this.snapshot.phase !== PairPhase.VERIFYING) {
      return; // 已取消/已被新流程取代
    }
    if (reply === null) {
      this.setKeyError('pair_err_no_response');
      this.snapshot.phase = PairPhase.PENDING_CODE;
      return;
    }
    if (reply.type === PacketType.PAIR_NAK) {
      this.snapshot.errorText = '';
      this.snapshot.errorKey = nakReasonKey(parsePairNakReason(reply.payload));
      this.snapshot.code = '';
      this.snapshot.phase = PairPhase.PENDING_CODE; // 留在码输入视图可重输/重新发起
      return;
    }
    if (reply.type !== PacketType.PAIR_OK) {
      this.setKeyError('pair_err_bad_reply');
      this.snapshot.phase = PairPhase.PENDING_CODE;
      return;
    }
    this.finishPairing(reply);
  }

  // 失败重试：有选中主机 → 重走连接+发起；否则重扫。
  public retry(): void {
    if (this.selected !== null) {
      const host: DiscoveredHost = this.selected;
      this.conn.disconnect();
      this.selectHost(host);
    } else {
      this.startDiscovery();
    }
  }

  // 取消：断开配对会话回列表视图。纯发现（DISCOVERING）尚未选中主机、不持有连接，
  // 只回退发现浮层态——已连接的会话不因路过发现页被拆；配对各态（CONNECTING 起）
  // 连接归本流程所有，照常断开回收。
  public cancel(): void {
    this.pairWaiter = null;
    this.selected = null;
    if (this.snapshot.phase === PairPhase.DISCOVERING) {
      if (this.conn.snapshot.state === ConnState.DISCOVERING) {
        this.conn.snapshot.state = ConnState.IDLE;
      }
    } else {
      this.conn.disconnect();
    }
    this.snapshot.phase = PairPhase.IDLE;
    this.snapshot.code = '';
    this.resetError();
  }

  // 快速连接上次的主机（token 已存）：直连进入 CONNECTED。
  public async quickConnect(): Promise<void> {
    if (this.snapshot.phase === PairPhase.CONNECTING) {
      return;
    }
    this.resetError();
    this.snapshot.phase = PairPhase.CONNECTING;
    let host: HostInfo | null = null;
    try {
      host = await this.settings.getLastHost();
    } catch (err) {
      hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', (err as Error).message);
    }
    if (host === null || host.tokenHex.length === 0) {
      this.snapshot.quickLabel = '';
      this.snapshot.phase = PairPhase.IDLE;
      return;
    }
    const token = hexToBytes(host.tokenHex);
    if (token === null || host.ip.length === 0) {
      this.setKeyError('pair_err_credential_invalid');
      this.snapshot.phase = PairPhase.FAILED;
      return;
    }
    this.snapshot.hostLabel = host.name.length > 0 ? host.name : host.ip;
    try {
      await this.conn.connect(host.ip, host.port, host.port, token, false);
    } catch (err) {
      this.setConnError(err as Error);
      this.snapshot.errorHintKey = 'pair_hint_auto_reconnect';
      this.snapshot.phase = PairPhase.FAILED;
      return;
    }
    this.enterPaired();
  }

  // ---- 内部 ----

  private finishPairing(reply: Packet): void {
    if (this.selected === null) {
      return;
    }
    const token = pairOkToken(reply.payload);
    if (token === null) {
      this.setKeyError('pair_err_reply_format');
      this.snapshot.phase = PairPhase.PENDING_CODE;
      return;
    }
    // 端到端自证：PAIR_OK 用刚签发的 token 封签，验签失败按 NAK 处理
    if (!verifyHmac(reply, new CryptoHmacProvider(token))) {
      hilog.warn(DOMAIN, TAG, 'PAIR_OK hmac verify fail');
      this.setKeyError('pair_err_reply_hmac');
      this.snapshot.code = '';
      this.snapshot.phase = PairPhase.PENDING_CODE;
      return;
    }
    const h = new HostInfo();
    h.name = this.snapshot.hostLabel;
    h.ip = this.selected.ip;
    h.port = this.selected.port;
    h.tokenHex = bytesToHex(token);
    this.settings.saveLastHost(h).catch((err: Error) => {
      if (err instanceof HostsFullError) {
        // 满 4 台未入库：本会话仍可用（token 已绑定 CONNECTED），页面轻提示引导先删除
        hilog.warn(DOMAIN, TAG, 'saveLastHost full: %{public}s', err.message);
        if (this.onHostsFull !== null) {
          this.onHostsFull();
        }
        return;
      }
      // 保存失败不影响本会话使用（token 已绑定 CONNECTED），记日志便于排查
      hilog.error(DOMAIN, TAG, 'saveLastHost fail: %{public}s', err.message);
    });
    this.conn.completePairing(token);
    this.refreshQuickLabel();
    this.enterPaired();
  }

  private enterPaired(): void {
    this.snapshot.phase = PairPhase.PAIRED;
    if (this.onPaired !== null) {
      this.onPaired();
    }
  }

  private onPairPacket(pkt: Packet): void {
    if (pkt.type !== PacketType.PAIR_OK && pkt.type !== PacketType.PAIR_NAK) {
      return;
    }
    if (this.pairWaiter !== null) {
      this.pairWaiter(pkt);
    }
  }

  private awaitPairReply(timeoutMs: number): Promise<Packet | null> {
    return new Promise<Packet | null>((resolve: (pkt: Packet | null) => void) => {
      let done: boolean = false;
      const timer = setTimeout(() => {
        if (!done) {
          done = true;
          this.pairWaiter = null;
          resolve(null);
        }
      }, timeoutMs);
      this.pairWaiter = (pkt: Packet | null) => {
        if (!done) {
          done = true;
          clearTimeout(timer);
          this.pairWaiter = null;
          resolve(pkt);
        }
      };
    });
  }

  private resetError(): void {
    this.snapshot.errorText = '';
    this.snapshot.errorKey = '';
    this.snapshot.errorParams = [];
    this.snapshot.errorHintKey = '';
  }

  // 静态文案错误：清动态文案，写资源键 + 参数（页面经 pageResFmt 解析为当前语言）。
  private setKeyError(key: string, params: string[] = []): void {
    this.snapshot.errorText = '';
    this.snapshot.errorKey = key;
    this.snapshot.errorParams = params;
  }

  // 连接层错误：ResError 转资源键；其余（系统 BusinessError 等）原样展示 message。
  private setConnError(err: Error): void {
    if (err instanceof ResError) {
      this.setKeyError(err.resKey, err.resParams);
    } else {
      this.snapshot.errorKey = '';
      this.snapshot.errorParams = [];
      this.snapshot.errorText = err.message;
    }
  }

  private refreshQuickLabel(): void {
    this.settings.getLastHost().then((h: HostInfo | null) => {
      this.snapshot.quickLabel = h !== null && h.tokenHex.length > 0 && h.ip.length > 0
        ? `${h.name.length > 0 ? h.name : h.ip} · ${h.ip}` : '';
    }).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', err.message);
    });
  }

  // 设备名（PAIR_REQ JSON 用）：marketName 优先、productModel 兜底，≤64 字节（API 只调一次并缓存）。
  private deviceName(): string {
    if (this.deviceNameCache !== null) {
      return this.deviceNameCache;
    }
    let raw: string = deviceInfo.marketName;
    if (raw.length === 0) {
      raw = deviceInfo.productModel;
    }
    this.deviceNameCache = sanitizeDeviceName(raw);
    return this.deviceNameCache;
  }
}
