// 语音浮件（PRD §4.5 单页融合形态）：右下角浮动麦克风，按住说话为唯一起手式
//（DOWN 起会话 / UP·CANCEL 收会话；快速点按由 VoiceModel.finishWhenReady 兜底成短会话）。
// 只认自己按下的 pointerId（KeyCap 同款模式，防多指串扰）；会话活跃/出错/空结果/待重发时
// 按钮上方浮出蹦字卡（中间结果实时、失败原因 + 重发/去设置引导）。非连接态禁用降透明度。
// 布局约束：本组件恒满宽，浮键行（停靠位 + 麦克风）宽度恒定——蹦字卡是流式兄弟节点，
// 不得把停靠位（键盘召唤键）挤动；卡片出现只把停靠簇向上顶高。
// 按压动效（motion.md §一 圆钮跟手缩放）：Down 走 animateTo 缩至 0.96，Up/Cancel 走
// curves.springMotion 弹簧回弹——双事件驱动可中断（stateStyles 无过程驱动，禁用）。
import { curves } from '@kit.ArkUI';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import { VoiceModel, VoicePhase } from '../common/voice/VoiceModel';
import { pageResStr } from './Res';

const MIC_SIZE_VP: number = 56; // 浮动麦克风直径（按住说话拇指目标，大于页头浮键 40）
const MIC_PRESS_SCALE: number = 0.96; // 按下跟手缩放档（motion.md §一 参考值）
const CARD_MAX_WIDTH_VP: number = 280; // 蹦字卡宽上限（右下角贴边，不横贯屏幕）

// 页面文案键全集（aboutToAppear 一次性解析为本地化字符串，build 内查表）。
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
  // 停靠位（键盘召唤键，宿主经尾闭包注入）：与麦克风同行居右，宽度不受蹦字卡影响。
  @BuilderParam trailing: () => void = this.emptyTrailing;
  @Local private pressed: boolean = false;
  @Local private pressScale: number = 1; // 按住键跟手缩放（层3 物理动效：按下形变/弹簧回弹）
  private pointerId: number = -1; // 只认自己按下的指针
  private texts: Map<string, string> = new Map();

  aboutToAppear(): void {
    for (const k of TEXT_KEYS) {
      this.texts.set(k, pageResStr(this.getUIContext(), k));
    }
  }

  // 会话收口不在此组件：宿主页面 aboutToDisappear 统一 cancelSession（PRD §12）。

  private str(key: string): string {
    const v: string | undefined = this.texts.get(key);
    return v !== undefined ? v : key;
  }

  private connected(): boolean {
    return this.conn.snapshot.state === ConnState.CONNECTED;
  }

  // 状态行文案键：错误态显示错误键，会话态显示阶段键，空闲显示持机提示。
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

  // 按住期间（含初始化与等待最终结果）按钮保持点亮。
  private holding(): boolean {
    return this.voice.phase === VoicePhase.INITIALIZING
      || this.voice.phase === VoicePhase.LISTENING
      || this.voice.phase === VoicePhase.RECOGNIZING
      || this.pressed;
  }

  // 会话活跃判定（SENDING 不算：此时 release 已发生）。
  private sessionActive(): boolean {
    return this.voice.phase === VoicePhase.INITIALIZING
      || this.voice.phase === VoicePhase.LISTENING
      || this.voice.phase === VoicePhase.RECOGNIZING;
  }

  // 蹦字卡可见性：会话进行中/发送中/出错/待重发/空结果收口。
  // DONE 且有结果 = 文本已上屏，卡片收起不占道（重发场景由 sendFailed 覆盖）。
  private cardVisible(): boolean {
    return this.sessionActive()
      || this.voice.phase === VoicePhase.SENDING
      || this.voice.phase === VoicePhase.ERROR
      || this.voice.sendFailed
      || (this.voice.phase === VoicePhase.DONE && this.voice.finalText.length === 0);
  }

  // 按住说话：DOWN 登记指针、起跟手缩放、press 起会话（model 对重复按下有相位门）；
  // UP/CANCEL（匹配自己的指针）回弹缩放并 release 收会话。
  // 缩放动效（motion.md §一）：animateTo 弹簧可中断（按住即停、抬手即弹，无「播完才响应」）。
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
      // 蹦字卡：中间结果（次级色，实时刷新）/ 最终结果 / 空结果提示 + 状态行 + 错误动作。
      if (this.cardVisible()) {
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
          // 状态行（识别中 / 已上屏 / 失败原因），错误态用警示色。
          Text(this.str(this.statusKey()))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor(this.isErrorPhase()
              ? $r('sys.color.warning') : $r('sys.color.font_secondary'))
            .width('100%')
            .maxLines(2)
          // 错误动作行：权限拒绝 → 去设置开启；发送失败 → 重发（finalText 已保留）。
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
        .width(CARD_MAX_WIDTH_VP)
        .alignItems(HorizontalAlign.Start)
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_primary'))
        .border({ width: 0.5, color: $r('sys.color.comp_divider') })
      }

      // 浮键行：停靠位（键盘召唤键）+ 浮动麦克风。行宽恒定，不受蹦字卡影响。
      Row({ space: 10 }) {
        this.trailing()

        // 浮动麦克风：非连接态禁用（enabled 关断触摸）并降透明度。
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
        // 动态操作提示：空闲播「按住说话」，按住中播「松开结束」。
        .accessibilityDescription(this.holding()
          ? this.str('voice_a11y_release_to_end') : this.str('voice_a11y_hold_to_speak'))
      }
      .alignItems(VerticalAlign.Bottom)
    }
    .alignItems(HorizontalAlign.End)
    .width('100%')
  }
}
