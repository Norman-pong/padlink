// 主控页（单页融合形态）：触摸面/键盘/语音收进一页——键盘为可召唤的底部停靠层
//（点触摸区不收起，收起走键条「收起」键），语音为右下角浮动麦克风按住说话（VoiceDock）。
// 布局为 Column 流式分段（页头行 / 触摸面 layoutWeight / 底部停靠簇），触摸面与停靠簇
// 区间互斥不叠层：叠层命中实测会双派——Transparent 容器子树的命中会下穿到下层触摸面
// （点键盘层同时触发鼠标点击），Block 阻下则子件失命中（子件全灭），故只留流式排布。
// 页头行：左锚状态胶囊 / 右锚设置圆键（同带 40vp）。
// 避让：setWindowLayoutFullScreen 后 getWindowAvoidArea/avoidAreaChange 返回 px（需 px2vp）；
// 底部手势条仅 TYPE_NAVIGATION_INDICATOR 可取；侧边手势区无 API，左右 12vp 经验边距（真机待校准）。
// 浮层：systemMaterial ULTRA_THIN + 透明背景（材质层在 background 之下，禁再叠 blur）；
// 模拟器假支持材质时 materialColor 半透色兜底，白底不显影再有 0.5vp 分隔线描边兜底容器边界。
// 调试入口门控单源：仅 settings.debugEnabled 开时胶囊可进调试页（与设置页版本连点入口同闸）；
// LOST 态胶囊动作恒为「重新连接」（去发现页），不再内嵌按钮（嵌套异义动作会互相抢命中）。
import { window, uiMaterial } from '@kit.ArkUI';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import type { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { KeyboardPage } from './KeyboardPage';
import { VoiceDock } from './VoiceDock';
import type { VoiceModel } from '../common/voice/VoiceModel';
import { pageResStr, pageResFmt, connStateKey } from './Res';

// 触摸面/胶囊度量单源（hds-components §一.2：禁散落魔数，本页全部边距在此收敛）。
const SIDE_MARGIN_VP: number = 12; // 触摸面左右经验边距：侧边手势区无 API 可查，真机待校准
const CAPSULE_MARGIN_VP: number = 16; // 页头行水平边距（框架标题栏 16vp 基准，§一.2a）兼顶部间距
const CAPSULE_PAD_H_VP: number = 12; // 胶囊内水平内边距
const CAPSULE_PAD_V_VP: number = 6; // 胶囊内垂直内边距
const CAPSULE_RADIUS_VP: number = 18; // 胶囊圆角（上下内边距 6 + 行高约 24 ≈ 直径 36，取半值 18 成胶囊）
// 浮动圆键：视觉直径 40（框架基准 38 对齐，兼满足 40vp 命中区）。
const FLOAT_BTN_SIZE_VP: number = 40;
const FLOAT_BTN_ICON_FP: number = 22; // SymbolGlyph 基准（框架 menu 键同值）
// 底部停靠浮键：56vp 拇指目标（按住说话需稳定按压面，大于页头浮键）。
const DOCK_BTN_SIZE_VP: number = 56;
const DOCK_BTN_ICON_FP: number = 26;
const DOCK_GAP_VP: number = 10; // 停靠簇内间距（浮键间 / 浮键与键盘层间）
const DOCK_SIDE_MARGIN_VP: number = 8; // 停靠簇水平边距（键盘区到屏缘：8 + 层内 4 = 12vp）
const CTRL_BUSY_TOP_GAP_VP: number = 12; // 控制权倒计时横幅距触摸面顶部的间距

@ComponentV2
export struct MainControlPage {
  @Require @Param conn: ConnectionManager;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param keyboard: KeyboardModel;
  @Require @Param voice: VoiceModel;
  @Require @Param settings: SettingsModel;
  @Require @Param pathStack: NavPathStack;

  @Local bottomAvoidVp: number = 0;
  @Local topAvoidVp: number = 0; // 状态栏避让：胶囊不得压入系统状态栏（否则点击被状态栏窗口拦截）
  @Local guideOpacity: number = 1; // 触摸面引导层透明度：手指按下即淡出（不挡操作），抬手淡回
  @Local kbVisible: boolean = false; // 键盘停靠层开关（浮动键盘键召唤，键条「收起」键收起）
  private win: window.Window | null = null;
  private avoidCb: ((data: window.AvoidAreaOptions) => void) | null = null;
  private sizeCb: ((data: window.Size) => void) | null = null;
  aboutToAppear(): void {
    this.setupWindow();
  }

  aboutToDisappear(): void {
    // 切页先复位引擎（未释放按键先发 release，CANCEL 路径已含左键 up + 滚动冲账 + 终止惯性）
    this.touchpad.cancelGesture();
    // 键盘层残留红线（PRD §11.2）：页面离开时层可能仍挂载，zoneLeave 幂等补收口
    if (this.keyboard.zoneActive) {
      this.keyboard.zoneLeave();
    }
    // 语音会话随页收口：cancel 在途识别，麦克风仅按住期间占用（PRD §12）
    this.voice.cancelSession();
    this.restoreWindow();
  }

  // 召唤键盘：先出层再进区——zoneActive 标记同步置位，KeyboardPage aboutToAppear
  // 据此补偿 doZoneEnter（与 TabContent 懒构建补偿同一模式）。
  private openKeyboard(): void {
    if (this.kbVisible) {
      return;
    }
    this.kbVisible = true;
    this.keyboard.zoneEnter();
  }

  // 收起键盘：先 zoneLeave（隐 IME + 释放残留键，组件仍在挂载态回调可达），再卸载层
  //（aboutToDisappear 内 releaseAll/restoreAvoid 幂等兜底）。
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
    this.bottomAvoidVp = this.getUIContext().px2vp(area.bottomRect.height); // px → vp（faqs-arkui-1114）
    const sys: window.AvoidArea =
      this.win.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM); // 状态栏高度（arkts-apis-window-e：TYPE_SYSTEM=0）
    this.topAvoidVp = this.getUIContext().px2vp(sys.topRect.height);
  }

  private resStr(name: string): string {
    return pageResStr(this.getUIContext(), name);
  }

  private rttText(): string {
    const rtt: number = this.conn.snapshot.rttMs;
    return rtt >= 0 ? `${rtt} ms` : '-- ms';
  }

  // 控制权冷静期是否进行中（daemon NOTICE 置位，倒计时归零或断开即消失）。
  private ctrlBusy(): boolean {
    return this.conn.snapshot.ctrlBusyLeftSec > 0;
  }

  // 胶囊无障碍播报：连接状态 + RTT 动态拼接（硬约束 11；占位格式走 string.json 双语）。
  // accessibilityText 重载只收 string | Resource（无 ResourceStr 联合），恒 $r → 标 Resource。
  private capsuleA11y(): Resource {
    const state: string = this.resStr(connStateKey(this.conn.snapshot.state));
    return $r('app.string.a11y_capsule_format', state, this.rttText());
  }

  // 胶囊动作门控：LOST 恒可点（重新连接 → 发现页）；调试页仅 debugEnabled 放行
  //（与设置页版本连点入口同闸，FR-7 隐藏入口不开侧门）。非可动态不给出按压反馈与操作提示。
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

  // 触摸面事件包装：手势流原样转发引擎；附带维护引导层淡出（按下隐去不挡操作，全指抬起淡回）。
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
    // 全指抬起判据：真机 Up 的 touches 不含已抬指，注入事件含——
    // 取 touches − changedTouches ≤ 0 两种口径同义（单指：0−1 / 1−1 均判全抬）。
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
        // 页头行：左锚状态胶囊 + 右锚设置圆键（流式独占顶部条带，不与触摸面叠层）。
        Row() {
          // 状态浮层：RECONNECTING 展示原因与轮次；LOST 给「重新连接」动作（整胶囊单动作，不嵌按钮）。
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
              // 原因/轮次（如「重连失败（第 N 次）」）
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
          .constraintSize({ maxWidth: '60%' }) // 长文案上限：避让右侧设置圆键，超出省略
          .padding({ left: CAPSULE_PAD_H_VP, right: CAPSULE_PAD_H_VP, top: CAPSULE_PAD_V_VP, bottom: CAPSULE_PAD_V_VP })
          .borderRadius(CAPSULE_RADIUS_VP)
          // 材质层在 background 之下：透明背景 + ImmersiveMaterial，禁再叠 backgroundBlurStyle。
          .backgroundColor(Color.Transparent)
          .border({ width: 0.5, color: $r('sys.color.comp_divider') }) // 材质不显影时描边兜底容器边界
          .systemMaterial(new uiMaterial.ImmersiveMaterial({
            style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
            materialColor: $r('app.color.capsule_material'), // 带透明度（硬约束 2），深浅色见 color.json
            interactive: this.capsuleActionable() // 不可动态不出按压形变（ affordance 与行为一致）
          }))
          .accessibilityLevel('yes')
          .accessibilityText(this.capsuleA11y())
          .accessibilityDescription(this.capsuleDesc())
          .onClick(() => {
            this.capsuleAction();
          })

          Blank()

          // 设置入口
          Stack({ alignContent: Alignment.Center }) {
            SymbolGlyph($r('sys.symbol.gearshape'))
              .fontSize(FLOAT_BTN_ICON_FP)
              .fontColor([$r('sys.color.icon_primary')])
          }
          .width(FLOAT_BTN_SIZE_VP)
          .height(FLOAT_BTN_SIZE_VP)
          .borderRadius(FLOAT_BTN_SIZE_VP / 2)
          .backgroundColor(Color.Transparent) // 材质层在 background 之下（硬约束 1），透明背景
          .border({ width: 0.5, color: $r('sys.color.comp_divider') }) // 与胶囊同规描边兜底
          .systemMaterial(new uiMaterial.ImmersiveMaterial({
            style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
            materialColor: $r('app.color.capsule_material'), // 与状态胶囊同参同色（层1 同族）
            interactive: true // 可点浮层按压弹性形变（motion.md §一：材质自带，免手写）
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

        // 触摸面段：layoutWeight 占满页头与停靠簇之间 + 中央引导（引导 Transparent 命中，不挡手势）
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
          // 控制权被占用：常驻倒计时（不拦手势——Transparent 透传给触摸面，冷却结束后可继续操作）
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

        // 底部停靠簇：VoiceDock（蹦字卡 + 浮键行，键盘召唤键经尾闭包注入其停靠位——
        // 行宽恒定，卡片出现只顶高不停键位），键盘层展开时停靠最底。流式独占底部条带（wrap 高）。
        Column({ space: DOCK_GAP_VP }) {
          VoiceDock({ voice: this.voice, conn: this.conn }) {
            if (!this.kbVisible) {
              // 键盘召唤浮键（键盘层展开时隐去）
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
        // IME 展开时底边距归零（RESIZE 下窗口底边即 IME 顶边，手势条避让成死空隙）；
        // IME 未起（键盘层收起态/用户收了 IME）仍让开手势条。
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

  // 控制权占用提示：常驻（不自动消失）+ 每秒递减的剩余秒数，倒计时结束自动隐藏。
  // 材质/描边与页头状态胶囊同族（层1 ULTRA_THIN + 0.5vp 分隔线兜底），不叠 blur、不挡触摸。
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
      interactive: false // 纯信息展示，无按压语义
    }))
    .accessibilityLevel('yes')
    // 播报只报稳定语义（秒数每秒变化，纳入播报会造成反复打断）
    .accessibilityText($r('app.string.ctrl_busy_a11y'))
  }

  // 中央引导：连接态给三行手势速查，非连接态给状态与连接入口（Transparent 命中）。
  // 冗余提示逐条标 'no'（容器级 'no' 只藏自身藏不了子树，'no-hide-descendants' 才会连坐——
  // 那会误伤离线态的连接按钮）；离线态的状态文案与连接入口保持可读屏可达。
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
    .hitTestBehavior(HitTestMode.Transparent) // 提示文字区透传触摸给触摸面；按钮照常消费
  }
}

// 页内轻量日志（避免每处 import hilog 的样板）。
function hilogWarn(msg: string, err: BusinessError): void {
  console.warn(`[PadLink.MainControl] ${msg}: ${err.code} ${err.message}`);
}
