// HID usage 常量与符号层映射（USB HID 键盘页 0x07）。
// 值与 daemon/internal/inject/hid_map.go 全集（103 项）严格一致：daemon 负责 usage → Linux KEY_* 映射，
// APP 侧只引用 usage 常量，禁止自造映射。纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum。

// usage 常量类（只读；注释保留 daemon 侧对应 Linux KEY_* 便于对照）。
export class HidUsage {
  // 修饰键（0xE0-0xE7）
  public static readonly LEFT_CTRL: number = 0xE0; // KEY_LEFTCTRL(29)
  public static readonly LEFT_SHIFT: number = 0xE1; // KEY_LEFTSHIFT(42)
  public static readonly LEFT_ALT: number = 0xE2; // KEY_LEFTALT(56)
  public static readonly LEFT_SUPER: number = 0xE3; // KEY_LEFTMETA(125)
  public static readonly RIGHT_CTRL: number = 0xE4; // KEY_RIGHTCTRL(97)
  public static readonly RIGHT_SHIFT: number = 0xE5; // KEY_RIGHTSHIFT(54)
  public static readonly RIGHT_ALT: number = 0xE6; // KEY_RIGHTALT(100)
  public static readonly RIGHT_SUPER: number = 0xE7; // KEY_RIGHTMETA(126)

  // 字母 A-Z（0x04-0x1D）
  public static readonly A: number = 0x04;
  public static readonly B: number = 0x05;
  public static readonly C: number = 0x06;
  public static readonly D: number = 0x07;
  public static readonly E: number = 0x08;
  public static readonly F: number = 0x09;
  public static readonly G: number = 0x0A;
  public static readonly H: number = 0x0B;
  public static readonly I: number = 0x0C;
  public static readonly J: number = 0x0D;
  public static readonly K: number = 0x0E;
  public static readonly L: number = 0x0F;
  public static readonly M: number = 0x10;
  public static readonly N: number = 0x11;
  public static readonly O: number = 0x12;
  public static readonly P: number = 0x13;
  public static readonly Q: number = 0x14;
  public static readonly R: number = 0x15;
  public static readonly S: number = 0x16;
  public static readonly T: number = 0x17;
  public static readonly U: number = 0x18;
  public static readonly V: number = 0x19;
  public static readonly W: number = 0x1A;
  public static readonly X: number = 0x1B;
  public static readonly Y: number = 0x1C;
  public static readonly Z: number = 0x1D;

  // 数字行 1-0（0x1E-0x27）
  public static readonly NUM_1: number = 0x1E;
  public static readonly NUM_2: number = 0x1F;
  public static readonly NUM_3: number = 0x20;
  public static readonly NUM_4: number = 0x21;
  public static readonly NUM_5: number = 0x22;
  public static readonly NUM_6: number = 0x23;
  public static readonly NUM_7: number = 0x24;
  public static readonly NUM_8: number = 0x25;
  public static readonly NUM_9: number = 0x26;
  public static readonly NUM_0: number = 0x27;

  // 控制键
  public static readonly ENTER: number = 0x28; // KEY_ENTER(28)
  public static readonly ESC: number = 0x29; // KEY_ESC(1)
  public static readonly BACKSPACE: number = 0x2A; // KEY_BACKSPACE(14)
  public static readonly TAB: number = 0x2B; // KEY_TAB(15)
  public static readonly SPACE: number = 0x2C; // KEY_SPACE(57)

  // 符号区（0x32 为非美式键盘专用键，hid_map.go 未收录，这里同步不定义）
  public static readonly MINUS: number = 0x2D; // KEY_MINUS(12) '-'
  public static readonly EQUAL: number = 0x2E; // KEY_EQUAL(13) '='
  public static readonly LEFT_BRACKET: number = 0x2F; // KEY_LEFTBRACE(26) '['
  public static readonly RIGHT_BRACKET: number = 0x30; // KEY_RIGHTBRACE(27) ']'
  public static readonly BACKSLASH: number = 0x31; // KEY_BACKSLASH(43) '\'
  public static readonly SEMICOLON: number = 0x33; // KEY_SEMICOLON(39) ';'
  public static readonly APOSTROPHE: number = 0x34; // KEY_APOSTROPHE(40) '\''
  public static readonly GRAVE: number = 0x35; // KEY_GRAVE(41) '`'
  public static readonly COMMA: number = 0x36; // KEY_COMMA(51) ','
  public static readonly DOT: number = 0x37; // KEY_DOT(52) '.'
  public static readonly SLASH: number = 0x38; // KEY_SLASH(53) '/'

