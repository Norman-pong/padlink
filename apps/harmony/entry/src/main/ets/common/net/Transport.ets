// 网络传输层：UDP 单播/广播通道 + TCP 可靠通道（@kit.NetworkKit）。
// API 依据（现查文档）：
// - js-apis-socket：constructUDPSocketInstance/bind/send/close/setExtraOptions/on('message')/
//   constructTCPSocketInstance/connect/send/on('message')/on('close')/on('error')；
//   NetAddress/UDPSendOptions/UDPExtraOptions(broadcast)/TCPConnectOptions/TCPSendOptions。
// - faqs-network-96：广播姿势 bind 0.0.0.0 →（bind 成功后）setExtraOptions({broadcast:true})
//   → getAllNets 筛 BEARER_WIFI → netHandle.bindSocket 锁 WiFi 出口。
// - faqs-network-86：socket 仅可发 string|ArrayBuffer；Uint8Array.buffer 即 ArrayBuffer。
// - faqs-network-115：bind 失败常见错误码（排障日志用）。
// TCP 粘包按 19B 头 payload_len（偏移 7，大端）分帧后交 decode；seal 同步（CryptoHmacProvider）。
import { socket, connection } from '@kit.NetworkKit';
import { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { Protocol, PacketType, PacketFlag } from '../proto/PacketType';
import type { Packet } from '../proto/Packet';
import { encode, decode } from '../proto/Codec';
import { seal, verifyHmac } from '../proto/Auth';
import type { HmacProvider } from '../proto/Auth';
import { newMove, newScroll, newEcho, newRaw } from '../proto/Packet';
import { CryptoHmacProvider } from './CryptoHmac';

const TAG: string = 'PadLink.Transport';
const DOMAIN: number = 0x5050;

// TCP 建连/握手超时（指数退避重连期间同样使用）。
export const TCP_CONNECT_TIMEOUT_MS: number = 3000;

// 发送/接收计数（调试面板展示）。
export class TransportStats {
  public udpSendOk: number = 0;
  public udpSendFail: number = 0;
  public tcpSendOk: number = 0;
  public tcpSendFail: number = 0;
  public recvOk: number = 0;
  public recvDecodeFail: number = 0;
  public recvHmacFail: number = 0;
}

// 解码+验签后的业务包回调（ECHO/HELLO/PAIR_*/ERR 由上层分发）。
export type PacketHandler = (pkt: Packet) => void;
// UDP 原始报文回调（M2 发现/配对流预留）。
export type RawUdpHandler = (data: ArrayBuffer, fromAddr: string, fromPort: number) => void;

// UDP 通道：bind 0.0.0.0 端口 0（系统随机）→ setExtraOptions 广播 → 锁 WiFi 出口。
export class UdpChannel {
  private sock: socket.UDPSocket = socket.constructUDPSocketInstance();
  private opened: boolean = false;
  private closed: boolean = false;
  public onMessage: RawUdpHandler | null = null;

  public open(): Promise<void> {
    if (this.opened) {
      return Promise.resolve();
    }
    const bindAddr: socket.NetAddress = { address: '0.0.0.0', port: 0, family: 1 };
    const extra: socket.UDPExtraOptions = { broadcast: true };
    return this.sock.bind(bindAddr)
      .then(() => this.sock.setExtraOptions(extra)) // 必须在 bind 成功之后（faqs-network-96）
      .then(() => this.bindToWifi())
      .then(() => {
        this.sock.on('message', (info: socket.SocketMessageInfo) => {
          if (this.onMessage !== null) {
            this.onMessage(info.message, info.remoteInfo.address, info.remoteInfo.port);
          }
        });
        this.opened = true;
      });
  }

  // getAllNets → getNetCapabilitiesSync 筛 BEARER_WIFI → netHandle.bindSocket（faqs-network-96）。
  private bindToWifi(): Promise<void> {
    return connection.getAllNets().then((nets: connection.NetHandle[]) => {
      for (const net of nets) {
        try {
          const cap: connection.NetCapabilities = connection.getNetCapabilitiesSync(net);
          if (cap.bearerTypes.indexOf(connection.NetBearType.BEARER_WIFI) >= 0) {
            return net.bindSocket(this.sock);
          }
        } catch (err) {
          const e = err as BusinessError;
          hilog.warn(DOMAIN, TAG, 'getNetCapabilitiesSync fail: %{public}d %{public}s', e.code, e.message);
        }
      }
      hilog.warn(DOMAIN, TAG, 'no wifi netHandle, udp not bound to wifi');
      return Promise.resolve(); // 无 WiFi（如纯调试环境）不锁定出口，单播仍走默认网络
    });
  }

  public sendTo(addr: string, port: number, data: ArrayBuffer): Promise<void> {
    const address: socket.NetAddress = { address: addr, port: port, family: 1 };
    const opt: socket.UDPSendOptions = { data: data, address: address };
    return this.sock.send(opt);
  }

  public sendBroadcast(port: number, data: ArrayBuffer): Promise<void> {
    return this.sendTo('255.255.255.255', port, data);
  }

  public close(): void {
    if (this.closed || !this.opened) {
      return;
    }
    this.closed = true;
    this.opened = false;
    this.sock.off('message');
    this.sock.close().catch((err: BusinessError) => {
      hilog.warn(DOMAIN, TAG, 'udp close fail: %{public}d %{public}s', err.code, err.message);
    });
  }
}

// TCP 通道：connect + 收包粘包分帧（19B 头 payload_len）。
export class TcpChannel {
  private sock: socket.TCPSocket = socket.constructTCPSocketInstance();
  private rx: Uint8Array = new Uint8Array(0);
  private eventsBound: boolean = false;
  public onRawPacket: ((data: ArrayBuffer) => void) | null = null;
  public onFramingError: (() => void) | null = null;
  public onClosed: (() => void) | null = null;
  public onError: ((err: BusinessError) => void) | null = null;

  public connect(addr: string, port: number, timeoutMs: number): Promise<void> {
    this.bindEvents();
    const address: socket.NetAddress = { address: addr, port: port, family: 1 };
    const opt: socket.TCPConnectOptions = { address: address, timeout: timeoutMs };
    return this.sock.connect(opt);
  }

  // 重连前重建底层 socket：旧实例已损坏（半开/EOF），复用可能失败；重建后事件需重新挂接。
  public reset(): void {
    this.close();
    this.sock = socket.constructTCPSocketInstance();
    this.eventsBound = false;
    this.rx = new Uint8Array(0);
  }

  private bindEvents(): void {
    if (this.eventsBound) {
      return;
    }
    this.eventsBound = true;
    this.sock.on('message', (info: socket.SocketMessageInfo) => {
      this.feed(new Uint8Array(info.message));
    });
    this.sock.on('close', () => {
      if (this.onClosed !== null) {
        this.onClosed();
      }
    });
    this.sock.on('error', (err: BusinessError) => {
      if (this.onError !== null) {
        this.onError(err);
      }
    });
  }

  // 粘包分帧：凑满 19B 头 → 读 payload_len → 凑满整包回调；超限 payload 视为流错位，清缓冲重同步。
  private feed(chunk: Uint8Array): void {
    const merged = new Uint8Array(this.rx.length + chunk.length);
    merged.set(this.rx, 0);
    merged.set(chunk, this.rx.length);
    this.rx = merged;
    for (;;) {
      if (this.rx.length < Protocol.HEADER_SIZE) {
        return;
      }
      const dv = new DataView(this.rx.buffer, this.rx.byteOffset, this.rx.byteLength);
      const plen: number = dv.getUint16(7, false);
      if (plen > Protocol.MAX_PAYLOAD) {
        this.rx = new Uint8Array(0);
        if (this.onFramingError !== null) {
          this.onFramingError();
        }
        return;
      }
      const total: number = Protocol.HEADER_SIZE + plen;
      if (this.rx.length < total) {
        return;
      }
      const frame: Uint8Array = this.rx.slice(0, total);
      this.rx = this.rx.slice(total);
      if (this.onRawPacket !== null) {
        // Uint8Array.buffer 静态类型为 ArrayBufferLike，收窄为 ArrayBuffer（arkts-no-structural-typing）
        this.onRawPacket(frame.buffer as ArrayBuffer);
      }
    }
  }

  public send(data: ArrayBuffer): Promise<void> {
    const opt: socket.TCPSendOptions = { data: data };
    return this.sock.send(opt);
  }

  public close(): void {
    this.rx = new Uint8Array(0);
    this.sock.close().catch((err: BusinessError) => {
      hilog.warn(DOMAIN, TAG, 'tcp close fail: %{public}d %{public}s', err.code, err.message);
    });
  }
}

// 会话传输：持有 16 位回绕 seq、token 派生的 HMAC 提供方与统计计数。
// seal 为同步实现（CryptoHmacProvider），发送路径无异步排队；
// MOVE/SCROLL 走 UDP 单播（新值覆盖语义，协议层允许丢包）。
export class Transport {
  private seqN: number = 0;
  private hostAddr: string = '';
  private udpPortN: number = 0;
  private provider: HmacProvider | null = null;
  public readonly udp: UdpChannel = new UdpChannel();
  public readonly tcp: TcpChannel = new TcpChannel();
  public readonly stats: TransportStats = new TransportStats();
  // 解码+验签通过后的包统一出口（ConnectionManager 分发）。
  public onPacket: PacketHandler | null = null;

  public setToken(token: Uint8Array): void {
    this.provider = new CryptoHmacProvider(token);
  }

  public hasToken(): boolean {
    return this.provider !== null;
  }

  public nextSeq(): number {
    const s: number = this.seqN;
    this.seqN = (this.seqN + 1) & 0xffff;
    return s;
  }

  // 打开两通道并记住单播目标。udpPort/tcpPort 同为协议默认 53021（可配）。
  public open(host: string, udpPort: number, tcpPort: number): Promise<void> {
    this.hostAddr = host;
    this.udpPortN = udpPort;
    this.tcp.onRawPacket = (data: ArrayBuffer) => {
      this.handleWire(data);
    };
    this.tcp.onFramingError = () => {
      this.stats.recvDecodeFail++;
    };
    return this.udp.open().then(() => this.tcp.connect(host, tcpPort, TCP_CONNECT_TIMEOUT_MS));
  }

  public close(): void {
    this.udp.close();
    this.tcp.close();
  }

  public sendMove(dx: number, dy: number): void {
    this.sendUnicastSealed(newMove(this.nextSeq(), dx, dy));
  }

  public sendScroll(dyHiRes: number): void {
    this.sendUnicastSealed(newScroll(this.nextSeq(), dyHiRes));
  }

  private sendUnicastSealed(pkt: Packet): void {
    if (this.provider === null) {
      return; // 未认证（未配对/未连接）不外发
    }
    seal(pkt, this.provider);
    const wire: ArrayBuffer = encode(pkt);
    this.udp.sendTo(this.hostAddr, this.udpPortN, wire)
      .then(() => {
        this.stats.udpSendOk++;
      })
      .catch((err: BusinessError) => {
        this.stats.udpSendFail++;
        hilog.warn(DOMAIN, TAG, 'udp send fail: %{public}d %{public}s', err.code, err.message);
      });
  }

  // TCP 可靠发送（BUTTON/KEY/TEXT/HELLO/ECHO/BYE）。未认证时静默丢弃并计数（断连期间手势不崩 UI）。
  public sendReliable(pkt: Packet): Promise<void> {
    if (this.provider === null) {
      this.stats.tcpSendFail++;
      return Promise.resolve();
    }
    seal(pkt, this.provider);
    const wire: ArrayBuffer = encode(pkt);
    return this.tcp.send(wire).then(() => {
      this.stats.tcpSendOk++;
    }).catch((err: BusinessError) => {
      this.stats.tcpSendFail++;
      hilog.warn(DOMAIN, TAG, 'tcp send fail: %{public}d %{public}s', err.code, err.message);
    });
  }

  // 心跳/RTT 探测：ECHO payload = 发送方 Unix 毫秒时间戳，对端原样回传。
  public sendEcho(): void {
    const pkt: Packet = newEcho(this.nextSeq(), new Date().getTime());
    this.sendReliable(pkt);
  }

  // HELLO：payload 2B 大端 ver（版本协商，token 认证自首包 seal 开始）。
  public sendHello(): Promise<void> {
    const p = new Uint8Array(2);
    p[0] = (Protocol.VER >> 8) & 0xff;
    p[1] = Protocol.VER & 0xff;
    const pkt: Packet = newRaw(PacketType.HELLO, this.nextSeq(), p.buffer as ArrayBuffer);
    return this.sendReliable(pkt);
  }

  public sendBye(): void {
    const pkt: Packet = newRaw(PacketType.BYE, this.nextSeq(), new ArrayBuffer(0));
    this.sendReliable(pkt);
  }

  // 收包管线：decode → FlagAuth 时常数时间验签 → 业务分发；两类失败分别计数（调试面板展示）。
  private handleWire(frame: ArrayBuffer): void {
    const r = decode(frame);
    if (!r.ok || r.packet === null) {
      this.stats.recvDecodeFail++;
      return;
    }
    const pkt: Packet = r.packet;
    if ((pkt.flags & PacketFlag.AUTH) !== 0) {
      if (this.provider === null || !verifyHmac(pkt, this.provider)) {
        this.stats.recvHmacFail++;
        return;
      }
    }
    this.stats.recvOk++;
    if (this.onPacket !== null) {
      this.onPacket(pkt);
    }
  }
}
