// 设置页（FR-5 / FR-7）：灵敏度 Slider（0.5–2.0 步进 0.1，实时生效+持久化）、
// 自然滚动开关、已配对主机管理（列表四态：加载/空态/列表/轻提示反馈；点行进半模态操作单——
// 重命名/设默认/删除，bindSheet FIT_CONTENT；删除仍走 AlertDialog 二次确认）、
// 调试面板开关（控制版本号连点入口显隐）、关于区（应用版本+协议版本）。
// 隐藏入口（FR-7）：调试开关开启时，版本号连点 5 次跳调试页。
// 应用版本经 bundleManager.getBundleInfoForSelfSync 读取（js-apis-bundlemanager，API10+）。
// 弹窗/反馈 API（文档现查）：删除二次确认走 UIContext.showAlertDialog（AlertDialog.show 自
// API18 废弃，ts-methods-alert-dialog-box / arkts-fixes-style-dialog）；操作反馈走
// UIContext.getPromptAction().showToast（arkts-apis-uicontext-promptaction）；重命名因需
// TextInput 输入用页内蒙层弹层实现（CustomDialogController 依赖 V1 状态装饰器，V2 页面从简），
// 蒙层打开时 onBackPressed 拦截系统返回先关弹层。
// HDS（D5/D3）：HdsNavDestination + 滚动模糊标题栏（单源 HdsTitleBar.ets）+ Scroller 绑定；
// 页底色 ohos_id_color_sub_background、卡面 comp_background_primary、分隔线 comp_divider
// 0.5vp；蒙层用系统 token ohos_id_color_mask_regular（原 rgba 硬编码清零）。
import { bundleManager } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { HdsNavDestination } from '@kit.UIDesignKit';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import { HostInfo, validateHostName } from '../common/viewmodel/HostsStore';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { GroupCard, GroupHeader } from './GroupCard';
import { PressedRow } from './PressedRow';
import { pageErrText } from './Res';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { enterKeyboardAvoidResize, restoreKeyboardAvoidMode } from './KeyboardAvoid';

