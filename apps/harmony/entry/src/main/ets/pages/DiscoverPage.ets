// 发现与配对页：进入自动 UDP 广播发现 → 主机列表 → 选中连接 → 4 位确认码（输满自动提交）→ 回主控页。
// 四态齐备（发现中/列表/空态/失败重试），无白屏无假死；已有 lastHost 时页顶给快速连接入口。
// HDS：HdsNavDestination + 滚动模糊标题栏（单源 HdsTitleBar.ets）。
import { HdsNavDestination } from '@kit.UIDesignKit';
import { DEFAULT_PORT, PairPhase } from '../common/viewmodel/PairingModel';
import type { PairingModel } from '../common/viewmodel/PairingModel';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import { DiscoveredHost } from '../common/net/DiscoverCore';
import { HostInfo } from '../common/viewmodel/HostsStore';
import { pageResStr, pageResFmt, pageErrText } from './Res';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { PressedRow } from './PressedRow';
import { HostRow } from './HostRow';
import { PairedHostsSection } from './PairedHostsSection';
import { GroupHeader } from './GroupCard';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { enterKeyboardAvoidResize, restoreKeyboardAvoidMode } from './KeyboardAvoid';

const PAIRED_STAY_MS: number = 800;

@ComponentV2
export struct DiscoverPage {
  @Require @Param pathStack: NavPathStack;
  @Require @Param pairing: PairingModel;
  @Require @Param settings: SettingsModel;

  @Local manualIp: string = '';
  @Local pairedHosts: HostInfo[] = [];
  @Local pairedLoading: boolean = false;
  @Local pairedError: string = '';

  private scroller: Scroller = new Scroller();
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET;
  private popTimer: number = -1;

  aboutToAppear(): void {
    // 确认码输入 + 下方按钮必须 RESIZE，否则键盘弹起后按钮滚不到。
    this.kbPrev = enterKeyboardAvoidResize(this.getUIContext());
    this.pairing.onPaired = (): void => {
      this.schedulePop();
    };
    // 列表已满未入库：轻提示引导先删除，连接本身不受影响。
    this.pairing.onHostsFull = (): void => {
      this.getUIContext().getPromptAction().showToast({
        message: this.resStr('pair_hosts_full'),
        duration: 2000,
      });
    };
    this.pairing.onPageEnter();
    this.refreshPairedHosts();
  }

  aboutToDisappear(): void {
    restoreKeyboardAvoidMode(this.getUIContext(), this.kbPrev);
    this.pairing.onPageLeave();
    this.pairing.onPaired = null;
    this.pairing.onHostsFull = null;
    if (this.popTimer >= 0) {
      clearTimeout(this.popTimer);
      this.popTimer = -1;
    }
  }

  private schedulePop(): void {
    if (this.popTimer >= 0) {
      return;
    }
    this.popTimer = setTimeout(() => {
      this.popTimer = -1;
      this.pathStack.pop();
    }, PAIRED_STAY_MS);
  }

  private resStr(name: string): string {
    return pageResStr(this.getUIContext(), name);
  }

  private refreshPairedHosts(): void {
    this.pairedLoading = true;
    this.pairedError = '';
    this.settings.listHosts().then((list: HostInfo[]) => {
      this.pairedHosts = list;
      this.pairedLoading = false;
    }).catch((err: Error) => {
      this.pairedHosts = [];
      this.pairedError = pageErrText(this.getUIContext(), err);
      this.pairedLoading = false;
    });
  }

  private hostLabel(h: HostInfo): string {
    return h.name.length > 0 ? h.name : h.ip;
  }

  private isActive(h: HostInfo): boolean {
    return h.ip === this.pairing.currentHostIp();
  }

  private switchTo(h: HostInfo): void {
    if (this.isActive(h)) {
      this.pathStack.pop();
      return;
    }
    this.pairing.connectKnown(h).then((ok: boolean) => {
      if (!ok) {
        this.refreshPairedHosts(); // 失败原因在页面错误区展示，列表同步刷新
      }
    });
  }

