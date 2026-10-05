// 连接管理（FR-6）：状态机 + 1Hz TCP 心跳 + 指数退避重连。
// 状态：IDLE / DISCOVERING / PAIRING（M2 页面流接入）/ CONNECTED / RECONNECTING / LOST（M1 手动 connect 直达 CONNECTED）。
// 失联判定：心跳 1Hz，连续 3 个窗口（各 1s）无回包 → RECONNECTING；
// 退避 1/2/4/8/15s 封顶，连续 8 次重连失败 → LOST（释放一切，等待手动重连）。
// 状态/RTT 经 @ObservedV2/@Trace 快照暴露给 UI；时间源统一 new Date().getTime()（系统 API）。
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { PacketType } from '../proto/PacketType';
import type { Packet } from '../proto/Packet';
import { utf8DecodeStrict } from '../proto/Utf8';
import { Transport, TCP_CONNECT_TIMEOUT_MS } from './Transport';
import { ResError } from '../ResError';

const TAG: string = 'PadLink.ConnMgr';
const DOMAIN: number = 0x5050;

// 心跳与重连参数（PRD §4.7：1Hz ECHO、3 次超时判失联、退避 1/2/4/8…上限 15s）。
export class ConnPolicy {
  public static readonly HEARTBEAT_INTERVAL_MS: number = 1000;
  public static readonly MAX_MISSED: number = 3;
  public static readonly HELLO_TIMEOUT_MS: number = 3000;
  public static readonly BACKOFF_MS: number[] = [1000, 2000, 4000, 8000, 15000];
  public static readonly MAX_RECONNECT_FAILURES: number = 8;
}

// 状态机状态（readonly 常量类，同 GestureStateName 风格；不占位的不用 TS enum）。
export class ConnState {
  // 空闲（未连接 / 已手动断开）。
  public static readonly IDLE: number = 0;
  // 发现中（M2 接入：UDP 广播搜索主机）。
  public static readonly DISCOVERING: number = 1;
  // 配对中（M2 接入：PAIR_REQ 握手）。
  public static readonly PAIRING: number = 2;
  // 已连接（HELLO 握手完成，心跳正常）。
  public static readonly CONNECTED: number = 3;
  // 重连中（心跳超时/TCP 断开，指数退避重试）。
  public static readonly RECONNECTING: number = 4;
  // 失联（重连失败达上限，已释放一切资源，需手动重连）。
  public static readonly LOST: number = 5;
}

// UI 可观察快照：字段仅由 ConnectionManager 写入（UI 只读），@Trace 驱动刷新。
@ObservedV2
export class ConnectionState {
  @Trace state: number = ConnState.IDLE;
  // 最近一次 ECHO 往返毫秒；-1 表示未知（未连接/尚无回包）。
  @Trace rttMs: number = -1;
  // 最近一次失败原因（string.json 资源键 + 参数，页面解析为当前语言文案；''= 无）。
  @Trace lastErrorKey: string = '';
  @Trace lastErrorParams: string[] = [];
  // 当前重连轮次（1 起；CONNECTED 时归零）。
  @Trace reconnectAttempt: number = 0;
}

// RTT 观察者（调试面板 echo 自测用）。
export type RttListener = (rttMs: number) => void;
// PAIR_* / DISCOVER_RESP 观察者（M2 配对流预留）。
export type PairPacketListener = (pkt: Packet) => void;

export class ConnectionManager {
  public readonly snapshot: ConnectionState = new ConnectionState();
  public readonly transport: Transport = new Transport();

  private hostAddr: string = '';
  private udpPortN: number = 53021;
  private tcpPortN: number = 53021;
  private heartbeatTimer: number = -1;
  private reconnectTimer: number = -1;
  private missed: number = 0;
  private awaitingReply: boolean = false;
  private backoffIdx: number = 0;
  private manualClose: boolean = false;
  // 会话代次：每次 connect/disconnect/LOST 递增，隔离在途异步回调（防旧会话写新状态）。
  private epoch: number = 0;
  private helloWaiter: ((ok: boolean) => void) | null = null;
  private rttListeners: RttListener[] = [];
  public onPairPacket: PairPacketListener | null = null;

