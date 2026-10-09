// 页面公共小工具：按资源名取文案（getStringByNameSync；Resource 版 getStringSync 自 API20 废弃）。
// $r 需字面量，动态键（如 PAIR_NAK 文案键）统一走这里。
import type { UIContext } from '@kit.ArkUI';
import { ResError } from '../common/ResError';
import { ConnState } from '../common/net/ConnectionManager';

// 连接状态 → 文案资源键（$r 需字面量，动态键统一走这里；键表全仓单源）。
export function connStateKey(state: number): string {
  if (state === ConnState.CONNECTED) {
    return 'state_connected';
  }
  if (state === ConnState.RECONNECTING) {
    return 'state_reconnecting';
  }
  if (state === ConnState.LOST) {
    return 'state_lost';
  }
  if (state === ConnState.DISCOVERING) {
    return 'state_discovering';
  }
  if (state === ConnState.PAIRING) {
    return 'state_pairing';
  }
  return 'state_idle';
}

export function pageResStr(uiContext: UIContext, name: string): string {
  const ctx = uiContext.getHostContext();
  if (ctx === undefined) {
    return '';
  }
  return ctx.resourceManager.getStringByNameSync(name);
}

// 带参格式化（%1$s 按序替换）：ResError/快照内资源键的解析出口。
export function pageResFmt(uiContext: UIContext, name: string, params: string[]): string {
  const ctx = uiContext.getHostContext();
  if (ctx === undefined) {
    return '';
  }
  return ctx.resourceManager.getStringByNameSync(name, ...params);
}

// catch 统一出口：可本地化错误解析为当前语言文案；其余原样展示 message（系统错误码等）。
export function pageErrText(uiContext: UIContext, err: Error): string {
  if (err instanceof ResError) {
    return pageResFmt(uiContext, err.resKey, err.resParams);
  }
  return err.message;
}
