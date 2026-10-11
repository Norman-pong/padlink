// 页面公共小工具：按资源名取文案（getStringByNameSync；Resource 版 getStringSync 自 API20 废弃）。
// $r 需字面量，动态键（如 PAIR_NAK 文案键）统一走这里。
import type { UIContext } from '@kit.ArkUI';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ResError } from '../common/ResError';
import { ConnState } from '../common/net/ConnectionManager';

const TAG: string = 'PadLink.Res';
const DOMAIN: number = 0x5050;

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
// 参数先归一为字符串：动态来源（socket 回调的 message、可选参数）可能是 undefined，
// 资源框架遇到非字符串参数抛 9001008（占位符类型不符）。本函数在 build 内被调用，
// 抛出即 JS 致命错误杀掉进程（实测：前台恢复渲染时闪退），故格式化失败降级为
// 「无参原文 + 日志」，绝不向上抛；参数缺失一律落为空串，不显示 undefined。
export function pageResFmt(uiContext: UIContext, name: string, params: string[]): string {
  const ctx = uiContext.getHostContext();
  if (ctx === undefined) {
    return '';
  }
  const args: string[] = params.map((p: string) => String(p ?? ''));
  try {
    return ctx.resourceManager.getStringByNameSync(name, ...args);
  } catch (err) {
    const e = err as BusinessError;
    hilog.warn(DOMAIN, TAG, 'fmt %{public}s fail: %{public}s',
      name, e.message !== undefined ? e.message : '');
    try {
      return ctx.resourceManager.getStringByNameSync(name);
    } catch (err2) {
      return name; // 键不存在：显示键名，保留可定位性
    }
  }
}

// catch 统一出口：可本地化错误解析为当前语言文案；其余原样展示 message（系统错误码等）。
export function pageErrText(uiContext: UIContext, err: Error): string {
  if (err instanceof ResError) {
    return pageResFmt(uiContext, err.resKey, err.resParams);
  }
  return String(err.message ?? '');
}
