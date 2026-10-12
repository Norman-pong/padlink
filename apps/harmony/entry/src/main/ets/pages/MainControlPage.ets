// 主控页：触摸面（layoutWeight 占满）+ 可召唤的键盘停靠层 + 右下角语音浮件。
// 触摸面与停靠簇禁叠层：叠层命中会双派（Transparent 子树命中下穿到下层触摸面）或子件全灭
// （Block 阻下），故只留流式排布。
// 避让：setWindowLayoutFullScreen 后 getWindowAvoidArea/avoidAreaChange 返回 px（需 px2vp）；
// 底部手势条仅 TYPE_NAVIGATION_INDICATOR 可取，侧边手势区无 API 只能经验边距。
// 浮层：材质层在 background 之下，必须透明背景且 materialColor 带透明度，禁再叠 blur。
// 调试入口门控单源：仅 settings.debugEnabled 开时胶囊可进调试页。
import { window, uiMaterial } from '@kit.ArkUI';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import type { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { KeyboardPage } from './KeyboardPage';
import { VoiceDock } from './VoiceDock';
import type { VoiceModel } from '../common/voice/VoiceModel';
import { pageResStr, pageResFmt, connStateKey } from './Res';

// 本页边距单源，禁散落魔数。
const SIDE_MARGIN_VP: number = 12; // 侧边手势区无 API 可查，经验值
const CAPSULE_MARGIN_VP: number = 16; // 框架标题栏 16vp 基准，兼顶部间距
const CAPSULE_PAD_H_VP: number = 12;
const CAPSULE_PAD_V_VP: number = 6;
const CAPSULE_RADIUS_VP: number = 18;
// 浮键直径 40：兼满足 40vp 最小命中区（框架基准 38）。
const FLOAT_BTN_SIZE_VP: number = 40;
const FLOAT_BTN_ICON_FP: number = 22;
const DOCK_BTN_SIZE_VP: number = 56; // 按住说话的拇指目标，大于页头浮键
const DOCK_BTN_ICON_FP: number = 26;
const DOCK_GAP_VP: number = 10;
const DOCK_SIDE_MARGIN_VP: number = 8; // 与键盘层内 4vp 合计 12vp 到屏缘
const CTRL_BUSY_TOP_GAP_VP: number = 12;

@ComponentV2
export struct MainControlPage {
  @Require @Param conn: ConnectionManager;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param keyboard: KeyboardModel;
  @Require @Param voice: VoiceModel;
  @Require @Param settings: SettingsModel;
  @Require @Param pathStack: NavPathStack;

  @Local bottomAvoidVp: number = 0;
  @Local topAvoidVp: number = 0; // 压入状态栏会被系统窗口拦截点击
  @Local guideOpacity: number = 1;
  @Local kbVisible: boolean = false;
  private win: window.Window | null = null;
  private avoidCb: ((data: window.AvoidAreaOptions) => void) | null = null;
  private sizeCb: ((data: window.Size) => void) | null = null;
  // getLastWindow 异步返回晚于 aboutToDisappear 时（快速进出页），据此丢弃迟到的窗口操作。
  private disposed: boolean = false;
  aboutToAppear(): void {
    this.setupWindow();
  }

  aboutToDisappear(): void {
    // 离开页面必须收口：未释放按键先发 release、键盘层可能仍挂载需 zoneLeave、
    // 语音会话 cancel（麦克风仅按住期间占用）。
    this.disposed = true;
    this.touchpad.cancelGesture();
    if (this.keyboard.zoneActive) {
      this.keyboard.zoneLeave();
    }
    this.voice.cancelSession();
    this.restoreWindow();
  }

  // 必须先置 kbVisible 再 zoneEnter：KeyboardPage 挂载时按 zoneActive 补偿进区。
  private openKeyboard(): void {
    if (this.kbVisible) {
      return;
    }
    this.kbVisible = true;
    this.keyboard.zoneEnter();
  }

  // 先 zoneLeave 再卸载层（隐 IME + 释放残留键）。
  private closeKeyboard(): void {
    if (!this.kbVisible) {
      return;
    }
    this.keyboard.zoneLeave();
    this.kbVisible = false;
  }

  private setupWindow(): void {
    const ctx = this.getUIContext().getHostContext() as common.UIAbilityContext;
    window.getLastWindow(ctx).then((win: window.Window) => {
      if (this.disposed) {
        return; // 页面已销毁：不设全屏/常亮、不挂监听（restoreWindow 此时已跑过）
      }
      this.win = win;
      win.setWindowLayoutFullScreen(true).catch((err: BusinessError) => {
        hilogWarn('layoutFullScreen fail', err);
      });
      win.setWindowKeepScreenOn(true).catch((err: BusinessError) => {
        hilogWarn('keepScreenOn fail', err);
      });
      this.refreshAvoid();
      this.avoidCb = (data: window.AvoidAreaOptions) => {
        if (data.type === window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR) {
          this.refreshAvoid();
        }
      };
      win.on('avoidAreaChange', this.avoidCb);
      this.sizeCb = (data: window.Size) => {
        this.refreshAvoid();
      };
      win.on('windowSizeChange', this.sizeCb);
    }).catch((err: BusinessError) => {
      hilogWarn('getLastWindow fail', err);
    });
  }

  private restoreWindow(): void {
    if (this.win === null) {
      return;
    }
    const win = this.win;
    this.win = null;
    if (this.avoidCb !== null) {
      win.off('avoidAreaChange', this.avoidCb);
      this.avoidCb = null;
    }
    if (this.sizeCb !== null) {
      win.off('windowSizeChange', this.sizeCb);
      this.sizeCb = null;
    }
    win.setWindowKeepScreenOn(false).catch((err: BusinessError) => {
      hilogWarn('keepScreenOn off fail', err);
    });
    win.setWindowLayoutFullScreen(false).catch((err: BusinessError) => {
      hilogWarn('layoutFullScreen off fail', err);
    });
  }

  private refreshAvoid(): void {
    if (this.win === null) {
      return;
    }
    const area: window.AvoidArea =
      this.win.getWindowAvoidArea(window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR);
    this.bottomAvoidVp = this.getUIContext().px2vp(area.bottomRect.height); // px → vp
    const sys: window.AvoidArea =
      this.win.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM);
    this.topAvoidVp = this.getUIContext().px2vp(sys.topRect.height);
  }

  private resStr(name: string): string {
    return pageResStr(this.getUIContext(), name);
  }

  private rttText(): string {
    const rtt: number = this.conn.snapshot.rttMs;
    return rtt >= 0 ? `${rtt} ms` : '-- ms';
  }

  private ctrlBusy(): boolean {
    return this.conn.snapshot.ctrlBusyLeftSec > 0;
  }

  // accessibilityText 只收 string | Resource（无 ResourceStr 联合），故返回类型标 Resource。
  private capsuleA11y(): Resource {
    const state: string = this.resStr(connStateKey(this.conn.snapshot.state));
    return $r('app.string.a11y_capsule_format', state, this.rttText());
  }

  // LOST 恒可点（重新连接 → 发现页）；调试页仅 debugEnabled 放行（隐藏入口不开侧门）。
  private capsuleActionable(): boolean {
    return this.conn.snapshot.state === ConnState.LOST || this.settings.debugEnabled;
  }

  private capsuleAction(): void {
    if (this.conn.snapshot.state === ConnState.LOST) {
      this.pathStack.pushPathByName('discover', null);
      return;
    }
    if (this.settings.debugEnabled) {
      this.pathStack.pushPathByName('debug', null);
    }
  }

  private capsuleDesc(): string {
    if (this.conn.snapshot.state === ConnState.LOST) {
      return this.resStr('state_reconnect_btn');
    }
    if (this.settings.debugEnabled) {
      return this.resStr('page_debug');
    }
    return '';
  }

  private connected(): boolean {
    return this.conn.snapshot.state === ConnState.CONNECTED;
  }

  private onSurfaceTouch(ev: TouchEvent): void {
    this.touchpad.onTouch(ev);
    if (ev.type === TouchType.Down) {
      if (this.guideOpacity !== 0) {
        this.getUIContext().animateTo({ duration: 120, curve: Curve.EaseOut }, () => {
          this.guideOpacity = 0;
        });
      }
      return;
    }
    // 全指抬起判据：真机 Up 的 touches 不含已抬指、注入事件含，故取差 ≤ 0 兼容两种口径。
    if ((ev.type === TouchType.Up || ev.type === TouchType.Cancel)
      && ev.touches.length - ev.changedTouches.length <= 0) {
      this.getUIContext().animateTo({ duration: 300, curve: Curve.EaseIn }, () => {
        this.guideOpacity = 1;
      });
    }
  }

  build() {
    NavDestination() {
      Column() {
        Row() {
          Column({ space: 4 }) {
            Row() {
              Text(this.resStr(connStateKey(this.conn.snapshot.state)))
                .fontSize($r('sys.float.ohos_id_text_size_body3'))
                .fontColor($r('sys.color.font_primary'))
              if (this.conn.snapshot.state === ConnState.CONNECTED) {
                Text(this.rttText())
                  .fontSize($r('sys.float.ohos_id_text_size_body3'))
                  .fontColor($r('sys.color.font_secondary'))
                  .margin({ left: 8 })
              }
            }
            if (this.conn.snapshot.state === ConnState.RECONNECTING
              && this.conn.snapshot.lastErrorKey.length > 0) {
              Text(pageResFmt(this.getUIContext(), this.conn.snapshot.lastErrorKey,
                this.conn.snapshot.lastErrorParams))
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
                .maxLines(1)
                .textOverflow({ overflow: TextOverflow.Ellipsis })
            }
            if (this.conn.snapshot.state === ConnState.LOST) {
              Text($r('app.string.state_reconnect_btn'))
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.brand'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .constraintSize({ maxWidth: '60%' })
          .padding({ left: CAPSULE_PAD_H_VP, right: CAPSULE_PAD_H_VP, top: CAPSULE_PAD_V_VP, bottom: CAPSULE_PAD_V_VP })
          .borderRadius(CAPSULE_RADIUS_VP)
          // 材质层在 background 之下：必须透明背景，禁再叠 backgroundBlurStyle。
          .backgroundColor(Color.Transparent)
          .border({ width: 0.5, color: $r('sys.color.comp_divider') }) // 材质不显影时描边兜底边界
          .systemMaterial(new uiMaterial.ImmersiveMaterial({
            style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
            materialColor: $r('app.color.capsule_material'), // 必须带透明度
            interactive: this.capsuleActionable()
          }))
          .accessibilityLevel('yes')
          .accessibilityText(this.capsuleA11y())
          .accessibilityDescription(this.capsuleDesc())
          .onClick(() => {
            this.capsuleAction();
          })

          Blank()

          Stack({ alignContent: Alignment.Center }) {
            SymbolGlyph($r('sys.symbol.gearshape'))
              .fontSize(FLOAT_BTN_ICON_FP)
              .fontColor([$r('sys.color.icon_primary')])
          }
          .width(FLOAT_BTN_SIZE_VP)
          .height(FLOAT_BTN_SIZE_VP)
          .borderRadius(FLOAT_BTN_SIZE_VP / 2)
          .backgroundColor(Color.Transparent)
          .border({ width: 0.5, color: $r('sys.color.comp_divider') })
          .systemMaterial(new uiMaterial.ImmersiveMaterial({
            style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
            materialColor: $r('app.color.capsule_material'),
            interactive: true // 材质自带的按压形变，免手写
          }))
          .onClick(() => {
            this.pathStack.pushPathByName('settings', null);
          })
          .accessibilityLevel('yes')
          .accessibilityText($r('app.string.page_settings'))
        }
        .width('100%')
        .padding({ left: CAPSULE_MARGIN_VP, right: CAPSULE_MARGIN_VP })
        .margin({ top: this.topAvoidVp + CAPSULE_MARGIN_VP })

        Stack({ alignContent: Alignment.Center }) {
          Column()
            .width('100%')
            .height('100%')
            .padding({ left: SIDE_MARGIN_VP, right: SIDE_MARGIN_VP })
            .onTouch((ev: TouchEvent) => {
              this.onSurfaceTouch(ev);
            })
          if (!this.kbVisible) {
            this.touchGuide()
          }
          // 覆盖触摸面的信息层必须 Transparent，否则吞掉手势。
          if (this.ctrlBusy()) {
            Column() {
              this.ctrlBusyBanner()
            }
            .width('100%')
            .height('100%')
            .padding({ top: CTRL_BUSY_TOP_GAP_VP })
            .justifyContent(FlexAlign.Start)
            .alignItems(HorizontalAlign.Center)
            .hitTestBehavior(HitTestMode.Transparent)
          }
        }
        .width('100%')
        .layoutWeight(1)

        // 底部停靠簇：行宽恒定（键盘召唤键经尾闭包注入停靠位），卡片出现只顶高不停键位。
        Column({ space: DOCK_GAP_VP }) {
          VoiceDock({ voice: this.voice, conn: this.conn }) {
            if (!this.kbVisible) {
              Stack({ alignContent: Alignment.Center }) {
                SymbolGlyph($r('sys.symbol.keyboard'))
                  .fontSize(DOCK_BTN_ICON_FP)
                  .fontColor([$r('sys.color.icon_primary')])
              }
              .width(DOCK_BTN_SIZE_VP)
              .height(DOCK_BTN_SIZE_VP)
              .borderRadius(DOCK_BTN_SIZE_VP / 2)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
              .border({ width: 0.5, color: $r('sys.color.comp_divider') })
              .onClick(() => {
                this.openKeyboard();
              })
              .accessibilityLevel('yes')
              .accessibilityText($r('app.string.main_kb_toggle_a11y'))
            }
          }

          if (this.kbVisible) {
            KeyboardPage({
              model: this.keyboard,
              onClose: () => {
                this.closeKeyboard();
              }
            })
          }
        }
        .alignItems(HorizontalAlign.End)
        .width('100%')
        // IME 起（RESIZE 下窗口底边即 IME 顶边）时底边距归零，否则手势条避让成死空隙。
        .padding({ left: DOCK_SIDE_MARGIN_VP, right: DOCK_SIDE_MARGIN_VP,
          bottom: this.kbVisible && this.keyboard.imeVisible ? 0 : this.bottomAvoidVp })
      }
      .width('100%')
      .height('100%')
    }
    .hideTitleBar(true)
    .width('100%')
    .height('100%')
  }

  @Builder
  ctrlBusyBanner() {
    Column({ space: 2 }) {
      Text($r('app.string.ctrl_busy_title'))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_secondary'))
        .accessibilityLevel('no')
      Text(pageResFmt(this.getUIContext(), 'ctrl_busy_countdown_fmt',
        [String(this.conn.snapshot.ctrlBusyLeftSec)]))
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor($r('sys.color.brand'))
        .accessibilityLevel('no')
    }
    .padding({ left: CAPSULE_PAD_H_VP, right: CAPSULE_PAD_H_VP, top: CAPSULE_PAD_V_VP, bottom: CAPSULE_PAD_V_VP })
    .borderRadius(CAPSULE_RADIUS_VP)
    .backgroundColor(Color.Transparent)
    .border({ width: 0.5, color: $r('sys.color.comp_divider') })
    .systemMaterial(new uiMaterial.ImmersiveMaterial({
      style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
      materialColor: $r('app.color.capsule_material'),
      interactive: false
    }))
    .accessibilityLevel('yes')
    // 播报不含秒数（每秒变化会反复打断读屏）。
    .accessibilityText($r('app.string.ctrl_busy_a11y'))
  }

  // 冗余提示逐条标 'no'：容器级 'no' 藏不了子树，'no-hide-descendants' 会连坐误伤离线态连接按钮。
  @Builder
  touchGuide() {
    Column({ space: 12 }) {
      if (this.connected()) {
        SymbolGlyph($r('sys.symbol.touchpad'))
          .fontSize(32)
          .fontColor([$r('sys.color.icon_tertiary')])
          .accessibilityLevel('no')
        Text($r('app.string.main_hint_move'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_tertiary'))
          .accessibilityLevel('no')
        Text($r('app.string.main_hint_scroll'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_tertiary'))
          .accessibilityLevel('no')
        Text($r('app.string.main_hint_keyboard'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_tertiary'))
          .accessibilityLevel('no')
      } else {
        SymbolGlyph($r('sys.symbol.computer'))
          .fontSize(40)
          .fontColor([$r('sys.color.icon_tertiary')])
          .accessibilityLevel('no')
        Text(this.resStr(connStateKey(this.conn.snapshot.state)))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontColor($r('sys.color.font_secondary'))
        Text($r('app.string.main_guide_offline_hint'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_tertiary'))
          .textAlign(TextAlign.Center)
        Button($r('app.string.main_connect_host'))
          .margin({ top: 8 })
          .onClick(() => {
            this.pathStack.pushPathByName('discover', null);
          })
      }
    }
    .width('70%')
    .alignItems(HorizontalAlign.Center)
    .opacity(this.guideOpacity)
    .hitTestBehavior(HitTestMode.Transparent) // 提示区透传触摸给触摸面；按钮照常消费
  }
}

function hilogWarn(msg: string, err: BusinessError): void {
  hilog.warn(0x5050, 'PadLink.MainControl', '%{public}s: %{public}d %{public}s', msg, err.code, err.message);
}
