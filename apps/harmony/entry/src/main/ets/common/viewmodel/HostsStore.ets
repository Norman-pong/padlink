// 已配对主机列表纯逻辑（无 @ohos 依赖、目录内自闭合，tools/etstest 可跑）：
// 持久化形态为 preferences 单键 JSON（hosts_json：HostInfo[]，上限 4 与 daemon
// internal/pairing DefaultMaxClients 一致）+ default_ip 两键；本类只管列表操作，
// 存取经构造注入的字符串回调（SettingsModel 注 preferences 适配，单测注内存实现）。
// 操作：upsert（按 ip+port 去重插入/更新并置默认，满额拒绝）、重命名、删除（删默认
// 顺延列表首个）、设默认；名称 ≤64B 校验复用 PairingCore.sanitizeDeviceName（同一
// daemon MaxNameLen 约定）。
import { sanitizeDeviceName, PairPolicy } from './PairingCore';
import { ResError } from '../ResError';

// 主机列表常量。
export class HostsPolicy {
  // 列表上限（daemon DefaultMaxClients=4）。
  public static readonly LIMIT: number = 4;
  // preferences 键名。
  public static readonly KEY_HOSTS: string = 'hosts_json';
  public static readonly KEY_DEFAULT: string = 'default_ip';
  // 主机名最大字节数（daemon MaxNameLen）。
  public static readonly NAME_MAX_BYTES: number = PairPolicy.NAME_MAX_BYTES;
}

// 已配对主机档案（M2 配对流产出；M1 由调试面板手动连接写入）。
export class HostInfo {
  public name: string = '';
  public ip: string = '';
  public port: number = 53021;
  public tokenHex: string = '';
}

// 列表已满且为新主机时抛出；调用方按 instanceof 给「先在设置中删除」提示。
export class HostsFullError extends ResError {
  constructor(limit: number) {
    super('hosts_full_fmt', [String(limit)], `已配对主机已满（上限 ${limit} 台），请先删除`);
    this.name = 'HostsFullError';
  }
}

// 字符串存取回调（真实实现走 preferences getSync/put；单测用内存 Map）。
export interface HostsKVStore {
  getString(key: string): string;
  putString(key: string, value: string): void;
}

// hosts_json 解析：非法 JSON 按空列表处理；畸形条目（缺 IP/端口越界）跳过；
// 重复 ip+port 保留首个；超出上限截断（正常写入路径不会超，防御手改/降级数据）。
export function parseHosts(json: string): HostInfo[] {
  const out: HostInfo[] = [];
  if (json.length === 0) {
    return out;
  }
  let parsed: Object | null;
  try {
    parsed = JSON.parse(json) as Object | null;
  } catch (err) {
    return out; // 非法 JSON：脏数据不致命，按空列表恢复（用户重新配对即可）
  }
  if (parsed === null || !Array.isArray(parsed)) {
    return out;
  }
  const arr = parsed as Array<Object>;
  for (const item of arr) {
    if (out.length >= HostsPolicy.LIMIT) {
      break;
    }
    const h = toHost(item);
    if (h === null) {
      continue;
    }
    if (out.some((x: HostInfo): boolean => x.ip === h.ip && x.port === h.port)) {
      continue;
    }
    out.push(h);
  }
  return out;
}

// 主机名校验：去首尾空白；为空或超 64B（daemon MaxNameLen）返回 ''，否则返回清洗名。
// ≤64B 判定复用 sanitizeDeviceName：其截断结果与修剪串不一致即说明超限。
export function validateHostName(raw: string): string {
  const trimmed: string = raw.trim();
  if (trimmed.length === 0) {
    return '';
  }
  if (sanitizeDeviceName(trimmed, HostsPolicy.NAME_MAX_BYTES) !== trimmed) {
    return '';
  }
  return trimmed;
}

// 单条 JSON 记录 → HostInfo；形态非法返回 null。
function toHost(item: Object | null): HostInfo | null {
  if (item === null) {
    return null;
  }
  const rec = item as Record<string, Object>;
  const ip = fieldStr(rec, 'ip');
  if (ip.length === 0) {
    return null;
  }
  const port = fieldPort(rec);
  if (port < 0) {
    return null;
  }
  const h = new HostInfo();
  h.ip = ip;
  h.port = port;
  h.name = fieldStr(rec, 'name');
  h.tokenHex = fieldStr(rec, 'tokenHex');
  return h;
}

