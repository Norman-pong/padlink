// 配对 ViewModel：发现 → 选择主机 → 无 token 连接 → PAIR_REQ 发起 → 码校验 → PAIR_OK 验签绑定 token → CONNECTED。
// 设备名取 @ohos.deviceInfo（marketName 优先、productModel 兜底），发送前经 sanitizeDeviceName 截到 ≤64 字节。
import { deviceInfo } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';

import type { ConnectionManager } from '../net/ConnectionManager';
import { Discoverer, DiscoverPolicy } from '../net/Discoverer';
import type { DiscoveredHost } from '../net/DiscoverCore';
import { hexToBytes, bytesToHex } from '../net/HexCodec';
import { OneShotWaiter } from '../net/OneShotWaiter';
import type { SettingsModel } from './SettingsModel';
import { HostInfo } from './SettingsModel';
import { HostsFullError, hostKeyOf, findHost, findHostById } from './HostsStore';
import { pickHealCandidate, buildMigratedHost } from './HostHealCore';
import { armMigrationPersist } from './HostHeal';
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
export const DEFAULT_PORT: number = 53021;
// PAIR_OK/PAIR_NAK 应答超时：覆盖 daemon 侧校验与回包。
const PAIR_REPLY_TIMEOUT_MS: number = 4000;

export class PairPhase {
  public static readonly IDLE: number = 0;
  public static readonly DISCOVERING: number = 1;
  public static readonly CONNECTING: number = 2;
  public static readonly PENDING_CODE: number = 3;
  public static readonly VERIFYING: number = 4;
  public static readonly PAIRED: number = 5;
  public static readonly FAILED: number = 6;
}

// UI 可观察快照：字段仅由 PairingModel 写入（页面只读）。
@ObservedV2
export class PairingSnapshot {
  @Trace phase: number = PairPhase.IDLE;
  @Trace hosts: DiscoveredHost[] = [];
  // 已配对主机的 "ip:port" 集合（发现页据此标注「已配对」并走 token 直连）。
  @Trace knownHostKeys: string[] = [];
  @Trace hostLabel: string = '';
  @Trace quickLabel: string = '';
  @Trace errorText: string = '';
  // 静态错误文案资源键（页面 getStringByNameSync 取）。
  @Trace errorKey: string = '';
  @Trace errorParams: string[] = [];
  @Trace errorHintKey: string = '';
  // 确认码输入镜像（4 格回显 + 输满自动提交）。
  @Trace code: string = '';
}

export class PairingModel {
  public readonly snapshot: PairingSnapshot = new PairingSnapshot();
  // 配对/快连成功回调（页面借此短暂停留后返回主控页）。
  public onPaired: (() => void) | null = null;
  // 配对成功但档案保存被拒（列表满 4 台）回调：页面 toast 提示先删除。
  public onHostsFull: (() => void) | null = null;

  private conn: ConnectionManager;
  private settings: SettingsModel;
  private discoverer: Discoverer = new Discoverer();
  private selected: DiscoveredHost | null = null;
  private pairWaiter: OneShotWaiter<Packet | null> | null = null;
  private deviceNameCache: string | null = null;

  constructor(conn: ConnectionManager, settings: SettingsModel) {
    this.conn = conn;
    this.settings = settings;
    // PAIR_*/DISCOVER_RESP 经 Transport→ConnectionManager 路由至此（连接态由 CM 处理，配对态在此消费）
    conn.onPairPacket = (pkt: Packet) => {
      this.onPairPacket(pkt);
    };
  }


  // 进入发现页：刷新快连入口；空闲态自动发起一次发现。
  public onPageEnter(): void {
    this.refreshQuickLabel();
    this.refreshKnownHosts();
    // 残留的 PAIRED（成功视图已展示过）按全新进入处理，可再扫再配。
    if (this.snapshot.phase === PairPhase.PAIRED) {
      this.snapshot.phase = PairPhase.IDLE;
      this.snapshot.hosts = [];
    }
    if (this.snapshot.phase === PairPhase.IDLE && this.snapshot.hosts.length === 0) {
      this.startDiscovery();
    }
  }

  // 离开发现页：配对进行中则取消会话（PAIRED/已完成连接不回收）。
  public onPageLeave(): void {
    const p: number = this.snapshot.phase;
    if (p === PairPhase.DISCOVERING || p === PairPhase.CONNECTING
      || p === PairPhase.PENDING_CODE || p === PairPhase.VERIFYING) {
      this.cancel();
    }
  }


  // UDP 广播发现（窗口内收集全部应答，按 IP 去重）。conn 浮层态经 CM 的 enter/exit 收口。
  public async startDiscovery(): Promise<void> {
    if (this.snapshot.phase === PairPhase.DISCOVERING) {
      return;
    }
    this.resetError();
    this.snapshot.phase = PairPhase.DISCOVERING;
    this.conn.enterDiscovering();
    try {
      const hosts: DiscoveredHost[] = await this.discoverer.discover(DiscoverPolicy.WINDOW_MS, DEFAULT_PORT);
      this.snapshot.hosts = hosts;
      this.snapshot.phase = PairPhase.IDLE;
    } catch (err) {
      this.snapshot.hosts = [];
      this.setKeyError('pair_discover_failed_fmt', [(err as Error).message]);
      this.snapshot.phase = PairPhase.FAILED;
    }
    this.conn.exitDiscovering();
  }