const TAG: string = 'PadLink.SettingsPage';
const DOMAIN: number = 0x5050;
const VERSION_TAP_WINDOW_MS: number = 1500;
const VERSION_TAP_COUNT: number = 5;
const TOAST_MS: number = 2000;
const DIVIDER_WIDTH_VP: number = 0.5; // D3：分隔线规格与分组卡规范对齐

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
  private scroller: Scroller = new Scroller(); // 页面滚动器（标题栏滚动模糊联动）
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET; // 进入页面前避让模式（退出恢复）

  // 已配对主机（四态：加载中/加载失败/空态/列表；操作反馈用 toast）
  @Local hosts: HostInfo[] = [];
  @Local defaultIp: string = '';
  @Local hostsLoading: boolean = true;
  @Local hostsError: string = '';
  // 重命名弹层（''=关闭）
  @Local renameIp: string = '';
  @Local renamePort: number = 53021;
  @Local renameText: string = '';
  @Local renameError: string = '';
  // 主机操作半模态（null=关闭）：点行拉起，动作单承载重命名/设默认/删除
  @Local actionHost: HostInfo | null = null;

  aboutToAppear(): void {
    // 重命名弹层 TextInput + 保存/取消按钮：RESIZE 键盘避让（faqs-arkui-1073）
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

  // ---- 已配对主机 ----

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

  // 重命名提交：非空且 ≤64B（daemon MaxNameLen）校验失败给行内原因，成功轻提示并刷新。
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

  // 删除二次确认（AlertDialog）：只删本机档案，不触碰连接——若删除的是当前连接主机，
  // 连接保持不断开，仅该主机不再被 AutoConnect 自动重连（AutoConnect 只按列表默认主机发起）。
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

  // 设为默认：立即换档，AutoConnect 下次触发按新默认主机重连；当前连接不受影响。
  private makeDefault(h: HostInfo): void {
    this.settings.setDefault(h.ip, h.port).then(() => {
      this.toast(this.resStr('settings_host_default_done'));
      this.refreshHosts();
    }).catch((err: Error) => {
      this.toast(pageErrText(this.getUIContext(), err));
    });
  }

  // ---- 既有设置项 ----

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

  // 版本号连点 5 次（VERSION_TAP_WINDOW_MS 窗口内累计）→ 调试页（FR-7 隐藏入口；调试开关关闭时入口整体隐藏）。
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
          // D3 分组列表：组头（caption）+ 分组卡（GroupCard 单源），四组——触控/主机/调试/关于。
          Column({ space: 16 }) {
            // ---- 触控 ----
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
                // 自然滚动（开关行：读屏由 Toggle 单点播报「自然滚动，开关，已开启」，
                // 行标题标 'no' 防同文双读）
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

            // ---- 已配对主机（FR-5 主机管理） ----
            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_hosts_section'))
              this.hostsSection()
            }
            .alignItems(HorizontalAlign.Start)
            .width('100%')

            // ---- 调试 ----
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

            // ---- 关于（FR-7 隐藏入口挂在版本行） ----
            Column({ space: 8 }) {
              GroupHeader($r('app.string.settings_about'))
              GroupCard() {
                // 品牌 logo：装饰性展示（应用身份已由版本行播报，读屏跳过）
                Image($r('app.media.app_logo'))
                  .width(64)
                  .height(64)
                  .borderRadius(14) // 系统图标圆角观感（≈22%）
                  .alignSelf(ItemAlign.Center)
                  .margin({ bottom: 4 })
                  .accessibilityLevel('no')
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
        // 内容短于视口时顶对齐：Scroll 默认居中会让表单页顶部悬空一段空白
        .align(Alignment.Top)
        // 穿透到标题栏下方显影滚动模糊（ui-design-navigation-dynamic-blur 官方模式）
        .clip(false)

        // 重命名弹层（页内蒙层实现，置于滚动层之上）
        if (this.renameIp.length > 0) {
          this.renameOverlay()
        }
      }
      .width('100%')
      .height('100%')
      // 主机操作半模态（bindSheet builder 直调 @Builder，官方形态；手势关闭经 onDisappear 复位）
      .bindSheet(this.actionHost !== null, this.hostActionSheet(), {
        detents: [SheetSize.FIT_CONTENT],
        title: { title: this.actionHost !== null ? this.hostName(this.actionHost) : '' },
        onDisappear: (): void => {
          this.actionHost = null;
        }
      })
    }
    .titleBar({
      // 主控页常驻 layoutFullScreen，子页窗口实为全屏形态；两开关分别管标题栏避让状态栏
      // 与内容避让标题栏，缺一不可（MateX7 实测）。
      style: scrollBlurTitleBarStyle(),
      content: {
        title: { mainTitle: $r('app.string.page_settings') }
      },
      avoidLayoutSafeArea: true,
      enableComponentSafeArea: true
    })
    .bindToScrollable([this.scroller])
    .backgroundColor($r('sys.color.ohos_id_color_sub_background')) // D3 分组列表页底色（覆盖标题栏区域，防上下拼色）
    // 重命名弹层打开时系统返回先关弹层（自绘蒙层不自带返回行为），否则直接退出整页丢输入
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
    // 组头由调用处 GroupHeader 承担；这里只产四态内容（卡面规格与 GroupCard 同源：12 内边距/12 圆角/primary）。
    Column({ space: 8 }) {
      if (this.hostsLoading) {
        // 加载态
        Row({ space: 12 }) {
          LoadingProgress()
            .width(20)
            .height(20)
            .color($r('sys.color.brand'))
          Text($r('app.string.settings_hosts_loading'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.font_secondary'))
        }
        .width('100%')
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_primary'))
      } else if (this.hostsError.length > 0) {
        // 失败态：给原因 + 重试
        Column({ space: 8 }) {
          Text($r('app.string.settings_hosts_load_fail'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.warning'))
          Text(this.hostsError)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.font_secondary'))
          Button($r('app.string.discover_retry'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .onClick(() => {
              this.refreshHosts();
            })
        }
        .alignItems(HorizontalAlign.Start)
        .width('100%')
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_primary'))
      } else if (this.hosts.length === 0) {
        // 空态
        Text($r('app.string.settings_hosts_empty'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_secondary'))
          .width('100%')
          .padding(12)
          .borderRadius(12)
          .backgroundColor($r('sys.color.comp_background_primary'))
      } else {
        // 列表态
        Column({ space: 8 }) {
          ForEach(this.hosts, (h: HostInfo) => {
            this.hostItem(h)
          }, (h: HostInfo) => `${h.ip}:${h.port}`)
        }
        .width('100%')
      }
      // 发现页入口行：连接态下配对第二台主机的唯一可达路径（发现页 menu 槽只通向设置）。
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
    // 整行可点（PressedRow 单源按压态），点行拉起半模态动作单——低频/破坏性操作收进动作单，
    // 不在行内排小按钮（命中区下限 40vp）；chevron 此处为钻取语义（进入该主机的操作上下文）
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

  // 主机操作单（半模态）：重命名 / 设为默认（非默认才出）/ 删除（破坏性，警示色 + 二次确认）。
  // 动作先关面板再执行，删除确认走 AlertDialog。
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
      if (this.actionHost !== null && !this.isDefault(this.actionHost)) {
        PressedRow({
          a11yText: this.resStr('settings_host_set_default'),
          onActivated: (): void => {
            const h: HostInfo | null = this.actionHost;
            this.actionHost = null;
            if (h !== null) {
              this.makeDefault(h);
            }
          }
        }) {
          Text($r('app.string.settings_host_set_default'))
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
      // 蒙层：点击不关闭（防误触丢输入），仅按钮退出
      Column()
        .width('100%')
        .height('100%')
        .backgroundColor($r('sys.color.ohos_id_color_mask_regular')) // 弹层蒙层：系统 token，深浅色自适应
      Column({ space: 12 }) {
        Text($r('app.string.settings_host_rename'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontWeight(FontWeight.Bold)
          .fontColor($r('sys.color.font_primary'))
        TextInput({ text: this.renameText, placeholder: this.resStr('settings_host_name_hint') })
          .height(40)
          .maxLength(64)
          .defaultFocus(true)
          // 输入框底色区别于弹层卡面（同色会隐形）；tertiary 为输入件底色非列表卡面，不属 D3 范围
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
    // getStringSync(Resource) 自 API20 废弃（js-apis-resource-manager），改用 getStringByNameSync
    return ctx.resourceManager.getStringByNameSync(name);
  }
}