  public static readonly CAPS_LOCK: number = 0x39; // KEY_CAPSLOCK(58)

  // F1-F12（0x3A-0x45）
  public static readonly F1: number = 0x3A;
  public static readonly F2: number = 0x3B;
  public static readonly F3: number = 0x3C;
  public static readonly F4: number = 0x3D;
  public static readonly F5: number = 0x3E;
  public static readonly F6: number = 0x3F;
  public static readonly F7: number = 0x40;
  public static readonly F8: number = 0x41;
  public static readonly F9: number = 0x42;
  public static readonly F10: number = 0x43;
  public static readonly F11: number = 0x44;
  public static readonly F12: number = 0x45;

  // 锁定/屏幕键
  public static readonly PRINT_SCREEN: number = 0x46; // KEY_SYSRQ(99)
  public static readonly SCROLL_LOCK: number = 0x47; // KEY_SCROLLLOCK(70)
  public static readonly PAUSE: number = 0x48; // KEY_PAUSE(119)

  // 导航区
  public static readonly INSERT: number = 0x49; // KEY_INSERT(110)
  public static readonly HOME: number = 0x4A; // KEY_HOME(102)
  public static readonly PAGE_UP: number = 0x4B; // KEY_PAGEUP(104)
  public static readonly DELETE: number = 0x4C; // KEY_DELETE(111)
  public static readonly END: number = 0x4D; // KEY_END(107)
  public static readonly PAGE_DOWN: number = 0x4E; // KEY_PAGEDOWN(109)

  // 方向键
  public static readonly RIGHT: number = 0x4F; // KEY_RIGHT(106)
  public static readonly LEFT: number = 0x50; // KEY_LEFT(105)
  public static readonly DOWN: number = 0x51; // KEY_DOWN(108)
  public static readonly UP: number = 0x52; // KEY_UP(103)

  // 小键盘：NumLock 与运算符
  public static readonly NUM_LOCK: number = 0x53; // KEY_NUMLOCK(69)
  public static readonly KP_SLASH: number = 0x54; // KEY_KPSLASH(98)
  public static readonly KP_ASTERISK: number = 0x55; // KEY_KPASTERISK(55)
  public static readonly KP_MINUS: number = 0x56; // KEY_KPMINUS(74)
  public static readonly KP_PLUS: number = 0x57; // KEY_KPPLUS(78)
  public static readonly KP_ENTER: number = 0x58; // KEY_KPENTER(96)

  // 小键盘数字 KP1-KP9,KP0 与 KP.
  public static readonly KP_1: number = 0x59;
  public static readonly KP_2: number = 0x5A;
  public static readonly KP_3: number = 0x5B;
  public static readonly KP_4: number = 0x5C;
  public static readonly KP_5: number = 0x5D;
  public static readonly KP_6: number = 0x5E;
  public static readonly KP_7: number = 0x5F;
  public static readonly KP_8: number = 0x60;
  public static readonly KP_9: number = 0x61;
  public static readonly KP_0: number = 0x62;
  public static readonly KP_DOT: number = 0x63;
}

// 符号层映射条目：一个物理键位的主层字符与 shift 变体（PRD §4.4 符号层）。
// [主 usage, 需 Shift]：符号层键按下时先组合 Shift 再发主 usage（StickyModifiers.takeSequence 的 extraShift）。
export interface ShiftSym {
  base: string; // 主层显示字符
  shifted: string; // 符号层显示字符（Shift 变体）
  usage: number; // 承载该字符对的物理键 usage（未按 Shift 的键位）
  needShift: boolean; // 符号层是否需组合 Shift（本表全为 true）
  a11yKey: string; // 符号层字符的无障碍朗读文案键（string.json；空串回退读字符本身）
  baseA11yKey: string; // 主层字符的无障碍朗读文案键（空串回退读字符本身）
}

