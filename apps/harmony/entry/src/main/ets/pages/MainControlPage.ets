// 主控页（FR-1/FR-2/FR-6）：全屏触摸面 + 三功能区 Tabs（三指切换）+ 右上角状态浮层胶囊。
// 避让与沉浸（PRD §6.3，现查文档）：
// - faqs-arkui-1114：setWindowLayoutFullScreen(true) + getWindowAvoidArea 返回 px（需 px2vp）
//   + on('avoidAreaChange') 动态监听；windowSizeChange 双监听覆盖旋转/分屏。
// - arkts-apis-window-e：TYPE_NAVIGATION_INDICATOR(4) 是取底部手势条的唯一途径；
//   侧边手势区无 API（TYPE_SYSTEM_GESTURE 所有设备返回空）→ 左右各 12vp 经验边距（真机待校准）。
// - faqs-arkui-196：进入主控页 setWindowKeepScreenOn(true)，退出恢复 false。
// 状态浮层（HDS 层1 材质 + 硬约束1/2）：.systemMaterial(ImmersiveMaterial ULTRA_THIN) +
// 透明背景——材质层位于 background 之下，禁再叠 backgroundBlurStyle；materialColor 用带透明度的
// token 色（capsule_material，深浅各一档）。API26 模拟器 uiMaterial 假支持（渲染无效果，lore
// 1938f08），此时 materialColor 直接作 backgroundColor 兜底（定则二：可读性不依赖材质）；真机
// 上材质滤镜叠加半透色为增益形态。65%（0xA6）半透度按「深/浅两色内容上 font_primary 文字均可读」
// 取定。胶囊整体 accessibilityText 随连接状态+RTT 动态拼接（硬约束 11），点击进调试页。
import { window, uiMaterial } from '@kit.ArkUI';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import type { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { KeyboardPage } from './KeyboardPage';
import { VoicePage } from './VoicePage';
import type { VoiceModel } from '../common/voice/VoiceModel';
import { pageResStr } from './Res';

// 触摸面/胶囊度量单源（hds-components §一.2：禁散落魔数，本页全部边距在此收敛）。
const SIDE_MARGIN_VP: number = 12; // 触摸面左右经验边距：侧边手势区无 API 可查，真机待校准
const CAPSULE_MARGIN_VP: number = 12; // 胶囊距屏幕右缘/状态栏下缘的间距
const CAPSULE_PAD_H_VP: number = 12; // 胶囊内水平内边距
const CAPSULE_PAD_V_VP: number = 6; // 胶囊内垂直内边距
const CAPSULE_RADIUS_VP: number = 18; // 胶囊圆角（上下内边距 6 + 行高约 24 ≈ 直径 36，取半值 18 成胶囊）

@ComponentV2
export struct MainControlPage {
  @Require @Param conn: ConnectionManager;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param keyboard: KeyboardModel;
  @Require @Param voice: VoiceModel;
  @Require @Param pathStack: NavPathStack;

  @Local bottomAvoidVp: number = 0;
  @Local topAvoidVp: number = 0; // 状态栏避让：胶囊不得压入系统状态栏（否则点击被状态栏窗口拦截）
  // 三功能区当前索引：不再绑定 Tabs 的 index 参数——直接改 index 跳页无切换动效
  //（ts-container-tabs「直接修改index跳页时，切换动效不生效」），动效统一走 TabsController.changeIndex。
  private curTabIndex: number = 0;
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
    this.touchpad.cancelGesture();
    // 离开语音 Tab 即取消会话（PRD §12：麦克风仅按住期间占用；空闲时为 no-op）
    this.voice.cancelSession();
    this.curTabIndex = (this.curTabIndex + direction + 3) % 3;
    // D8（motion.md §五红线：禁手写复刻 tab 切换动效）：changeIndex 是官方内置切换动效的
    // 唯一入口，默认 300ms cubicBezier 翻页（ts-container-tabs animationDuration/curve）；
    // scrollable(false) 只关手势滑动，不影响该内置过渡——不自绘任何补间动画。
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

  private stateRes(): Resource {
    const s: number = this.conn.snapshot.state;
    if (s === ConnState.CONNECTED) {
      return $r('app.string.state_connected');
    }
    if (s === ConnState.RECONNECTING) {
      return $r('app.string.state_reconnecting');
    }
    if (s === ConnState.LOST) {
      return $r('app.string.state_lost');
    }
    if (s === ConnState.DISCOVERING) {
      return $r('app.string.state_discovering');
    }
    if (s === ConnState.PAIRING) {
      return $r('app.string.state_pairing');
    }
    return $r('app.string.state_idle');
  }

  // $r 需字面量，无障碍文案键无法从 stateRes 复用——同 DebugPanelPage.connRes 的键表模式。
  private stateKey(): string {
    const s: number = this.conn.snapshot.state;
    if (s === ConnState.CONNECTED) {
      return 'state_connected';
    }
    if (s === ConnState.RECONNECTING) {
      return 'state_reconnecting';
    }
    if (s === ConnState.LOST) {
      return 'state_lost';
    }
    if (s === ConnState.DISCOVERING) {
      return 'state_discovering';
    }
    if (s === ConnState.PAIRING) {
      return 'state_pairing';
    }
    return 'state_idle';
  }

  private rttText(): string {
    const rtt: number = this.conn.snapshot.rttMs;
    return rtt >= 0 ? `${rtt} ms` : '-- ms';
  }

  // 胶囊无障碍播报：连接状态 + RTT 动态拼接（硬约束 11；占位格式走 string.json 双语）。
  // accessibilityText 重载只收 string | Resource（无 ResourceStr 联合），恒 $r → 标 Resource。
  private capsuleA11y(): Resource {
    const state: string = pageResStr(this.getUIContext(), this.stateKey());
    return $r('app.string.a11y_capsule_format', state, this.rttText());
  }

  build() {
    NavDestination() {
      Stack({ alignContent: Alignment.TopEnd }) {
        // 三功能区容器（FR-2）：触摸板 / 键盘 / 语音；默认页签条隐藏，三指手势驱动 index。
        // 归层律备注（D8 定案）：三功能区切换无页签条为既定交互形态，官方无「无页签条的切换
        // 容器」对应能力（HdsTabs 必带页签条，查证 ui-design-hdstabs/ts-container-tabs）——
        // plain Tabs + barHeight(0) 隐藏指示器为合法形态，切换动效走 changeIndex 内置翻页。
        Tabs({ barPosition: BarPosition.End, controller: this.tabController }) {
          TabContent() {
            Column()
              .width('100%')
              .height('100%')
              .padding({ left: SIDE_MARGIN_VP, right: SIDE_MARGIN_VP, bottom: this.bottomAvoidVp })
              .onTouch((ev: TouchEvent) => {
                this.touchpad.onTouch(ev);
              })
          }
          .tabBar($r('app.string.tab_touchpad'))

          TabContent() {
            // 自绘 QWERTY 键盘（PRD §4.4）：KeyCap 直接发 HID usage，不经系统输入法。
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

        // 状态浮层（FR-6）：ULTRA_THIN 沉浸材质胶囊，@ObservedV2 快照驱动，点击进调试页。
        // 非 CONNECTED 态文案区分：RECONNECTING 展示原因与轮次；LOST 额外给「重新连接」按钮跳发现页。
        Column({ space: 4 }) {
          Row() {
            Text(this.stateRes())
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
            && this.conn.snapshot.lastError.length > 0) {
            // lastError 携带原因/轮次（tryReconnect 写入「重连失败（第 N 次）」），满足浮层轮次展示
            Text(this.conn.snapshot.lastError)
              .fontSize($r('sys.float.ohos_id_text_size_body2'))
              .fontColor($r('sys.color.font_secondary'))
              .maxLines(1)
              .textOverflow({ overflow: TextOverflow.Ellipsis })
          }
          if (this.conn.snapshot.state === ConnState.LOST) {
            Button($r('app.string.state_reconnect_btn'))
              .buttonStyle(ButtonStyleMode.NORMAL)
              .height(28)
              .onClick(() => {
                this.pathStack.pushPathByName('discover', null);
              })
          }
        }
        .alignItems(HorizontalAlign.End)
        .padding({ left: CAPSULE_PAD_H_VP, right: CAPSULE_PAD_H_VP, top: CAPSULE_PAD_V_VP, bottom: CAPSULE_PAD_V_VP })
        .borderRadius(CAPSULE_RADIUS_VP)
        // 材质分层律（硬约束 1）：材质层在 background 之下——透明背景 + ImmersiveMaterial，
        // 禁再叠 backgroundBlurStyle；interactive:false 胶囊为静态浮层，无按压形变需求。
        .backgroundColor(Color.Transparent)
        .systemMaterial(new uiMaterial.ImmersiveMaterial({
          style: uiMaterial.ImmersiveStyle.ULTRA_THIN,
          materialColor: $r('app.color.capsule_material'), // 带透明度（硬约束 2），深浅色见 color.json
          interactive: false
        }))
        .margin({ top: this.topAvoidVp + CAPSULE_MARGIN_VP, right: CAPSULE_MARGIN_VP })
        .accessibilityLevel('yes')
        .accessibilityText(this.capsuleA11y())
        .accessibilityDescription($r('app.string.page_debug'))
        .onClick(() => {
          this.pathStack.pushPathByName('debug', null);
        })
      }
      .width('100%')
      .height('100%')
    }
    .hideTitleBar(true)
    .width('100%')
    .height('100%')
  }
}

// 页内轻量日志（避免每处 import hilog 的样板）。
function hilogWarn(msg: string, err: BusinessError): void {
  console.warn(`[PadLink.MainControl] ${msg}: ${err.code} ${err.message}`);
}
