// 服务端安装向导（GuidePage）：把「手机装了 APP、电脑没装服务端」的用户一步步带到可配对——
// 电脑浏览器打开下载页 → 按系统安装启动 → 回手机重扫配对；附「装好了还搜不到」排查。
// 纯展示页：无状态、无权限、无网络依赖（安装发生在电脑上，指南必须离线可读）。
// 骨架与 DebugPanelPage 同构：HdsNavDestination + 滚动模糊标题栏（单源 HdsTitleBar.ets）。
import { HdsNavDestination } from '@kit.UIDesignKit';
import { scrollBlurTitleBarStyle } from './HdsTitleBar';
import { GroupCard, GroupHeader } from './GroupCard';

@ComponentV2
export struct GuidePage {
  private scroller: Scroller = new Scroller();

  // 说明行
  @Builder
  bodyText(res: Resource) {
    Text(res)
      .fontSize($r('sys.float.ohos_id_text_size_body2'))
      .fontColor($r('sys.color.font_secondary'))
      .width('100%')
  }

  // 命令块（在电脑上敲的命令，等宽观感由底色块承担，不引入字体依赖）
  @Builder
  codeBlock(res: Resource) {
    Text(res)
      .fontSize($r('sys.float.ohos_id_text_size_body2'))
      .fontColor($r('sys.color.font_primary'))
      .padding(10)
      .width('100%')
      .borderRadius(8)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  build() {
    HdsNavDestination() {
      Scroll(this.scroller) {
        Column({ space: 16 }) {
          // ---- 第 1 步：电脑浏览器打开下载页 ----
          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_step1_title'))
            GroupCard() {
              this.bodyText($r('app.string.guide_step1_body'))
              // 下载地址：用户在电脑上手敲，大字居中单独成块
              Text($r('app.string.guide_download_url'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontWeight(FontWeight.Medium)
                .fontColor($r('sys.color.font_primary'))
                .textAlign(TextAlign.Center)
                .padding(12)
                .width('100%')
                .borderRadius(8)
                .backgroundColor($r('sys.color.comp_background_tertiary'))
              this.bodyText($r('app.string.guide_step1_hint'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          // ---- 第 2 步：按电脑系统安装并启动 ----
          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_step2_title'))
            GroupCard() {
              Text($r('app.string.guide_ubuntu_title'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontWeight(FontWeight.Medium)
                .fontColor($r('sys.color.font_primary'))
                .width('100%')
              this.bodyText($r('app.string.guide_ubuntu_s1'))
              this.codeBlock($r('app.string.guide_ubuntu_c1'))
              this.bodyText($r('app.string.guide_ubuntu_s2'))
              this.bodyText($r('app.string.guide_ubuntu_s3'))
              this.codeBlock($r('app.string.guide_ubuntu_c2'))
              this.bodyText($r('app.string.guide_ubuntu_s4'))
              this.codeBlock($r('app.string.guide_ubuntu_c3'))
            }
            GroupCard() {
              Text($r('app.string.guide_macos_title'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontWeight(FontWeight.Medium)
                .fontColor($r('sys.color.font_primary'))
                .width('100%')
              this.bodyText($r('app.string.guide_macos_s1'))
              this.bodyText($r('app.string.guide_macos_s2'))
              this.codeBlock($r('app.string.guide_macos_c1'))
              this.bodyText($r('app.string.guide_macos_s3'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          // ---- 第 3 步：回手机连接 ----
          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_step3_title'))
            GroupCard() {
              this.bodyText($r('app.string.guide_step3_body'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          // ---- 排查：装好了还是搜不到 ----
          Column({ space: 8 }) {
            GroupHeader($r('app.string.guide_trouble_title'))
            GroupCard() {
              this.bodyText($r('app.string.guide_trouble_1'))
              this.bodyText($r('app.string.guide_trouble_2'))
              this.bodyText($r('app.string.guide_trouble_3'))
              this.bodyText($r('app.string.guide_trouble_4'))
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
      // 内容短于视口时顶对齐：Scroll 默认居中会让顶部悬空一段空白
      .align(Alignment.Top)
      // 穿透到标题栏下方显影滚动模糊（ui-design-navigation-dynamic-blur 官方模式）
      .clip(false)
    }
    .titleBar({
      // 主控页常驻 layoutFullScreen，子页窗口实为全屏形态；两开关分别管标题栏避让状态栏
      // 与内容避让标题栏，缺一不可（MateX7 实测）。
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
