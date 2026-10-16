// 已绑定 token 会话的下行拒收判据（纯逻辑，node 可直跑）。
// ECHO 应答自协议 v1 起恒随客户端 token 封 FlagAuth（PROTOCOL.md §4.6）：
// 未封签的 ECHO 应答无法证明应答方持有 token——放行会被假主机用假心跳维持假会话。
// HELLO（版本协商）与 ERR 按协议不封签，不在此拒收范围。

import { PacketType, PacketFlag } from '../proto/PacketType';

export function shouldDropUnsealed(pktType: number, pktFlags: number, hasToken: boolean): boolean {
  if (!hasToken) {
    return false;
  }
  if ((pktFlags & PacketFlag.AUTH) !== 0) {
    return false;
  }
  return pktType === PacketType.ECHO;
}