function fieldStr(rec: Record<string, Object>, key: string): string {
  const v = rec[key];
  return typeof v === 'string' ? v : '';
}

function fieldPort(rec: Record<string, Object>): number {
  const v = rec['port'];
  if (typeof v !== 'number' || !Number.isInteger(v) || v <= 0 || v > 65535) {
    return -1;
  }
  return v;
}

export class HostsStore {
  private kv: HostsKVStore;
  private hosts: HostInfo[] = [];
  // 默认主机 IP；未设/已失效时回退列表首个（getDefault 兜底）。
  private defaultIp: string = '';

  constructor(kv: HostsKVStore) {
    this.kv = kv;
    this.hosts = parseHosts(kv.getString(HostsPolicy.KEY_HOSTS));
    const d = kv.getString(HostsPolicy.KEY_DEFAULT);
    this.defaultIp = this.indexOfIp(d) >= 0 ? d : (this.hosts.length > 0 ? this.hosts[0].ip : '');
  }

  // 列表快照（浅拷贝；项内容由本类独占维护，调用方不得改写字段）。
  public listHosts(): HostInfo[] {
    return this.hosts.slice();
  }

  // 默认主机：default_ip 命中则返回；未设/已失效回退列表首个；空列表返回 null。
  public getDefault(): HostInfo | null {
    if (this.hosts.length === 0) {
      return null;
    }
    const idx = this.indexOfIp(this.defaultIp);
    return idx >= 0 ? this.hosts[idx] : this.hosts[0];
  }

  // 插入或更新（按 ip+port 去重）并置为默认；新主机且列表已满抛 HostsFullError。
  public upsert(h: HostInfo): void {
    if (h.ip.length === 0) {
      throw new ResError('hosts_ip_empty', [], '主机 IP 不能为空');
    }
    const copy = copyOf(h);
    const idx = this.indexOfAt(copy.ip, copy.port);
    if (idx >= 0) {
      this.hosts[idx] = copy;
    } else {
      if (this.hosts.length >= HostsPolicy.LIMIT) {
        throw new HostsFullError(HostsPolicy.LIMIT);
      }
      this.hosts.push(copy);
    }
    this.defaultIp = copy.ip;
    this.persist();
  }

  // 重命名：目标不存在或名称非法（空/超 64B）返回 false；成功返回 true 并持久化。
  public renameHost(ip: string, port: number, newName: string): boolean {
    const name = validateHostName(newName);
    if (name.length === 0) {
      return false;
    }
    const idx = this.indexOfAt(ip, port);
    if (idx < 0) {
      return false;
    }
    this.hosts[idx].name = name;
    this.persist();
    return true;
  }

  // 删除：目标不存在返回 false。删除的是默认主机时默认顺延列表首个（无剩余则清空）。
  public removeHost(ip: string, port: number): boolean {
    const idx = this.indexOfAt(ip, port);
    if (idx < 0) {
      return false;
    }
    const wasDefault: boolean = this.hosts[idx].ip === this.defaultIp;
    this.hosts.splice(idx, 1);
    if (wasDefault) {
      this.defaultIp = this.hosts.length > 0 ? this.hosts[0].ip : '';
    }
    this.persist();
    return true;
  }

  // 设为默认：目标不存在返回 false。
  public setDefault(ip: string, port: number): boolean {
    const idx = this.indexOfAt(ip, port);
    if (idx < 0) {
      return false;
    }
    this.defaultIp = this.hosts[idx].ip;
    this.persist();
    return true;
  }

  private indexOfIp(ip: string): number {
    return this.hosts.findIndex((h: HostInfo): boolean => h.ip === ip);
  }

  private indexOfAt(ip: string, port: number): number {
    return this.hosts.findIndex((h: HostInfo): boolean => h.ip === ip && h.port === port);
  }

  private persist(): void {
    this.kv.putString(HostsPolicy.KEY_HOSTS, JSON.stringify(this.hosts));
    this.kv.putString(HostsPolicy.KEY_DEFAULT, this.defaultIp);
  }
}

function copyOf(h: HostInfo): HostInfo {
  const c = new HostInfo();
  c.name = h.name;
  c.ip = h.ip;
  c.port = h.port;
  c.tokenHex = h.tokenHex;
  return c;
}
