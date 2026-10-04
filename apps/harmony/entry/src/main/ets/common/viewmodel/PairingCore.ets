// 配对流程纯逻辑（无 @ohos 依赖、目录内自闭合，tools/etstest 可跑）：
// PAIR_NAK reason→文案资源键映射、确认码校验、PAIR_REQ payload 构造、设备名 64B 截断。
// 文案本体按 lore 约束存 string.json（中英双份），本层只产出稳定资源键（$r 需字面量，页面用
// getStringByNameSync 取键）。协议依据：daemon README「协议 v1 约定」+ internal/pairing（CodeLen=4、
// MaxNameLen=64 字节、Nak reason 0..4）。

// PAIR_NAK reason 枚举（daemon/session/server.go Nak* 一一对应）。
export class PairNak {
  public static readonly WRONG_CODE: number = 0;
  public static readonly EXPIRED: number = 1;
  public static readonly TOO_MANY: number = 2;
  public static readonly NO_SESSION: number = 3;
  public static readonly FULL: number = 4;
}

// 配对常量。
export class PairPolicy {
  public static readonly CODE_LEN: number = 4;
  public static readonly TOKEN_LEN: number = 32;
  public static readonly NAME_MAX_BYTES: number = 64;
}

// reason → 文案资源键（string.json；未知值兜底通用失败文案）。
export function nakReasonKey(reason: number): string {
  if (reason === PairNak.WRONG_CODE) {
    return 'pair_nak_wrong_code';
  }
  if (reason === PairNak.EXPIRED) {
    return 'pair_nak_expired';
  }
  if (reason === PairNak.TOO_MANY) {
    return 'pair_nak_too_many';
  }
  if (reason === PairNak.NO_SESSION) {
    return 'pair_nak_no_session';
  }
  if (reason === PairNak.FULL) {
    return 'pair_nak_full';
  }
  return 'pair_nak_unknown';
}

// 确认码校验：恰为 4 个 ASCII 数字（对齐 daemon isDigits + CodeLen）。
export function validatePairCode(code: string): boolean {
  if (code.length !== PairPolicy.CODE_LEN) {
    return false;
  }
  for (let i = 0; i < code.length; i++) {
    const c: number = code.charCodeAt(i);
    if (c < 0x30 || c > 0x39) {
      return false;
    }
  }
  return true;
}

// PAIR_NAK payload（1B reason）→ 数值；形态非法返回 -1（按未知原因兜底）。
export function parsePairNakReason(payload: ArrayBuffer): number {
  if (payload.byteLength !== 1) {
    return -1;
  }
  return new Uint8Array(payload)[0];
}

// PAIR_OK payload → token 字节副本（32B）；形态非法返回 null。
export function pairOkToken(payload: ArrayBuffer): Uint8Array | null {
  if (payload.byteLength !== PairPolicy.TOKEN_LEN) {
    return null;
  }
  const out = new Uint8Array(PairPolicy.TOKEN_LEN);
  out.set(new Uint8Array(payload));
  return out;
}

// 发起配对 PAIR_REQ payload（JSON 文本，调用方 UTF-8 编码后发送）。
// daemon 侧校验：len(name) > 64 字节 → ERR(2)+关，故发送前必须截断（sanitizeDeviceName）。
export function buildPairReqInitJson(name: string): string {
  return `{"name":"${jsonEscape(sanitizeDeviceName(name))}"}`;
}

// 确认码尝试 PAIR_REQ payload（4B ASCII 数字）；非法入参返回 null。
export function buildPairReqCodeBytes(code: string): Uint8Array | null {
  if (!validatePairCode(code)) {
    return null;
  }
  const out = new Uint8Array(PairPolicy.CODE_LEN);
  for (let i = 0; i < PairPolicy.CODE_LEN; i++) {
    out[i] = code.charCodeAt(i);
  }
  return out;
}

// 设备名清洗：去首尾空白 + 按 UTF-8 字节数截断（不劈开多字节码点；孤立代理按 3B 计，
// 与 Utf8.encode 按 U+FFFD 编码的体积一致）。maxBytes ≤ 0 时返回空串。
export function sanitizeDeviceName(raw: string, maxBytes: number = PairPolicy.NAME_MAX_BYTES): string {
  const s: string = raw.trim();
  if (maxBytes <= 0 || utf8ByteLen(s) <= maxBytes) {
    return s;
  }
  let bytes = 0;
  let end = 0;
  while (end < s.length) {
    const u: number = s.charCodeAt(end);
    const cpBytes: number = utf8BytesOfUnit(u, end + 1 < s.length ? s.charCodeAt(end + 1) : -1);
    if (bytes + cpBytes > maxBytes) {
      break;
    }
    bytes += cpBytes;
    end += surrogateUnits(u, end + 1 < s.length ? s.charCodeAt(end + 1) : -1);
  }
  return s.slice(0, end);
}

// ---- 内部：UTF-8 字节体积估算（与 common/proto/Utf8 编码规则一致） ----

function utf8ByteLen(s: string): number {
  let total = 0;
  let i = 0;
  while (i < s.length) {
    const u: number = s.charCodeAt(i);
    total += utf8BytesOfUnit(u, i + 1 < s.length ? s.charCodeAt(i + 1) : -1);
    i += surrogateUnits(u, i + 1 < s.length ? s.charCodeAt(i + 1) : -1);
  }
  return total;
}

// 单个码点的 UTF-8 字节数；孤立代理按 U+FFFD 的 3 字节计。
function utf8BytesOfUnit(u: number, next: number): number {
  if (u >= 0xd800 && u <= 0xdbff) {
    return next >= 0xdc00 && next <= 0xdfff ? 4 : 3;
  }
  if (u >= 0xdc00 && u <= 0xdfff) {
    return 3; // 孤立低位代理
  }
  if (u < 0x80) {
    return 1;
  }
  if (u < 0x800) {
    return 2;
  }
  return 3;
}

// 码点占用字符串单元数（代理对为 2，其余 1）。
function surrogateUnits(u: number, next: number): number {
  return u >= 0xd800 && u <= 0xdbff && next >= 0xdc00 && next <= 0xdfff ? 2 : 1;
}

function jsonEscape(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c: string = s.charAt(i);
    if (c === '"' || c === '\\') {
      out += '\\' + c;
    } else if (s.charCodeAt(i) < 0x20) {
      out += '\\u' + hex4(s.charCodeAt(i));
    } else {
      out += c;
    }
  }
  return out;
}

function hex4(v: number): string {
  const digits = '0123456789abcdef';
  let out = '';
  for (let shift = 12; shift >= 0; shift -= 4) {
    out += digits.charAt((v >> shift) & 0xf);
  }
  return out;
}
