// 语音浮件：按住说话为唯一起手式（DOWN 起会话 / UP·CANCEL 收会话；快速点按由 finishWhenReady 兜底）。
// 只认自己按下的 pointerId（防多指串扰）；按钮上方浮出蹦字卡（中间结果/失败原因+重发/去设置）。
// 布局约束：浮键行宽度恒定——蹦字卡是流式兄弟节点，出现只把停靠簇顶高，不得挤动停靠位。
// 按压动效走 animateTo + springMotion 双事件驱动可中断；stateStyles 无过程驱动，禁用。
import { curves } from '@kit.ArkUI';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import { VoiceModel, VoicePhase } from '../common/voice/VoiceModel';
import { pageResStr } from './Res';

const MIC_SIZE_VP: number = 56; // 按住说话的拇指目标，大于页头浮键
const MIC_PRESS_SCALE: number = 0.96;
const CARD_MAX_WIDTH_VP: number = 280;

// 文案键全集：aboutToAppear 一次性解析（$r 需字面量，动态键不能拼名）。
const TEXT_KEYS: string[] = [
  'voice_btn_label', 'voice_btn_a11y', 'voice_hold_hint',
  'voice_a11y_hold_to_speak', 'voice_a11y_release_to_end',
  'voice_need_connect', 'voice_phase_initializing', 'voice_phase_listening',
  'voice_phase_recognizing', 'voice_phase_sending', 'voice_phase_done', 'voice_empty_hint',
  'voice_err_mic_permission', 'voice_open_settings', 'voice_err_busy',
  'voice_err_create_failed', 'voice_err_start_failed', 'voice_err_max_audio',
  'voice_err_engine', 'voice_err_recognize', 'voice_err_unknown', 'voice_err_send_failed',
  'voice_resend',
];

@ComponentV2
export struct VoiceDock {
  @Require @Param voice: VoiceModel;
  @Require @Param conn: ConnectionManager;
  @Param inputEpoch: number = 0;
  @Param compact: boolean = false;
  @BuilderParam trailing: () => void = this.emptyTrailing;
  @Local private pressed: boolean = false;
  @Local private pressScale: number = 1;
  private pointerId: number = -1; // 只认自己按下的指针
  private texts: Map<string, string> = new Map();

  @Monitor('inputEpoch')
  resetPress(): void {
    this.pointerId = -1;
    this.pressed = false;
    this.pressScale = 1;
  }

  aboutToAppear(): void {
    for (const k of TEXT_KEYS) {
      this.texts.set(k, pageResStr(this.getUIContext(), k));
    }
  }

  // 会话收口不在此组件：宿主页 aboutToDisappear 统一 cancelSession（麦克风仅按住期间占用）。

  private str(key: string): string {
    const v: string | undefined = this.texts.get(key);
    return v !== undefined ? v : key;
  }

  private connected(): boolean {
    return this.conn.snapshot.state === ConnState.CONNECTED;
  }

  private statusKey(): string {
    if (this.voice.phase === VoicePhase.ERROR) {
      return this.voice.errorKey.length > 0 ? this.voice.errorKey : 'voice_err_unknown';
    }
    switch (this.voice.phase) {
      case VoicePhase.INITIALIZING:
        return 'voice_phase_initializing';
      case VoicePhase.LISTENING:
        return 'voice_phase_listening';
      case VoicePhase.RECOGNIZING:
        return 'voice_phase_recognizing';
      case VoicePhase.SENDING:
        return 'voice_phase_sending';
      case VoicePhase.DONE:
        return 'voice_phase_done';
      default:
        return 'voice_hold_hint';
    }
  }

  private isErrorPhase(): boolean {
    return this.voice.phase === VoicePhase.ERROR;
  }

  private holding(): boolean {
    return this.voice.phase === VoicePhase.INITIALIZING
      || this.voice.phase === VoicePhase.LISTENING
      || this.voice.phase === VoicePhase.RECOGNIZING
      || this.pressed;
  }

  // SENDING 不算会话活跃：此时 release 已发生。
  private sessionActive(): boolean {
    return this.voice.phase === VoicePhase.INITIALIZING
      || this.voice.phase === VoicePhase.LISTENING
      || this.voice.phase === VoicePhase.RECOGNIZING;
  }

