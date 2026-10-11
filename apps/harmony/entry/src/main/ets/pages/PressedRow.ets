// 按压行公共容器：触发动作的列表行统一走此件，卡面 token 单源。
// 按压态必须 onTouch 自管三处置——DOWN 置位 / UP·CANCEL 复位 / 不可见强制复位：禁裸用
// stateStyles.pressed（按下触发导航、页面在 touch-up 前被遮盖时复位事件概率性丢失 → 行永久
// 灰底闩锁），禁 clickEffect（缩放回弹非按压底色反馈）。
@ComponentV2
export struct PressedRow {
  @Param onActivated: () => void = () => {};
  @Param a11yText: string = '';
  @Local private pressed: boolean = false;
  @BuilderParam content: () => void = this.empty;

  @Builder
  private empty() {
  }

  build() {
    Column() {
      this.content()
    }
    .alignItems(HorizontalAlign.Start)
    .width('100%')
    .padding(12)
    .borderRadius(12)
    .backgroundColor(this.pressed
      ? $r('sys.color.interactive_pressed') : $r('sys.color.comp_background_primary'))
    .onClick(() => {
      this.onActivated();
    })
    .onTouch((ev: TouchEvent): void => {
      if (ev.type === TouchType.Down) {
        this.pressed = true;
      }
      if (ev.type === TouchType.Up || ev.type === TouchType.Cancel) {
        this.pressed = false;
      }
    })
    // 兜底复位：行被导航/转场遮盖不可见时 UP 可能已丢失，强制退出按压态防闩锁；
    // 仅在完全不可见（收缩方向且 ratio ≤ 0）时复位，避免滚动中误清仍在按压的行。
    .onVisibleAreaChange([0.0, 1.0], (isExpanding: boolean, currentRatio: number): void => {
      if (!isExpanding && currentRatio <= 0) {
        this.pressed = false;
      }
    })
    .accessibilityLevel('yes')
    .accessibilityText(this.a11yText)
  }
}
