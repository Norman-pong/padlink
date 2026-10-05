// 按压行公共容器（D10/D13 第二处重复即抽）：触发动作的列表行（发现页快捷连接行/主机行）
// 统一按压态与卡面 token，一处定义全仓生效。
// 按压态走 onTouch 自管（motion.md §一）：DOWN 置位 / UP·CANCEL 复位 / onVisibleAreaChange
// 不可见强制复位——禁 stateStyles.pressed 裸用（按下触发导航、页面在 touch-up 前被遮盖时
// 复位事件概率性丢失 → 行永久灰底闩锁），禁 clickEffect（缩放回弹非按压底色反馈）。
// 底色 token 单源（D3 分组列表规范）：常态 comp_background_primary 卡面、按压
// interactive_pressed，随深浅色自动适配；内边距/圆角与全应用卡面规格同源。
@ComponentV2
export struct PressedRow {
  @Param onActivated: () => void = () => {};
  @Param a11yText: string = ''; // 行整体无障碍播报文案（硬约束 11）；空串回落读行内 Text
  @Local private pressed: boolean = false;
  // 官方 V1/V2 通用模式：@BuilderParam 缺省绑定本组件 @Builder（父组件始终经尾闭包传入）。
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
    // 兜底复位：行被导航/转场遮盖不可见时触摸 UP 可能已丢失，强制退出按压态防闩锁。
    // SDK 签名（common.d.ts，API13+ VisibleAreaChangeCallback）：(isExpanding, currentRatio)；
    // 仅在完全不可见（ratio 归 0 且收缩方向）时复位，避免滚动中误清仍在按压的行。
    .onVisibleAreaChange([0.0, 1.0], (isExpanding: boolean, currentRatio: number): void => {
      if (!isExpanding && currentRatio <= 0) {
        this.pressed = false;
      }
    })
    .accessibilityLevel('yes')
    .accessibilityText(this.a11yText)
  }
}
