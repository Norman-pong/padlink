// 安装发生在电脑上；指南与命令保持离线可用。
import { HdsNavDestination } from '@kit.UIDesignKit';
import { TabSegmentButtonV2, SegmentButtonV2Items } from '@kit.ArkUI';
import { pasteboard } from '@kit.BasicServicesKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { GroupCard, GroupHeader } from './GroupCard';
import { pageResStr, pageResFmt } from './Res';

const TAG: string = 'PadLink.Guide';
const DOMAIN: number = 0x5050;

@ComponentV2
export struct GuidePage {
  @Local private selectedSystem: number = 0;
  @Local private copyingKey: string = '';
  private scroller: Scroller = new Scroller();
  private systems: SegmentButtonV2Items = new SegmentButtonV2Items([
    { text: $r('app.string.guide_windows_title') },
    { text: $r('app.string.guide_macos_title') },
    { text: $r('app.string.guide_ubuntu_title') }
  ]);

  private resStr(name: string): string {
    return pageResStr(this.getUIContext(), name);
  }

  // 写系统粘贴板无需权限。
  private async copyCmd(key: string): Promise<void> {
    if (this.copyingKey !== '') {
      return;
    }
    this.copyingKey = key;
    try {
      const data: pasteboard.PasteData =
        pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, this.resStr(key));
      await pasteboard.getSystemPasteboard().setData(data);
      this.getUIContext().getPromptAction()
        .showToast({ message: this.resStr('guide_copied'), duration: 2000 });
    } catch (err) {
      const error = err as BusinessError;
      hilog.error(DOMAIN, TAG, 'pasteboard setData fail %{public}d %{public}s', error.code, error.message);
      this.getUIContext().getPromptAction()
        .showToast({ message: this.resStr('guide_copy_failed'), duration: 2000 });
    } finally {
      this.copyingKey = '';
    }
  }

  @Builder
  bodyText(res: Resource) {
    Text(res)
      .fontSize($r('sys.float.ohos_id_text_size_body2'))
      .fontColor($r('sys.color.font_secondary'))
      .width('100%')
  }

  @Builder
  commandBlock(label: Resource, key: string) {
    Column({ space: 8 }) {
      Row({ space: 8 }) {
        Text(label)
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_secondary'))
          .layoutWeight(1)
        Button(this.copyingKey === key ? $r('app.string.guide_copying') : $r('app.string.guide_copy'))
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .buttonStyle(ButtonStyleMode.NORMAL)
          .enabled(this.copyingKey === '')
          .accessibilityText(pageResFmt(this.getUIContext(), 'guide_copy_accessibility', [this.resStr(key)]))
          .onClick(() => {
            this.copyCmd(key);
          })
          .id('copy_' + key)
      }
      .width('100%')
      Text(this.resStr(key))
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_primary'))
        .wordBreak(WordBreak.BREAK_ALL)
        .width('100%')
        .id(key)
    }
    .padding($r('sys.float.padding_level6'))
    .width('100%')
    .borderRadius($r('sys.float.corner_radius_level4'))
    .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  build() {
    HdsNavDestination() {
      Scroll(this.scroller) {
        Column({ space: 16 }) {
          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_step1_title'))
            TabSegmentButtonV2({
              items: this.systems,
              selectedIndex: this.selectedSystem,
              $selectedIndex: (index: number): void => { this.selectedSystem = index; }
            })
            GroupCard() {
              if (this.selectedSystem === 0) {
                this.bodyText($r('app.string.guide_windows_terminal'))
                this.commandBlock($r('app.string.guide_install_label'), 'guide_windows_install_cmd')
                this.bodyText($r('app.string.guide_windows_finish'))
              } else if (this.selectedSystem === 1) {
                this.bodyText($r('app.string.guide_unix_terminal'))
                this.commandBlock($r('app.string.guide_install_label'), 'guide_install_cmd')
                this.bodyText($r('app.string.guide_macos_s1'))
              } else {
                this.bodyText($r('app.string.guide_unix_terminal'))
                this.commandBlock($r('app.string.guide_install_label'), 'guide_install_cmd')
                this.bodyText($r('app.string.guide_ubuntu_s1'))
                this.bodyText($r('app.string.guide_ubuntu_s2'))
                this.commandBlock($r('app.string.guide_voice_label'), 'guide_ubuntu_c1')
              }
              this.bodyText($r('app.string.guide_step1_hint'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_step3_title'))
            GroupCard() {
              this.bodyText($r('app.string.guide_step3_body'))
              this.commandBlock($r('app.string.guide_status_label'), 'guide_status_cmd')
              this.commandBlock($r('app.string.guide_pair_label'), 'guide_pair_cmd')
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_manage_title'))
            GroupCard() {
              this.commandBlock($r('app.string.guide_update_label'), 'guide_update_cmd')
              this.bodyText($r('app.string.guide_update_hint'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_trouble_title'))
            GroupCard() {
              this.bodyText($r('app.string.guide_trouble_1'))
              this.bodyText($r('app.string.guide_trouble_2'))
              if (this.selectedSystem === 0) {
                this.bodyText($r('app.string.guide_windows_firewall'))
              }
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')
        }
        .padding(16)
        .width('100%')
        .constraintSize({ maxWidth: 640 })
      }
      .width('100%')
      .height('100%')
      .scrollBar(BarState.Off)
      // 顶对齐：Scroll 默认居中会让短内容顶部悬空。
      .align(Alignment.Top)
      // 穿透到标题栏下方显影滚动模糊。
      .clip(false)
    }
    .titleBar({
      // 仅 avoidLayoutSafeArea 内容会钻栏下、仅 enableComponentSafeArea 栏高塌陷，双开缺一不可。
      style: scrollBlurTitleBarStyle(),
      content: {
        title: { mainTitle: $r('app.string.page_guide') }
      },
      avoidLayoutSafeArea: true,
      enableComponentSafeArea: true
    })
    .bindToScrollable([this.scroller])
    .backgroundColor($r('sys.color.ohos_id_color_sub_background'))
  }
}
