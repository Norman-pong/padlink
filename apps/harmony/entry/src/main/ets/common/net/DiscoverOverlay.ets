// 发现浮层态规则：conn 只在断开态下挂 DISCOVERING，回收只认自写的态——不踩踏活动会话。
import { ConnState } from './ConnState';

export function canEnterDiscoverOverlay(state: number): boolean {
  return state === ConnState.IDLE || state === ConnState.LOST || state === ConnState.DISCOVERING;
}

export function shouldExitDiscoverOverlay(state: number): boolean {
  return state === ConnState.DISCOVERING;
}
