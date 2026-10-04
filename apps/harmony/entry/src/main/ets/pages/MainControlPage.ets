// 主控页（FR-1/FR-2/FR-6）：全屏触摸面 + 三功能区 Tabs（三指切换）+ 右上角状态浮层胶囊。
// 避让与沉浸（PRD §6.3，现查文档）：
// - faqs-arkui-1114：setWindowLayoutFullScreen(true) + getWindowAvoidArea 返回 px（需 px2vp）
//   + on('avoidAreaChange') 动态监听；windowSizeChange 双监听覆盖旋转/分屏。
// - arkts-apis-window-e：TYPE_NAVIGATION_INDICATOR(4) 是取底部手势条的唯一途径；
//   侧边手势区无 API（TYPE_SYSTEM_GESTURE 所有设备返回空）→ 左右各 12vp 经验边距（真机待校准）。
// - faqs-arkui-196：进入主控页 setWindowKeepScreenOn(true)，退出恢复 false。
// 状态浮层：backgroundBlurStyle(BlurStyle.BACKGROUND_THIN)（ts-universal-attributes-background，
// 近距景深预定义材质，HDS 归层 THIN/ADAPTIVE 档），@Trace 快照驱动刷新，点击进调试页。
import { window } from '@kit.ArkUI';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager } from '../common/net/ConnectionManager';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import type { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { KeyboardPage } from './KeyboardPage';
import { VoicePage } from './VoicePage';
import type { VoiceModel } from '../common/voice/VoiceModel';

const SIDE_MARGIN_VP: number = 12; // 左右经验边距：侧边手势区无 API 可查，真机待校准

@ComponentV2
export struct MainControlPage {
  @Require @Param conn: ConnectionManager;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param keyboard: KeyboardModel;
  @Require @Param voice: VoiceModel;
  @Require @Param pathStack: NavPathStack;

  @Local curTabIndex: number = 0;
  @Local bottomAvoidVp: number = 0;
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

  private rttText(): string {
    const rtt: number = this.conn.snapshot.rttMs;
    return rtt >= 0 ? `${rtt} ms` : '-- ms';
  }

  build() {
    NavDestination() {
      Stack({ alignContent: Alignment.TopEnd }) {
        // 三功能区容器（FR-2）：触摸板 / 键盘 / 语音；默认页签条隐藏，三指手势驱动 index。
        Tabs({ barPosition: BarPosition.End, index: this.curTabIndex }) {
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

        // 状态浮层（FR-6）：THIN 材质悬浮胶囊，@ObservedV2 快照驱动，点击进调试页。
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
        .padding({ left: 12, right: 12, top: 6, bottom: 6 })
        .borderRadius(18)
        .backgroundBlurStyle(BlurStyle.BACKGROUND_THIN)
        .margin({ top: 12, right: 12 })
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
