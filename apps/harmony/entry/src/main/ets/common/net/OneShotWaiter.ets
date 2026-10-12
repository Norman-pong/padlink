// 一次性等待器：settle 仅一次（先到先得，迟到 deliver 丢弃）。
// 纯逻辑层（无 @ohos 依赖）；调用方丢弃引用即「脱钩」——脱钩后超时照常触发，
// 等待方按超时值收口，与「回包触发」二选一。
export class OneShotWaiter<T> {
  private settled: boolean = false;
  private deliverFn: ((v: T) => void) | null = null;
  private timer: number = -1;

  public wait(timeoutMs: number, timeoutValue: T): Promise<T> {
    return new Promise<T>((resolve: (v: T) => void) => {
      const settle = (v: T): void => {
        if (this.settled) {
          return;
        }
        this.settled = true;
        if (this.timer >= 0) {
          clearTimeout(this.timer);
          this.timer = -1;
        }
        resolve(v);
      };
      this.deliverFn = settle;
      this.timer = setTimeout(() => {
        settle(timeoutValue);
      }, timeoutMs);
    });
  }

  public deliver(v: T): void {
    if (this.deliverFn !== null) {
      this.deliverFn(v);
    }
  }
}
