// 连接管理：状态机 + 1Hz TCP 心跳 + 指数退避重连。
// 失联判定：连续 3 个 1s 窗口无回包 → RECONNECTING；退避 1/2/4/8/15s 封顶，连续 8 次失败 → LOST（释放一切等手动重连）。
// 状态/RTT 经 @ObservedV2/@Trace 快照暴露给 UI。
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { NoticeCode, PacketType } from '../proto/PacketType';
import type { Packet } from '../proto/Packet';
import { utf8DecodeStrict } from '../proto/Utf8';
import { Transport, TCP_CONNECT_TIMEOUT_MS } from './Transport';
import { ResError } from '../ResError';

const TAG: string = 'PadLink.ConnMgr';
const DOMAIN: number = 0x5050;

// 错误文案入资源参数前必须落为字符串：socket 回调的 message 可能 undefined，
// 资源框架对非字符串参数抛 9001008，调用点又在 build 内 → 直接杀进程。
function errText(err: Error): string {
  const msg: string | undefined = err.message;
  return msg !== undefined ? msg : '';
}

export class ConnPolicy {
  public static readonly HEARTBEAT_INTERVAL_MS: number = 1000;
  public static readonly MAX_MISSED: number = 3;
  public static readonly HELLO_TIMEOUT_MS: number = 3000;
  public static readonly BACKOFF_MS: number[] = [1000, 2000, 4000, 8000, 15000];
  public static readonly MAX_RECONNECT_FAILURES: number = 8;
  public static readonly CTRL_BUSY_TICK_MS: number = 1000;
}

// 状态常量（不用 TS enum：纯逻辑层须 node 可直跑）。
export class ConnState {
  public static readonly IDLE: number = 0;
  public static readonly DISCOVERING: number = 1;
  public static readonly PAIRING: number = 2;
  public static readonly CONNECTED: number = 3;
  public static readonly RECONNECTING: number = 4;
  // 重连失败达上限：已释放一切资源，需手动重连。
  public static readonly LOST: number = 5;
}

// UI 可观察快照：字段仅由 ConnectionManager 写入（UI 只读）；@Trace 是刷新驱动，字段缺了它写入不触发 UI 更新。
@ObservedV2
export class ConnectionState {
  @Trace state: number = ConnState.IDLE;
  @Trace rttMs: number = -1;
  // string.json 资源键 + 参数（页面解析文案）；空串 = 无。
  @Trace lastErrorKey: string = '';
  @Trace lastErrorParams: string[] = [];
  @Trace reconnectAttempt: number = 0;
  // 控制权冷静期剩余秒数（>0 = 本机输入被其他设备占用；主控页据此常驻倒计时）。
  // 由 daemon NOTICE(0, 剩余秒数) 驱动，本端按 1Hz 递减，断开/换会话即清零。
  @Trace ctrlBusyLeftSec: number = 0;
}

