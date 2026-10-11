// 页面公共小工具：动态资源键取文案（$r 需字面量，动态键统一走这里）；
// getStringSync(Resource) 自 API20 废弃，用 getStringByNameSync。
// 资源纪律：色值/字号一律系统 token，用户可见文案必须进 string.json（base/zh_CN/en_US 同键）。
import type { UIContext } from '@kit.ArkUI';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ResError } from '../common/ResError';
import { ConnState } from '../common/net/ConnectionManager';

const TAG: string = 'PadLink.Res';
const DOMAIN: number = 0x5050;

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

// 带参格式化（%1$s 按序替换）。参数必须先归一为字符串：资源框架遇非字符串参数抛 9001008，
// 调用点在 build 内属不可捕获，抛出即杀进程——故一律降级，绝不向上抛。
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

// catch 统一出口：ResError 本地化，其余原样展示 message。
export function pageErrText(uiContext: UIContext, err: Error): string {
  if (err instanceof ResError) {
    return pageResFmt(uiContext, err.resKey, err.resParams);
  }
  return String(err.message ?? '');
}