  // 选中主机：已配对（host_id 命中或同 ip+port 有 token）→ token 直连（不消耗电脑端名额）；
  // host_id 命中但地址已变 → 迁移连接（封签确认后才落盘）；token 失效 → 回落配对自愈。
  public async selectHost(host: DiscoveredHost): Promise<void> {
    this.resetError();
    this.selected = host;
    this.snapshot.hostLabel = host.name.length > 0 ? host.name : host.ip;
    this.snapshot.code = '';
    const known: HostInfo | null = await this.findKnownHost(host);
    if (known !== null && known.tokenHex.length > 0) {
      const moved: boolean = known.hostId.length > 0 && host.hostId.length > 0
        && known.hostId === host.hostId && (known.ip !== host.ip || known.port !== host.port);
      if (moved) {
        // 已配对主机换了地址：直连新地址，档案待封签确认后由钩子落盘
        const migrated = buildMigratedHost(known, host);
        if (await this.connectKnown(migrated, true)) {
          return;
        }
        hilog.warn(DOMAIN, TAG, '迁移连接失败，回落配对');
        this.resetError();
      } else {
        if (known.hostId.length === 0 && host.hostId.length > 0) {
          known.hostId = host.hostId; // 旧档案首次见到 daemon 身份：同地址即信任锚，直接回填
        }
        if (await this.connectKnown(known)) {
          return;
        }
        hilog.warn(DOMAIN, TAG, '已知主机 token 连接失败，回落配对');
        this.resetError();
      }
    }
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

  // 连接一台「已配对」主机（发现页选中、主机列表切换、快速连接、迁移自愈四路共用）。
  // persistOnVerified=true（迁移连接）：地址变更未经确认，落盘推迟到对端封签报文验签通过。
  // 成功后写成默认主机；返回 false = 凭据不可用或连接失败（错误已写入快照）。
  public async connectKnown(h: HostInfo, persistOnVerified: boolean = false): Promise<boolean> {
    hilog.info(DOMAIN, TAG, 'connectKnown → %{public}s:%{public}d', h.ip, h.port);
    const token = hexToBytes(h.tokenHex);
    if (token === null || h.ip.length === 0) {
      hilog.warn(DOMAIN, TAG, 'connectKnown 凭据不可用: %{public}s', h.ip);
      return false;
    }
    this.resetError();
    this.snapshot.hostLabel = h.name.length > 0 ? h.name : h.ip;
    this.snapshot.code = '';
    this.snapshot.phase = PairPhase.CONNECTING;
    try {
      await this.conn.connect(h.ip, h.port, h.port, token, false);
    } catch (err) {
      this.setConnError(err as Error);
      this.snapshot.phase = PairPhase.FAILED;
      return false;
    }
    if (persistOnVerified) {
      // beginSession 已清旧钩子，此处（连接成功后）装本次迁移的落盘钩子
      armMigrationPersist(this.conn, this.settings, h);
    } else {
      await this.markActiveHost(h);
    }
    this.refreshQuickLabel();
    this.enterPaired();
    return true;
  }

  // 当前已连接主机 IP（未连接为空串）：发现页据此标注「使用中」。
  public currentHostIp(): string {
    return this.conn.getHost();
  }

  private async markActiveHost(h: HostInfo): Promise<void> {
    try {
      await this.settings.saveLastHost(h);
      hilog.info(DOMAIN, TAG, 'active host saved: %{public}s', h.ip);
    } catch (err) {
      hilog.warn(DOMAIN, TAG, 'markActiveHost saveLastHost fail: %{public}s', (err as Error).message);
    }
  }

  // 发起（或重新发起）配对：PAIR_REQ JSON 带设备指纹（电脑端据此区分重配与新手机）；成功无回包。
  public async startPairing(): Promise<void> {
    if (this.selected === null || this.snapshot.phase === PairPhase.PENDING_CODE) {
      return;
    }
    this.resetError();
    this.snapshot.code = '';
    this.snapshot.phase = PairPhase.PENDING_CODE;
    let devId: string = '';
    try {
      devId = await this.settings.getDeviceId();
    } catch (err) {
      hilog.warn(DOMAIN, TAG, 'getDeviceId fail: %{public}s', (err as Error).message);
    }
    const json: string = buildPairReqInitJson(this.deviceName(), devId);
    const bytes: Uint8Array = utf8Encode(json);
    const pkt: Packet = newRaw(PacketType.PAIR_REQ, this.conn.transport.nextSeq(), bytes.buffer as ArrayBuffer);
    // 发送失败不上抛（同 sendReliable 口径）；会话已死时由提交码的超时/NAK 路径自愈
    await this.conn.transport.sendUnsealed(pkt);
  }

  public restartPairing(): void {
    if (this.selected === null) {
      return;
    }
    const host: DiscoveredHost = this.selected;
    this.conn.disconnect(); // 丢弃旧会话（回 IDLE）再重连
    this.selectHost(host);
  }

  public setCode(code: string): void {
    this.snapshot.code = code;
  }

  // 提交确认码：PAIR_REQ 4B ASCII → PAIR_OK（新 token 封签，verifyHmac 自证）/ PAIR_NAK（可重输）。
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

  public retry(): void {
    if (this.selected !== null) {
      const host: DiscoveredHost = this.selected;
      this.conn.disconnect();
      this.selectHost(host);
    } else {
      this.startDiscovery();
    }
  }

  // 取消：配对各态持有连接照常断开；纯发现（未选中主机）只回退发现浮层态。
  public cancel(): void {
    this.pairWaiter = null;
    this.selected = null;
    if (this.snapshot.phase === PairPhase.DISCOVERING) {
      this.conn.exitDiscovering();
    } else {
      this.conn.disconnect();
    }
    this.snapshot.phase = PairPhase.IDLE;
    this.snapshot.code = '';
    this.resetError();
  }

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
    // 与发现页选中、主机列表切换共用同一条连接路径（含「记为默认主机」语义）
    if (!await this.connectKnown(host)) {
      // 有身份档案且旧地址连不上：静默发现一次，命中新地址则迁移连接
      if (host.hostId.length > 0 && await this.healAndConnect(host)) {
        return;
      }
      this.snapshot.errorHintKey = 'pair_hint_auto_reconnect';
    }
  }

