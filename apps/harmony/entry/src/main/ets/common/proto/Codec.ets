// PadLink 线协议 v1 编解码（19B 头 + payload，大端序）。
// 校验顺序严格按 PROTOCOL.md §2.2：
// 长度≥19 → magic → ver → payload_len≤1400 → payload_len==剩余长度 →（FlagAuth 时）HMAC（由 Auth.ets 承担）。
import { Protocol, PacketType } from './PacketType';
import type { Packet } from './Packet';
import { utf8DecodeStrict } from './Utf8';

// 解码失败类别，语义与 Go 哨兵错误一一对应：
// BadMagic↔ErrBadMagic BadVersion↔ErrBadVersion LenRange↔ErrLenRange Truncated↔ErrTruncated
// UnknownType↔ErrUnknownType PayloadLen↔ErrPayloadLen BadUTF8↔ErrBadUTF8 BadHMAC↔ErrBadHMAC
export class DecodeError {
  public static readonly BadMagic: number = 1;
  public static readonly BadVersion: number = 2;
  public static readonly LenRange: number = 3;
  public static readonly Truncated: number = 4;
  public static readonly UnknownType: number = 5;
  public static readonly PayloadLen: number = 6;
  public static readonly BadUTF8: number = 7;
  public static readonly BadHMAC: number = 8;
}

// 解码结果：ok=true 时 packet 有效；ok=false 时 kind 指明失败类别。
// BadHMAC 不由 decode 产生（本层不持有 key），供会话层处理 verifyHmac 失败时归类。
export class DecodeResult {
  public ok: boolean = false;
  public packet: Packet | null = null;
  public kind: number = 0;
  public message: string = '';
}

const MAGIC_0: number = 0x50; // 'P'
const MAGIC_1: number = 0x4c; // 'L'

function fail(kind: number, message: string): DecodeResult {
  const r: DecodeResult = new DecodeResult();
  r.ok = false;
  r.kind = kind;
  r.message = message;
  return r;
}

// 序列化为线格式：reserved 置 0，hmac 字段原样写入。payload 超限直接抛错（fail fast）。
export function encode(pkt: Packet): ArrayBuffer {
  const plen: number = pkt.payload.byteLength;
  if (plen > Protocol.MAX_PAYLOAD) {
    throw new Error(`payload_len ${plen} > ${Protocol.MAX_PAYLOAD}`);
  }
  const buf = new ArrayBuffer(Protocol.HEADER_SIZE + plen);
  const dv = new DataView(buf);
  dv.setUint8(0, MAGIC_0);
  dv.setUint8(1, MAGIC_1);
  dv.setUint8(2, Protocol.VER);
  dv.setUint8(3, pkt.type & 0xff);
  dv.setUint8(4, pkt.flags & 0xff);
  dv.setUint16(5, pkt.seq & 0xffff, false);
  dv.setUint16(7, plen, false);
  // 偏移 9..10 reserved 恒 0
  for (let i = 0; i < 8; i++) {
    dv.setUint8(11 + i, pkt.hmac[i] & 0xff);
  }
  new Uint8Array(buf).set(new Uint8Array(pkt.payload), Protocol.HEADER_SIZE);
  return buf;
}

