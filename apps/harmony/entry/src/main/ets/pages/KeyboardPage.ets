// 自绘 QWERTY 键盘页（PRD §4.4 铁律）：不经系统输入法，KeyCap 直接发 HID usage
// （daemon 映射 Linux KEY_*；中文经 fcitx 在电脑屏出候选，APP 无感知）。
// 布局（竖屏，自底向上）：数字行(Esc 1-0 ⌫) / Tab+QWERTYUIOP+- / Caps+ASDFGHJKL+;' /
// Shift+ZXCVBNM+/+Enter / 底行（Ctrl Alt Super 符号层 | 空格 , .）+ 右下方向键独立小簇。
// 键宽 layoutWeight 按比例分配（百分比语义），行/键间距 gap，适配不同屏宽。
// 符号层：数字行变 !@#… 与常用标点的 Shift 变体（: " < > ? _），层键点亮指示。
// 无障碍（docs/HARMONY-DOCIDS.md 五，现查 ts-universal-attributes-accessibility）：
// 每键 accessibilityText（朗读名）+ accessibilityLevel('yes')；粘滞修饰/层键加
// accessibilityChecked 播报点亮态、accessibilityDescription 说明操作后果。
// 按压态 KeyCap onTouch 自管（DOWN 置位 / UP、CANCEL 复位，只认自己的 pointerId）。
import { HidUsage } from '../common/hid/KeyCodes';
import type { ShiftSym } from '../common/hid/KeyCodes';
import { letterUsage, symVariantOf } from '../common/hid/KeyCodes';
import { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { StickyMod } from '../common/keyboard/StickyModifiers';
import { pageResStr } from './Res';

const KEY_GAP_VP: number = 4; // 键间距/行间距
const KEY_HEIGHT_VP: number = 42; // 键高（触摸目标）
const SIDE_PADDING_VP: number = 6; // 键区左右边距
const KEY_RADIUS_VP: number = 6; // 键帽圆角

// 一颗可发键的描述（由页面按当前层生成；修饰/层键不走此结构，各自显式构造）。
interface KeySpec {
  label: string; // 显示字符（当前层）
  a11y: string; // 无障碍朗读文案
  hid: number; // HID usage
  withShift: boolean; // 符号层键：发送时组合临时 Shift
  weight: number; // 行内宽度权重（layoutWeight 百分比分摊）
}

// 无障碍文案键全集（aboutToAppear 一次性解析，build 内查表）。
const A11Y_KEYS: string[] = [
  'kb_key_esc', 'kb_key_tab', 'kb_key_caps', 'kb_key_shift', 'kb_key_ctrl', 'kb_key_alt',
  'kb_key_super', 'kb_key_enter', 'kb_key_backspace', 'kb_key_space',
  'kb_key_up', 'kb_key_down', 'kb_key_left', 'kb_key_right',
  'kb_key_minus', 'kb_key_semicolon', 'kb_key_quote', 'kb_key_comma', 'kb_key_period', 'kb_key_slash',
  'kb_sym_exclamation', 'kb_sym_at', 'kb_sym_hash', 'kb_sym_dollar', 'kb_sym_percent', 'kb_sym_caret',
  'kb_sym_ampersand', 'kb_sym_asterisk', 'kb_sym_lparen', 'kb_sym_rparen', 'kb_sym_underscore',
  'kb_sym_colon', 'kb_sym_dquote', 'kb_sym_less', 'kb_sym_greater', 'kb_sym_question',
  'kb_layer_to_symbols', 'kb_layer_to_letters', 'kb_a11y_modifier_hint', 'kb_a11y_layer_hint',
];

// 单键帽：按压高亮自管；active（粘滞修饰/符号层点亮）用系统 token 强调色 + 反白文字。
@ComponentV2
struct KeyCap {
  @Param label: string = '';
  @Param a11y: string = '';
  @Param hint: string = '';
  @Param active: boolean = false; // 点亮（粘滞修饰 / 符号层激活）
  @Param checkable: boolean = false; // 是否向读屏播报选中态（粘滞/层键 true）
  @Param weight: number = 1;
  // 回调走 @Param（V2 规则：父组件只能初始化 @Param 成员，普通成员禁止外部初始化）。
  @Param onPress: () => void = () => {};
  @Param onRelease: () => void = () => {};
  @Local private pressed: boolean = false;
  @Local private pointerId: number = -1; // 只认自己按下的指针

  private lit(): boolean {
    return this.active || this.pressed;
  }

  // DOWN 置位按压并发 down；UP/CANCEL 复位并发 up（CANCEL 也必须补 up，电脑端按键不可残留）。
  private handleTouch(ev: TouchEvent): void {
    if (ev.type === TouchType.Down) {
      if (this.pointerId === -1 && ev.changedTouches.length > 0) {
        this.pointerId = ev.changedTouches[0].id;
        this.pressed = true;
        this.onPress();
      }
      return;
    }
    if (ev.type === TouchType.Up || ev.type === TouchType.Cancel) {
      let matched: boolean = false;
      for (let i = 0; i < ev.changedTouches.length; i++) {
        if (ev.changedTouches[i].id === this.pointerId) {
          matched = true;
        }
      }
      if (matched) {
        this.pointerId = -1;
        this.pressed = false;
        this.onRelease();
      }
    }
  }

  build() {
    Text(this.label)
      .fontSize($r('sys.float.ohos_id_text_size_body1'))
      .fontColor(this.lit() ? $r('sys.color.font_on_primary') : $r('sys.color.font_primary'))
      .textAlign(TextAlign.Center)
      .maxLines(1)
      .width('100%')
      .height(KEY_HEIGHT_VP)
      .borderRadius(KEY_RADIUS_VP)
      .backgroundColor(this.lit()
        ? $r('sys.color.comp_background_emphasize') : $r('sys.color.comp_background_tertiary'))
      .layoutWeight(this.weight)
      .onTouch((ev: TouchEvent) => {
        this.handleTouch(ev);
      })
      .accessibilityText(this.a11y.length > 0 ? this.a11y : this.label)
      .accessibilityLevel('yes')
      .accessibilityDescription(this.hint)
      .accessibilityChecked(this.checkable ? this.active : undefined)
  }
}

@ComponentV2
export struct KeyboardPage {
  @Require @Param model: KeyboardModel;
  @Local symLayer: boolean = false; // 符号层开关（层状态：键帽标签与激活指示联动）
  private a11yTexts: Map<string, string> = new Map();

  aboutToAppear(): void {
    for (const k of A11Y_KEYS) {
      this.a11yTexts.set(k, pageResStr(this.getUIContext(), k));
    }
  }

  aboutToDisappear(): void {
    // 切页复位：补发一切未抬起的 up（防电脑端残留按键）并清空粘滞点亮。
    this.model.releaseAll();
  }

  private str(key: string): string {
    const v: string | undefined = this.a11yTexts.get(key);
    return v !== undefined ? v : key;
  }

  // ---- 行描述生成（每次渲染按当前层重算） ----

  private charSpec(ch: string): KeySpec {
    const spec: KeySpec = { label: ch, a11y: ch, hid: letterUsage(ch), withShift: false, weight: 1 };
    return spec;
  }

  private letterSpecs(chars: string): KeySpec[] {
    const out: KeySpec[] = [];
    for (let i = 0; i < chars.length; i++) {
      out.push(this.charSpec(chars.charAt(i)));
    }
    return out;
  }

  // 主层符号 / 符号层 Shift 变体两用（数字行与标点键共用；字母无变体走 charSpec）。
  private symAwareSpec(base: string, weight: number): KeySpec {
    const v: ShiftSym | null = symVariantOf(base);
    if (v !== null && this.symLayer) {
      const spec: KeySpec = {
        label: v.shifted, a11y: this.str(v.a11yKey), hid: v.usage, withShift: true, weight: weight
      };
      return spec;
    }
    const a11y: string = v !== null && v.baseA11yKey.length > 0 ? this.str(v.baseA11yKey) : base;
    const hid: number = v !== null ? v.usage : 0;
    const spec: KeySpec = { label: base, a11y: a11y, hid: hid, withShift: false, weight: weight };
    return spec;
  }

  private digitSpecs(): KeySpec[] {
    const out: KeySpec[] = [];
    const digits: string = '1234567890';
    for (let i = 0; i < digits.length; i++) {
      out.push(this.symAwareSpec(digits.charAt(i), 1));
    }
    return out;
  }

  private punctSpecs(): KeySpec[] {
    const out: KeySpec[] = [];
    out.push(this.symAwareSpec(',', 1));
    out.push(this.symAwareSpec('.', 1));
    return out;
  }

  // ---- 键帽构造 Builder ----

  // 普通按键：DOWN 发 down、UP/CANCEL 发 up（长按不连发——key repeat 由 daemon 负责）。
  @Builder
  private tapKey(label: string, a11y: string, hid: number, weight: number) {
    KeyCap({
      label: label, a11y: a11y, weight: weight,
      onPress: () => {
        this.model.beginKey(hid, false);
      },
      onRelease: () => {
        this.model.endKey(hid);
      }
    })
  }

  @Builder
  private specKey(spec: KeySpec) {
    KeyCap({
      label: spec.label, a11y: spec.a11y, weight: spec.weight,
      onPress: () => {
        this.model.beginKey(spec.hid, spec.withShift);
      },
      onRelease: () => {
        this.model.endKey(spec.hid);
      }
    })
  }

  // 粘滞修饰键：DOWN 即切换点亮（即时反馈），不产生线上流量；点亮态 @Trace 驱动。
  @Builder
  private modKey(label: string, a11yKey: string, mod: number, latched: boolean, weight: number) {
    KeyCap({
      label: label, a11y: this.str(a11yKey), hint: this.str('kb_a11y_modifier_hint'),
      active: latched, checkable: true, weight: weight,
      onPress: () => {
        this.model.toggleModifier(mod);
      }
    })
  }

  // 符号层切换键：激活指示 = 点亮底色 + 标签切 'ABC'。
  @Builder
  private layerKey() {
    KeyCap({
      label: this.symLayer ? 'ABC' : '#+=',
      a11y: this.symLayer ? this.str('kb_layer_to_letters') : this.str('kb_layer_to_symbols'),
      hint: this.str('kb_a11y_layer_hint'),
      active: this.symLayer, checkable: true, weight: 1.3,
      onPress: () => {
        this.symLayer = !this.symLayer;
      }
    })
  }

  build() {
    Column({ space: KEY_GAP_VP }) {
      // 数字行：Esc | 1-0（符号层 !..)） | ⌫
      Row({ space: KEY_GAP_VP }) {
        this.tapKey('Esc', this.str('kb_key_esc'), HidUsage.ESC, 1.3)
        ForEach(this.digitSpecs(), (spec: KeySpec) => {
          this.specKey(spec)
        }, (spec: KeySpec) => `${spec.label}:${spec.hid}:${spec.withShift}`)
        this.tapKey('⌫', this.str('kb_key_backspace'), HidUsage.BACKSPACE, 1.3)
      }
      .width('100%')

      // 字母行 1：Tab | QWERTYUIOP | -
      Row({ space: KEY_GAP_VP }) {
        this.tapKey('Tab', this.str('kb_key_tab'), HidUsage.TAB, 1.4)
        ForEach(this.letterSpecs('qwertyuiop'), (spec: KeySpec) => {
          this.specKey(spec)
        }, (spec: KeySpec) => `${spec.label}:${spec.hid}`)
        this.specKey(this.symAwareSpec('-', 1))
      }
      .width('100%')

      // 字母行 2：Caps | ASDFGHJKL | ; '
      Row({ space: KEY_GAP_VP }) {
        this.tapKey('Caps', this.str('kb_key_caps'), HidUsage.CAPS_LOCK, 1.4)
        ForEach(this.letterSpecs('asdfghjkl'), (spec: KeySpec) => {
          this.specKey(spec)
        }, (spec: KeySpec) => `${spec.label}:${spec.hid}`)
        this.specKey(this.symAwareSpec(';', 1))
        this.specKey(this.symAwareSpec('\'', 1))
      }
      .width('100%')

      // 字母行 3：Shift(粘滞) | ZXCVBNM | / | Enter
      Row({ space: KEY_GAP_VP }) {
        this.modKey('Shift', 'kb_key_shift', StickyMod.SHIFT, this.model.shiftLatched, 1.5)
        ForEach(this.letterSpecs('zxcvbnm'), (spec: KeySpec) => {
          this.specKey(spec)
        }, (spec: KeySpec) => `${spec.label}:${spec.hid}`)
        this.specKey(this.symAwareSpec('/', 1))
        this.tapKey('Enter', this.str('kb_key_enter'), HidUsage.ENTER, 1.5)
      }
      .width('100%')

      // 底行：左（Ctrl Alt Super 符号层 / 空格 , .）+ 右（方向键独立小簇）
      Row({ space: KEY_GAP_VP }) {
        Column({ space: KEY_GAP_VP }) {
          Row({ space: KEY_GAP_VP }) {
            this.modKey('Ctrl', 'kb_key_ctrl', StickyMod.CTRL, this.model.ctrlLatched, 1)
            this.modKey('Alt', 'kb_key_alt', StickyMod.ALT, this.model.altLatched, 1)
            this.modKey('Super', 'kb_key_super', StickyMod.SUPER, this.model.superLatched, 1)
            this.layerKey()
          }
          .width('100%')

          Row({ space: KEY_GAP_VP }) {
            this.tapKey('', this.str('kb_key_space'), HidUsage.SPACE, 2.3)
            ForEach(this.punctSpecs(), (spec: KeySpec) => {
              this.specKey(spec)
            }, (spec: KeySpec) => `${spec.label}:${spec.hid}:${spec.withShift}`)
          }
          .width('100%')
        }
        .layoutWeight(3.2)

        Column({ space: KEY_GAP_VP }) {
          this.tapKey('↑', this.str('kb_key_up'), HidUsage.UP, 1)
          Row({ space: KEY_GAP_VP }) {
            this.tapKey('←', this.str('kb_key_left'), HidUsage.LEFT, 1)
            this.tapKey('↓', this.str('kb_key_down'), HidUsage.DOWN, 1)
            this.tapKey('→', this.str('kb_key_right'), HidUsage.RIGHT, 1)
          }
          .width('100%')
        }
        .layoutWeight(1.1)
      }
      .width('100%')
    }
    .width('100%')
    .height('100%')
    .padding({ left: SIDE_PADDING_VP, right: SIDE_PADDING_VP, top: 8 })
    .justifyContent(FlexAlign.End) // 键盘贴底（类输入法布局），上方留空
  }
}
