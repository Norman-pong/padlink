// 静默自动重连：启动/回前台时默认主机有 token 且当前为断开态（IDLE/LOST）→ 后台 connect，失败不弹窗。
// 有 host_id 的档案：旧 IP 单发探测失败后静默发现，按身份命中新地址则迁移连接
//（迁移档案经 HostHeal 封签确认后才落盘）；无候选回落旧 IP 退避重连（原行为）。
import { ConnState } from '../net/ConnState';
import type { ConnectionManager } from '../net/ConnectionManager';
import { Discoverer } from '../net/Discoverer';
import { hexToBytes } from '../net/HexCodec';
import type { SettingsModel } from './SettingsModel';
import type { HostInfo } from './HostsStore';
import { pickHealCandidate, buildMigratedHost } from './HostHealCore';
import { armMigrationPersist } from './HostHeal';
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
    if (h.hostId.length === 0) {
      // 旧档案：autoRetry 首连失败转 RECONNECTING 退避（内部不 reject）
      conn.connect(h.ip, h.port, h.port, token, true).catch((err: Error) => {
        hilog.warn(DOMAIN, TAG, 'auto reconnect kick fail: %{public}s', err.message);
      });
      return;
    }
    // 有身份档案：先单发探测旧 IP（autoRetry=false，失败即抛），
    // 失败后走发现自愈——避免对已换地址的主机空转 8 轮退避。
    conn.connect(h.ip, h.port, h.port, token, false).then((): void => {
      hilog.info(DOMAIN, TAG, '旧 IP 直连成功');
    }, (err: Error): void => {
      hilog.info(DOMAIN, TAG, '旧 IP 探测失败，尝试发现自愈: %{public}s', err.message);
      healAndReconnect(conn, settings, h, token);
    });
  }).catch((err: Error) => {
    hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', err.message);
  });
}

// 静默发现 → host_id 命中新地址 → 迁移连接（封签确认后落盘）；无候选回落旧 IP 退避重连。
function healAndReconnect(conn: ConnectionManager, settings: SettingsModel, h: HostInfo, token: Uint8Array): void {
  new Discoverer().discover().then((hosts) => {
    const cand = pickHealCandidate(hosts, h);
    if (cand === null) {
      hilog.info(DOMAIN, TAG, '发现自愈无候选，回落旧 IP 退避重连');
      conn.connect(h.ip, h.port, h.port, token, true).catch((): void => {});
      return;
    }
    const migrated = buildMigratedHost(h, cand);
    hilog.info(DOMAIN, TAG, '发现自愈命中 %{public}s:%{public}d（host_id 一致）', migrated.ip, migrated.port);
    // 钩子在 connect 返回后装：beginSession 会清旧钩子，先装会被本次 connect 摘掉
    conn.connect(migrated.ip, migrated.port, migrated.port, token, true).then((): void => {
      armMigrationPersist(conn, settings, migrated);
    }).catch((err: Error): void => {
      hilog.warn(DOMAIN, TAG, '迁移连接失败（token 可能已失效，待发现页重新配对）: %{public}s', err.message);
    });
  }).catch((err: Error): void => {
    hilog.warn(DOMAIN, TAG, '发现自愈扫描失败: %{public}s', err.message);
    conn.connect(h.ip, h.port, h.port, token, true).catch((): void => {});
  });
}
