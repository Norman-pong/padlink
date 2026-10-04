// 粘滞修饰键状态机（PRD §4.4 铁律）：
// - 点按修饰键（Shift/Ctrl/Alt/Super）→ 点亮保持，可多修饰叠加（如 Ctrl+Shift）；再点按同一键 → 取消；
// - 任一非修饰键触发（takeSequence）→ 组合序列一次生成并全部自动复位：
//   修饰 down（固定序 Shift→Ctrl→Alt→Super）→ 主键 down → 主键 up → 修饰 up（逆序）；
// - 纯修饰点按不产生任何按键序列（toggle 只翻状态）。
// 取舍（v1 从简）：不实现「双击 Shift 锁定全大写」模式；如需扩展可在 toggle 处加时间窗判定，
// 锁定态需独立于粘滞态（不被 takeSequence 复位）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum、不持有时间源（无定时器）。
// 注意：单测 harness 按目录整树加载（tools/etstest/run.mjs keyboard），目录须自包含，
// 故修饰键 usage 在此定义（与 common/hid/KeyCodes.ets LEFT_* 及 daemon hid_map.go 严格一致）。

// 修饰键标识（取值即固定发送序下标：Shift → Ctrl → Alt → Super）。
export class StickyMod {
  public static readonly SHIFT: number = 0;
  public static readonly CTRL: number = 1;
  public static readonly ALT: number = 2;
  public static readonly SUPER: number = 3;
}

// 单条按键步骤：HID usage + down/up（对应 newKey 的 hidUsage/down 字段）。
export interface KeyStep {
  hid: number;
  down: boolean;
}

// 各修饰键的 HID usage（左位变体）；数组下标与 StickyMod 对应，即固定发送序。
const MOD_SHIFT: number = 0xE1; // HidUsage.LEFT_SHIFT
const MOD_CTRL: number = 0xE0; // HidUsage.LEFT_CTRL
const MOD_ALT: number = 0xE2; // HidUsage.LEFT_ALT
const MOD_SUPER: number = 0xE3; // HidUsage.LEFT_SUPER
const MOD_USAGES: number[] = [MOD_SHIFT, MOD_CTRL, MOD_ALT, MOD_SUPER];

function step(hid: number, down: boolean): KeyStep {
  const s: KeyStep = { hid: hid, down: down };
  return s;
}

export class StickyModifiers {
  private latched: boolean[] = [false, false, false, false];

  public isLatched(mod: number): boolean {
    return this.latched[mod];
  }

  public anyLatched(): boolean {
    for (const v of this.latched) {
      if (v) {
        return true;
      }
    }
    return false;
  }

  // 点按修饰键：点亮 / 再点取消。返回新状态（供 UI 同步）。只翻本键，不动其他修饰键（叠加靠多次点按）。
  public toggle(mod: number): boolean {
    this.latched[mod] = !this.latched[mod];
    return this.latched[mod];
  }

  // 全部复位（切页等场景显式调用；纯本机状态，无线上语义）。
  public clear(): void {
    this.latched = [false, false, false, false];
  }

  // 当前点亮修饰键的 usage 列表，按固定发送序（Shift→Ctrl→Alt→Super）。
  public latchedUsages(): number[] {
    const out: number[] = [];
    for (let i = 0; i < MOD_USAGES.length; i++) {
      if (this.latched[i]) {
        out.push(MOD_USAGES[i]);
      }
    }
    return out;
  }

  // 非修饰键触发：生成完整组合序列，并自动复位全部粘滞态（PRD：任一非修饰键触发后修饰键全部复位）。
  // 序列：修饰 down（固定序）→ 主键 down → 主键 up → 修饰 up（逆序）。
  // extraShift=true 追加一个临时 Shift（符号层键用）：仅本次生效、不占用粘滞态；
  // Shift 已点亮时不重复加入。
  public takeSequence(hid: number, extraShift: boolean = false): KeyStep[] {
    const mods: number[] = [];
    if (extraShift && !this.latched[StickyMod.SHIFT]) {
      mods.push(StickyMod.SHIFT);
    }
    for (let i = 0; i < this.latched.length; i++) {
      if (this.latched[i]) {
        mods.push(i);
      }
    }
    const seq: KeyStep[] = [];
    for (const m of mods) {
      seq.push(step(MOD_USAGES[m], true));
    }
    seq.push(step(hid, true));
    seq.push(step(hid, false));
    for (let i = mods.length - 1; i >= 0; i--) {
      seq.push(step(MOD_USAGES[mods[i]], false));
    }
    this.clear();
    return seq;
  }
}
