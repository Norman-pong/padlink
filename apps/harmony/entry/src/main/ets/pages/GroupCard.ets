// 静态分组卡容器：卡面样式单源，可点击行改用 PressedRow。
@ComponentV2
export struct GroupCard {
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

@Builder
export function GroupHeader(label: Resource) {
  Text(label)
    .fontSize($r('sys.float.ohos_id_text_size_caption1'))
    .fontColor($r('sys.color.font_secondary'))
    .margin({ left: 4 })
}
