// 主机行公共件：名称 + IP + 可选徽标 + 可选 chevron；徽标语义由调用方决定（默认主机 / 使用中）。
import { PressedRow } from './PressedRow';

@ComponentV2
export struct HostRow {
  @Require @Param label: string;
  @Require @Param ip: string;
  @Param badge: Resource | null = null;
  @Param showChevron: boolean = false;
  @Param a11yExtra: string = '';
  @Param onRowActivated: () => void = () => {};

  build() {
    PressedRow({
      a11yText: `${this.label}，${this.ip}${this.a11yExtra}`,
      onActivated: (): void => {
        this.onRowActivated();
      }
    }) {
      Row({ space: 8 }) {
        Column({ space: 2 }) {
          Text(this.label)
            .fontSize($r('sys.float.ohos_id_text_size_body1'))
            .fontColor($r('sys.color.font_primary'))
            .maxLines(1)
            .textOverflow({ overflow: TextOverflow.Ellipsis })
          Text(this.ip)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.font_secondary'))
            .maxLines(1)
            .textOverflow({ overflow: TextOverflow.Ellipsis })
        }
        .alignItems(HorizontalAlign.Start)
        .layoutWeight(1)
        if (this.badge !== null) {
          Text(this.badge)
            .fontSize($r('sys.float.ohos_id_text_size_caption1'))
            .fontColor($r('sys.color.brand'))
            .padding({ left: 8, right: 8, top: 2, bottom: 2 })
            .borderRadius(10)
            .border({ width: 1, color: $r('sys.color.brand') })
        }
        if (this.showChevron) {
          SymbolGlyph($r('sys.symbol.chevron_forward'))
            .fontSize(20)
            .fontColor([$r('sys.color.icon_tertiary')])
        }
      }
      .width('100%')
      .alignItems(VerticalAlign.Center)
    }
  }
}
