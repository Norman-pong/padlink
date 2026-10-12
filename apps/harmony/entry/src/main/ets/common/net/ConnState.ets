// 连接状态常量（纯逻辑层，无 @ohos 依赖：net 目录单测与 DiscoverOverlay 规则共用）。
// 不用 TS enum：纯逻辑层须 node 可直跑。
export class ConnState {
  public static readonly IDLE: number = 0;
  public static readonly DISCOVERING: number = 1;
  public static readonly PAIRING: number = 2;
  public static readonly CONNECTED: number = 3;
  public static readonly RECONNECTING: number = 4;
  // 重连失败达上限：已释放一切资源，需手动重连。
  public static readonly LOST: number = 5;
}
