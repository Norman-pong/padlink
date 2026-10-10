// PadLink 报文模型与构造函数（与 daemon/internal/proto/proto.go 构造器一一对应）。
// 类型化字段仅在 type 匹配时有效，其余保持零值（同 Go 零值语义）。
import { PacketType } from './PacketType';
import { utf8Encode } from './Utf8';

// MOVE：相对指针位移，单位为计数。
export interface MoveField {
  dx: number;
  dy: number;
}

// SCROLL：高分辨率滚轮，单位 = 1/120 滚轮格。
export interface ScrollField {
  dyHiRes: number;
}

// BUTTON：btn 1=左 2=中 3=右；down=按下。
export interface ButtonField {
  btn: number;
  down: boolean;
}

// KEY：USB HID 键盘页 usage。
export interface KeyField {
  hidUsage: number;
  down: boolean;
}

// ECHO：发送方 Unix 毫秒时间戳（|ts| < 2^53，Number 安全）。
export interface EchoField {
  tsMs: number;
}

// NOTICE：daemon → 手机提示（code 语义见 PROTOCOL.md §4.7）。
export interface NoticeField {
  code: number;
  arg: number;
}

// 一次解码后的完整报文。
export interface Packet {
  type: number;
  flags: number;
  seq: number;
  hmac: Uint8Array; // 8 字节 hmac_trunc，未认证时全 0
  payload: ArrayBuffer; // 原始 payload（decode 时为独立副本）

  move: MoveField;
  scroll: ScrollField;
  button: ButtonField;
  key: KeyField;
  echo: EchoField;
  notice: NoticeField; // type == NOTICE 时有效
  text: string; // type == TEXT 时有效
}

function zeroPacket(type: number, seq: number, payload: ArrayBuffer): Packet {
  const pkt: Packet = {
    type: type,
    flags: 0,
    seq: seq,
    hmac: new Uint8Array(8),
    payload: payload,
    move: { dx: 0, dy: 0 },
    scroll: { dyHiRes: 0 },
    button: { btn: 0, down: false },
    key: { hidUsage: 0, down: false },
    echo: { tsMs: 0 },
    notice: { code: 0, arg: 0 },
    text: '',
  };
  return pkt;
}

function be16(out: Uint8Array, off: number, v: number): void {
  out[off] = (v >>> 8) & 0xff;
  out[off + 1] = v & 0xff;
}

function be32(out: Uint8Array, off: number, v: number): void {
  out[off] = (v >>> 24) & 0xff;
  out[off + 1] = (v >>> 16) & 0xff;
  out[off + 2] = (v >>> 8) & 0xff;
  out[off + 3] = v & 0xff;
}

// 会话层报文（HELLO/DISCOVER_RESP/PAIR_*/BYE/ERR），payload 透传。
export function newRaw(type: number, seq: number, payload: ArrayBuffer): Packet {
  const copy = new ArrayBuffer(payload.byteLength);
  new Uint8Array(copy).set(new Uint8Array(payload));
  return zeroPacket(type, seq, copy);
}

export function newMove(seq: number, dx: number, dy: number): Packet {
  const p = new Uint8Array(4);
  be16(p, 0, dx & 0xffff);
  be16(p, 2, dy & 0xffff);
  const pkt: Packet = zeroPacket(PacketType.MOVE, seq, p.buffer as ArrayBuffer);
  pkt.move = { dx: dx, dy: dy };
  return pkt;
}

export function newScroll(seq: number, dyHiRes: number): Packet {
  const p = new Uint8Array(4);
  be32(p, 0, dyHiRes | 0);
  const pkt: Packet = zeroPacket(PacketType.SCROLL, seq, p.buffer as ArrayBuffer);
  pkt.scroll = { dyHiRes: dyHiRes };
  return pkt;
}

export function newButton(seq: number, btn: number, down: boolean): Packet {
  const p = new Uint8Array(2);
  p[0] = btn & 0xff;
  p[1] = down ? 1 : 0;
  const pkt: Packet = zeroPacket(PacketType.BUTTON, seq, p.buffer as ArrayBuffer);
  pkt.button = { btn: btn, down: down };
  return pkt;
}

export function newKey(seq: number, hidUsage: number, down: boolean): Packet {
  const p = new Uint8Array(3);
  be16(p, 0, hidUsage & 0xffff);
  p[2] = down ? 1 : 0;
  const pkt: Packet = zeroPacket(PacketType.KEY, seq, p.buffer as ArrayBuffer);
  pkt.key = { hidUsage: hidUsage, down: down };
  return pkt;
}

export function newText(seq: number, text: string): Packet {
  const bytes: Uint8Array = utf8Encode(text);
  const pkt: Packet = zeroPacket(PacketType.TEXT, seq, bytes.buffer as ArrayBuffer);
  pkt.text = text;
  return pkt;
}

export function newEcho(seq: number, tsMs: number): Packet {
  const p = new Uint8Array(8);
  const negative: boolean = tsMs < 0;
  const abs: number = negative ? -tsMs : tsMs;
  let hi: number = Math.floor(abs / 4294967296);
  let lo: number = abs - hi * 4294967296;
  if (negative) {
    // 64 位补码：按位取反后整体 +1
    hi = ~hi >>> 0;
    lo = (~lo >>> 0) + 1;
    if (lo > 0xffffffff) {
      lo = 0;
      hi = (hi + 1) >>> 0;
    }
  }
  be32(p, 0, hi | 0);
  be32(p, 4, lo | 0);
  const pkt: Packet = zeroPacket(PacketType.ECHO, seq, p.buffer as ArrayBuffer);
  pkt.echo = { tsMs: tsMs };
  return pkt;
}

// NOTICE 报文（daemon → 手机；daemon 侧 Seal 后下发，手机端只收不发）。
export function newNotice(seq: number, code: number, arg: number): Packet {
  const p = new Uint8Array(2);
  p[0] = code & 0xff;
  p[1] = arg & 0xff;
  const pkt: Packet = zeroPacket(PacketType.NOTICE, seq, p.buffer as ArrayBuffer);
  pkt.notice = { code: code, arg: arg };
  return pkt;
}
