// 单报点输入模型：事件源把每次触摸回调转成样本注入引擎。
// tMs 由事件源注入（引擎不取时钟、不持定时器，超时判定用样本间 tMs 差惰性比较——可测性）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum（保证 node 可直跑）。

// 触摸样本事件类别。
export class TouchKind {
  public static readonly DOWN: number = 0;
  public static readonly MOVE: number = 1;
  public static readonly UP: number = 2;
  public static readonly CANCEL: number = 3;
}

// 单个指针的一次报点（x/y 单位 vp，tMs 为单调毫秒）。
export interface TouchSample {
  pointerId: number;
  x: number;
  y: number;
  kind: number;
  tMs: number;
}
