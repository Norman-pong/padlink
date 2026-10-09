// 主控页：全屏触摸面 + 三功能区 Tabs（三指切换）+ 页头行（左锚状态胶囊 / 中锚圆点 / 右锚设置圆键，同带 40vp）。
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
import { VoicePage } from './VoicePage';
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
// 位置指示：命中区 40vp（可触达命中区下限），视觉点 8vp、选中胶囊 20vp。
const ZONE_DOT_HIT_VP: number = 40;
const ZONE_DOT_SIZE_VP: number = 8;
const ZONE_DOT_ACTIVE_W_VP: number = 20;
const ZONE_COUNT: number = 3;

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
  // 直接改 Tabs index 跳页无切换动效，动效统一走 changeIndex；@Local 驱动圆点/区名刷新。
  @Local curTabIndex: number = 0;
  private tabController: TabsController = new TabsController();
  private win: window.Window | null = null;
  private avoidCb: ((data: window.AvoidAreaOptions) => void) | null = null;
  private sizeCb: ((data: window.Size) => void) | null = null;

  aboutToAppear(): void {
    this.touchpad.onSwitchTab = (direction: number) => {
      this.switchTab(direction);
    };
    this.setupWindow();
  }

  aboutToDisappear(): void {
    // 切页先复位引擎（FR-2：未释放按键先发 release，CANCEL 路径已含左键 up + 滚动冲账）
    this.touchpad.cancelGesture();
    this.touchpad.onSwitchTab = null;
    // 语音会话随页收口：cancel 在途识别，麦克风仅按住期间占用（PRD §12）
    this.voice.cancelSession();
    this.restoreWindow();
  }

  // 三指左右滑（FR-2）：触摸板 ⇄ 键盘 ⇄ 语音 循环；系统三指下滑为截屏，不拦截。
  private switchTab(direction: number): void {
    this.switchTo((this.curTabIndex + direction + ZONE_COUNT) % ZONE_COUNT);
  }

  // 切换唯一入口（三指手势与位置指示点共用）。
  private switchTo(index: number): void {
    if (index === this.curTabIndex) {
      return;
    }
    this.touchpad.cancelGesture();
    // 离开语音 Tab 即取消会话（PRD §12：麦克风仅按住期间占用；空闲时为 no-op）
    this.voice.cancelSession();
    if (this.curTabIndex === 1) {
      this.keyboard.zoneLeave();
    }
    if (index === 1) {
      this.keyboard.zoneEnter();
    }
    this.curTabIndex = index;
    // changeIndex 是官方内置切换动效的唯一入口（scrollable(false) 不影响该过渡），禁手写复刻。
    this.tabController.changeIndex(this.curTabIndex);
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

  // 圆点无障碍播报用区名（视觉层不出区名：各区内容自证身份，读屏经圆点播报）。
  private zoneName(index: number): string {
    if (index === 0) {
      return pageResStr(this.getUIContext(), 'tab_touchpad');
    }
    if (index === 1) {
      return pageResStr(this.getUIContext(), 'tab_keyboard');
    }
    return pageResStr(this.getUIContext(), 'tab_voice');
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
    if ((ev.type === TouchType.Up || ev.type === TouchType.Cancel) && ev.touches.length === 0) {
      this.getUIContext().animateTo({ duration: 300, curve: Curve.EaseIn }, () => {
        this.guideOpacity = 1;
      });
    }
  }

  build() {
    NavDestination() {
      Stack({ alignContent: Alignment.TopEnd }) {
        // 三功能区（HdsTabs 必带页签条，无页签条形态只能 plain Tabs + barHeight(0)）。
        Tabs({ barPosition: BarPosition.End, controller: this.tabController }) {
          TabContent() {
            // 触摸面 + 中央引导（引导 Transparent 命中，不挡手势）
            Stack({ alignContent: Alignment.Center }) {
              Column()
                .width('100%')
                .height('100%')
                .padding({ left: SIDE_MARGIN_VP, right: SIDE_MARGIN_VP, bottom: this.bottomAvoidVp })
                .onTouch((ev: TouchEvent) => {
                  this.onSurfaceTouch(ev);
                })
              this.touchGuide()
            }
          }
          .tabBar($r('app.string.tab_touchpad'))

          TabContent() {
            // 键盘区：系统输入法承接文本 + 自绘键条（PRD §4.4）。
            Column() {
              KeyboardPage({ model: this.keyboard })
            }
            .width('100%')
            .height('100%')
            .padding({ bottom: this.bottomAvoidVp })
          }
          .tabBar($r('app.string.tab_keyboard'))

          TabContent() {
            // 语音听写（PRD §4.5）：按住说话，最终结果发 TEXT（daemon 写剪贴板 + Ctrl+V）。
            Column() {
              VoicePage({ voice: this.voice, conn: this.conn })
            }
            .width('100%')
            .height('100%')
            .padding({ bottom: this.bottomAvoidVp })
          }
          .tabBar($r('app.string.tab_voice'))
        }
        .barHeight(0) // 隐藏默认页签条/指示器
        .scrollable(false) // 横滑语义归触摸板手势，禁内容滑动切换
        .width('100%')
        .height('100%')

        // barHeight(0) 后 Tabs 无独立指示器（Indicator 为 Swiper 专属），此处自绘。
        // 单行圆点与左右浮件同带同高；不出区名文字（各区内容自证身份，读屏经圆点播报区名）。
        Row() {
          ForEach([0, 1, 2], (i: number) => {
            Column() {
              Row()
                .width(this.curTabIndex === i ? ZONE_DOT_ACTIVE_W_VP : ZONE_DOT_SIZE_VP)
                .height(ZONE_DOT_SIZE_VP)
                .borderRadius(ZONE_DOT_SIZE_VP / 2)
                .backgroundColor(this.curTabIndex === i
                  ? $r('sys.color.brand') : $r('sys.color.icon_tertiary'))
            }
            .width(ZONE_DOT_HIT_VP)
            .height(ZONE_DOT_HIT_VP)
            .justifyContent(FlexAlign.Center)
            .alignItems(HorizontalAlign.Center)
            .onClick(() => {
              this.switchTo(i);
            })
            .accessibilityLevel('yes')
            .accessibilityText($r('app.string.a11y_zone_format', this.zoneName(i), i + 1))
            .accessibilityDescription($r('app.string.a11y_zone_switch_hint'))
          }, (i: number) => `zone-${i}`)
        }
        .width('100%')
        .height(FLOAT_BTN_SIZE_VP)
        .justifyContent(FlexAlign.Center)
        .margin({ top: this.topAvoidVp + CAPSULE_MARGIN_VP })
        .hitTestBehavior(HitTestMode.Transparent) // 空白区放行给下层功能区，圆点命中区照常消费

        // 页头行：左锚状态胶囊 + 右锚设置圆键，与中间圆点同带同高；空白区 Transparent 放行给下层。
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
          .constraintSize({ maxWidth: '33%' }) // 长文案上限：不与中锚圆点相碰，超出省略
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
        .hitTestBehavior(HitTestMode.Transparent) // 空白区放行给下层功能区，胶囊/圆键照常消费
      }
      .width('100%')
      .height('100%')
    }
    .hideTitleBar(true)
    .width('100%')
    .height('100%')
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
        Text($r('app.string.main_hint_switch'))
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
