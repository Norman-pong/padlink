// 键盘区：系统输入法承接文本（组合态不上行，仅提交增量镜像到电脑）+ 自绘键条补键语义。
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

// 单键帽：按压高亮自管，长按由 KeyStep 两段式直发。
@ComponentV2
struct KeyCap {
  @Param label: string = '';
  @Param a11y: string = '';
  @Param hint: string = '';
  @Param active: boolean = false;
  @Param checkable: boolean = false;
  @Param weight: number = 1;
  @Param onPress: () => void = () => {};
  @Param onRelease: () => void = () => {};
  @Local private pressed: boolean = false;
  @Local private pointerId: number = -1; // 只认自己按下的指针

  private lit(): boolean {
    return this.active || this.pressed;
  }

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
  @Local inputText: string = '';
  private lastStable: string = ''; // 增量 diff 基准，组合态期间不改写
  private textCtrl: TextAreaController = new TextAreaController();
  private a11yTexts: Map<string, string> = new Map();
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET;

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
      this.doZoneEnter();
    }
  }

  aboutToDisappear(): void {
    this.model.releaseAll();
    this.model.onZoneEnter = null;
    this.model.onZoneLeave = null;
    this.restoreAvoid();
  }

  private doZoneEnter(): void {
    this.kbPrev = enterKeyboardAvoidResize(this.getUIContext());
    try {
      this.getUIContext().getFocusController().requestFocus('kb_input');
    } catch (_e) {
    }
    try {
      inputMethod.getController().showTextInput().catch((_err: BusinessError) => {
      });
    } catch (_e) {
    }
  }

  private doZoneLeave(): void {
    try {
      inputMethod.getController().hideTextInput().catch((_err: BusinessError) => {
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

  // IME 组合态（拼音预上屏）不上行，组合清空即提交。
  private onInput(value: string, previewText?: PreviewText): void {
    this.inputText = value;
    if (previewText !== undefined && previewText.value.length > 0) {
      return;
    }
    this.model.commitText(this.lastStable, value);
    this.lastStable = value;
  }

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
      Blank()

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
