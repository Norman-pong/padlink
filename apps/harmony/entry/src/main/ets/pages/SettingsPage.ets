// 设置页（FR-5 / FR-7）：灵敏度 Slider（0.5–2.0 步进 0.1，实时生效+持久化）、
// 自然滚动开关、调试面板开关（控制版本号连点入口显隐）、关于区（应用版本+协议版本）。
// 隐藏入口（FR-7）：调试开关开启时，版本号 Text 连点 5 次（400ms 内累计）跳调试页。
// 应用版本经 bundleManager.getBundleInfoForSelfSync 读取（js-apis-bundlemanager，API10+）。
import { bundleManager } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';

const TAG: string = 'PadLink.SettingsPage';
const DOMAIN: number = 0x5050;
const VERSION_TAP_WINDOW_MS: number = 400;
const VERSION_TAP_COUNT: number = 5;

@ComponentV2
export struct SettingsPage {
  @Require @Param settings: SettingsModel;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param pathStack: NavPathStack;

  @Local sensValue: number = 1.2;
  @Local naturalOn: boolean = true;
  @Local debugOn: boolean = false;
  @Local appVersion: string = '';
  private tapTimes: number[] = [];

  aboutToAppear(): void {
    this.sensValue = this.settings.sensitivity;
    this.naturalOn = this.settings.naturalScroll;
    this.debugOn = this.settings.debugEnabled;
    this.appVersion = this.loadVersion();
  }

  private loadVersion(): string {
    try {
      const info: bundleManager.BundleInfo =
        bundleManager.getBundleInfoForSelfSync(bundleManager.BundleFlag.GET_BUNDLE_INFO_DEFAULT);
      return info.versionName;
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'getBundleInfoForSelfSync fail: %{public}d %{public}s', e.code, e.message);
      return '';
    }
  }

  // 灵敏度：拖动实时生效；End/Click 落盘（避免滑动过程高频 flush）。
  private onSensChange(value: number, mode: SliderChangeMode): void {
    this.sensValue = value;
    this.touchpad.applySettings(value, this.naturalOn);
    if (mode === SliderChangeMode.End || mode === SliderChangeMode.Click) {
      this.settings.setSensitivity(value).catch((err: BusinessError) => {
        hilog.error(DOMAIN, TAG, 'setSensitivity fail: %{public}d %{public}s', err.code, err.message);
      });
    }
  }

  private onNaturalChange(isOn: boolean): void {
    this.naturalOn = isOn;
    this.touchpad.applySettings(this.sensValue, isOn);
    this.settings.setNaturalScroll(isOn).catch((err: BusinessError) => {
      hilog.error(DOMAIN, TAG, 'setNaturalScroll fail: %{public}d %{public}s', err.code, err.message);
    });
  }

  private onDebugChange(isOn: boolean): void {
    this.debugOn = isOn;
    this.settings.setDebugEnabled(isOn).catch((err: BusinessError) => {
      hilog.error(DOMAIN, TAG, 'setDebugEnabled fail: %{public}d %{public}s', err.code, err.message);
    });
  }

  // 版本号连点 5 次（400ms 内累计）→ 调试页（FR-7 隐藏入口；调试开关关闭时入口整体隐藏）。
  private onVersionClick(): void {
    const now: number = new Date().getTime();
    this.tapTimes = this.tapTimes.filter((t: number): boolean => now - t <= VERSION_TAP_WINDOW_MS);
    this.tapTimes.push(now);
    if (this.tapTimes.length >= VERSION_TAP_COUNT) {
      this.tapTimes = [];
      this.pathStack.pushPathByName('debug', null);
    }
  }

  build() {
    NavDestination() {
      Scroll() {
        Column({ space: 16 }) {
          // 灵敏度
          Column({ space: 8 }) {
            Row() {
              Text($r('app.string.settings_sensitivity'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
              Blank()
              Text(this.sensValue.toFixed(1))
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
            }
            .width('100%')
            Slider({ value: this.sensValue, min: 0.5, max: 2.0, step: 0.1 })
              .width('100%')
              .onChange((value: number, mode: SliderChangeMode) => {
                this.onSensChange(value, mode);
              })
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          // 自然滚动
          Row() {
            Text($r('app.string.settings_natural_scroll'))
              .fontSize($r('sys.float.ohos_id_text_size_body1'))
              .fontColor($r('sys.color.font_primary'))
              .layoutWeight(1)
            Toggle({ type: ToggleType.Switch, isOn: this.naturalOn })
              .onChange((isOn: boolean) => {
                this.onNaturalChange(isOn);
              })
          }
          .width('100%')

          // 调试面板开关
          Row() {
            Column({ space: 4 }) {
              Text($r('app.string.settings_debug'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
              Text($r('app.string.settings_debug_hint'))
                .fontSize($r('sys.float.ohos_id_text_size_body3'))
                .fontColor($r('sys.color.font_secondary'))
            }
            .alignItems(HorizontalAlign.Start)
            .layoutWeight(1)
            Toggle({ type: ToggleType.Switch, isOn: this.debugOn })
              .onChange((isOn: boolean) => {
                this.onDebugChange(isOn);
              })
          }
          .width('100%')
          .alignItems(VerticalAlign.Center)

          Divider()
            .color($r('sys.color.comp_divider'))

          // 关于（FR-7 隐藏入口挂在版本行）
          Column({ space: 8 }) {
            Text($r('app.string.settings_about'))
              .fontSize($r('sys.float.ohos_id_text_size_body1'))
              .fontColor($r('sys.color.font_primary'))
            if (this.debugOn) {
              Text(`${this.resStr('settings_version')}: ${this.appVersion}`)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
                .onClick(() => {
                  this.onVersionClick();
                })
            } else {
              Text(`${this.resStr('settings_version')}: ${this.appVersion}`)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
            }
            Text($r('app.string.protocol_version'))
              .fontSize($r('sys.float.ohos_id_text_size_body2'))
              .fontColor($r('sys.color.font_secondary'))
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')
        }
        .padding(16)
        .width('100%')
      }
      .width('100%')
      .height('100%')
      .scrollBar(BarState.Off)
    }
    .title($r('app.string.page_settings'))
  }

  private resStr(name: string): string {
    const ctx = this.getUIContext().getHostContext();
    if (ctx === undefined) {
      return '';
    }
    // getStringSync(Resource) 自 API20 废弃（js-apis-resource-manager），改用 getStringByNameSync
    return ctx.resourceManager.getStringByNameSync(name);
  }
}
