// 静默自动重连：启动/回前台时默认主机有 token 且当前为断开态（IDLE/LOST）→ 后台 connect(autoRetry=true)，失败不弹窗。
import { ConnState } from '../net/ConnState';
import type { ConnectionManager } from '../net/ConnectionManager';
import { hexToBytes } from '../net/HexCodec';
import type { SettingsModel } from './SettingsModel';
import { hilog } from '@kit.PerformanceAnalysisKit';

const TAG: string = 'PadLink.AutoConn';
const DOMAIN: number = 0x5050;
// 冷却时间窗：连接进行中快照仍为 IDLE，靠它防重复触发。
const KICK_COOLDOWN_MS: number = 5000;

let lastKickMs: number = 0;

export function tryAutoReconnect(conn: ConnectionManager, settings: SettingsModel): void {
  const now: number = new Date().getTime();
  if (now - lastKickMs < KICK_COOLDOWN_MS) {
    return;
  }
  const state: number = conn.snapshot.state;
  if (state !== ConnState.IDLE && state !== ConnState.LOST) {
    return; // 配对/连接/重连进行中不抢会话；LOST 由回前台路径接管（重试耗尽后唯一自愈入口）
  }
  settings.getLastHost().then((h) => {
    if (h === null || h.tokenHex.length === 0 || h.ip.length === 0) {
      return; // 从未配对过：静默跳过
    }
    const token = hexToBytes(h.tokenHex);
    if (token === null) {
      hilog.warn(DOMAIN, TAG, 'lastHost tokenHex 非法，跳过自动重连');
      return;
    }
    // 冷却只对「真正发起的重连」计时：冷启动时 onPageShow 先于 settings.init 完成触发，
    // 此刻拿不到 lastHost 不应占用冷却窗，否则 init 后的重连会被误拦。
    if (new Date().getTime() - lastKickMs < KICK_COOLDOWN_MS) {
      return;
    }
    lastKickMs = new Date().getTime();
    hilog.info(DOMAIN, TAG, '自动重连目标: %{public}s:%{public}d', h.ip, h.port);
    // autoRetry：首连失败转 RECONNECTING 退避（内部不 reject）；catch 仅为未捕获兜底
    conn.connect(h.ip, h.port, h.port, token, true).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'auto reconnect kick fail: %{public}s', err.message);
    });
  }).catch((err: Error) => {
    hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', err.message);
  });
}
