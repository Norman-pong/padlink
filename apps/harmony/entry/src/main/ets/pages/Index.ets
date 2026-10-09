// 应用宿主：HdsNavigation + NavPathStack（PRD §6.1 导航一律 Navigation；D4 升级 HDS 官方
// 导航容器 @kit.UIDesignKit，titleMode MINI + hideBackButton(true)，ui-design-hdsnavigation）。
// 页面注册：discover（发现与配对）/ control（主控，首页）/ settings / debug。
// 共享对象（ConnectionManager/TouchpadModel/SettingsModel/PairingModel）在此创建，经 @Param 下发各页。
// App 启动（首页即主控页）：settings 就绪后按 lastHost 静默自动重连（FR-6，AutoConnect）。
import type { BusinessError } from '@kit.BasicServicesKit';
import type { common } from '@kit.AbilityKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { HdsNavigation, HdsNavigationTitleMode } from '@kit.UIDesignKit';
import { ConnectionManager } from '../common/net/ConnectionManager';
import { SettingsModel } from '../common/viewmodel/SettingsModel';
import { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import { KeyboardModel } from '../common/keyboard/KeyboardModel';
import { VoiceModel } from '../common/voice/VoiceModel';
import { PairingModel } from '../common/viewmodel/PairingModel';
import { tryAutoReconnect } from '../common/viewmodel/AutoConnect';
import { MainControlPage } from './MainControlPage';
import { DiscoverPage } from './DiscoverPage';
import { SettingsPage } from './SettingsPage';
import { DebugPanelPage } from './DebugPanelPage';

const TAG: string = 'PadLink.Index';
const DOMAIN: number = 0x5050;

@Entry
@ComponentV2
struct Index {
  pathStack: NavPathStack = new NavPathStack();
  conn: ConnectionManager = new ConnectionManager();
  settings: SettingsModel = new SettingsModel();
  touchpad: TouchpadModel = new TouchpadModel();
  keyboard: KeyboardModel = new KeyboardModel();
  voice: VoiceModel = new VoiceModel();
  pairing: PairingModel = new PairingModel(this.conn, this.settings);

  aboutToAppear(): void {
    this.touchpad.bindTransport(this.conn.transport);
    this.keyboard.bindTransport(this.conn.transport);
    const ctx = this.getUIContext().getHostContext();
    if (ctx !== undefined) {
      this.voice.attach(this.conn, ctx as common.UIAbilityContext); // 语音权限弹窗/连接发送依赖
      this.settings.init(ctx).then(() => {
        this.touchpad.applySettings(this.settings.sensitivity, this.settings.naturalScroll);
        tryAutoReconnect(this.conn, this.settings); // lastHost 有 token → 后台静默重连（失败进退避循环）
      }).catch((err: BusinessError) => {
        hilog.error(DOMAIN, TAG, 'settings init fail: %{public}d %{public}s', err.code, err.message);
      });
    }
  }

  build() {
    // D4：HdsNavigation 官方导航根容器；titleMode MINI + hideBackButton(true)。
    // 首页为主控页（全屏沉浸，D6 不动结构），根标题栏整体隐藏（hideTitleBar），
    // discover/settings/debug 子页各自的 HdsNavDestination 标题栏带滚动模糊（D5）。
    HdsNavigation(this.pathStack) {
      MainControlPage({ conn: this.conn, touchpad: this.touchpad, keyboard: this.keyboard,
        voice: this.voice, settings: this.settings, pathStack: this.pathStack })
    }
    .mode(NavigationMode.Stack)
    .titleMode(HdsNavigationTitleMode.MINI)
    .hideBackButton(true)
    .hideTitleBar(true)
    .navDestination(this.pageMap)
  }

  @Builder
  pageMap(name: string) {
    if (name === 'discover') {
      DiscoverPage({ pathStack: this.pathStack, pairing: this.pairing })
    } else if (name === 'settings') {
      SettingsPage({ settings: this.settings, touchpad: this.touchpad, pathStack: this.pathStack })
    } else if (name === 'debug') {
      DebugPanelPage({ conn: this.conn, touchpad: this.touchpad, settings: this.settings, pathStack: this.pathStack })
    }
  }
}
