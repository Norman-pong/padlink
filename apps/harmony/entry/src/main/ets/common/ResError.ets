// 可本地化错误：非 UI 层只携带资源键与参数，页面经 pageErrText 解析；message 仅供日志不进 UI。
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
