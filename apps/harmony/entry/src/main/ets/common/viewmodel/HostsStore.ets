// 已配对主机列表纯逻辑（无 @ohos 依赖、目录内自闭合，node 可直跑）。
// 持久化形态：preferences 单键 JSON（hosts_json: HostInfo[]，上限 4 与 daemon DefaultMaxClients 一致）+ default_ip。
// 存取经构造注入的字符串回调（SettingsModel 注 preferences 适配，单测注内存实现）。
// 名称 ≤64B 校验复用 PairingCore.sanitizeDeviceName（与 daemon MaxNameLen 同源）。
import { sanitizeDeviceName, PairPolicy } from './PairingCore';
import { ResError } from '../ResError';

export class HostsPolicy {
  // 列表上限（daemon DefaultMaxClients=4）。
  public static readonly LIMIT: number = 4;
  public static readonly KEY_HOSTS: string = 'hosts_json';
  public static readonly KEY_DEFAULT: string = 'default_ip';
  // 主机名最大字节数（daemon MaxNameLen）。
  public static readonly NAME_MAX_BYTES: number = PairPolicy.NAME_MAX_BYTES;
}

export class HostInfo {
  public name: string = '';
  public ip: string = '';
  public port: number = 53021;
  public tokenHex: string = '';
}

// 列表已满且为新主机时抛出；调用方按 instanceof 给「先删除」提示。
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

// 非法 JSON 按空列表处理；畸形条目（缺 IP/端口越界）跳过；重复 ip+port 保留首个；
// 超出上限截断（防御手改/降级数据）。
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

// 去首尾空白；为空或超 64B 返回 ''。
// ≤64B 判定复用 sanitizeDeviceName：截断结果与修剪串不一致即超限。
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

// 主机档案键（去重/查找唯一判据：ip+port）。
export function hostKeyOf(ip: string, port: number): string {
  return `${ip}:${port}`;
}

// ip+port 精确匹配；发现页「已配对」标注与直连路由共用本判据。
export function findHost(hosts: HostInfo[], ip: string, port: number): HostInfo | null {
  for (const h of hosts) {
    if (h.ip === ip && h.port === port) {
      return h;
    }
  }
  return null;
}

export class HostsStore {
  private kv: HostsKVStore;
  private hosts: HostInfo[] = [];
  // 默认主机 IP；未设/已失效时回退列表首个。
  private defaultIp: string = '';

  constructor(kv: HostsKVStore) {
    this.kv = kv;
    this.hosts = parseHosts(kv.getString(HostsPolicy.KEY_HOSTS));
    const d = kv.getString(HostsPolicy.KEY_DEFAULT);
    this.defaultIp = this.indexOfIp(d) >= 0 ? d : (this.hosts.length > 0 ? this.hosts[0].ip : '');
  }

  public listHosts(): HostInfo[] {
    return this.hosts.slice();
  }

  public getDefault(): HostInfo | null {
    if (this.hosts.length === 0) {
      return null;
    }
    const idx = this.indexOfIp(this.defaultIp);
    return idx >= 0 ? this.hosts[idx] : this.hosts[0];
  }

  // 按 ip+port 去重插入/更新并置默认；新主机且列表已满抛 HostsFullError。
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

  // 目标不存在返回 false；删的是默认主机时默认顺延列表首个（无剩余则清空）。
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
    return this.hosts.findIndex((h: HostInfo): boolean => hostKeyOf(h.ip, h.port) === hostKeyOf(ip, port));
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