  // 不挂 chevron：› 是导航语义，本行点击即切换。
  @Builder
  hostRow(h: HostInfo) {
    HostRow({
      label: this.hostLabel(h),
      ip: h.ip,
      badge: this.isActive(h) ? $r('app.string.discover_hosts_in_use') : null,
      a11yExtra: this.isActive(h) ? `，${this.resStr('discover_hosts_in_use')}` : '',
      onRowActivated: (): void => {
        this.switchTo(h);
      }
    })
  }

  private errorText(): string {
    const snap = this.pairing.snapshot;
    if (snap.errorKey.length > 0) {
      return pageResFmt(this.getUIContext(), snap.errorKey, snap.errorParams);
    }
    return snap.errorText;
  }

  @Builder
  codeSlot(index: number) {
    Text(this.pairing.snapshot.code.length > index ? this.pairing.snapshot.code.charAt(index) : '')
      .layoutWeight(1)
      .height(64)
      .fontSize(32)
      .fontWeight(FontWeight.Bold)
      .textAlign(TextAlign.Center)
      .borderRadius(12)
      .border({ width: 1.5, color: this.pairing.snapshot.code.length === index
        ? $r('sys.color.brand') : $r('sys.color.comp_divider') })
      .backgroundColor($r('sys.color.comp_background_tertiary'))
      .accessibilityLevel('no') // 视觉镜像，读屏由覆盖其上的 TextInput 承担
  }

  build() {
    HdsNavDestination() {
      Scroll(this.scroller) {
        Column({ space: 16 }) {
          if (this.pairing.snapshot.phase === PairPhase.DISCOVERING) {
            this.progressView($r('app.string.discover_searching'))
          } else if (this.pairing.snapshot.phase === PairPhase.CONNECTING) {
            this.progressView($r('app.string.discover_connecting'))
          } else if (this.pairing.snapshot.phase === PairPhase.VERIFYING) {
            this.progressView($r('app.string.pair_verifying'))
          } else if (this.pairing.snapshot.phase === PairPhase.PENDING_CODE) {
            this.pairingView()
          } else if (this.pairing.snapshot.phase === PairPhase.PAIRED) {
            this.successView()
          } else if (this.pairing.snapshot.phase === PairPhase.FAILED) {
            this.failedView()
          } else {
            this.listView()
          }
        }
        .width('100%')
        .constraintSize({ minHeight: '100%' })
        .justifyContent(this.pairing.snapshot.phase === PairPhase.IDLE ? FlexAlign.Start : FlexAlign.Center)
        .padding(16)
      }
      .width('100%')
      .height('100%')
      .align(Alignment.Top)
      .clip(false)
    }
    .titleBar({
      // 仅 avoidLayoutSafeArea 内容会钻栏下、仅 enableComponentSafeArea 栏高塌陷，双开缺一不可。
      style: scrollBlurTitleBarStyle(),
      content: {
        title: { mainTitle: $r('app.string.page_discover') },
        // 图标传纯 ResourceStr 由框架自适应着色；label 必填（溢出行只渲染 label）。
        menu: {
          value: [{
            content: {
              icon: $r('sys.symbol.gearshape'),
              label: $r('app.string.page_settings'),
              action: (): void => {
                this.pathStack.pushPathByName('settings', null);
              }
            }
          }]
        }
      },
      avoidLayoutSafeArea: true,
      enableComponentSafeArea: true
    })
    .bindToScrollable([this.scroller])
    .backgroundColor($r('sys.color.ohos_id_color_sub_background')) // 覆盖标题栏区域，防上下拼色
    // 从子页返回不重建页面，主机列表必须在 onShown 重新加载。
    .onShown(() => {
      this.refreshPairedHosts();
    })
  }

  @Builder
  progressView(label: Resource) {
    Column({ space: 16 }) {
      LoadingProgress()
        .width(48)
        .height(48)
        .color($r('sys.color.brand'))
      Text(label)
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor($r('sys.color.font_secondary'))
    }
    .width('100%')
    .justifyContent(FlexAlign.Center)
  }

