// 键盘 ViewModel：键条按键与输入法提交增量 → HID/TEXT 包；APP 只发 down/up，key repeat 由 daemon 负责。
import { newKey, newText } from '../proto/Packet';
import { HidUsage } from '../hid/KeyCodes';
import type { Transport } from '../net/Transport';
import { StickyModifiers, StickyMod } from './StickyModifiers';
import type { KeyStep } from './StickyModifiers';
import { CommitPlan } from './CommitPlan';

@ObservedV2
export class KeyboardModel {
  // 粘滞修饰点亮快照（UI 只读，本类写入）。
  @Trace shiftLatched: boolean = false;
  @Trace ctrlLatched: boolean = false;
  @Trace altLatched: boolean = false;
  @Trace superLatched: boolean = false;
  // IME 可见性（KeyboardPage 的 TextArea onEditChange 写入）：主控页据此把停靠簇底边距
  // 收成 0 让键条贴齐 IME 顶——RESIZE 避让下窗口底边即 IME 顶边，手势条避让成死空隙。
  @Trace imeVisible: boolean = false;

  // 区域生命周期回调（页面注册：唤起/收起输入法）。
  public onZoneEnter: (() => void) | null = null;
  public onZoneLeave: (() => void) | null = null;
  // zoneEnter 可能早于停靠层挂载（回调尚为 null），页面 onDidBuild 据此补偿进区。
  public zoneActive: boolean = false;

  private sticky: StickyModifiers = new StickyModifiers();
  private transport: Transport | null = null;
  // usage → 待发 up 半段。
  private pendingUps: Map<number, KeyStep[]> = new Map();

  public bindTransport(t: Transport): void {
    this.transport = t;
  }

  // 点按修饰键：纯本机状态，不产生线上流量。
  public toggleModifier(mod: number): void {
    const on: boolean = this.sticky.toggle(mod);
    if (mod === StickyMod.SHIFT) {
      this.shiftLatched = on;
    } else if (mod === StickyMod.CTRL) {
      this.ctrlLatched = on;
    } else if (mod === StickyMod.ALT) {
      this.altLatched = on;
    } else {
      this.superLatched = on;
    }
  }

  // DOWN 发 down 半段并登记 up 半段；CANCEL 也必须经 endKey 补发，否则电脑端残留按键。
  public beginKey(hid: number, withShift: boolean): void {
    const seq: KeyStep[] = this.sticky.takeSequence(hid, withShift);
    const ups: KeyStep[] = [];
    for (const s of seq) {
      if (s.down) {
        this.send(s.hid, true);
      } else {
        ups.push(s);
      }
    }
    this.pendingUps.set(hid, ups);
  }

  public endKey(hid: number): void {
    if (!this.pendingUps.has(hid)) {
      return;
    }
    const ups: KeyStep[] = this.pendingUps.get(hid) as KeyStep[];
    this.pendingUps.delete(hid);
    for (const s of ups) {
      this.send(s.hid, false);
    }
  }

  // 切页复位：补发一切未抬起的 up（防电脑端残留按键）。
  public releaseAll(): void {
    for (const ups of this.pendingUps.values()) {
      for (const s of ups) {
        this.send(s.hid, false);
      }
    }
    this.pendingUps.clear();
    this.sticky.clear();
    this.shiftLatched = false;
    this.ctrlLatched = false;
    this.altLatched = false;
    this.superLatched = false;
  }

  // ---- 区域生命周期 ----

  public zoneEnter(): void {
    this.zoneActive = true;
    if (this.onZoneEnter !== null) {
      this.onZoneEnter();
    }
  }

  public zoneLeave(): void {
    this.zoneActive = false;
    this.releaseAll();
    if (this.onZoneLeave !== null) {
      this.onZoneLeave();
    }
  }

  // ---- 系统输入法提交流 ----

  // 提交增量入口（页面 onChange 且 previewText 为空时调，组合态由页面过滤）。
  public commitText(oldText: string, newTextValue: string): void {
    const d = CommitPlan.diff(oldText, newTextValue);
    for (let i = 0; i < d.deletes; i++) {
      this.tapKey(HidUsage.BACKSPACE);
    }
    if (d.inserts.length === 0) {
      return;
    }
    const chars: string[] = Array.from(d.inserts);
    const single: boolean = chars.length === 1;
    const comboShot: boolean = single && this.anyLatched()
      && CommitPlan.charToHid(chars[0]) !== null;
    if (!comboShot && this.anyLatched()) {
      this.clearLatches();
    }
    let textRun: string = '';
    for (const ch of chars) {
      const h = CommitPlan.charToHid(ch);
      if (h !== null) {
        if (textRun.length > 0) {
          this.sendTextRun(textRun);
          textRun = '';
        }
        this.tapKey(h.usage, h.withShift);
      } else {
        textRun += ch;
      }
    }
    if (textRun.length > 0) {
      this.sendTextRun(textRun);
    }
  }

  private tapKey(usage: number, withShift: boolean = false): void {
    this.beginKey(usage, withShift);
    this.endKey(usage);
  }

  // TEXT payload 上限 1400B（协议 §4.5），按码点分块不劈 UTF-8。
  private sendTextRun(text: string): void {
    const MAX_B: number = 1400;
    const chars: string[] = Array.from(text);
    let chunk: string = '';
    let bytes: number = 0;
    for (const ch of chars) {
      const b: number = utf8Len(ch);
      if (bytes + b > MAX_B) {
        this.pushText(chunk);
        chunk = '';
        bytes = 0;
      }
      chunk += ch;
      bytes += b;
    }
    if (chunk.length > 0) {
      this.pushText(chunk);
    }
  }

  private pushText(chunk: string): void {
    if (this.transport !== null) {
      this.transport.sendReliable(newText(this.transport.nextSeq(), chunk));
    }
  }

  private anyLatched(): boolean {
    return this.shiftLatched || this.ctrlLatched || this.altLatched || this.superLatched;
  }

  private clearLatches(): void {
    this.sticky.clear();
    this.shiftLatched = false;
    this.ctrlLatched = false;
    this.altLatched = false;
    this.superLatched = false;
  }

  // sendReliable 断连期间静默丢弃/内部计数，Promise 已自捕获。
  private send(hid: number, down: boolean): void {
    if (this.transport !== null) {
      this.transport.sendReliable(newKey(this.transport.nextSeq(), hid, down));
    }
  }
}

function utf8Len(ch: string): number {
  const cp: number = ch.codePointAt(0) as number;
  if (cp <= 0x7F) {
    return 1;
  }
  if (cp <= 0x7FF) {
    return 2;
  }
  if (cp <= 0xFFFF) {
    return 3;
  }
  return 4;
}
