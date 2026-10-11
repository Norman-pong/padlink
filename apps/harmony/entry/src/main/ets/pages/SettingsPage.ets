// 设置页：灵敏度/自然滚动/已配对主机管理（四态 + 半模态操作单）/调试开关/关于（版本+协议版本+隐私）。
// 删除确认走 UIContext.showAlertDialog（AlertDialog.show 自 API18 废弃）；重命名需 TextInput 故用
// 页内蒙层（CustomDialogController 依赖 V1 装饰器，V2 页从简），蒙层打开时系统返回必须先关弹层。
import { bundleManager } from '@kit.AbilityKit';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { privacyManager } from '@kit.AppGalleryKit';
import { HdsNavDestination } from '@kit.UIDesignKit';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import { HostInfo, validateHostName } from '../common/viewmodel/HostsStore';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { GroupCard, GroupHeader } from './GroupCard';
import { PairedHostsSection } from './PairedHostsSection';
import type { PairingModel } from '../common/viewmodel/PairingModel';
import { PressedRow } from './PressedRow';
import { pageErrText } from './Res';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { enterKeyboardAvoidResize, restoreKeyboardAvoidMode } from './KeyboardAvoid';

const TAG: string = 'PadLink.SettingsPage';
const DOMAIN: number = 0x5050;
const VERSION_TAP_WINDOW_MS: number = 1500;
const VERSION_TAP_COUNT: number = 5;
const TOAST_MS: number = 2000;
const DIVIDER_WIDTH_VP: number = 0.5;
// AGC 托管链接不可得时（模拟器/未托管）回落仓库底稿。
const PRIVACY_FALLBACK_URL: string = 'https://github.com/Norman-pong/padlink/blob/main/PRIVACY.md';

@ComponentV2
export struct SettingsPage {
  @Require @Param settings: SettingsModel;
  @Require @Param pairing: PairingModel;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param pathStack: NavPathStack;

  @Local sensValue: number = 1.2;
  @Local naturalOn: boolean = true;
  @Local debugOn: boolean = false;
  @Local appVersion: string = '';
  private tapTimes: number[] = [];
  private scroller: Scroller = new Scroller();
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET;

  @Local hosts: HostInfo[] = [];
  @Local defaultIp: string = '';
  @Local hostsLoading: boolean = true;
  @Local hostsError: string = '';
  @Local renameIp: string = '';
  @Local renamePort: number = 53021;
  @Local renameText: string = '';
  @Local renameError: string = '';
  @Local actionHost: HostInfo | null = null;

  aboutToAppear(): void {
    // 重命名弹层含底部按钮，必须 RESIZE，否则键盘弹起后按钮滚不到。
    this.kbPrev = enterKeyboardAvoidResize(this.getUIContext());
    this.sensValue = this.settings.sensitivity;
    this.naturalOn = this.settings.naturalScroll;
    this.debugOn = this.settings.debugEnabled;
    this.appVersion = this.loadVersion();
    this.refreshHosts();
  }

