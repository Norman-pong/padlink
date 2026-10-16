// 主机 IP 自愈会话钩子：迁移档案的持久化以对端证明持有 token 为前提——
// 首个 FlagAuth 报文验签通过（假主机伪造不出，PROTOCOL.md §4.10）才落盘。
import type { ConnectionManager } from '../net/ConnectionManager';
import type { SettingsModel } from './SettingsModel';
import type { HostInfo } from './HostsStore';
import { hilog } from '@kit.PerformanceAnalysisKit';

const TAG: string = 'PadLink.HostHeal';
const DOMAIN: number = 0x5050;

// 装一次性落盘钩子（先装后连）：验签通过且会话未被其他连接取代时持久化迁移档案。
export function armMigrationPersist(conn: ConnectionManager, settings: SettingsModel, migrated: HostInfo): void {
  conn.transport.onVerified = () => {
    conn.transport.onVerified = null;
    if (conn.getHost() !== migrated.ip || conn.getTcpPort() !== migrated.port) {
      hilog.warn(DOMAIN, TAG, '封签确认到达时会话已切换，放弃迁移落盘');
      return;
    }
    settings.saveLastHost(migrated).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, '迁移档案落盘失败: %{public}s', err.message);
    });
  };
}
