// PadLink 线协议 v1 常量（PROTOCOL.md §2/§3）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器。不用 TS enum（保证 node 可直跑）。

// 事件类型（PROTOCOL.md §3），未定义值解码时丢弃。
export class PacketType {
  public static readonly HELLO: number = 1;
  public static readonly DISCOVER_RESP: number = 2;
  public static readonly PAIR_REQ: number = 3;
  public static readonly PAIR_OK: number = 4;
  public static readonly PAIR_NAK: number = 5;
  public static readonly MOVE: number = 6;
  public static readonly SCROLL: number = 7;
  public static readonly BUTTON: number = 8;
  public static readonly KEY: number = 9;
  public static readonly TEXT: number = 10;
  public static readonly ECHO: number = 11;
  public static readonly BYE: number = 12;
  public static readonly ERR: number = 13;
  // daemon → 手机提示（唯一的下行事件，PROTOCOL.md §4.7）；未定义 code 忽略。
  public static readonly NOTICE: number = 14;
}

// NOTICE.code 取值（PROTOCOL.md §4.7）。
export class NoticeCode {
  // 控制被其他设备占用：arg = 冷静期剩余秒数。
  public static readonly CONTROL_BUSY: number = 0;
}

// flags 位定义（PROTOCOL.md §2.1）。
export class PacketFlag {
  // bit0：hmac_trunc 有效
  public static readonly AUTH: number = 0x01;
}

// 协议级常量。
export class Protocol {
  public static readonly VER: number = 1;
  public static readonly HEADER_SIZE: number = 19;
  public static readonly MAX_PAYLOAD: number = 1400;
}
