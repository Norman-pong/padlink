// 指针加速与 vp→计数换算：慢速 1:1、快速最高 maxGain（分段线性）。
// 纯函数无内部状态；亚像素残差由引擎持有并经入参/出参回传。
import type { GestureConfig } from './GestureConfig';

// dx/dy 为本次应发的整数计数；resX/resY 为引擎保管的亚像素残差。
export interface AccelResult {
  dx: number;
  dy: number;
  resX: number;
  resY: number;
}

export function gain(vVpMs: number, cfg: GestureConfig): number {
  if (vVpMs <= cfg.accelV1) {
    return 1.0;
  }
  if (vVpMs >= cfg.accelV2) {
    return cfg.accelMaxGain;
  }
  const t: number = (vVpMs - cfg.accelV1) / (cfg.accelV2 - cfg.accelV1);
  return 1.0 + t * (cfg.accelMaxGain - 1.0);
}

// resXIn/resYIn 为上次残差，出参带回新残差（防慢速丢位移）。
// dtMs ≤ 0（同帧重复报点）按 1ms 计；速度取位移向量模长，方向无关。
export function toCounts(dVpX: number, dVpY: number, dtMs: number, cfg: GestureConfig,
  resXIn: number, resYIn: number): AccelResult {
  const dt: number = dtMs > 0 ? dtMs : 1;
  const speed: number = Math.hypot(dVpX, dVpY) / dt;
  const g: number = gain(speed, cfg);
  const effX: number = dVpX * cfg.sensitivity * g + resXIn;
  const effY: number = dVpY * cfg.sensitivity * g + resYIn;
  const dx: number = Math.round(effX);
  const dy: number = Math.round(effY);
  const res: AccelResult = { dx: dx, dy: dy, resX: effX - dx, resY: effY - dy };
  return res;
}
