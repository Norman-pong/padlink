// 已配对主机卡（设置页与发现与配对页共用，第二处重复即抽组件）。
// 只产四态内容（加载/失败/空态/列表），组头由调用方用 GroupHeader 承担；
// 卡面规格与 GroupCard 同源（12 内边距/12 圆角/comp_background_primary）。
// 行内容走 @BuilderParam 槽位：设置页给"打开操作单"的行，发现页给"点击切换"的行。
// 槽位传参只认「箭头裹 @Builder 调用」形态（`row: (h) => { this.hostRow(h) }`），
// 直写 DSL 或直传方法引用都会在运行时崩（HDS 红线）。
import { HostInfo } from '../common/viewmodel/HostsStore';

@ComponentV2
export struct PairedHostsSection {
  @Require @Param hosts: HostInfo[];
  @Require @Param loading: boolean;
  @Require @Param errorText: string;
  @Require @Param emptyLabel: Resource;
  @Require @Param onRetry: () => void;
  @BuilderParam row: (h: HostInfo) => void = this.emptyRow;

  @Builder
  private emptyRow(h: HostInfo) {
  }

  build() {
    Column({ space: 8 }) {
      if (this.loading) {
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
      } else if (this.errorText.length > 0) {
        // 失败态：给原因 + 重试
        Column({ space: 8 }) {
          Text($r('app.string.settings_hosts_load_fail'))
            .fontSize($r('sys.float.ohos_id_text_size_body2'))
            .fontColor($r('sys.color.warning'))
          Text(this.errorText)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.font_secondary'))
          Button($r('app.string.discover_retry'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .onClick(() => {
              this.onRetry();
            })
        }
        .alignItems(HorizontalAlign.Start)
        .width('100%')
        .padding(12)
        .borderRadius(12)
        .backgroundColor($r('sys.color.comp_background_primary'))
      } else if (this.hosts.length === 0) {
        // 空态
        Text(this.emptyLabel)
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
            this.row(h);
          }, (h: HostInfo) => `${h.ip}:${h.port}`)
        }
        .width('100%')
      }
    }
    .alignItems(HorizontalAlign.Start)
    .width('100%')
  }
}
