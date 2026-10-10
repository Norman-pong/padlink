// 发现与配对页（FR-0）：进入自动 UDP 广播发现 → 主机列表（名称/os/daemon 版/已配对数）→
// 选中连接 → 大号 4 位确认码输入（输满自动提交）→ PAIR_OK 保存 lastHost 回主控页。
// 四态齐备（发现中/列表/空态/失败重试），全程无白屏无假死；已有 lastHost 时页顶给快速连接入口。
// UI 文案全部走 string.json（中英双份，lore 约束）；PAIR_NAK 文案键由 PairingCore 映射。
// HDS（D5）：HdsNavDestination 根容器 + 滚动模糊标题栏（样式单源见 HdsTitleBar.ets）+
// bindToScrollable 绑定主机列表 Scroller；内容区在标题栏下方（enableComponentSafeArea 承担
// 状态栏避让），列表 clip(false)+cachedCount 让滚动内容穿透到标题栏下方显影模糊
// （ui-design-navigation-dynamic-blur 官方模式）。页底色/卡面 token 对齐 D3 分组列表规范。
import { HdsNavDestination } from '@kit.UIDesignKit';
import { DEFAULT_PORT, PairPhase } from '../common/viewmodel/PairingModel';
import type { PairingModel } from '../common/viewmodel/PairingModel';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import { DiscoveredHost } from '../common/net/DiscoverCore';
import { pageResStr, pageResFmt } from './Res';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { PressedRow } from './PressedRow';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { enterKeyboardAvoidResize, restoreKeyboardAvoidMode } from './KeyboardAvoid';

const PAIRED_STAY_MS: number = 800; // 成功提示停留时长，之后返回主控页

@ComponentV2
export struct DiscoverPage {
  @Require @Param pathStack: NavPathStack;
  @Require @Param pairing: PairingModel;
  @Require @Param settings: SettingsModel;

  @Local manualIp: string = ''; // 调试手动配对的 IP 输入镜像

  private scroller: Scroller = new Scroller(); // 主机列表滚动器（标题栏滚动模糊联动）
  private kbPrev: KeyboardAvoidMode = KeyboardAvoidMode.OFFSET; // 进入页面前避让模式（退出恢复）
  private popTimer: number = -1;

  aboutToAppear(): void {
    // 确认码 TextInput + 下方按钮：RESIZE 键盘避让（faqs-arkui-1073）
    this.kbPrev = enterKeyboardAvoidResize(this.getUIContext());
    this.pairing.onPaired = (): void => {
      this.schedulePop();
    };
    // 配对成功但本机主机列表已满（4 台）未入库：轻提示引导先到设置删除（连接本身不受影响）
    this.pairing.onHostsFull = (): void => {
      this.getUIContext().getPromptAction().showToast({
        message: this.resStr('pair_hosts_full'),
        duration: 2000,
      });
    };
    this.pairing.onPageEnter();
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

  // 配对/快连成功：短暂停留后回主控页（CONNECTED 心跳已由 ConnectionManager 启动）。
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

  // 错误展示：静态键（PAIR_NAK 映射）优先，其次动态文案。
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
      .width(56)
      .height(64)
      .fontSize(32)
      .fontWeight(FontWeight.Bold)
      .textAlign(TextAlign.Center)
      .borderRadius(12)
      .border({ width: 1.5, color: this.pairing.snapshot.code.length === index
        ? $r('sys.color.brand') : $r('sys.color.comp_divider') })
      .backgroundColor($r('sys.color.comp_background_tertiary'))
      .accessibilityLevel('no') // 数字格的视觉镜像，读屏由覆盖其上的 TextInput 统一承担
  }

