// UDP 发现器（FR-0）：广播 HELLO → 收集窗口内全部 DISCOVER_RESP 单播应答。
// 广播姿势复用 UdpChannel（faqs-network-96：bind 0.0.0.0 → setExtraOptions({broadcast}) → bindSocket 锁 WiFi）。
// 广播地址按任务口径仅用 255.255.255.255（子网定向广播为可选优化，未做：需网段计算与多地址并发，
// 收益仅覆盖禁定向转发的 AP 场景，真机联调再评估）。
// 依据（现查文档）：js-apis-socket（UDPSocket 收发/faqs-network-86 ArrayBuffer）、js-apis-net-connection（UdpChannel 内部）。
import { hilog } from '@kit.PerformanceAnalysisKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { UdpChannel, buildHelloPacket } from './Transport';
import { encode, decode } from '../proto/Codec';
import { PacketType } from '../proto/PacketType';
import { utf8DecodeStrict } from '../proto/Utf8';
import { DiscoveredHost, parseDiscoverRespJson, upsertHost } from './DiscoverCore';

const TAG: string = 'PadLink.Discover';
const DOMAIN: number = 0x5050;

// 默认发现窗口与广播间隔（窗口内重发 4 次，容忍丢包）。
export class DiscoverPolicy {
  public static readonly WINDOW_MS: number = 2500;
  public static readonly BROADCAST_INTERVAL_MS: number = 600;
}

export class Discoverer {
  private running: Promise<DiscoveredHost[]> | null = null;

  // 单飞：进行中重复调用复用同一在途 Promise。超时返回已收列表（不去重跨轮）。
  public discover(timeoutMs: number = DiscoverPolicy.WINDOW_MS, port: number = 53021): Promise<DiscoveredHost[]> {
    if (this.running !== null) {
      return this.running;
    }
    const p = this.runDiscovery(timeoutMs, port);
    this.running = p;
    const clear = (): void => {
      if (this.running === p) {
        this.running = null;
      }
    };
    p.then(clear, clear);
    return p;
  }

  private async runDiscovery(timeoutMs: number, port: number): Promise<DiscoveredHost[]> {
    const ch = new UdpChannel();
    let hosts: DiscoveredHost[] = [];
    let windowTimer: number = -1;
    let broadcastTimer: number = -1;
    let seq: number = 0;
    ch.onMessage = (data: ArrayBuffer, fromAddr: string, _fromPort: number) => {
      const r = decode(data);
      if (!r.ok || r.packet === null || r.packet.type !== PacketType.DISCOVER_RESP) {
        return; // 非发现应答（含本机广播回环的 HELLO）一律忽略
      }
      const text: string | null = utf8DecodeStrict(new Uint8Array(r.packet.payload));
      if (text === null) {
        return;
      }
      const h = parseDiscoverRespJson(text, fromAddr);
      if (h !== null) {
        hosts = upsertHost(hosts, h);
      }
    };
    const cleanup = (): void => {
      if (windowTimer >= 0) {
        clearTimeout(windowTimer);
      }
      if (broadcastTimer >= 0) {
        clearInterval(broadcastTimer);
      }
      ch.close(); // 不留 socket 泄漏
    };
    try {
      await ch.open();
      const sendOnce = (): void => {
        ch.sendBroadcast(port, encode(buildHelloPacket(seq++))).catch((err: BusinessError) => {
          // 广播失败不终止窗口（多为瞬时 EBUSY），下轮重发兜底
          hilog.warn(DOMAIN, TAG, 'broadcast fail: %{public}d %{public}s', err.code, err.message);
        });
      };
      sendOnce();
      broadcastTimer = setInterval(sendOnce, DiscoverPolicy.BROADCAST_INTERVAL_MS);
      await new Promise<void>((resolve: () => void) => {
        windowTimer = setTimeout(resolve, timeoutMs);
      });
      return hosts;
    } finally {
      cleanup();
    }
  }
}
