// 应用宿主：Navigation + NavPathStack（PRD §6.1 导航一律 Navigation）。
// 页面注册：discover（发现与配对占位，M2 接入）/ control（主控，首页）/ settings / debug。
// 共享对象（ConnectionManager/TouchpadModel/SettingsModel）在此创建，经 @Param 下发各页。
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ConnectionManager } from '../common/net/ConnectionManager';
import { SettingsModel } from '../common/viewmodel/SettingsModel';
import { TouchpadModel } from '../common/viewmodel/TouchpadModel';
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

  aboutToAppear(): void {
    this.touchpad.bindTransport(this.conn.transport);
    const ctx = this.getUIContext().getHostContext();
    if (ctx !== undefined) {
      this.settings.init(ctx).then(() => {
        this.touchpad.applySettings(this.settings.sensitivity, this.settings.naturalScroll);
      }).catch((err: BusinessError) => {
        hilog.error(DOMAIN, TAG, 'settings init fail: %{public}d %{public}s', err.code, err.message);
      });
    }
  }

  build() {
    Navigation(this.pathStack) {
      MainControlPage({ conn: this.conn, touchpad: this.touchpad, pathStack: this.pathStack })
    }
    .mode(NavigationMode.Stack)
    .hideTitleBar(true)
    .hideToolBar(true)
    .navDestination(this.pageMap)
  }

  @Builder
  pageMap(name: string) {
    if (name === 'discover') {
      DiscoverPage({ pathStack: this.pathStack })
    } else if (name === 'settings') {
      SettingsPage({ settings: this.settings, touchpad: this.touchpad, pathStack: this.pathStack })
    } else if (name === 'debug') {
      DebugPanelPage({ conn: this.conn, touchpad: this.touchpad, settings: this.settings, pathStack: this.pathStack })
    }
  }
}
