// 应用宿主：HdsNavigation + NavPathStack 注册 discover/control/settings/debug/guide 五页；
// 共享对象（ConnectionManager/SettingsModel/TouchpadModel/PairingModel 等）在此创建，经 @Param 下发。
// 启动即主控页：settings 就绪后按 lastHost 静默自动重连。
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
import { GuidePage } from './GuidePage';

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
      this.voice.attach(this.conn, ctx as common.UIAbilityContext);
      this.settings.init(ctx).then(() => {
        this.touchpad.applySettings(this.settings.sensitivity, this.settings.naturalScroll);
        tryAutoReconnect(this.conn, this.settings);
      }).catch((err: BusinessError) => {
        hilog.error(DOMAIN, TAG, 'settings init fail: %{public}d %{public}s', err.code, err.message);
      });
    }
  }

  // 回前台补一次静默重连（AutoConnect 冷却窗防重复；仅在断开态生效）。
  onPageShow(): void {
    tryAutoReconnect(this.conn, this.settings);
  }

  aboutToDisappear(): void {
    this.voice.dispose(); // ASR 引擎常驻资源最终收口（幂等，再次 press 自动重建）
  }

  build() {
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
      DiscoverPage({ pathStack: this.pathStack, pairing: this.pairing, settings: this.settings })
    } else if (name === 'settings') {
      SettingsPage({ settings: this.settings, pairing: this.pairing, touchpad: this.touchpad, pathStack: this.pathStack })
    } else if (name === 'debug') {
      DebugPanelPage({ conn: this.conn, touchpad: this.touchpad, settings: this.settings,
        pairing: this.pairing, pathStack: this.pathStack })
    } else if (name === 'guide') {
      GuidePage()
    }
  }
}