export type RttListener = (rttMs: number) => void;
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
  private ctrlBusyTimer: number = -1;
  public onPairPacket: PairPacketListener | null = null;

  constructor() {
    this.transport.onPacket = (pkt: Packet) => {
      this.handlePacket(pkt);
    };
    // TCP 断开回调不在此挂：旧 socket 的 close/error 异步到达，只在构造时挂一次会被
    // 当成「当前会话断开」；改由 beginSession 后的 bindTcpEvents 按会话代次挂载。
  }

  // 按会话挂载 TCP 事件回调：闭包捕获本会话代次，代次不符即丢弃。
  private bindTcpEvents(myEpoch: number): void {
    this.transport.tcp.onClosed = () => {
      if (this.epoch !== myEpoch) {
        return;
      }
      this.onTcpDown('conn_tcp_closed');
    };
    this.transport.tcp.onError = (err: BusinessError) => {
      if (this.epoch !== myEpoch) {
        return;
      }
      this.onTcpDown('conn_tcp_error_fmt', [String(err.code), errText(err)]);
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

  public addRttListener(cb: RttListener): void {
    this.rttListeners.push(cb);
  }

  public removeRttListener(cb: RttListener): void {
    const idx: number = this.rttListeners.indexOf(cb);
    if (idx >= 0) {
      this.rttListeners.splice(idx, 1);
    }
  }


  // 按 daemon 给出的剩余秒数起倒计时；已在倒计时则只刷新剩余值（每轮冷静期只收一次 NOTICE）。
  private startCtrlBusy(leftSec: number): void {
    this.snapshot.ctrlBusyLeftSec = leftSec > 0 ? Math.floor(leftSec) : 1; // arg=0（未知）按 1 秒兜底
    if (this.ctrlBusyTimer >= 0) {
      return;
    }
    this.ctrlBusyTimer = setInterval(() => {
      if (this.snapshot.ctrlBusyLeftSec <= 1) {
        this.stopCtrlBusy();
        return;
      }
      this.snapshot.ctrlBusyLeftSec -= 1;
    }, ConnPolicy.CTRL_BUSY_TICK_MS);
  }

  private stopCtrlBusy(): void {
    if (this.ctrlBusyTimer >= 0) {
      clearInterval(this.ctrlBusyTimer);
      this.ctrlBusyTimer = -1;
    }
    this.snapshot.ctrlBusyLeftSec = 0;
  }

  // 手动连接：TCP 建连 + HELLO 握手 → CONNECTED → 心跳；失败抛 ResError。
  // autoRetry=true（静默自动重连）：首连失败不抛错、不回 IDLE，转入 RECONNECTING 退避重试。
  public async connect(host: string, port: number, udpPort: number, token: Uint8Array,
    autoRetry: boolean = false): Promise<void> {
    const myEpoch: number = this.beginSession();
    this.bindTcpEvents(myEpoch);
    this.transport.setToken(token);
    this.hostAddr = host;
    this.udpPortN = udpPort;
    this.tcpPortN = port;
    this.snapshot.lastErrorKey = '';
    this.snapshot.lastErrorParams = [];
    this.snapshot.rttMs = -1;
    // 连接进行中保持 IDLE：加载态由发起页自管 connecting 标志。
    try {
      await this.transport.open(host, udpPort, port);
    } catch (err) {
      const e = err as BusinessError;
      if (autoRetry && this.epoch === myEpoch) {
        this.enterReconnectingFromInitial('conn_connect_failed_fmt', [String(e.code), errText(e)]);
        return;
      }
      this.teardown(this.epoch === myEpoch);
      this.snapshot.state = ConnState.IDLE;
      throw new ResError('conn_connect_failed_fmt', [String(e.code), errText(e)], `连接失败：${e.code} ${errText(e)}`);
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

  // 无 token 配对连接：建连 + 未认证 HELLO 握手 → PAIRING，不启动心跳
  //（tokenless ECHO 会触发 daemon ERR 断连），待 completePairing 绑定 token 后开始。
  public async connectForPairing(host: string, port: number, udpPort: number): Promise<void> {
    const myEpoch: number = this.beginSession();
    this.bindTcpEvents(myEpoch);
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
      throw new ResError('conn_connect_failed_fmt', [String(e.code), errText(e)], `连接失败：${e.code} ${errText(e)}`);
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


  // 开启新会话：清定时器/旧 socket，递增代次使在途回调失效。
  private beginSession(): number {
    this.epoch++;
    hilog.info(DOMAIN, TAG, 'beginSession #%{public}d（旧会话定时器/套接字作废）', this.epoch);
    // 旧会话的心跳与重连定时器必须随会话一起停：它们只有状态守卫、没有代次守卫，
    // 继续运行会驱动/打断新会话。
    this.stopHeartbeat();
    if (this.reconnectTimer >= 0) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = -1;
    }
    this.transport.tcp.onClosed = null; // 先摘回调：紧随其后的 teardown 会关旧 socket
    this.transport.tcp.onError = null;
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

  // 首连失败转重连：释放 socket 后进 RECONNECTING 退避（至多 8 轮 → LOST）。
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
      // 首连失败路径可能已 teardown 关闭 UDP 数据面，重连成功前补开（保持 MOVE/SCROLL 通道）
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
    hilog.info(DOMAIN, TAG, 'teardown #%{public}d closeBoth=%{public}s state=%{public}d',
      this.epoch, closeBoth ? 'true' : 'false', this.snapshot.state);
    this.stopHeartbeat();
    this.stopCtrlBusy(); // 会话结束：冷静期提示随之消失（控制权已与本机无关）
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
    hilog.info(DOMAIN, TAG, 'TCP 断开 #%{public}d reason=%{public}s state=%{public}d',
      this.epoch, reasonKey, this.snapshot.state);
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
    if (pkt.type === PacketType.NOTICE) {
      // daemon → 手机提示（多设备控制权裁决）：只驱动冷静期倒计时，不改连接状态
      if (pkt.notice.code === NoticeCode.CONTROL_BUSY) {
        this.startCtrlBusy(pkt.notice.arg);
      }
      return;
    }
    if (pkt.type === PacketType.PAIR_REQ || pkt.type === PacketType.PAIR_OK
      || pkt.type === PacketType.PAIR_NAK || pkt.type === PacketType.DISCOVER_RESP) {
      if (this.onPairPacket !== null) {
        this.onPairPacket(pkt);
      }
    }
  }
}
