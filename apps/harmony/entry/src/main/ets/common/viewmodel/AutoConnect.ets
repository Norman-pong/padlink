// 静默自动重连（FR-6）：App 启动（主控页为首页，Index 建立时）lastHost 有 token 且当前空闲
// → 后台 connect(autoRetry=true)，首连失败也进 RECONNECTING 退避循环，失败不弹窗——
// 浮层展示状态与原因（RECONNECTING 显示 lastError，LOST 给手动重连入口）。
import { ConnState } from '../net/ConnectionManager';
import type { ConnectionManager } from '../net/ConnectionManager';
import { hexToBytes } from '../net/HexCodec';
import type { SettingsModel } from './SettingsModel';
import { hilog } from '@kit.PerformanceAnalysisKit';

const TAG: string = 'PadLink.AutoConn';
const DOMAIN: number = 0x5050;
// 触发冷却：防止重复进入主控页时连发（连接进行中快照仍为 IDLE，靠时间窗去重）。
const KICK_COOLDOWN_MS: number = 5000;

let lastKickMs: number = 0;

export function tryAutoReconnect(conn: ConnectionManager, settings: SettingsModel): void {
  const now: number = new Date().getTime();
  if (now - lastKickMs < KICK_COOLDOWN_MS) {
    return;
  }
  lastKickMs = now;
  if (conn.snapshot.state !== ConnState.IDLE) {
    return; // 配对/重连进行中不抢会话
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
    // autoRetry：首连失败转入 RECONNECTING 退避重试（内部不 reject）；catch 仅为未捕获兜底
    conn.connect(h.ip, h.port, h.port, token, true).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'auto reconnect kick fail: %{public}s', err.message);
    });
  }).catch((err: Error) => {
    hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', err.message);
  });
}
