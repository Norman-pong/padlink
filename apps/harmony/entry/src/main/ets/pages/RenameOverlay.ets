// 重命名蒙层：编辑态自管，保存经 settings.renameHost；蒙层点击不关闭（防误触丢输入），仅按钮退出。
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import { validateHostName } from '../common/viewmodel/HostsStore';
import { pageResStr, pageErrText } from './Res';

const TOAST_MS: number = 2000;

@ComponentV2
export struct RenameOverlay {
  @Require @Param settings: SettingsModel;
  @Require @Param ip: string;
  @Require @Param port: number;
  @Param initialName: string = '';
  @Param onDone: () => void = () => {}; // 保存成功：父页关层并刷新

  @Local text: string = '';
  @Local error: string = '';

  aboutToAppear(): void {
    this.text = this.initialName;
  }

  // 名称上限 64B 对齐 daemon MaxNameLen。
  private save(): void {
    const name = validateHostName(this.text);
    if (name.length === 0) {
      this.error = pageResStr(this.getUIContext(), 'settings_host_name_invalid');
      return;
    }
    this.settings.renameHost(this.ip, this.port, name).then(() => {
      this.getUIContext().getPromptAction().showToast({
        message: pageResStr(this.getUIContext(), 'settings_host_renamed'),
        duration: TOAST_MS,
      });
      this.onDone();
    }).catch((err: Error) => {
      this.error = pageErrText(this.getUIContext(), err);
    });
  }

  build() {
    Column() {
      Column()
        .width('100%')
        .height('100%')
        .backgroundColor($r('sys.color.ohos_id_color_mask_regular'))
      Column({ space: 12 }) {
        Text($r('app.string.settings_host_rename'))
          .fontSize($r('sys.float.ohos_id_text_size_body1'))
          .fontWeight(FontWeight.Bold)
          .fontColor($r('sys.color.font_primary'))
        TextInput({ text: this.text, placeholder: pageResStr(this.getUIContext(), 'settings_host_name_hint') })
          .height(40)
          .maxLength(64)
          .defaultFocus(true)
          // 输入框必须用 tertiary：与弹层卡面同色会隐形。
          .backgroundColor($r('sys.color.comp_background_tertiary'))
          .onChange((v: string) => {
            this.text = v;
            this.error = '';
          })
        if (this.error.length > 0) {
          Text(this.error)
            .fontSize($r('sys.float.ohos_id_text_size_body3'))
            .fontColor($r('sys.color.warning'))
            .width('100%')
        }
        Row({ space: 12 }) {
          Button($r('app.string.pair_cancel'))
            .buttonStyle(ButtonStyleMode.NORMAL)
            .layoutWeight(1)
            .onClick(() => {
              this.onDone();
            })
          Button($r('app.string.settings_host_save'))
            .layoutWeight(1)
            .onClick(() => {
              this.save();
            })
        }
        .width('100%')
      }
      .padding(20)
      .borderRadius(16)
      .backgroundColor($r('sys.color.comp_background_primary'))
      .margin({ left: 32, right: 32 })
    }
    .width('100%')
    .height('100%')
    .justifyContent(FlexAlign.Center)
  }
}
