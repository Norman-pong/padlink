// 键盘 ViewModel（PRD §4.4 混合形态）：系统输入法承接文本 + 自绘键条补键语义。
// - 提交流（commitText）：输入框 onChange 的提交增量——可映射 ASCII 逐字符走 HID（保留按键
//   语义、不踩电脑剪贴板），不可映射（CJK/emoji）合并成 TEXT run 走剪贴板注入（协议 §4.5）；
//   删除镜像为 KEY Backspace；单字符提交遇粘滞修饰合成组合键（Ctrl+C 等），多字符提交视为
//   短语——清除粘滞直发（组合键语义只对单字符有意义）；
// - 键条（beginKey/endKey）：长按不连发，APP 只发 down/up，key repeat 由 daemon 侧负责（协议约定）；
// - 两段式发送：DOWN 发「修饰 down（固定序）+ 主键 down」，UP/CANCEL 发「主键 up + 修饰 up（逆序）」——
//   CANCEL 也必须补 up，否则电脑端残留按住的键；按 usage 登记待抬序列，支持多键同时按住（两手拇指）；
// - 粘滞修饰态经 @ObservedV2/@Trace 快照暴露 UI（字段只由本类写入，@Trace 驱动点亮刷新）。
import { newKey, newText } from '../proto/Packet';
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

  // 区域生命周期（页面注册）：进入键盘区 → 页面唤起输入法/切 RESIZE 避让；离开 → 收起并恢复。
  public onZoneEnter: (() => void) | null = null;
  public onZoneLeave: (() => void) | null = null;
  // 当前是否键盘区激活（页面懒构建时补偿首次 zoneEnter 用；非 UI 观察字段）。
  public zoneActive: boolean = false;

  private sticky: StickyModifiers = new StickyModifiers();
  private transport: Transport | null = null;
  // usage → 抬起阶段待发步骤（beginKey 登记 / endKey 消费）；键位以 usage 区分，天然支持同键唯一。
  private pendingUps: Map<number, KeyStep[]> = new Map();

  public bindTransport(t: Transport): void {
    this.transport = t;
  }

  // 点按修饰键（DOWN 即切换，点亮反馈即时；再点取消）。纯本机状态，不产生线上流量。
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

  // 非修饰键按下（DOWN）：takeSequence 生成组合序列并复位粘滞态，发送 down 半段、登记 up 半段。
  // withShift：符号层键的临时 Shift（不占用粘滞态；Shift 已点亮时不重复）。
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

  // 非修饰键抬起（UP/CANCEL）：发送登记的 up 半段（主键 up → 修饰 up 逆序）；未按过该键时为 no-op。
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

  // 切页复位：补发一切未抬起的 up（防电脑端残留按键），清空粘滞态与在按登记。
  // 粘滞态是纯本机软件状态，复位只影响 UI 点亮，无需线上流量。
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

  // 进入键盘区（主控页 switchTo 调）：页面回调唤起输入法。
  public zoneEnter(): void {
    this.zoneActive = true;
    if (this.onZoneEnter !== null) {
      this.onZoneEnter();
    }
  }

  // 离开键盘区：补发未抬起键、清粘滞（防电脑端残留），页面回调收起输入法。
  public zoneLeave(): void {
    this.zoneActive = false;
    this.releaseAll();
    if (this.onZoneLeave !== null) {
      this.onZoneLeave();
    }
  }

  // ---- 系统输入法提交流 ----

  // 提交增量入口（页面 onChange 且 previewText 为空时调；组合态由页面过滤）。
  public commitText(oldText: string, newTextValue: string): void {
    const d = CommitPlan.diff(oldText, newTextValue);
    for (let i = 0; i < d.deletes; i++) {
      this.tapKey(0x2A); // HidUsage.BACKSPACE：IME 删除镜像为 PC 退格（粘滞 Ctrl 叠加=删词）
    }
    if (d.inserts.length === 0) {
      return;
    }
    const chars: string[] = Array.from(d.inserts);
    const single: boolean = chars.length === 1;
    const comboShot: boolean = single && this.anyLatched()
      && CommitPlan.charToHid(chars[0]) !== null;
    if (!comboShot && this.anyLatched()) {
      this.clearLatches(); // 多字符提交是短语，组合键语义不适用，直接清粘滞
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

  // 单键完整点击（down+up 立即成对）：提交流的逐字符与退格镜像用。
  private tapKey(usage: number, withShift: boolean = false): void {
    this.beginKey(usage, withShift);
    this.endKey(usage);
  }

  // TEXT run 分块发送（协议 §4.5 payload 上限 1400B；按码点切不劈 UTF-8 序列）。
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

  // sendReliable 未认证时内部静默丢弃、失败内部计数（断连期间按键不崩 UI），Promise 已自捕获。
  private send(hid: number, down: boolean): void {
    if (this.transport !== null) {
      this.transport.sendReliable(newKey(this.transport.nextSeq(), hid, down));
    }
  }
}

// 单字符 UTF-8 字节数（码点区间判定，免引入编码器）。
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
