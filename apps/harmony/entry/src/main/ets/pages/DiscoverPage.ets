// 发现与配对页（FR-0）：进入自动 UDP 广播发现 → 主机列表（名称/os/daemon 版/已配对数）→
// 选中连接 → 大号 4 位确认码输入（输满自动提交）→ PAIR_OK 保存 lastHost 回主控页。
// 四态齐备（发现中/列表/空态/失败重试），全程无白屏无假死；已有 lastHost 时页顶给快速连接入口。
// UI 文案全部走 string.json（中英双份，lore 约束）；PAIR_NAK 文案键由 PairingCore 映射。
import { PairPhase } from '../common/viewmodel/PairingModel';
import type { PairingModel } from '../common/viewmodel/PairingModel';
import type { DiscoveredHost } from '../common/net/DiscoverCore';
import { pageResStr } from './Res';

const PAIRED_STAY_MS: number = 800; // 成功提示停留时长，之后返回主控页

@ComponentV2
export struct DiscoverPage {
  @Require @Param pathStack: NavPathStack;
  @Require @Param pairing: PairingModel;

  private popTimer: number = -1;

  aboutToAppear(): void {
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
      return this.resStr(snap.errorKey);
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
  }

  build() {
    NavDestination() {
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
        // 设置页入口（FR-5）：主机管理/灵敏度/调试开关的可达路径（此前 settings 已注册但无入口）。
        Row() {
          Blank()
          Text($r('app.string.page_settings'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.font_emphasize'))
            .onClick(() => {
              this.pathStack.pushPathByName('settings', null);
            })
        }
        .width('100%')
      }
      .width('100%')
      .height('100%')
      .padding(16)
    }
    .title($r('app.string.page_discover'))
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
        .fontColor($r('sys.color.font_emphasize'))
        .textAlign(TextAlign.Center)
        .width('100%')
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
        // 快速连接入口（lastHost 已有 token）：标题 + 主机标签，点击直连
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
        .width('100%')
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_tertiary'))
        .onClick(() => {
          this.pairing.quickConnect();
        })
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
        List({ space: 8 }) {
          ForEach(this.pairing.snapshot.hosts, (h: DiscoveredHost) => {
            ListItem() {
              Column({ space: 4 }) {
                Text(h.name.length > 0 ? h.name : h.ip)
                  .fontSize($r('sys.float.ohos_id_text_size_body1'))
                  .fontColor($r('sys.color.font_primary'))
                  .maxLines(1)
                  .textOverflow({ overflow: TextOverflow.Ellipsis })
                Text(`${h.os} · daemon ${h.daemon} · ${this.resStr('discover_paired_count')} ${h.paired}`)
                  .fontSize($r('sys.float.ohos_id_text_size_body2'))
                  .fontColor($r('sys.color.font_secondary'))
                  .maxLines(1)
                  .textOverflow({ overflow: TextOverflow.Ellipsis })
              }
              .alignItems(HorizontalAlign.Start)
              .width('100%')
              .padding(12)
              .borderRadius(12)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
              .onClick(() => {
                this.pairing.selectHost(h);
              })
            }
          }, (h: DiscoveredHost) => h.ip)
        }
        .layoutWeight(1)
        .width('100%')
      } else {
        // 空态：无主机 + 排查提示 + 重扫
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
        }
        .width('100%')
        .layoutWeight(1)
        .justifyContent(FlexAlign.Center)
      }
    }
    .width('100%')
    .layoutWeight(1)
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
      // 4 格确认码：视觉格子 + 覆盖透明 TextInput 承接数字键盘，输满自动提交
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
          .onChange((v: string) => {
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
          .fontColor($r('sys.color.font_emphasize'))
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
