// 主机 IP 自愈纯逻辑（无 @ohos 依赖，node 可直跑）：候选判定与迁移档案构造。
// host_id 来自未认证发现应答（局域网可冒用），只作候选提示；
// 迁移档案的持久化由 HostHeal.armMigrationPersist 在封签验签通过后兑现。

import type { DiscoveredHost } from '../net/DiscoverCore';
import { HostInfo } from './HostsStore';

// 候选：发现列表中与 target 同 host_id 且地址不同的主机；无身份或无候选返回 null。
export function pickHealCandidate(hosts: DiscoveredHost[], target: HostInfo): DiscoveredHost | null {
  if (target.hostId.length === 0) {
    return null;
  }
  for (const d of hosts) {
    if (d.hostId === target.hostId && (d.ip !== target.ip || d.port !== target.port)) {
      return d;
    }
  }
  return null;
}

// 迁移档案：身份/token/名称沿用既有记录（名称空时取发现名），仅换地址。
export function buildMigratedHost(target: HostInfo, cand: DiscoveredHost): HostInfo {
  const m = new HostInfo();
  m.name = target.name.length > 0 ? target.name : cand.name;
  m.ip = cand.ip;
  m.port = cand.port;
  m.tokenHex = target.tokenHex;
  m.hostId = target.hostId;
  return m;
}