  // 发现自愈：扫描 → host_id 命中新地址 → 迁移连接（封签确认后落盘）。命中并连上返回 true。
  private async healAndConnect(h: HostInfo): Promise<boolean> {
    let hosts: DiscoveredHost[] = [];
    try {
      hosts = await this.discoverer.discover();
    } catch (err) {
      hilog.warn(DOMAIN, TAG, 'heal discover fail: %{public}s', (err as Error).message);
      return false;
    }
    const cand = pickHealCandidate(hosts, h);
    if (cand === null) {
      return false;
    }
    hilog.info(DOMAIN, TAG, '快速连接自愈命中 %{public}s:%{public}d', cand.ip, cand.port);
    return this.connectKnown(buildMigratedHost(h, cand), true);
  }


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
    h.hostId = this.selected.hostId;
    this.settings.saveLastHost(h).catch((err: Error) => {
      if (err instanceof HostsFullError) {
        // 满 4 台未入库：本会话仍可用（token 已绑定），页面轻提示引导先删除
        hilog.warn(DOMAIN, TAG, 'saveLastHost full: %{public}s', err.message);
        if (this.onHostsFull !== null) {
          this.onHostsFull();
        }
        return;
      }
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
      this.pairWaiter.deliver(pkt);
    }
  }

  private awaitPairReply(timeoutMs: number): Promise<Packet | null> {
    const w: OneShotWaiter<Packet | null> = new OneShotWaiter<Packet | null>();
    this.pairWaiter = w;
    return w.wait(timeoutMs, null);
  }

  private resetError(): void {
    this.snapshot.errorText = '';
    this.snapshot.errorKey = '';
    this.snapshot.errorParams = [];
    this.snapshot.errorHintKey = '';
  }

  private setKeyError(key: string, params: string[] = []): void {
    this.snapshot.errorText = '';
    this.snapshot.errorKey = key;
    this.snapshot.errorParams = params;
  }

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

  // 本机已配对主机键集合（id:hostId 与 ip:port 双形态）：发现页标注与 selectHost 直连共用。
  private refreshKnownHosts(): void {
    this.settings.listHosts().then((hosts: HostInfo[]) => {
      const keys: string[] = [];
      for (const h of hosts) {
        keys.push(hostKeyOf(h.ip, h.port, h.hostId));
        keys.push(`${h.ip}:${h.port}`);
      }
      this.snapshot.knownHostKeys = keys;
    }).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'listHosts fail: %{public}s', err.message);
      this.snapshot.knownHostKeys = [];
    });
  }

  public isKnownHost(host: DiscoveredHost): boolean {
    const keys: string[] = this.snapshot.knownHostKeys;
    if (host.hostId.length > 0 && keys.indexOf(`id:${host.hostId}`) >= 0) {
      return true;
    }
    return keys.indexOf(`${host.ip}:${host.port}`) >= 0;
  }

  private findKnownHost(host: DiscoveredHost): Promise<HostInfo | null> {
    return this.settings.listHosts().then((hosts: HostInfo[]) => {
      if (host.hostId.length > 0) {
        const byId = findHostById(hosts, host.hostId);
        if (byId !== null) {
          return byId;
        }
      }
      return findHost(hosts, host.ip, host.port);
    }).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'findKnownHost fail: %{public}s', err.message);
      return null;
    });
  }

  // 设备名（PAIR_REQ JSON 用）：marketName 优先、productModel 兜底，≤64 字节（只调一次并缓存）。
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
