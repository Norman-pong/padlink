// 发现流程纯逻辑（无 @ohos 依赖，node 可直跑）：DISCOVER_RESP JSON 解析与按 IP 去重。
// DISCOVER_RESP payload 约定：{"name","os","daemon","ver","paired","port"}；来源 IP 取 UDP 报文来源地址，不进 JSON。
// 解析按外部不可信处理：字段类型逐一校验，非法即返回 null 由调用方丢弃。

export class DiscoveredHost {
  public name: string = '';
  public os: string = '';
  public daemon: string = '';
  public ver: number = 0;
  public paired: number = 0;
  public port: number = 53021;
  public ip: string = '';
}

// JSON 文本 → DiscoveredHost（ip 由调用方传入）；非法返回 null。
export function parseDiscoverRespJson(text: string, ip: string): DiscoveredHost | null {
  let obj: Record<string, Object>;
  try {
    const parsed: Object | null = JSON.parse(text) as Object;
    if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return null;
    }
    obj = parsed as Record<string, Object>;
  } catch (err) {
    return null; // JSON.parse 抛 SyntaxError（外部输入）
  }
  const h = new DiscoveredHost();
  h.ip = ip;
  const nameVal: Object | undefined = obj['name'];
  const osVal: Object | undefined = obj['os'];
  const daemonVal: Object | undefined = obj['daemon'];
  const verVal: Object | undefined = obj['ver'];
  const pairedVal: Object | undefined = obj['paired'];
  const portVal: Object | undefined = obj['port'];
  if (typeof nameVal === 'string') {
    h.name = nameVal as string;
  }
  if (typeof osVal === 'string') {
    h.os = osVal as string;
  }
  if (typeof daemonVal === 'string') {
    h.daemon = daemonVal as string;
  }
  if (typeof verVal === 'number' && (verVal as number) >= 0) {
    h.ver = verVal as number;
  }
  if (typeof pairedVal === 'number' && (pairedVal as number) >= 0) {
    h.paired = pairedVal as number;
  }
  // port 是后续 TCP 连接目标，必须存在且合法；其余字段缺失/类型错不可作为连接依据
  if (typeof portVal !== 'number') {
    return null;
  }
  const port: number = portVal as number;
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return null;
  }
  h.port = port;
  return h;
}

// 按 IP 去重合并（后到覆盖先到，保持首现顺序）；返回新数组不改入参（@Trace 重赋值才触发刷新）。
export function upsertHost(list: DiscoveredHost[], host: DiscoveredHost): DiscoveredHost[] {
  const out: DiscoveredHost[] = [];
  let replaced: boolean = false;
  for (const h of list) {
    if (h.ip === host.ip) {
      out.push(host);
      replaced = true;
    } else {
      out.push(h);
    }
  }
  if (!replaced) {
    out.push(host);
  }
  return out;
}
