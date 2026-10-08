// 键盘区（PRD §4.4 混合形态）：系统输入法承接文本 + 自绘键条补键语义（TermNext 式取舍）。
// - 输入区：TextArea 常驻底部；IME 组合态（onChange previewText 非空，拼音预上屏）不上行，
//   提交增量经 model.commitText（ASCII 逐键 HID 保留按键语义、CJK/emoji 合并 TEXT 走剪贴板
//   注入、删除镜像 Backspace、单字符提交 × 粘滞修饰 = 组合键）；
// - 键条（贴底）：Esc Tab Ctrl Alt Shift Super（粘滞点亮）⌫ Enter ←↑↓→，KeyCap 两段式直发
//   HID（长按连发由 daemon 负责）；IME 顶起时随 RESIZE 避让整体上移；
// - 区域生命周期：主控页 switchTo 经 model.zoneEnter/zoneLeave 驱动——进入唤起输入法 + 切
//   RESIZE 避让，离开收起输入法 + 补发未抬键（PRD §11.2）；首次进区由 aboutToAppear 按
//   model.zoneActive 补偿（TabContent 懒构建晚于首次 zoneEnter 调用）。
// 无障碍：键条每键 accessibilityText（朗读名）+ accessibilityLevel('yes')；粘滞修饰加
// accessibilityChecked 播报点亮态、accessibilityDescription 说明操作后果。
import { HidUsage } from '../common/hid/KeyCodes';
import { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { StickyMod } from '../common/keyboard/StickyModifiers';
import { inputMethod } from '@kit.IMEKit';
import { pageResStr } from './Res';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { enterKeyboardAvoidResize, restoreKeyboardAvoidMode } from './KeyboardAvoid';

const KEY_GAP_VP: number = 4; // 键间距
const KEY_HEIGHT_VP: number = 42; // 键高（触摸目标）
const SIDE_PADDING_VP: number = 6; // 键区左右边距
const KEY_RADIUS_VP: number = 6; // 键帽圆角
const INPUT_MAX_HEIGHT_VP: number = 132; // 输入区高上限（约 3 行；超出内部滚动）

// 键条朗读文案键全集（aboutToAppear 一次性解析，build 内查表）。
const A11Y_KEYS: string[] = [
  'kb_key_esc', 'kb_key_tab', 'kb_key_shift', 'kb_key_ctrl', 'kb_key_alt',
  'kb_key_super', 'kb_key_enter', 'kb_key_backspace',
  'kb_key_up', 'kb_key_down', 'kb_key_left', 'kb_key_right', 'kb_a11y_modifier_hint',
];

// 单键帽：按压高亮自管；active（粘滞修饰点亮）用系统 token 强调色 + 反白文字。
// 动效取舍（motion.md §一 归层律）：按键物理隐喻 = 按压底色反馈（lit 底色），不加跟手缩放——
// 缩放是圆钮/卡片类浮层的形变隐喻，键帽按压保持几何稳定更符合物理键盘直觉（D9 定案）。
@ComponentV2
struct KeyCap {
  @Param label: string = '';
  @Param a11y: string = '';
  @Param hint: string = '';
  @Param active: boolean = false; // 点亮（粘滞修饰）
  @Param checkable: boolean = false; // 是否向读屏播报选中态（粘滞键 true）
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
  @Local inputText: string = ''; // 输入框显示值（组合态预上屏含在内）
  private lastStable: string = ''; // 上次已提交值（增量 diff 基准；组合态不改写）
  private textCtrl: TextAreaController = new TextAreaController();
  private a11yTexts: Map<string, string> = new Map();
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET; // 进区前避让模式（离区恢复）

  aboutToAppear(): void {
    for (const k of A11Y_KEYS) {
      this.a11yTexts.set(k, pageResStr(this.getUIContext(), k));
    }
    this.model.onZoneEnter = (): void => {
      this.doZoneEnter();
    };
    this.model.onZoneLeave = (): void => {
      this.doZoneLeave();
    };
    if (this.model.zoneActive) {
      this.doZoneEnter(); // 首次进区补偿：懒构建晚于 switchTo 的 zoneEnter 调用
    }
  }

  aboutToDisappear(): void {
    // 切页复位：补发一切未抬起的 up（防电脑端残留按键）并清空粘滞点亮。
    this.model.releaseAll();
    this.model.onZoneEnter = null;
    this.model.onZoneLeave = null;
    this.restoreAvoid();
  }

  // 进区：RESIZE 避让（输入区随 IME 顶起）+ 聚焦唤起输入法。
  private doZoneEnter(): void {
    this.kbPrev = enterKeyboardAvoidResize(this.getUIContext());
    try {
      this.getUIContext().getFocusController().requestFocus('kb_input');
    } catch (_e) {
      // 聚焦失败不致命：点按输入框可再唤起
    }
    try {
      inputMethod.getController().showTextInput().catch((_err: BusinessError) => {
        // 唤起失败（无已聚焦编辑框等）不致命：点按输入框可再唤起
      });
    } catch (_e) {
    }
  }

  // 离区：收起输入法 + 失焦 + 恢复避让模式（补发未抬键由 model.zoneLeave 的 releaseAll 承担）。
  private doZoneLeave(): void {
    try {
      inputMethod.getController().hideTextInput().catch((_err: BusinessError) => {
        // 收起失败不致命：区域已失焦，下次进区会重新唤起
      });
    } catch (_e) {
    }
    this.getUIContext().getFocusController().clearFocus();
    this.restoreAvoid();
  }

  private restoreAvoid(): void {
    restoreKeyboardAvoidMode(this.getUIContext(), this.kbPrev);
  }

  private str(key: string): string {
    const v: string | undefined = this.a11yTexts.get(key);
    return v !== undefined ? v : key;
  }

  // 提交判定：组合态（拼音预上屏）只更新显示值不上行；组合清空即提交，按稳定值差分。
  private onInput(value: string, previewText?: PreviewText): void {
    this.inputText = value;
    if (previewText !== undefined && previewText.value.length > 0) {
      return; // 组合中：字母尚未选词，不进提交流
    }
    this.model.commitText(this.lastStable, value);
    this.lastStable = value;
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

  build() {
    Column({ space: KEY_GAP_VP }) {
      Blank() // 输入区贴底（类输入法布局），上方留白

      // 输入区：系统输入法的承接面；组合态只显不上行，提交增量镜像到电脑
      TextArea({ text: this.inputText, controller: this.textCtrl,
        placeholder: this.str('kb_input_placeholder') })
        .id('kb_input')
        .width('100%')
        .constraintSize({ maxHeight: INPUT_MAX_HEIGHT_VP })
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor($r('sys.color.font_primary'))
        .placeholderColor($r('sys.color.font_tertiary'))
        .backgroundColor($r('sys.color.comp_background_tertiary'))
        .borderRadius(KEY_RADIUS_VP)
        .padding({ left: 12, right: 12, top: 8, bottom: 8 })
        .showCounter(false)
        .onChange((value: string, previewText?: PreviewText) => {
          this.onInput(value, previewText);
        })

      // 键条：横向可滚（窄折叠态放下全排），长按直发 HID 两段式
      Scroll() {
        Row({ space: KEY_GAP_VP }) {
          this.tapKey('Esc', this.str('kb_key_esc'), HidUsage.ESC, 1)
          this.tapKey('Tab', this.str('kb_key_tab'), HidUsage.TAB, 1)
          this.modKey('Ctrl', 'kb_key_ctrl', StickyMod.CTRL, this.model.ctrlLatched, 1)
          this.modKey('Alt', 'kb_key_alt', StickyMod.ALT, this.model.altLatched, 1)
          this.modKey('Shift', 'kb_key_shift', StickyMod.SHIFT, this.model.shiftLatched, 1)
          this.modKey('Super', 'kb_key_super', StickyMod.SUPER, this.model.superLatched, 1)
          this.tapKey('⌫', this.str('kb_key_backspace'), HidUsage.BACKSPACE, 1)
          this.tapKey('Enter', this.str('kb_key_enter'), HidUsage.ENTER, 1)
          this.tapKey('←', this.str('kb_key_left'), HidUsage.LEFT, 1)
          this.tapKey('↑', this.str('kb_key_up'), HidUsage.UP, 1)
          this.tapKey('↓', this.str('kb_key_down'), HidUsage.DOWN, 1)
          this.tapKey('→', this.str('kb_key_right'), HidUsage.RIGHT, 1)
        }
        .height(KEY_HEIGHT_VP)
      }
      .scrollable(ScrollDirection.Horizontal)
      .scrollBar(BarState.Off)
      .width('100%')
    }
    .width('100%')
    .height('100%')
    .padding({ left: SIDE_PADDING_VP, right: SIDE_PADDING_VP, bottom: 8 })
    .justifyContent(FlexAlign.End)
  }
}
