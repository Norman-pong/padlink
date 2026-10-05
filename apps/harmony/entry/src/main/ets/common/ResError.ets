// 可本地化错误（lore 约束：UI 文案必须进 string.json 中英双份）。
// 非 UI 层（net/viewmodel）抛出时只携带资源键与参数，由页面经 pageResFmt 解析为当前语言；
// message 保留技术细节（错误码/系统原因），仅供 hilog 日志，不进 UI。
export class ResError extends Error {
  public readonly resKey: string;
  public readonly resParams: string[];

  constructor(resKey: string, resParams: string[] = [], detail: string = '') {
    super(detail.length > 0 ? detail : resKey);
    this.name = 'ResError';
    this.resKey = resKey;
    this.resParams = resParams;
  }
}
