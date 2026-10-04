// 发现与配对页（FR-0 占位）：M2 接入 UDP 广播发现 + 配对码流程；
// 当前提供手动连接入口（跳调试面板的手动连接区，M1 联调用）。
@ComponentV2
export struct DiscoverPage {
  @Require @Param pathStack: NavPathStack;

  build() {
    NavDestination() {
      Column({ space: 24 }) {
        Text($r('app.string.discover_placeholder'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontColor($r('sys.color.font_secondary'))
          .textAlign(TextAlign.Center)
          .width('100%')
        Button($r('app.string.manual_connect'))
          .onClick(() => {
            this.pathStack.pushPathByName('debug', null);
          })
      }
      .width('100%')
      .height('100%')
      .justifyContent(FlexAlign.Center)
      .padding(24)
    }
    .title($r('app.string.page_discover'))
  }
}