  private cardVisible(): boolean {
    return this.sessionActive()
      || this.voice.phase === VoicePhase.SENDING
      || this.voice.phase === VoicePhase.ERROR
      || this.voice.sendFailed
      || (this.voice.phase === VoicePhase.DONE && this.voice.finalText.length === 0);
  }

  // DOWN 登记指针并起会话；UP/CANCEL 必须匹配自己的指针才收会话。
  private handleTouch(ev: TouchEvent): void {
    if (ev.type === TouchType.Down) {
      if (this.pointerId === -1 && ev.changedTouches.length > 0) {
        this.pointerId = ev.changedTouches[0].id;
        this.pressed = true;
        this.getUIContext().animateTo({
          duration: 120,
          curve: Curve.EaseOut
        }, () => {
          this.pressScale = MIC_PRESS_SCALE;
        });
        this.voice.press();
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
        this.getUIContext().animateTo({
          curve: curves.springMotion(0.42, 0.72)
        }, () => {
          this.pressScale = 1;
        });
        this.voice.release();
      }
    }
  }

  @Builder
  private emptyTrailing(): void {
  }

  build() {
    Column({ space: 10 }) {
      if (this.cardVisible()) {
        Scroll() {
          Column({ space: 8 }) {
            if (this.voice.interimText.length > 0) {
              Text(this.voice.interimText)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
                .width('100%')
            }
            if (this.voice.finalText.length > 0) {
              Text(this.voice.finalText)
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
                .width('100%')
            }
            if (this.voice.phase === VoicePhase.DONE && this.voice.finalText.length === 0) {
              Text(this.str('voice_empty_hint'))
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_tertiary'))
            }
            Text(this.str(this.statusKey()))
              .fontSize($r('sys.float.ohos_id_text_size_body2'))
              .fontColor(this.isErrorPhase()
                ? $r('sys.color.warning') : $r('sys.color.font_secondary'))
              .width('100%')
              .maxLines(2)
            if (this.isErrorPhase() && this.voice.errorKey === 'voice_err_mic_permission') {
              Button(this.str('voice_open_settings'))
                .buttonStyle(ButtonStyleMode.NORMAL)
                .onClick(() => {
                  this.voice.openPermissionSettings();
                })
            }
            if (this.voice.sendFailed) {
              Button(this.str('voice_resend'))
                .buttonStyle(ButtonStyleMode.NORMAL)
                .onClick(() => {
                  this.voice.resend();
                })
            }
          }
          .width('100%')
          .alignItems(HorizontalAlign.Start)
        }
        .constraintSize({ maxWidth: '100%', maxHeight: this.compact ? 96 : undefined })
        .width(CARD_MAX_WIDTH_VP)
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_primary'))
        .border({ width: 0.5, color: $r('sys.color.comp_divider') })
      }

      Row({ space: 10 }) {
        this.trailing()

        Stack({ alignContent: Alignment.Center }) {
          SymbolGlyph(this.holding() ? $r('sys.symbol.mic_fill') : $r('sys.symbol.mic'))
            .fontSize(26)
            .fontColor([this.holding()
              ? $r('sys.color.font_on_primary') : $r('sys.color.icon_primary')])
        }
        .width(MIC_SIZE_VP)
        .height(MIC_SIZE_VP)
        .borderRadius(MIC_SIZE_VP / 2)
        .backgroundColor(this.holding()
          ? $r('sys.color.comp_background_emphasize') : $r('sys.color.comp_background_tertiary'))
        .opacity(this.connected() ? 1 : 0.4)
        .enabled(this.connected())
        .scale({ x: this.pressScale, y: this.pressScale })
        .onTouch((ev: TouchEvent) => {
          this.handleTouch(ev);
        })
        .accessibilityText(this.str('voice_btn_a11y'))
        .accessibilityLevel('yes')
        .accessibilityDescription(this.holding()
          ? this.str('voice_a11y_release_to_end') : this.str('voice_a11y_hold_to_speak'))
      }
      .alignItems(VerticalAlign.Bottom)
    }
    .alignItems(HorizontalAlign.End)
    .width('100%')
  }
}
