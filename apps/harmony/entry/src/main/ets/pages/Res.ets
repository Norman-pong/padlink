// 页面公共小工具：按资源名取文案（getStringByNameSync；Resource 版 getStringSync 自 API20 废弃）。
// $r 需字面量，动态键（如 PAIR_NAK 文案键）统一走这里。
import type { UIContext } from '@kit.ArkUI';

export function pageResStr(uiContext: UIContext, name: string): string {
  const ctx = uiContext.getHostContext();
  if (ctx === undefined) {
    return '';
  }
  return ctx.resourceManager.getStringByNameSync(name);
}
