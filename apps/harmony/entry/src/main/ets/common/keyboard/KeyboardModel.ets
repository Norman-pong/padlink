// 键盘 ViewModel（PRD §4.4 铁律）：自绘按键 → StickyModifiers 组合序列 → Transport.sendReliable(newKey)。
// - 不经系统输入法：直接发 HID usage，daemon 映射 Linux KEY_*；中文经 fcitx 在电脑屏出候选，APP 无感知；
// - 长按不连发：APP 只发 down/up，key repeat 由 daemon 侧负责（协议约定）；
// - 两段式发送：DOWN 发「修饰 down（固定序）+ 主键 down」，UP/CANCEL 发「主键 up + 修饰 up（逆序）」——
//   CANCEL 也必须补 up，否则电脑端残留按住的键；按 usage 登记待抬序列，支持多键同时按住（两手拇指）；
// - 粘滞修饰态经 @ObservedV2/@Trace 快照暴露 UI（字段只由本类写入，@Trace 驱动点亮刷新）。
import { newKey } from '../proto/Packet';
import type { Transport } from '../net/Transport';
import { StickyModifiers, StickyMod } from './StickyModifiers';
import type { KeyStep } from './StickyModifiers';

@ObservedV2
export class KeyboardModel {
  // 粘滞修饰点亮快照（UI 只读，本类写入）。
  @Trace shiftLatched: boolean = false;
  @Trace ctrlLatched: boolean = false;
  @Trace altLatched: boolean = false;
  @Trace superLatched: boolean = false;

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

  // sendReliable 未认证时内部静默丢弃、失败内部计数（断连期间按键不崩 UI），Promise 已自捕获。
  private send(hid: number, down: boolean): void {
    if (this.transport !== null) {
      this.transport.sendReliable(newKey(this.transport.nextSeq(), hid, down));
    }
  }
}