  aboutToDisappear(): void {
    restoreKeyboardAvoidMode(this.getUIContext(), this.kbPrev);
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

  private refreshHosts(): void {
    this.hostsLoading = true;
    this.hostsError = '';
    this.settings.listHosts().then((list: HostInfo[]) => {
      this.hosts = list;
      return this.settings.getLastHost();
    }).then((def: HostInfo | null) => {
      this.defaultIp = def !== null ? def.ip : '';
      this.hostsLoading = false;
    }).catch((err: Error) => {
      hilog.warn(DOMAIN, TAG, 'listHosts fail: %{public}s', err.message);
      this.hosts = [];
      this.hostsError = pageErrText(this.getUIContext(), err);
      this.hostsLoading = false;
    });
  }

  private hostName(h: HostInfo): string {
    return h.name.length > 0 ? h.name : h.ip;
  }

  private isDefault(h: HostInfo): boolean {
    return h.ip === this.defaultIp;
  }

  private toast(text: string): void {
    this.getUIContext().getPromptAction().showToast({
      message: text,
      duration: TOAST_MS,
    });
  }

  private openRename(h: HostInfo): void {
    this.renameIp = h.ip;
    this.renamePort = h.port;
    this.renameText = h.name;
    this.renameError = '';
  }

  private closeRename(): void {
    this.renameIp = '';
    this.renameText = '';
    this.renameError = '';
  }

  // 名称上限 64B 对齐 daemon MaxNameLen。
  private saveRename(): void {
    const name = validateHostName(this.renameText);
    if (name.length === 0) {
      this.renameError = this.resStr('settings_host_name_invalid');
      return;
    }
    const ip = this.renameIp;
    const port = this.renamePort;
    this.settings.renameHost(ip, port, name).then(() => {
      this.closeRename();
      this.toast(this.resStr('settings_host_renamed'));
      this.refreshHosts();
    }).catch((err: Error) => {
      this.renameError = pageErrText(this.getUIContext(), err);
    });
  }

  // 只删本机档案不断连接：删除当前主机仅使其不再被 AutoConnect 重连。
  private confirmRemove(h: HostInfo): void {
    this.getUIContext().showAlertDialog({
      title: this.resStr('settings_host_delete_title'),
      message: `${this.hostName(h)}（${h.ip}）\n${this.resStr('settings_host_delete_confirm')}`,
      autoCancel: true,
      primaryButton: {
        value: this.resStr('settings_host_delete'),
        action: (): void => {
          this.removeHost(h);
        },
      },
      secondaryButton: {
        value: this.resStr('pair_cancel'),
        action: (): void => {
        },
      },
    });
  }

  private removeHost(h: HostInfo): void {
    this.settings.removeHost(h.ip, h.port).then(() => {
      this.toast(this.resStr('settings_host_deleted'));
      this.refreshHosts();
    }).catch((err: Error) => {
      this.toast(pageErrText(this.getUIContext(), err));
    });
  }

  // 成功后 PairingModel 把该主机记为默认主机，AutoConnect 与快速连接随之跟随。
  private connectHost(h: HostInfo): void {
    this.pairing.connectKnown(h).then((ok: boolean) => {
      if (ok) {
        this.toast(this.resStr('settings_host_default_done'));
        this.pathStack.pop();
        return;
      }
      this.toast(this.resStr('settings_host_connect_fail'));
      this.refreshHosts();
    });
  }

  // 灵敏度：End/Click 才落盘，避免滑动过程高频 flush。
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
    HdsNavDestination() {
      Stack() {
        Scroll(this.scroller) {
          Column({ space: 16 }) {
            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_group_touchpad'))
              GroupCard() {
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
                Divider()
                  .strokeWidth(DIVIDER_WIDTH_VP)
                  .color($r('sys.color.comp_divider'))
                // 行标题标 'no'：Toggle 已单点播报，否则同文双读。
                Row() {
                  Text($r('app.string.settings_natural_scroll'))
                    .fontSize($r('sys.float.ohos_id_text_size_body1'))
                    .fontColor($r('sys.color.font_primary'))
                    .layoutWeight(1)
                    .accessibilityLevel('no')
                  Toggle({ type: ToggleType.Switch, isOn: this.naturalOn })
                    .onChange((isOn: boolean) => {
                      this.onNaturalChange(isOn);
                    })
                    .accessibilityText($r('app.string.settings_natural_scroll'))
                }
                .width('100%')
              }
            }
            .alignItems(HorizontalAlign.Start)
            .width('100%')

            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_hosts_section'))
              this.hostsSection()
            }
            .alignItems(HorizontalAlign.Start)
            .width('100%')

            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_group_debug'))
              GroupCard() {
                Row() {
                  Column({ space: 4 }) {
                    Text($r('app.string.settings_debug'))
                      .fontSize($r('sys.float.ohos_id_text_size_body1'))
                      .fontColor($r('sys.color.font_primary'))
                      .accessibilityLevel('no')
                    Text($r('app.string.settings_debug_hint'))
                      .fontSize($r('sys.float.ohos_id_text_size_body3'))
                      .fontColor($r('sys.color.font_secondary'))
                      .accessibilityLevel('no')
                  }
                  .alignItems(HorizontalAlign.Start)
                  .layoutWeight(1)
                  Toggle({ type: ToggleType.Switch, isOn: this.debugOn })
                    .onChange((isOn: boolean) => {
                      this.onDebugChange(isOn);
                    })
                    .accessibilityText($r('app.string.settings_debug'))
                }
                .width('100%')
                .alignItems(VerticalAlign.Center)
              }
            }
            .alignItems(HorizontalAlign.Start)
            .width('100%')

            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_about'))
              GroupCard() {
                Image($r('app.media.app_logo'))
                  .width(64)
                  .height(64)
                  .borderRadius(14)
                  .alignSelf(ItemAlign.Center)
                  .margin({ bottom: 4 })
                  .accessibilityLevel('no') // 装饰性展示，读屏跳过
                if (this.debugOn) {
                  Text(`${this.resStr('settings_version')}: ${this.appVersion}`)
                    .fontSize($r('sys.float.ohos_id_text_size_body2'))
                    .fontColor($r('sys.color.font_secondary'))
                    .onClick(() => {
                      this.onVersionClick();
                    })
                    .accessibilityDescription($r('app.string.settings_version_a11y_hint'))
                } else {
                  Text(`${this.resStr('settings_version')}: ${this.appVersion}`)
                    .fontSize($r('sys.float.ohos_id_text_size_body2'))
                    .fontColor($r('sys.color.font_secondary'))
                }
                Text($r('app.string.protocol_version'))
                  .fontSize($r('sys.float.ohos_id_text_size_body2'))
                  .fontColor($r('sys.color.font_secondary'))
                Text($r('app.string.settings_privacy'))
                  .fontSize($r('sys.float.ohos_id_text_size_body2'))
                  .fontColor($r('sys.color.font_secondary'))
                  .onClick(() => {
                    this.openPrivacyPolicy();
                  })
              }
              PressedRow({
                a11yText: this.resStr('settings_guide'),
                onActivated: (): void => {
                  this.pathStack.pushPathByName('guide', null);
                }
              }) {
                Row({ space: 12 }) {
                  SymbolGlyph($r('sys.symbol.computer'))
                    .fontSize(22)
                    .fontColor([$r('sys.color.brand')])
                  Text($r('app.string.settings_guide'))
                    .fontSize($r('sys.float.ohos_id_text_size_body1'))
                    .fontColor($r('sys.color.brand'))
                    .layoutWeight(1)
                  SymbolGlyph($r('sys.symbol.chevron_forward'))
                    .fontSize(20)
                    .fontColor([$r('sys.color.icon_tertiary')])
                }
                .width('100%')
                .alignItems(VerticalAlign.Center)
              }
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
        // 顶对齐：Scroll 默认居中会让短内容顶部悬空。
        .align(Alignment.Top)
        // 穿透到标题栏下方显影滚动模糊。
        .clip(false)

        if (this.renameIp.length > 0) {
          this.renameOverlay()
        }
      }
      .width('100%')
      .height('100%')
      // bindSheet builder 直调 @Builder（官方形态）；手势关闭经 onDisappear 复位。
      .bindSheet(this.actionHost !== null, this.hostActionSheet(), {
        detents: [SheetSize.FIT_CONTENT],
        title: { title: this.actionHost !== null ? this.hostName(this.actionHost) : '' },
        onDisappear: (): void => {
          this.actionHost = null;
        }
      })
    }
    .titleBar({
      // 仅 avoidLayoutSafeArea 内容会钻栏下、仅 enableComponentSafeArea 栏高塌陷，双开缺一不可。
      style: scrollBlurTitleBarStyle(),
      content: {
        title: { mainTitle: $r('app.string.page_settings') }
      },
      avoidLayoutSafeArea: true,
      enableComponentSafeArea: true
    })
    .bindToScrollable([this.scroller])
    .backgroundColor($r('sys.color.ohos_id_color_sub_background')) // 覆盖标题栏区域，防上下拼色
    // 从子页返回不重建页面，主机列表必须在 onShown 重新加载。
    .onShown(() => {
      this.refreshHosts();
    })
    // 自绘蒙层不自带返回行为，返回键必须先关弹层，否则直接退页丢输入。
    .onBackPressed((): boolean => {
      if (this.renameIp.length > 0) {
        this.closeRename();
        return true;
      }
      return false;
    })
  }

  @Builder
  hostsSection() {
    PairedHostsSection({
      hosts: this.hosts,
      loading: this.hostsLoading,
      errorText: this.hostsError,
      emptyLabel: $r('app.string.settings_hosts_empty'),
      onRetry: (): void => {
        this.refreshHosts();
      },
      row: (h: HostInfo): void => {
        this.hostItem(h);
      }
    })
    Column({ space: 8 }) {
      // 连接态下配对第二台主机的唯一入口（发现页 menu 槽只通向设置）。
      PressedRow({
        a11yText: this.resStr('page_discover'),
        onActivated: (): void => {
          this.pathStack.pushPathByName('discover', null);
        }
      }) {
        Row({ space: 12 }) {
          SymbolGlyph($r('sys.symbol.plus'))
            .fontSize(22)
            .fontColor([$r('sys.color.brand')])
          Text($r('app.string.page_discover'))
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.brand'))
            .layoutWeight(1)
          SymbolGlyph($r('sys.symbol.chevron_forward'))
            .fontSize(20)
            .fontColor([$r('sys.color.icon_tertiary')])
        }
        .width('100%')
        .alignItems(VerticalAlign.Center)
      }
    }
    .alignItems(HorizontalAlign.Start)
    .width('100%')
  }

  @Builder
  hostItem(h: HostInfo) {
    // 整行可点；低频/破坏性操作收进动作单（行内小按钮达不到 40vp 命中区），chevron 为钻取语义。
    PressedRow({
      a11yText: `${this.hostName(h)}，${h.ip}`,
      onActivated: (): void => {
        this.actionHost = h;
      }
    }) {
      Row({ space: 8 }) {
        Column({ space: 2 }) {
          Text(this.hostName(h))
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.font_primary'))
            .maxLines(1)
            .textOverflow({ overflow: TextOverflow.Ellipsis })
          Text(h.ip)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.font_secondary'))
            .maxLines(1)
            .textOverflow({ overflow: TextOverflow.Ellipsis })
        }
        .alignItems(HorizontalAlign.Start)
        .layoutWeight(1)
        if (this.isDefault(h)) {
          Text($r('app.string.settings_hosts_default'))
            .fontSize($r('sys.float.ohos_id_text_size_caption1'))
            .fontColor($r('sys.color.brand'))
            .padding({ left: 8, right: 8, top: 2, bottom: 2 })
            .borderRadius(10)
            .border({ width: 1, color: $r('sys.color.brand') })
        }
        SymbolGlyph($r('sys.symbol.chevron_forward'))
          .fontSize(20)
          .fontColor([$r('sys.color.icon_tertiary')])
      }
      .width('100%')
      .alignItems(VerticalAlign.Center)
    }
  }

  // 动作先关面板再执行（删除确认走 AlertDialog 二次确认）。
  @Builder
  hostActionSheet() {
    Column({ space: 8 }) {
      PressedRow({
        a11yText: this.resStr('settings_host_rename'),
        onActivated: (): void => {
          const h: HostInfo | null = this.actionHost;
          this.actionHost = null;
          if (h !== null) {
            this.openRename(h);
          }
        }
      }) {
        Text($r('app.string.settings_host_rename'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontColor($r('sys.color.font_primary'))
      }
      if (this.actionHost !== null && this.actionHost.ip !== this.pairing.currentHostIp()) {
        PressedRow({
          a11yText: this.resStr('settings_host_connect'),
          onActivated: (): void => {
            const h: HostInfo | null = this.actionHost;
            this.actionHost = null;
            if (h !== null) {
              this.connectHost(h);
            }
          }
        }) {
          Text($r('app.string.settings_host_connect'))
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.font_primary'))
        }
      }
      PressedRow({
        a11yText: this.resStr('settings_host_delete'),
        onActivated: (): void => {
          const h: HostInfo | null = this.actionHost;
          this.actionHost = null;
          if (h !== null) {
            this.confirmRemove(h);
          }
        }
      }) {
        Text($r('app.string.settings_host_delete'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontColor($r('sys.color.warning'))
      }
    }
    .width('100%')
    .padding({ left: 16, right: 16, bottom: 16 })
  }

  @Builder
  renameOverlay() {
    Column() {
      // 蒙层点击不关闭（防误触丢输入），仅按钮退出。
      Column()
        .width('100%')
        .height('100%')
        .backgroundColor($r('sys.color.ohos_id_color_mask_regular'))
      Column({ space: 12 }) {
        Text($r('app.string.settings_host_rename'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontWeight(FontWeight.Bold)
          .fontColor($r('sys.color.font_primary'))
        TextInput({ text: this.renameText, placeholder: this.resStr('settings_host_name_hint') })
          .height(40)
          .maxLength(64)
          .defaultFocus(true)
          // 输入框必须用 tertiary：与弹层卡面同色会隐形。
          .backgroundColor($r('sys.color.comp_background_tertiary'))
          .onChange((v: string) => {
            this.renameText = v;
            this.renameError = '';
          })
        if (this.renameError.length > 0) {
          Text(this.renameError)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.warning'))
            .width('100%')
        }
        Row({ space: 12 }) {
          Button($r('app.string.pair_cancel'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .layoutWeight(1)
            .onClick(() => {
              this.closeRename();
            })
          Button($r('app.string.settings_host_save'))
            .layoutWeight(1)
            .onClick(() => {
              this.saveRename();
            })
        }
        .width('100%')
      }
      .padding(20)
      .borderRadius(16)
      .backgroundColor($r('sys.color.comp_background_primary'))
      .margin({ left: 32, right: 32 })
    }
    .width('100%')
    .height('100%')
    .justifyContent(FlexAlign.Center)
  }

  private resStr(name: string): string {
    const ctx = this.getUIContext().getHostContext();
    if (ctx === undefined) {
      return '';
    }
    // getStringSync(Resource) 自 API20 废弃，改用 getStringByNameSync。
    return ctx.resourceManager.getStringByNameSync(name);
  }

  // AGC 托管隐私链接优先，服务不可达（模拟器/侧载未托管）回落仓库底稿。
  private openPrivacyPolicy(): void {
    let url: string = PRIVACY_FALLBACK_URL;
    try {
      const links: privacyManager.AppPrivacyLink[] = privacyManager.getAppPrivacyMgmtInfo().privacyInfo;
      if (links.length > 0 && links[0].url.length > 0) {
        url = links[0].url;
      }
    } catch (err) {
      hilog.info(DOMAIN, TAG, 'privacy mgmt unavailable, fallback to repo copy');
    }
    const ctx = this.getUIContext().getHostContext() as common.UIAbilityContext | undefined;
    if (ctx === undefined) {
      this.toast(this.resStr('settings_privacy_open_fail'));
      return;
    }
    ctx.openLink(url).catch((err: BusinessError) => {
      hilog.warn(DOMAIN, TAG, 'openLink fail %{public}d %{public}s', err.code, err.message);
      this.toast(this.resStr('settings_privacy_open_fail'));
    });
  }
}
