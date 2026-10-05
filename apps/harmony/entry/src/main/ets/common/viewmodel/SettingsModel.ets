// 设置持久化（FR-5）：@ohos.data.preferences 用户首选项。
// 文档依据：js-apis-data-preferences——getPreferences(context, options): Promise<Preferences>；
// put(key,value) 后必须 flush() 才落盘（faqs-local-database-management-28）；读用 getSync（实例加载后为内存读）。
// 对外读写一律 Promise 封装（任务口径），实例在 init() 完成前拒绝写入（fail fast 不静默丢）。
// 已配对主机（FR-5 主机管理）：列表存单键 JSON（hosts_json，上限 4 与 daemon 一致）+ default_ip，
// 列表操作抽纯类 HostsStore（本类注入 preferences 存取回调组合之）；旧版 lastHost_* 单档案在
// init 时一次性迁移进 hosts_json 后清理，getLastHost/saveLastHost 对外签名不变。
import { preferences } from '@kit.ArkData';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { HostInfo, HostsKVStore, HostsPolicy, HostsStore } from './HostsStore';
import { ResError } from '../ResError';

// 旧版单档案键（迁移源；迁移成功后删除）。
const LEGACY_KEYS: string[] = ['lastHost_ip', 'lastHost_port', 'lastHost_tokenHex', 'lastHost_name'];

const TAG: string = 'PadLink.Settings';
const DOMAIN: number = 0x5050;
const STORE_NAME: string = 'padlink_settings';

// HostsStore 的 preferences 存取适配：读走 getSync（实例加载后为内存读），
// 写 put 后排队，由 flush() 统一落盘（任一失败上抛，fail fast 不静默丢）。
class PreferencesKv implements HostsKVStore {
  private store: preferences.Preferences | null = null;
  private pending: Promise<void>[] = [];

  public attach(store: preferences.Preferences): void {
    this.store = store;
  }

  public getString(key: string): string {
    const store = this.store;
    if (store === null) {
      return '';
    }
    const v = store.getSync(key, '');
    return typeof v === 'string' ? v : '';
  }

