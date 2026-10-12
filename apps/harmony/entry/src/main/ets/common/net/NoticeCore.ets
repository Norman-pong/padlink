// NOTICE 文案纯逻辑：TEXT 未上屏原因码（daemon TextFail*）→ 文案资源键。无 @ohos 依赖，node 可直跑。
export class TextFailReason {
  public static readonly UNKNOWN: number = 0;
  public static readonly NON_TEXT: number = 1;
  public static readonly UNAVAILABLE: number = 2;
}

export function textFailToastKey(arg: number): string {
  if (arg === TextFailReason.NON_TEXT) {
    return 'notice_text_failed_nontext';
  }
  if (arg === TextFailReason.UNAVAILABLE) {
    return 'notice_text_failed_unavailable';
  }
  return 'notice_text_failed';
}