  constructor() {
    this.transport.onPacket = (pkt: Packet) => {
      this.handlePacket(pkt);
    };
    this.transport.tcp.onClosed = () => {
      this.onTcpDown('conn_tcp_closed');
    };
    this.transport.tcp.onError = (err: BusinessError) => {
      this.onTcpDown('conn_tcp_error_fmt', [String(err.code), err.message]);
    };
  }

  public getHost(): string {
    return this.hostAddr;
  }

  public getTcpPort(): number {
    return this.tcpPortN;
  }

  public isConnected(): boolean {
    return this.snapshot.state === ConnState.CONNECTED;
  }

  // 注册/移除 RTT 监听（调试面板）。
  public addRttListener(cb: RttListener): void {
    this.rttListeners.push(cb);
  }

  public removeRttListener(cb: RttListener): void {
    const idx: number = this.rttListeners.indexOf(cb);
    if (idx >= 0) {
      this.rttListeners.splice(idx, 1);
    }
  }

  // 手动连接（M1）：TCP 建连 + HELLO 握手 → CONNECTED → 心跳。失败抛错（文案进 UI 提示）。
  // autoRetry=true（FR-6 静默自动重连）：首连失败不抛错、不回 IDLE，转入 RECONNECTING 退避重试。
  public async connect(host: string, port: number, udpPort: number, token: Uint8Array,
    autoRetry: boolean = false): Promise<void> {
    const myEpoch: number = this.beginSession();
    this.transport.setToken(token);
    this.hostAddr = host;
    this.udpPortN = udpPort;
    this.tcpPortN = port;
    this.snapshot.lastErrorKey = '';
    this.snapshot.lastErrorParams = [];
    this.snapshot.rttMs = -1;
    // 连接进行中保持 IDLE：加载态由发起页（手动连接区）自管 connecting 标志。
    try {
      await this.transport.open(host, udpPort, port);
    } catch (err) {
      const e = err as BusinessError;
      if (autoRetry && this.epoch === myEpoch) {
        this.enterReconnectingFromInitial('conn_connect_failed_fmt', [String(e.code), e.message]);
        return;
      }
      this.teardown(this.epoch === myEpoch);
      this.snapshot.state = ConnState.IDLE;
      throw new ResError('conn_connect_failed_fmt', [String(e.code), e.message], `连接失败：${e.code} ${e.message}`);
    }
    if (this.epoch !== myEpoch) {
      return; // 会话已被新的 connect/disconnect 取代
    }
    this.transport.sendHello();
    const helloOk: boolean = await this.awaitHello(ConnPolicy.HELLO_TIMEOUT_MS);
    if (this.epoch !== myEpoch) {
      return;
    }
    if (!helloOk) {
      if (autoRetry && this.epoch === myEpoch) {
        this.enterReconnectingFromInitial('conn_hello_timeout');
        return;
      }
      this.teardown(true);
      this.snapshot.state = ConnState.IDLE;
      throw new ResError('conn_hello_timeout', [], '握手超时：未收到 daemon 的 HELLO 应答');
    }
    this.enterConnected();
  }

  // 无 token 配对连接（M2）：TCP 建连 + 未认证 HELLO 握手 → PAIRING（不启动心跳——
  // ECHO 属认证语义，须待 completePairing 绑定新 token 后开始）。失败抛错回 IDLE。
  public async connectForPairing(host: string, port: number, udpPort: number): Promise<void> {
    const myEpoch: number = this.beginSession();
    this.transport.clearToken();
    this.hostAddr = host;
    this.udpPortN = udpPort;
    this.tcpPortN = port;
    this.snapshot.lastErrorKey = '';
    this.snapshot.lastErrorParams = [];
    this.snapshot.rttMs = -1;
    try {
      await this.transport.open(host, udpPort, port);
    } catch (err) {
      const e = err as BusinessError;
      this.teardown(this.epoch === myEpoch);
      this.snapshot.state = ConnState.IDLE;
      throw new ResError('conn_connect_failed_fmt', [String(e.code), e.message], `连接失败：${e.code} ${e.message}`);
    }
    if (this.epoch !== myEpoch) {
      return;
    }
    this.transport.sendHello();
    const helloOk: boolean = await this.awaitHello(ConnPolicy.HELLO_TIMEOUT_MS);
    if (this.epoch !== myEpoch) {
      return;
    }
    if (!helloOk) {
      this.teardown(true);
      this.snapshot.state = ConnState.IDLE;
      throw new ResError('conn_hello_timeout', [], '握手超时：未收到 daemon 的 HELLO 应答');
    }
    this.snapshot.state = ConnState.PAIRING;
  }

