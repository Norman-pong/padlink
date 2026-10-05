// 静态分组卡公共容器（D3 分组列表规范）：卡面 comp_background_primary + 圆角/内边距单源，
// 设置页与调试面板共用（第二处重复即抽）。与 PressedRow 分工：触发动作的列表行用 PressedRow，
// 纯展示/表单分组用它。组头 GroupHeader 与卡配套：caption1 + 次级色 + 左缩进对齐卡内文字。
@ComponentV2
export struct GroupCard {
  // 官方 V1/V2 通用模式：@BuilderParam 缺省绑定本组件 @Builder（父组件始终经尾闭包传入）。
  @BuilderParam content: () => void = this.empty;

  @Builder
  private empty() {
  }

  build() {
    Column({ space: 8 }) {
      this.content()
    }
    .alignItems(HorizontalAlign.Start)
    .width('100%')
    .padding(12)
    .borderRadius(12)
    .backgroundColor($r('sys.color.comp_background_primary'))
  }
}

// 组头（D3）：卡在组头正下，左缩进 4vp 与卡内 12vp 内边距下的文字形成视觉对齐。
@Builder
export function GroupHeader(label: Resource) {
  Text(label)
    .fontSize($r('sys.float.ohos_id_text_size_caption1'))
    .fontColor($r('sys.color.font_secondary'))
    .margin({ left: 4 })
}
