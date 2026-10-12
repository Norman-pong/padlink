// 已配对主机卡：设置页与发现页共用，只产四态内容（加载/失败/空态/列表），组头由调用方给。
// @BuilderParam 槽位只认「箭头裹 @Builder 调用」形态（`row: (h) => { this.hostRow(h) }`），
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
        Text(this.emptyLabel)
          .fontSize($r('sys.float.ohos_id_text_size_body2'))
          .fontColor($r('sys.color.font_secondary'))
          .width('100%')
          .padding(12)
          .borderRadius(12)
          .backgroundColor($r('sys.color.comp_background_primary'))
      } else {
        Column({ space: 8 }) {
          // key 必须含 name：重命名后仅 name 变化，key 不变则 ForEach 复用旧节点、列表不刷新。
          ForEach(this.hosts, (h: HostInfo) => {
            this.row(h);
          }, (h: HostInfo) => `${h.ip}:${h.port}:${h.name}`)
        }
        .width('100%')
      }
    }
    .alignItems(HorizontalAlign.Start)
    .width('100%')
  }
}