  build() {
    HdsNavDestination() {
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
      .height('100%')
      .padding(16)
    }
    .titleBar({
      // 主控页常驻 layoutFullScreen，子页窗口实为全屏形态；两开关分别管标题栏避让状态栏
      // 与内容避让标题栏，缺一不可（MateX7 实测）。
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
    .backgroundColor($r('sys.color.ohos_id_color_sub_background')) // D3 分组列表页底色（覆盖标题栏区域，防上下拼色）
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
    .layoutWeight(1)
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
    .layoutWeight(1)
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
    .layoutWeight(1)
    .justifyContent(FlexAlign.Center)
  }

  @Builder
  listView() {
    Column({ space: 12 }) {
      if (this.pairing.snapshot.quickLabel.length > 0) {
        // 快速连接入口（lastHost 已有 token）：链接符号 + 标题 + 主机标签，点击直连（PressedRow 按压态 + a11y）
        // 动作行不挂 chevron：› 是导航语义，点击发起连接是动作语义（设置页入口行才是真导航）
        PressedRow({
          a11yText: `${this.resStr('discover_quick')}，${this.pairing.snapshot.quickLabel}`,
          onActivated: (): void => {
            this.pairing.quickConnect();
          }
        }) {
          Row({ space: 12 }) {
            SymbolGlyph($r('sys.symbol.link'))
              .fontSize(22)
              .fontColor([$r('sys.color.brand')])
            Column({ space: 4 }) {
              Text($r('app.string.discover_quick'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
              Text(this.pairing.snapshot.quickLabel)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
                .maxLines(1)
                .textOverflow({ overflow: TextOverflow.Ellipsis })
            }
            .alignItems(HorizontalAlign.Start)
            .layoutWeight(1)
          }
          .width('100%')
          .alignItems(VerticalAlign.Center)
        }
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
        List({ space: 8, scroller: this.scroller }) {
          ForEach(this.pairing.snapshot.hosts, (h: DiscoveredHost) => {
            ListItem() {
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
                  // 已配对标注：本机有该主机 token，点击直接用 token 连接（不再重新配对）
                  if (this.pairing.isKnownHost(h)) {
                    Text($r('app.string.discover_paired_tag'))
                      .fontSize($r('sys.float.ohos_id_text_size_body3'))
                      .fontColor($r('sys.color.brand'))
                      .padding({ left: 8, right: 8, top: 2, bottom: 2 })
                      .borderRadius(10)
                      .backgroundColor($r('sys.color.comp_background_tertiary'))
                      .accessibilityLevel('no') // 行的无障碍播报已含该语义
                  }
                }
                .width('100%')
                .alignItems(VerticalAlign.Center)
              }
            }
          }, (h: DiscoveredHost) => h.ip)
        }
        .width('100%')
        .layoutWeight(1)
        // 穿透到标题栏下方显影滚动模糊（ui-design-navigation-dynamic-blur）：
        // 关闭列表裁剪让滚出视口的内容仍渲染进栏区；cachedCount 保穿透项不因虚拟化消失。
        .clip(false)
        .cachedCount(3, true)
      } else {
        // 空态：无主机 + 排查提示 + 重扫；首次用户大概率是电脑没装服务端 → 安装向导入口
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
        .layoutWeight(1)
        .justifyContent(FlexAlign.Center)
      }
      // 调试入口（设置页开启调试面板后可见）：模拟器 NAT/AP 隔离等广播不可达场景，
      // 手工输入主机 IP 走与列表选中完全一致的配对流程（selectHost 复用，不新增协议路径）。
      if (this.settings.debugEnabled) {
        this.manualPairCard()
      }
    }
    .width('100%')
    .layoutWeight(1)
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

  // 手动配对：宽松校验点分四段（调试工具，连接错误由配对失败态兜底），构造合成
  // DiscoveredHost 走既有 selectHost 流程。
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

  // 主机行副文案：os · daemon 版本 · 已配对数（展示与无障碍播报共用）。
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
      // 4 格确认码：视觉格子（装饰，读屏隐藏）+ 覆盖透明 TextInput 承接数字键盘，输满自动提交
      Stack() {
        Row({ space: 12 }) {
          this.codeSlot(0)
          this.codeSlot(1)
          this.codeSlot(2)
          this.codeSlot(3)
        }
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
              return; // 受控组件程序化改值后的同值回放守卫（硬约束 19）
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
    .layoutWeight(1)
    .justifyContent(FlexAlign.Center)
  }
}