// 按 §2.2 顺序校验并解析。FlagAuth 置位时不在此校验 HMAC（同 Go.Decode）。
export function decode(data: ArrayBuffer): DecodeResult {
  const u = new Uint8Array(data);
  if (u.length < Protocol.HEADER_SIZE) {
    return fail(DecodeError.Truncated, `需要 ${Protocol.HEADER_SIZE} 字节头，实得 ${u.length}`);
  }
  if (u[0] !== MAGIC_0 || u[1] !== MAGIC_1) {
    return fail(DecodeError.BadMagic, `magic=${hex2(u[0])}${hex2(u[1])}`);
  }
  const dv = new DataView(data);
  const ver: number = dv.getUint8(2);
  if (ver !== Protocol.VER) {
    return fail(DecodeError.BadVersion, `ver=${ver}`);
  }
  const type: number = dv.getUint8(3);
  const flags: number = dv.getUint8(4);
  const seq: number = dv.getUint16(5, false);
  const plen: number = dv.getUint16(7, false);
  // 偏移 9..10 reserved 接收不校验
  const hmac = new Uint8Array(8);
  for (let i = 0; i < 8; i++) {
    hmac[i] = u[11 + i];
  }
  if (plen > Protocol.MAX_PAYLOAD) {
    return fail(DecodeError.LenRange, `payload_len=${plen} > ${Protocol.MAX_PAYLOAD}`);
  }
  if (plen !== u.length - Protocol.HEADER_SIZE) {
    return fail(DecodeError.Truncated, `payload_len=${plen}，实得 ${u.length - Protocol.HEADER_SIZE} 字节`);
  }
  const payload = new ArrayBuffer(plen);
  new Uint8Array(payload).set(u.subarray(Protocol.HEADER_SIZE));

  const pkt: Packet = {
    type: type,
    flags: flags,
    seq: seq,
    hmac: hmac,
    payload: payload,
    move: { dx: 0, dy: 0 },
    scroll: { dyHiRes: 0 },
    button: { btn: 0, down: false },
    key: { hidUsage: 0, down: false },
    echo: { tsMs: 0 },
    notice: { code: 0, arg: 0 },
    text: '',
  };
  const pv = new DataView(payload);
  switch (type) {
    case PacketType.MOVE: {
      if (plen !== 4) {
        return fail(DecodeError.PayloadLen, `MOVE 需要 4B，实得 ${plen}`);
      }
      pkt.move = { dx: pv.getInt16(0, false), dy: pv.getInt16(2, false) };
      break;
    }
    case PacketType.SCROLL: {
      if (plen !== 4) {
        return fail(DecodeError.PayloadLen, `SCROLL 需要 4B，实得 ${plen}`);
      }
      pkt.scroll = { dyHiRes: pv.getInt32(0, false) };
      break;
    }
    case PacketType.BUTTON: {
      if (plen !== 2) {
        return fail(DecodeError.PayloadLen, `BUTTON 需要 2B，实得 ${plen}`);
      }
      pkt.button = { btn: u[Protocol.HEADER_SIZE], down: u[Protocol.HEADER_SIZE + 1] !== 0 };
      break;
    }
    case PacketType.KEY: {
      if (plen !== 3) {
        return fail(DecodeError.PayloadLen, `KEY 需要 3B，实得 ${plen}`);
      }
      pkt.key = { hidUsage: pv.getUint16(0, false), down: u[Protocol.HEADER_SIZE + 2] !== 0 };
      break;
    }
    case PacketType.TEXT: {
      const text: string | null = utf8DecodeStrict(u.subarray(Protocol.HEADER_SIZE));
      if (text === null) {
        return fail(DecodeError.BadUTF8, 'TEXT payload 不是合法 UTF-8');
      }
      pkt.text = text;
      break;
    }
    case PacketType.ECHO: {
      if (plen !== 8) {
        return fail(DecodeError.PayloadLen, `ECHO 需要 8B，实得 ${plen}`);
      }
      pkt.echo = { tsMs: pv.getInt32(0, false) * 4294967296 + pv.getUint32(4, false) };
      break;
    }
    case PacketType.NOTICE: {
      if (plen !== 2) {
        return fail(DecodeError.PayloadLen, `NOTICE 需要 2B，实得 ${plen}`);
      }
      pkt.notice = { code: u[Protocol.HEADER_SIZE], arg: u[Protocol.HEADER_SIZE + 1] };
      break;
    }
    case PacketType.HELLO:
    case PacketType.DISCOVER_RESP:
    case PacketType.PAIR_REQ:
    case PacketType.PAIR_OK:
    case PacketType.PAIR_NAK:
    case PacketType.BYE:
    case PacketType.ERR:
      // 会话层 payload 本层不解析，保留在 pkt.payload
      break;
    default:
      return fail(DecodeError.UnknownType, `type=0x${hex2(type)}`);
  }
  const ok: DecodeResult = new DecodeResult();
  ok.ok = true;
  ok.packet = pkt;
  return ok;
}

function hex2(v: number): string {
  const digits = '0123456789ABCDEF';
  return digits.charAt((v >> 4) & 0xf) + digits.charAt(v & 0xf);
}
