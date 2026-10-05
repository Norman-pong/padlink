// 页面级键盘避让（faqs-arkui-1073 / hds-components.md §一.5b，D5）：
// 含底部按钮的滚动表单页（调试手动连接区/发现页确认码/设置页重命名弹层）用 RESIZE——
// 压缩窗口让 Scroll 视口同步缩小，底部按钮可滚达；默认 OFFSET 只上抬保焦点输入可见，
// 按钮仍被键盘遮住且 Scroll 滚不到（MateX7API26 实测复现：Connect 按钮滞留键盘下方）。
// 键盘避让是窗口级设置：页面 aboutToAppear 设置、aboutToDisappear 回退，不全局设置不回收；
// save/restore 返回旧值——settings→debug 页面嵌套时各自恢复自己的前任模式，不串档。
import { KeyboardAvoidMode } from '@kit.ArkUI';
import type { UIContext } from '@kit.ArkUI';

// 设为 RESIZE 并返回设置前的模式（调用方保存，退出时传给 restoreKeyboardAvoidMode）。
export function enterKeyboardAvoidResize(uiContext: UIContext): KeyboardAvoidMode {
  const prev: KeyboardAvoidMode = uiContext.getKeyboardAvoidMode();
  if (prev !== KeyboardAvoidMode.RESIZE) {
    uiContext.setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE);
  }
  return prev;
}

// 恢复 enterKeyboardAvoidResize 返回的前任模式。
export function restoreKeyboardAvoidMode(uiContext: UIContext, mode: KeyboardAvoidMode): void {
  uiContext.setKeyboardAvoidMode(mode);
}
