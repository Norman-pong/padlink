// 输入法提交流纯逻辑：提交增量 diff 与字符 → HID 映射（组合态由页面过滤，此处只见稳定文本）。
// codepoint 口径（代理对算 1 字符）。
import { HidUsage } from '../hid/KeyCodes';
import { SHIFT_SYMS } from '../hid/KeyCodes';

// 字符 → 物理键位（usage + 是否需临时 Shift）；不可映射返回 null（走 TEXT）。
export interface HidChar {
  ch: string;
  usage: number;
  withShift: boolean;
}

// 提交增量：删除码点数（先发）与插入文本（后发）。
export interface CommitDiff {
  deletes: number;
  inserts: string;
}

function buildMap(): Map<string, HidChar> {
  const m: Map<string, HidChar> = new Map<string, HidChar>();
  const a: number = 'a'.charCodeAt(0);
  for (let i = 0; i < 26; i++) {
    const lower: string = String.fromCharCode(a + i);
    const upper: string = lower.toUpperCase();
    m.set(lower, { ch: lower, usage: HidUsage.A + i, withShift: false });
    m.set(upper, { ch: upper, usage: HidUsage.A + i, withShift: true });
  }
  const d: number = '1'.charCodeAt(0);
  for (let i = 0; i < 10; i++) {
    const ch: string = String.fromCharCode(d + i);
    m.set(ch, { ch: ch, usage: HidUsage.NUM_1 + i, withShift: false });
  }
  for (const s of SHIFT_SYMS) {
    m.set(s.base, { ch: s.base, usage: s.usage, withShift: false });
    m.set(s.shifted, { ch: s.shifted, usage: s.usage, withShift: true });
  }
  m.set(' ', { ch: ' ', usage: HidUsage.SPACE, withShift: false });
  m.set('\n', { ch: '\n', usage: HidUsage.ENTER, withShift: false });
  m.set('\t', { ch: '\t', usage: HidUsage.TAB, withShift: false });
  m.set('=', { ch: '=', usage: HidUsage.EQUAL, withShift: false });
  m.set('+', { ch: '+', usage: HidUsage.EQUAL, withShift: true });
  m.set('[', { ch: '[', usage: HidUsage.LEFT_BRACKET, withShift: false });
  m.set(']', { ch: ']', usage: HidUsage.RIGHT_BRACKET, withShift: false });
  m.set('{', ch2hc('{', HidUsage.LEFT_BRACKET));
  m.set('}', ch2hc('}', HidUsage.RIGHT_BRACKET));
  m.set('\\', { ch: '\\', usage: HidUsage.BACKSLASH, withShift: false });
  m.set('|', { ch: '|', usage: HidUsage.BACKSLASH, withShift: true });
  m.set('`', { ch: '`', usage: HidUsage.GRAVE, withShift: false });
  m.set('~', { ch: '~', usage: HidUsage.GRAVE, withShift: true });
  return m;
}

function ch2hc(ch: string, usage: number): HidChar {
  return { ch: ch, usage: usage, withShift: true };
}

const CHAR_MAP: Map<string, HidChar> = buildMap();

export class CommitPlan {
  // 公共前后缀截断：旧中段=删除码点数，新中段=插入文本。
  public static diff(oldText: string, newText: string): CommitDiff {
    const oldChars: string[] = Array.from(oldText);
    const newChars: string[] = Array.from(newText);
    let prefix = 0;
    while (prefix < oldChars.length && prefix < newChars.length && oldChars[prefix] === newChars[prefix]) {
      prefix++;
    }
    let suffix = 0;
    while (suffix < oldChars.length - prefix && suffix < newChars.length - prefix
      && oldChars[oldChars.length - 1 - suffix] === newChars[newChars.length - 1 - suffix]) {
      suffix++;
    }
    return {
      deletes: oldChars.length - prefix - suffix,
      inserts: newChars.slice(prefix, newChars.length - suffix).join(''),
    };
  }

  // 不可映射（CJK/emoji 等）返回 null，走 TEXT。
  public static charToHid(ch: string): HidChar | null {
    const h: HidChar | undefined = CHAR_MAP.get(ch);
    return h !== undefined ? h : null;
  }
}
