// HMAC 认证（PROTOCOL.md §5）。
// hmac_trunc = HMAC-SHA256(key=token, 头 19B || payload)[:8]。
// 参与计算的头为"原样发送头"——flags 已含 FlagAuth；偏移 11..18（hmac 字段）全零。
// 该语义由黄金向量 2（testvectors.json id=2）钉死，不可改动。
import { PacketFlag, Protocol } from './PacketType';
import type { Packet } from './Packet';
import { encode } from './Codec';

// HMAC 提供方（同步签名；异步实现由 app 层包装后再注入）。
// 入参为 待哈希字节序列（零化 hmac 的 19B 头 || payload），返回值须为 8 字节截断 tag。
export interface HmacProvider {
  compute(input: Uint8Array): Uint8Array;
}

// 构造 HMAC 计算输入：encode 后将偏移 11..18 清零（flags 保持原样，含 FlagAuth）。
function authInput(pkt: Packet): Uint8Array {
  const wire = new Uint8Array(encode(pkt));
  for (let i = 11; i < Protocol.HEADER_SIZE; i++) {
    wire[i] = 0;
  }
  return wire;
}

function hmacTrunc(pkt: Packet, provider: HmacProvider): Uint8Array {
  return provider.compute(authInput(pkt));
}

// 计算并写入 hmac_trunc，置 FlagAuth。必须在 hmac 计算前置位 flags（参与计算的头即最终发送头）。
export function seal(pkt: Packet, provider: HmacProvider): void {
  pkt.flags = pkt.flags | PacketFlag.AUTH;
  pkt.hmac = hmacTrunc(pkt, provider);
}

// 校验已认证报文的 hmac_trunc。FlagAuth 未置位或比较失败均返回 false。
// 比较采用逐字节异或累积的常数时间实现。
export function verifyHmac(pkt: Packet, provider: HmacProvider): boolean {
  if ((pkt.flags & PacketFlag.AUTH) === 0) {
    return false;
  }
  const sum: Uint8Array = hmacTrunc(pkt, provider);
  let diff = 0;
  for (let i = 0; i < 8; i++) {
    diff |= (sum[i] ^ pkt.hmac[i]) & 0xff;
  }
  return diff === 0;
}