  // PAIR_OK 验签通过后调用：绑定新 token 并进入 CONNECTED（启动心跳）。
  public completePairing(token: Uint8Array): void {
    if (this.snapshot.state !== ConnState.PAIRING) {
      return;
    }
    this.transport.setToken(token);
    this.enterConnected();
  }

  // 手动断开：best-effort BYE 后释放一切，回 IDLE。
  public disconnect(): void {
    this.beginSession();
    try {
      if (this.transport.hasToken()) {
        this.transport.sendBye();
      }
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'bye send fail: %{public}d %{public}s', e.code, e.message);
    }
    this.teardown(true);
    this.snapshot.state = ConnState.IDLE;
    this.snapshot.rttMs = -1;
    this.snapshot.reconnectAttempt = 0;
  }

  // ---- 内部状态迁移 ----

  // 开启新会话：清定时器/旧 socket，递增代次使在途回调失效。
  private beginSession(): number {
    this.epoch++;
    this.manualClose = false;
    this.missed = 0;
    this.awaitingReply = false;
    this.backoffIdx = 0;
    this.helloWaiter = null;
    this.teardown(false);
    this.snapshot.reconnectAttempt = 0;
    return this.epoch;
  }

  private enterConnected(): void {
    this.missed = 0;
    this.awaitingReply = false;
    this.backoffIdx = 0;
    this.snapshot.state = ConnState.CONNECTED;
    this.snapshot.reconnectAttempt = 0;
    this.snapshot.lastErrorKey = '';
    this.snapshot.lastErrorParams = [];
    this.startHeartbeat();
  }

  // 首连失败转重连（FR-6 autoRetry）：释放 socket 后进 RECONNECTING 退避循环（至多 MAX_RECONNECT_FAILURES → LOST）。
  private enterReconnectingFromInitial(reasonKey: string, reasonParams: string[] = []): void {
    this.teardown(true);
    this.snapshot.state = ConnState.RECONNECTING;
    this.snapshot.lastErrorKey = reasonKey;
    this.snapshot.lastErrorParams = reasonParams;
    this.scheduleReconnect();
  }

  private enterReconnecting(reasonKey: string, reasonParams: string[] = []): void {
    if (this.manualClose || this.snapshot.state !== ConnState.CONNECTED) {
      return;
    }
    this.stopHeartbeat();
    this.backoffIdx = 0;
    this.snapshot.state = ConnState.RECONNECTING;
    this.snapshot.lastErrorKey = reasonKey;
    this.snapshot.lastErrorParams = reasonParams;
    this.scheduleReconnect();
  }

  private scheduleReconnect(): void {
    const myEpoch: number = this.epoch;
    const delay: number = ConnPolicy.BACKOFF_MS[Math.min(this.backoffIdx, ConnPolicy.BACKOFF_MS.length - 1)];
    this.backoffIdx++;
    this.snapshot.reconnectAttempt = this.backoffIdx;
    if (this.reconnectTimer >= 0) {
      clearTimeout(this.reconnectTimer);
    }
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = -1;
      this.tryReconnect(myEpoch);
    }, delay);
  }

  private async tryReconnect(myEpoch: number): Promise<void> {
    if (this.epoch !== myEpoch || this.manualClose || this.snapshot.state !== ConnState.RECONNECTING) {
      return;
    }
    try {
      this.transport.tcp.reset(); // 旧 TCP socket 已坏，重建后重挂事件
      await this.transport.tcp.connect(this.hostAddr, this.tcpPortN, TCP_CONNECT_TIMEOUT_MS);
      if (this.epoch !== myEpoch) {
        return;
      }
      this.transport.sendHello();
      const helloOk: boolean = await this.awaitHello(ConnPolicy.HELLO_TIMEOUT_MS);
      if (this.epoch !== myEpoch) {
        return;
      }
      if (!helloOk) {
        throw new ResError('conn_hello_timeout', [], '握手超时');
      }
      // 首连失败路径可能已 teardown(true) 关闭 UDP 数据面，重连成功前补开（保持 MOVE/SCROLL 通道）
      this.transport.udp.reset();
      await this.transport.udp.open();
      if (this.epoch !== myEpoch) {
        return;
      }
      this.enterConnected();
    } catch (err) {
      if (this.epoch !== myEpoch || this.manualClose) {
        return;
      }
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'reconnect fail: %{public}s', e.message !== undefined ? e.message : '');
      this.snapshot.lastErrorKey = 'conn_reconnect_attempt_fmt';
      this.snapshot.lastErrorParams = [String(this.backoffIdx)];
      if (this.backoffIdx >= ConnPolicy.MAX_RECONNECT_FAILURES) {
        this.enterLost('conn_reconnect_giveup');
      } else {
        this.scheduleReconnect();
      }
    }
  }

  private enterLost(reasonKey: string, reasonParams: string[] = []): void {
    this.teardown(true);
    this.snapshot.state = ConnState.LOST;
    this.snapshot.lastErrorKey = reasonKey;
    this.snapshot.lastErrorParams = reasonParams;
  }

  // 释放一切：心跳/重连定时器清空，两 socket 关闭（closeBoth=false 时保留连接供重连续用）。
  private teardown(closeBoth: boolean): void {
    this.stopHeartbeat();
    if (this.reconnectTimer >= 0) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = -1;
    }
    if (closeBoth) {
      this.transport.close();
    }
  }

  private startHeartbeat(): void {
    if (this.heartbeatTimer >= 0) {
      return;
    }
    this.heartbeatTimer = setInterval(() => {
      if (this.snapshot.state !== ConnState.CONNECTED) {
        return;
      }
      if (this.awaitingReply) {
        // 上一拍 ECHO 在 1s 窗口内无回包 = 一次超时
        this.awaitingReply = false;
        this.missed++;
        if (this.missed >= ConnPolicy.MAX_MISSED) {
          this.enterReconnecting('conn_heartbeat_timeout');
          return;
        }
      }
      this.awaitingReply = true;
      this.transport.sendEcho();
    }, ConnPolicy.HEARTBEAT_INTERVAL_MS);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer >= 0) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = -1;
    }
  }

  private onTcpDown(reasonKey: string, reasonParams: string[] = []): void {
    if (this.snapshot.state === ConnState.CONNECTED) {
      this.enterReconnecting(reasonKey, reasonParams);
    }
  }

  private awaitHello(timeoutMs: number): Promise<boolean> {
    return new Promise<boolean>((resolve: (ok: boolean) => void) => {
      let done: boolean = false;
      const timer = setTimeout(() => {
        if (!done) {
          done = true;
          this.helloWaiter = null;
          resolve(false);
        }
      }, timeoutMs);
      this.helloWaiter = (ok: boolean) => {
        if (!done) {
          done = true;
          clearTimeout(timer);
          this.helloWaiter = null;
          resolve(ok);
        }
      };
    });
  }

  private handlePacket(pkt: Packet): void {
    if (pkt.type === PacketType.ECHO) {
      const rtt: number = new Date().getTime() - pkt.echo.tsMs;
      this.snapshot.rttMs = rtt;
      this.missed = 0;
      this.awaitingReply = false;
      for (const cb of this.rttListeners) {
        cb(rtt);
      }
      return;
    }
    if (pkt.type === PacketType.HELLO) {
      if (this.helloWaiter !== null) {
        this.helloWaiter(true);
      }
      return;
    }
    if (pkt.type === PacketType.ERR) {
      const text: string | null = utf8DecodeStrict(new Uint8Array(pkt.payload));
      this.snapshot.lastErrorKey = 'conn_daemon_err_fmt';
      this.snapshot.lastErrorParams = [text !== null ? text : `${pkt.payload.byteLength}B`];
      return;
    }
    if (pkt.type === PacketType.PAIR_REQ || pkt.type === PacketType.PAIR_OK
      || pkt.type === PacketType.PAIR_NAK || pkt.type === PacketType.DISCOVER_RESP) {
      if (this.onPairPacket !== null) {
        this.onPairPacket(pkt); // M2 配对/发现流预留
      }
    }
  }
}