  public putString(key: string, value: string): void {
    const store = this.store;
    if (store === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    this.pending.push(store.put(key, value));
  }

  // 等待本批 put 完成并 flush 落盘；失败上抛由调用方处理。
  public async flush(): Promise<void> {
    const store = this.store;
    if (store === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    const writes = this.pending;
    this.pending = [];
    await Promise.all(writes);
    await store.flush();
  }
}

// HostInfo 自 HostsStore 再导出：既有调用方（PairingModel/DebugPanelPage）导入路径不变。
export { HostInfo, HostsFullError } from './HostsStore';

export class SettingsModel {
  private context: common.Context | undefined = undefined;
  private store: preferences.Preferences | null = null;
  // 主机列表纯逻辑（init 后可用）；写经 PreferencesKv 排队，flushPending 统一落盘。
  private kv: PreferencesKv = new PreferencesKv();
  private hostsStore: HostsStore | null = null;

  // 灵敏度（0.5–2.0，默认 1.2，与 GestureDefaults.SENSITIVITY 一致）。
  public sensitivity: number = 1.2;
  // 自然滚动（默认开）。
  public naturalScroll: boolean = true;
  // 调试面板开关（控制设置页版本号连点入口显隐，FR-7）。
  public debugEnabled: boolean = false;

  public init(context: common.Context): Promise<void> {
    this.context = context;
    const opt: preferences.Options = { name: STORE_NAME };
    return preferences.getPreferences(context, opt).then((store: preferences.Preferences) => {
      this.store = store;
      const sens = store.getSync('sensitivity', 1.2);
      if (typeof sens === 'number') {
        this.sensitivity = Math.min(2.0, Math.max(0.5, sens));
      }
      const nat = store.getSync('naturalScroll', true);
      if (typeof nat === 'boolean') {
        this.naturalScroll = nat;
      }
      const dbg = store.getSync('debugEnabled', false);
      if (typeof dbg === 'boolean') {
        this.debugEnabled = dbg;
      }
      this.kv.attach(store);
      this.hostsStore = new HostsStore(this.kv);
      return this.migrateLegacyHost(store);
    });
  }

  public async setSensitivity(v: number): Promise<void> {
    this.sensitivity = Math.min(2.0, Math.max(0.5, v));
    await this.putAndFlush('sensitivity', this.sensitivity);
  }

  public async setNaturalScroll(on: boolean): Promise<void> {
    this.naturalScroll = on;
    await this.putAndFlush('naturalScroll', on);
  }

  public async setDebugEnabled(on: boolean): Promise<void> {
    this.debugEnabled = on;
    await this.putAndFlush('debugEnabled', on);
  }

  // 读取默认主机（无默认回退列表首个；再无返回 null）——AutoConnect/快速连接按它重连。
  public async getLastHost(): Promise<HostInfo | null> {
    const hs = this.hostsStore;
    if (hs === null) {
      return null; // 未初始化：与既有口径一致按无档案处理
    }
    return hs.getDefault();
  }

  // 保存主机（upsert：按 ip+port 去重插入/更新并置为默认；满 4 台新增抛 HostsFullError）。
  public async saveLastHost(h: HostInfo): Promise<void> {
    const hs = this.hostsStore;
    if (hs === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    hs.upsert(h);
    await this.flushPending();
  }

  // 主机列表快照（未初始化抛错，页面给失败态+重试）。
  public async listHosts(): Promise<HostInfo[]> {
    const hs = this.hostsStore;
    if (hs === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    return hs.listHosts();
  }

  public async renameHost(ip: string, port: number, newName: string): Promise<void> {
    const hs = this.hostsStore;
    if (hs === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    if (!hs.renameHost(ip, port, newName)) {
      throw new ResError('settings_host_invalid', [], '主机不存在或名称非法');
    }
    await this.flushPending();
  }

  // 删除主机档案：只动本机偏好，不触碰连接（若删的是当前连接主机，连接保持，仅不再自动重连）。
  public async removeHost(ip: string, port: number): Promise<void> {
    const hs = this.hostsStore;
    if (hs === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    if (!hs.removeHost(ip, port)) {
      throw new ResError('settings_host_missing', [], '主机不存在');
    }
    await this.flushPending();
  }

  // 切换默认主机：AutoConnect 下次触发按新默认重连。
  public async setDefault(ip: string, port: number): Promise<void> {
    const hs = this.hostsStore;
    if (hs === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    if (!hs.setDefault(ip, port)) {
      throw new ResError('settings_host_missing', [], '主机不存在');
    }
    await this.flushPending();
  }

  // 旧版单档案（lastHost_*）一次性迁移进 hosts_json；已有 hosts_json 则跳过。
  private async migrateLegacyHost(store: preferences.Preferences): Promise<void> {
    const seeded = store.getSync(HostsPolicy.KEY_HOSTS, '');
    if (typeof seeded === 'string' && seeded.length > 0) {
      return; // 已是新结构
    }
    const ip = store.getSync('lastHost_ip', '');
    if (typeof ip !== 'string' || ip.length === 0) {
      return; // 从未配对
    }
    const hs = this.hostsStore;
    if (hs === null) {
      return;
    }
    const h = new HostInfo();
    const name = store.getSync('lastHost_name', '');
    const port = store.getSync('lastHost_port', 53021);
    const token = store.getSync('lastHost_tokenHex', '');
    h.ip = ip;
    h.name = typeof name === 'string' ? name : '';
    h.port = typeof port === 'number' ? port : 53021;
    h.tokenHex = typeof token === 'string' ? token : '';
    hs.upsert(h);
    await this.flushPending();
    // 旧键清理（best-effort）：迁移已落盘，清理失败仅记日志，下次 init 因 hosts_json 非空跳过
    try {
      for (const key of LEGACY_KEYS) {
        await store.delete(key);
      }
      await store.flush();
    } catch (err) {
      const e = err as BusinessError;
      hilog.warn(DOMAIN, TAG, 'legacy lastHost cleanup fail: %{public}d %{public}s', e.code, e.message);
    }
  }

  // 等待本批 put 完成并统一 flush 落盘；任一失败即抛（fail fast，不静默丢）。
  private async flushPending(): Promise<void> {
    try {
      await this.kv.flush();
    } catch (err) {
      const e = err as BusinessError;
      hilog.error(DOMAIN, TAG, 'hosts flush fail: %{public}d %{public}s', e.code, e.message);
      throw err as Error;
    }
  }

  private async putAndFlush(key: string, value: preferences.ValueType): Promise<void> {
    if (this.store === null) {
      throw new ResError('settings_store_not_ready', [], '设置存储未初始化');
    }
    try {
      const store = this.store;
      await store.put(key, value);
      await store.flush();
    } catch (err) {
      const e = err as BusinessError;
      hilog.error(DOMAIN, TAG, 'persist %{public}s fail: %{public}d %{public}s', key, e.code, e.message);
      throw err as Error;
    }
  }
}
