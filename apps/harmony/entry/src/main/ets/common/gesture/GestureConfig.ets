// 手势判定可配参数（判定阈值表 + 加速曲线拐点）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum（保证 node 可直跑）。

export class GestureDefaults {
  // 可调范围 0.5–2.0
  public static readonly SENSITIVITY: number = 1.2;
  public static readonly NATURAL_SCROLL: boolean = true;
  // 1vp → N 个 HI_RES 单位；可调 8–15
  public static readonly SCROLL_UNITS_PER_PIXEL: number = 10;
  public static readonly SCROLL_UNITS_MIN: number = 8;
  public static readonly SCROLL_UNITS_MAX: number = 15;
  public static readonly TAP_MAX_MS: number = 150;
  public static readonly TAP_MAX_VP: number = 8;
  public static readonly TWO_FINGER_TAP_MAX_MS: number = 40;
  public static readonly LONG_PRESS_MS: number = 300;
  // 双击后拖拽窗口：轻点抬起后此时段内同位置再按下并移动 = 按住左键拖拽
  public static readonly DOUBLE_TAP_DRAG_MS: number = 300;
  // 抬指惯性滑行参数；三个速度门限单位均为 vp/ms（手指位移速度，经 scrollUnitsPerPixel 换算）
  public static readonly FLING_MIN_VP_PER_MS: number = 0.15;
  public static readonly FLING_MAX_VP_PER_MS: number = 1.2;
  public static readonly FLING_STOP_VP_PER_MS: number = 0.03;
  public static readonly FLING_TAU_MS: number = 300;
  public static readonly FLING_MAX_MS: number = 900;
  // 加速曲线拐点（vp/ms）：≤v1 增益 1.0；v1..v2 线性升至 maxGain
  public static readonly ACCEL_V1: number = 0.2;
  public static readonly ACCEL_V2: number = 1.2;
  public static readonly ACCEL_MAX_GAIN: number = 2.5;
}

// 运行时配置（判定器只读；设置页修改后整体替换注入）。
export interface GestureConfig {
  sensitivity: number;
  naturalScroll: boolean;
  scrollUnitsPerPixel: number;
  tapMaxMs: number;
  tapMaxVp: number;
  twoFingerTapMaxMs: number;
  longPressMs: number;
  doubleTapDragMs: number;
  flingMinVpPerMs: number;
  flingMaxVpPerMs: number;
  flingStopVpPerMs: number;
  flingTauMs: number;
  flingMaxMs: number;
  accelV1: number;
  accelV2: number;
  accelMaxGain: number;
}

export function defaultGestureConfig(): GestureConfig {
  const cfg: GestureConfig = {
    sensitivity: GestureDefaults.SENSITIVITY,
    naturalScroll: GestureDefaults.NATURAL_SCROLL,
    scrollUnitsPerPixel: GestureDefaults.SCROLL_UNITS_PER_PIXEL,
    tapMaxMs: GestureDefaults.TAP_MAX_MS,
    tapMaxVp: GestureDefaults.TAP_MAX_VP,
    twoFingerTapMaxMs: GestureDefaults.TWO_FINGER_TAP_MAX_MS,
    longPressMs: GestureDefaults.LONG_PRESS_MS,
    doubleTapDragMs: GestureDefaults.DOUBLE_TAP_DRAG_MS,
    flingMinVpPerMs: GestureDefaults.FLING_MIN_VP_PER_MS,
    flingMaxVpPerMs: GestureDefaults.FLING_MAX_VP_PER_MS,
    flingStopVpPerMs: GestureDefaults.FLING_STOP_VP_PER_MS,
    flingTauMs: GestureDefaults.FLING_TAU_MS,
    flingMaxMs: GestureDefaults.FLING_MAX_MS,
    accelV1: GestureDefaults.ACCEL_V1,
    accelV2: GestureDefaults.ACCEL_V2,
    accelMaxGain: GestureDefaults.ACCEL_MAX_GAIN,
  };
  return cfg;
}
