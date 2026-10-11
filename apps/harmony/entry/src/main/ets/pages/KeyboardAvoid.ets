// 页面级键盘避让：含底部按钮的滚动表单页必须用 RESIZE——压缩窗口让 Scroll 视口同步缩小，
// 底部按钮可滚达；默认 OFFSET 只上抬保焦点输入可见，按钮仍被键盘遮住且 Scroll 滚不到。
// 键盘避让是窗口级设置：进入页设置、离开页恢复，且必须 save/restore 旧值——页面嵌套时
// 各自恢复自己的前任模式，不串档。
import { KeyboardAvoidMode } from '@kit.ArkUI';
import type { UIContext } from '@kit.ArkUI';

export function enterKeyboardAvoidResize(uiContext: UIContext): KeyboardAvoidMode {
  const prev: KeyboardAvoidMode = uiContext.getKeyboardAvoidMode();
  if (prev !== KeyboardAvoidMode.RESIZE) {
    uiContext.setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE);
  }
  return prev;
}

export function restoreKeyboardAvoidMode(uiContext: UIContext, mode: KeyboardAvoidMode): void {
  uiContext.setKeyboardAvoidMode(mode);
}