  @Builder
  successView() {
    Column({ space: 12 }) {
      Text($r('app.string.pair_success'))
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontWeight(FontWeight.Bold)
        .fontColor($r('sys.color.font_primary'))
      Text($r('app.string.pair_success_hint'))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_secondary'))
    }
    .width('100%')
    .justifyContent(FlexAlign.Center)
  }

  @Builder
  failedView() {
    Column({ space: 16 }) {
      Text($r('app.string.pair_failed_title'))
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor($r('sys.color.font_primary'))
      Text(this.errorText())
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.warning'))
        .textAlign(TextAlign.Center)
        .width('100%')
      if (this.pairing.snapshot.errorHintKey.length > 0) {
        Text(this.resStr(this.pairing.snapshot.errorHintKey))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_secondary'))
          .textAlign(TextAlign.Center)
          .width('100%')
      }
      Button($r('app.string.discover_retry'))
        .onClick(() => {
          this.pairing.retry();
        })
    }
    .width('100%')
    .justifyContent(FlexAlign.Center)
  }

  @Builder
  listView() {
    Column({ space: 12 }) {
      if (this.pairedLoading || this.pairedError.length > 0 || this.pairedHosts.length > 0) {
        // 点行即切换默认主机（自动重连与快速连接随之跟随）。
        Column({ space: 8 }) {
          GroupHeader($r('app.string.settings_hosts_section'))
          PairedHostsSection({
            hosts: this.pairedHosts,
            loading: this.pairedLoading,
            errorText: this.pairedError,
            emptyLabel: $r('app.string.settings_hosts_empty'),
            onRetry: (): void => {
              this.refreshPairedHosts();
            },
            row: (h: HostInfo): void => {
              this.hostRow(h);
            }
          })
        }
        .alignItems(HorizontalAlign.Start)
        .width('100%')
      }
      if (this.pairing.snapshot.hosts.length > 0) {
        Row() {
          Text($r('app.string.discover_found_title'))
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.font_primary'))
          Blank()
          Button($r('app.string.discover_refresh'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .onClick(() => {
              this.pairing.startDiscovery();
            })
        }
        .width('100%')
        Column({ space: 8 }) {
          ForEach(this.pairing.snapshot.hosts, (h: DiscoveredHost) => {
            PressedRow({
              a11yText: `${h.name.length > 0 ? h.name : h.ip}，${this.hostInfo(h)}`
                + (this.pairing.isKnownHost(h) ? `，${this.resStr('discover_paired_tag')}` : ''),
              onActivated: (): void => {
                this.pairing.selectHost(h);
              }
            }) {
              Row({ space: 12 }) {
                SymbolGlyph($r('sys.symbol.computer'))
                  .fontSize(22)
                  .fontColor([$r('sys.color.icon_secondary')])
                Column({ space: 4 }) {
                  Text(h.name.length > 0 ? h.name : h.ip)
                    .fontSize($r('sys.float.ohos_id_text_size_body1'))
                    .fontColor($r('sys.color.font_primary'))
                    .maxLines(1)
                    .textOverflow({ overflow: TextOverflow.Ellipsis })
                  Text(this.hostInfo(h))
                    .fontSize($r('sys.float.ohos_id_text_size_body2'))
                    .fontColor($r('sys.color.font_secondary'))
                    .maxLines(1)
                    .textOverflow({ overflow: TextOverflow.Ellipsis })
                }
                .alignItems(HorizontalAlign.Start)
                .layoutWeight(1)
                // 本机有该主机 token：点击直接连，不再重新配对。
                if (this.pairing.isKnownHost(h)) {
                  Text($r('app.string.discover_paired_tag'))
                    .fontSize($r('sys.float.ohos_id_text_size_body3'))
                    .fontColor($r('sys.color.brand'))
                    .padding({ left: 8, right: 8, top: 2, bottom: 2 })
                    .borderRadius(10)
                    .backgroundColor($r('sys.color.comp_background_tertiary'))
                    .accessibilityLevel('no') // 行的播报已含该语义
                }
              }
              .width('100%')
              .alignItems(VerticalAlign.Center)
            }
          }, (h: DiscoveredHost) => `${h.ip}:${h.name}`)
        }
        .width('100%')
      } else {
        Column({ space: 12 }) {
          Text($r('app.string.discover_empty_title'))
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.font_primary'))
          Text($r('app.string.discover_empty_hint'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.font_secondary'))
            .textAlign(TextAlign.Center)
          Button($r('app.string.discover_refresh'))
            .onClick(() => {
              this.pairing.startDiscovery();
            })
          Text($r('app.string.discover_guide_entry'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.brand'))
            .onClick(() => {
              this.pathStack.pushPathByName('guide', null);
            })
        }
        .width('100%')
        .justifyContent(FlexAlign.Center)
      }
      // 调试入口：手工输入 IP 走与列表选中同一条 selectHost 流程（不新增协议路径）。
      if (this.settings.debugEnabled) {
        this.manualPairCard()
      }
    }
    .width('100%')
  }

  @Builder
  manualPairCard() {
    Column({ space: 8 }) {
      Text($r('app.string.debug_manual_pair_title'))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_secondary'))
      Row({ space: 8 }) {
        TextInput({ text: this.manualIp, placeholder: this.resStr('debug_manual_pair_ip_hint') })
          .height(40)
          .layoutWeight(1)
          .backgroundColor($r('sys.color.comp_background_tertiary'))
          .onChange((v: string) => {
            this.manualIp = v;
          })
        Button($r('app.string.debug_manual_pair_button'))
          .onClick(() => {
            this.onManualPair();
          })
      }
      .width('100%')
    }
    .width('100%')
    .alignItems(HorizontalAlign.Start)
  }

  private onManualPair(): void {
    const ip: string = this.manualIp.trim();
    if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(ip)) {
      this.getUIContext().getPromptAction().showToast({
        message: this.resStr('debug_manual_pair_ip_invalid'),
        duration: 2000
      });
      return;
    }
    const h: DiscoveredHost = new DiscoveredHost();
    h.ip = ip;
    h.port = DEFAULT_PORT;
    h.name = ip;
    this.pairing.selectHost(h);
  }

  private hostInfo(h: DiscoveredHost): string {
    return `${h.os} · daemon ${h.daemon} · ${this.resStr('discover_paired_count')} ${h.paired}`;
  }

  @Builder
  pairingView() {
    Column({ space: 16 }) {
      Text(this.pairing.snapshot.hostLabel)
        .fontSize($r('sys.float.ohos_id_text_size_body1'))
        .fontColor($r('sys.color.font_primary'))
        .maxLines(1)
        .textOverflow({ overflow: TextOverflow.Ellipsis })
      Text($r('app.string.pair_code_hint'))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_secondary'))
        .textAlign(TextAlign.Center)
        .width('100%')
      Stack() {
        Row({ space: 12 }) {
          this.codeSlot(0)
          this.codeSlot(1)
          this.codeSlot(2)
          this.codeSlot(3)
        }
        .width('100%')
        .constraintSize({ maxWidth: 260 })
        TextInput({ text: this.pairing.snapshot.code })
          .maxLength(4)
          .type(InputType.Number)
          .inputFilter('[0-9]')
          .opacity(0)
          .width('100%')
          .height(64)
          .defaultFocus(true)
          .accessibilityText($r('app.string.pair_code_input_a11y'))
          .onChange((v: string) => {
            if (v === this.pairing.snapshot.code) {
              return; // 受控组件程序化改值的同值回放守卫
            }
            this.pairing.setCode(v);
            if (v.length === 4) {
              this.pairing.submitCode(v);
            }
          })
      }
      .width('100%')

      if (this.errorText().length > 0) {
        Text(this.errorText())
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.warning'))
          .textAlign(TextAlign.Center)
          .width('100%')
      }
      Row({ space: 12 }) {
        Button($r('app.string.pair_reinitiate'))
          .buttonStyle(ButtonStyleMode.NORMAL)
          .onClick(() => {
            this.pairing.restartPairing();
          })
        Button($r('app.string.pair_cancel'))
          .buttonStyle(ButtonStyleMode.NORMAL)
          .onClick(() => {
            this.pairing.cancel();
          })
      }
    }
    .width('100%')
    .justifyContent(FlexAlign.Center)
  }
}
