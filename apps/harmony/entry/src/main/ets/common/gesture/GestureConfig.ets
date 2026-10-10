// 手势判定可配参数（PRD §4.2 判定阈值表 + 加速曲线拐点）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum（保证 node 可直跑）。

// 默认值常量（与 PRD §4.2 表一致）。
export class GestureDefaults {
  // 光标灵敏度（PRD 设置范围 0.5–2.0）
  public static readonly SENSITIVITY: number = 1.2;
  // 自然滚动：手指下滑 → 内容随手指下移（视图上滚）
  public static readonly NATURAL_SCROLL: boolean = true;
  // 滚动换算：1vp 手指位移 → N 个 REL_WHEEL_HI_RES 单位，可调范围 8–15
  public static readonly SCROLL_UNITS_PER_PIXEL: number = 10;
  public static readonly SCROLL_UNITS_MIN: number = 8;
  public static readonly SCROLL_UNITS_MAX: number = 15;
  // 单指轻点：按下 ≤150ms 且总位移 ≤8vp
  public static readonly TAP_MAX_MS: number = 150;
  public static readonly TAP_MAX_VP: number = 8;
  // 双指轻点：两指按下时间差 ≤40ms
  public static readonly TWO_FINGER_TAP_MAX_MS: number = 40;
  // 长按触发：按下 ≥300ms 且位移 ≤8vp
  public static readonly LONG_PRESS_MS: number = 300;
  // 双击后拖拽（tap-and-drag）：轻点抬起后在此窗口内同位置再次按下并移动 = 按住左键拖拽
  // （第二次轻点抬起即双击；按住拖动即"双击后拖拽"，与真实触摸板一致）
  public static readonly DOUBLE_TAP_DRAG_MS: number = 300;
  // 抬指惯性滑行（动量滚动）：速度估计与衰减参数（真机手感待校准）
  // 触发下限/初速上限/结束下限单位均为 vp/ms（手指位移速度，经 scrollUnitsPerPixel 换算）
  public static readonly FLING_MIN_VP_PER_MS: number = 0.15;
  public static readonly FLING_MAX_VP_PER_MS: number = 1.2;
  public static readonly FLING_STOP_VP_PER_MS: number = 0.03;
  public static readonly FLING_TAU_MS: number = 300;
  public static readonly FLING_MAX_MS: number = 900;
  // 加速曲线（分段线性，单位 vp/ms）：≤v1 增益 1.0；v1..v2 线性升至 maxGain；>v2 恒 maxGain
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

// 默认配置工厂。
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