// 主层符号 → 符号层变体全集（数字行 shift 变体 !@#… + 常用标点；前 10 项即数字行）。
// 取舍：v1 只覆盖布局内物理键位的 shift 变体（= [ ] \ ` 等未上屏的符号暂不提供）。
export const SHIFT_SYMS: ShiftSym[] = [
  { base: '1', shifted: '!', usage: HidUsage.NUM_1, needShift: true, a11yKey: 'kb_sym_exclamation', baseA11yKey: '' },
  { base: '2', shifted: '@', usage: HidUsage.NUM_2, needShift: true, a11yKey: 'kb_sym_at', baseA11yKey: '' },
  { base: '3', shifted: '#', usage: HidUsage.NUM_3, needShift: true, a11yKey: 'kb_sym_hash', baseA11yKey: '' },
  { base: '4', shifted: '$', usage: HidUsage.NUM_4, needShift: true, a11yKey: 'kb_sym_dollar', baseA11yKey: '' },
  { base: '5', shifted: '%', usage: HidUsage.NUM_5, needShift: true, a11yKey: 'kb_sym_percent', baseA11yKey: '' },
  { base: '6', shifted: '^', usage: HidUsage.NUM_6, needShift: true, a11yKey: 'kb_sym_caret', baseA11yKey: '' },
  { base: '7', shifted: '&', usage: HidUsage.NUM_7, needShift: true, a11yKey: 'kb_sym_ampersand', baseA11yKey: '' },
  { base: '8', shifted: '*', usage: HidUsage.NUM_8, needShift: true, a11yKey: 'kb_sym_asterisk', baseA11yKey: '' },
  { base: '9', shifted: '(', usage: HidUsage.NUM_9, needShift: true, a11yKey: 'kb_sym_lparen', baseA11yKey: '' },
  { base: '0', shifted: ')', usage: HidUsage.NUM_0, needShift: true, a11yKey: 'kb_sym_rparen', baseA11yKey: '' },
  { base: '-', shifted: '_', usage: HidUsage.MINUS, needShift: true, a11yKey: 'kb_sym_underscore', baseA11yKey: 'kb_key_minus' },
  { base: ';', shifted: ':', usage: HidUsage.SEMICOLON, needShift: true, a11yKey: 'kb_sym_colon', baseA11yKey: 'kb_key_semicolon' },
  { base: '\'', shifted: '"', usage: HidUsage.APOSTROPHE, needShift: true, a11yKey: 'kb_sym_dquote', baseA11yKey: 'kb_key_quote' },
  { base: ',', shifted: '<', usage: HidUsage.COMMA, needShift: true, a11yKey: 'kb_sym_less', baseA11yKey: 'kb_key_comma' },
  { base: '.', shifted: '>', usage: HidUsage.DOT, needShift: true, a11yKey: 'kb_sym_greater', baseA11yKey: 'kb_key_period' },
  { base: '/', shifted: '?', usage: HidUsage.SLASH, needShift: true, a11yKey: 'kb_sym_question', baseA11yKey: 'kb_key_slash' },
];

// 主层字符 → 同一物理键位的符号层变体；无变体（字母/其它）返回 null。
// 数字行变体即本表前 10 项（'1'..'0' → '!'..')'）。
export function symVariantOf(baseCh: string): ShiftSym | null {
  for (const s of SHIFT_SYMS) {
    if (s.base === baseCh) {
      return s;
    }
  }
  return null;
}

// 小写字母 → HID usage（A-Z 连续 0x04-0x1D）；非法入参返回 0（不映射）。
export function letterUsage(lowerCh: string): number {
  const idx: number = lowerCh.charCodeAt(0) - 97; // 'a' = 0x61
  if (idx < 0 || idx > 25) {
    return 0;
  }
  return HidUsage.A + idx;
}
