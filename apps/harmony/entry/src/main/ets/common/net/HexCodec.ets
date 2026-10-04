// hex 编解码纯函数（32B token ↔ 64 字符 hex，配对/快速连接共用）。
// 抽公共依据：DebugPanelPage 原私有 hexToBytes 与 M2 PairingModel 的 bytesToHex 为第二处重复。

// hex 字符串 → 字节；非法（奇数长/非 hex 字符）返回 null（外部输入校验）。
export function hexToBytes(hex: string): Uint8Array | null {
  if (hex.length === 0 || hex.length % 2 !== 0) {
    return null;
  }
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    const hi: number = hexVal(hex.charCodeAt(i * 2));
    const lo: number = hexVal(hex.charCodeAt(i * 2 + 1));
    if (hi < 0 || lo < 0) {
      return null;
    }
    out[i] = (hi << 4) | lo;
  }
  return out;
}

// 字节 → 小写 hex 字符串。
export function bytesToHex(bytes: Uint8Array): string {
  const digits = '0123456789abcdef';
  let out = '';
  for (let i = 0; i < bytes.length; i++) {
    const v: number = bytes[i] & 0xff;
    out += digits.charAt(v >> 4) + digits.charAt(v & 0xf);
  }
  return out;
}

function hexVal(c: number): number {
  if (c >= 0x30 && c <= 0x39) {
    return c - 0x30; // '0'..'9'
  }
  if (c >= 0x41 && c <= 0x46) {
    return c - 0x41 + 10; // 'A'..'F'
  }
  if (c >= 0x61 && c <= 0x66) {
    return c - 0x61 + 10; // 'a'..'f'
  }
  return -1;
}
