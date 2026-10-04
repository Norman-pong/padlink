// 手写 UTF-8 编解码（TEXT payload 专用）。
// 纯逻辑层禁止 @ohos.util.TextDecoder/TextEncoder，故自行实现；
// 解码为严格校验：拒绝截断、超长编码、代理对码点（U+D800..U+DFFF）、越界码点（> U+10FFFF）。

// 字符串 → UTF-8 字节。孤立代理对按 U+FFFD 编码（与 WHATWG TextEncoder 行为一致）。
export function utf8Encode(text: string): Uint8Array {
  let byteLen = 0;
  let i = 0;
  while (i < text.length) {
    const cp: number = nextCodePoint(text, i);
    i += codePointUnits(cp);
    byteLen += codePointBytes(cp);
  }
  const out = new Uint8Array(byteLen);
  let pos = 0;
  i = 0;
  while (i < text.length) {
    const cp: number = nextCodePoint(text, i);
    i += codePointUnits(cp);
    pos = writeCodePoint(out, pos, cp);
  }
  return out;
}

// UTF-8 字节 → 字符串；非法输入返回 null（调用方映射为 BadUTF8 丢包）。
export function utf8DecodeStrict(bytes: Uint8Array): string | null {
  const parts: string[] = [];
  let i = 0;
  while (i < bytes.length) {
    const b0: number = bytes[i];
    let len = 0;
    let min = 0;
    let maxCp = 0x10ffff;
    if (b0 < 0x80) {
      len = 1;
    } else if (b0 >= 0xc2 && b0 <= 0xdf) {
      len = 2;
      min = 0x80;
    } else if (b0 >= 0xe0 && b0 <= 0xef) {
      len = 3;
      min = 0x800;
      if (b0 === 0xed) {
        maxCp = 0xd7ff; // 排除代理对区 U+D800..U+DFFF
      }
    } else if (b0 >= 0xf0 && b0 <= 0xf4) {
      len = 4;
      min = 0x10000;
      if (b0 === 0xf4) {
        maxCp = 0x10ffff; // b0 > 0xf4 已被上方区间拒绝
      }
    } else {
      // 0x80..0xC1 续字节位置错/超长首字节、0xF5..0xFF 非法定义
      return null;
    }
    if (i + len > bytes.length) {
      return null;
    }
    let cp = 0;
    if (len === 1) {
      cp = b0;
    } else {
      for (let k = 1; k < len; k++) {
        const bk: number = bytes[i + k];
        if ((bk & 0xc0) !== 0x80) {
          return null;
        }
      }
      cp = b0 & (0xff >> (len + 1));
      for (let k = 1; k < len; k++) {
        cp = (cp << 6) | (bytes[i + k] & 0x3f);
      }
      if (cp < min || cp > maxCp) {
        // 超长编码（cp < min）、代理对（ED A0..BF）、越界（F4 90..）
        return null;
      }
    }
    parts.push(fromCodePoint(cp));
    i += len;
  }
  return parts.join('');
}

// 从 position 起取一个码点（组合代理对）；孤立代理按 U+FFFD。
function nextCodePoint(text: string, pos: number): number {
  const u0: number = text.charCodeAt(pos);
  if (u0 >= 0xd800 && u0 <= 0xdbff && pos + 1 < text.length) {
    const u1: number = text.charCodeAt(pos + 1);
    if (u1 >= 0xdc00 && u1 <= 0xdfff) {
      return 0x10000 + ((u0 - 0xd800) << 10) + (u1 - 0xdc00);
    }
  }
  if (u0 >= 0xd800 && u0 <= 0xdfff) {
    return 0xfffd;
  }
  return u0;
}

function codePointUnits(cp: number): number {
  if (cp >= 0x10000) {
    return 2;
  }
  return 1;
}

function codePointBytes(cp: number): number {
  if (cp < 0x80) {
    return 1;
  }
  if (cp < 0x800) {
    return 2;
  }
  if (cp < 0x10000) {
    return 3;
  }
  return 4;
}

function writeCodePoint(out: Uint8Array, pos: number, cp: number): number {
  if (cp < 0x80) {
    out[pos] = cp;
    return pos + 1;
  }
  if (cp < 0x800) {
    out[pos] = 0xc0 | (cp >> 6);
    out[pos + 1] = 0x80 | (cp & 0x3f);
    return pos + 2;
  }
  if (cp < 0x10000) {
    out[pos] = 0xe0 | (cp >> 12);
    out[pos + 1] = 0x80 | ((cp >> 6) & 0x3f);
    out[pos + 2] = 0x80 | (cp & 0x3f);
    return pos + 3;
  }
  out[pos] = 0xf0 | (cp >> 18);
  out[pos + 1] = 0x80 | ((cp >> 12) & 0x3f);
  out[pos + 2] = 0x80 | ((cp >> 6) & 0x3f);
  out[pos + 3] = 0x80 | (cp & 0x3f);
  return pos + 4;
}

// 码点 → JS 字符串（>0xFFFF 时手工拆代理对，不用 fromCodePoint 以保最大兼容）。
function fromCodePoint(cp: number): string {
  if (cp < 0x10000) {
    return String.fromCharCode(cp);
  }
  const hi: number = 0xd800 + ((cp - 0x10000) >> 10);
  const lo: number = 0xdc00 + ((cp - 0x10000) & 0x3ff);
  return String.fromCharCode(hi) + String.fromCharCode(lo);
}
