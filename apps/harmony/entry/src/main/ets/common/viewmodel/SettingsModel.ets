// 设置持久化（FR-5）：@ohos.data.preferences 用户首选项。
// 文档依据：js-apis-data-preferences——getPreferences(context, options): Promise<Preferences>；
// put(key,value) 后必须 flush() 才落盘（faqs-local-database-management-28）；读用 getSync（实例加载后为内存读）。
// 对外读写一律 Promise 封装（任务口径），实例在 init() 完成前拒绝写入（fail fast 不静默丢）。
import { preferences } from '@kit.ArkData';
import type { common } from '@kit.AbilityKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';

const TAG: string = 'PadLink.Settings';
const DOMAIN: number = 0x5050;
const STORE_NAME: string = 'padlink_settings';

// 已配对主机档案（M2 配对流产出；M1 由调试面板手动连接写入）。
export class HostInfo {
  public name: string = '';
  public ip: string = '';
  public port: number = 53021;
  public tokenHex: string = '';
}

export class SettingsModel {
  private context: common.Context | undefined = undefined;
  private store: preferences.Preferences | null = null;

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

  // 读取 lastHost（无档案返回 null）。
  public async getLastHost(): Promise<HostInfo | null> {
    if (this.store === null) {
      return null;
    }
    const store = this.store;
    const ip = store.getSync('lastHost_ip', '');
    if (typeof ip !== 'string' || ip.length === 0) {
      return null;
    }
    const h = new HostInfo();
    const name = store.getSync('lastHost_name', '');
    const port = store.getSync('lastHost_port', 53021);
    const token = store.getSync('lastHost_tokenHex', '');
    h.ip = ip;
    h.name = typeof name === 'string' ? name : '';
    h.port = typeof port === 'number' ? port : 53021;
    h.tokenHex = typeof token === 'string' ? token : '';
    return h;
  }

  public async saveLastHost(h: HostInfo): Promise<void> {
    if (this.store === null) {
      throw new Error('设置存储未初始化');
    }
    const store = this.store;
    await store.put('lastHost_ip', h.ip);
    await store.put('lastHost_port', h.port);
    await store.put('lastHost_tokenHex', h.tokenHex);
    await store.put('lastHost_name', h.name);
    await store.flush();
  }

  private async putAndFlush(key: string, value: preferences.ValueType): Promise<void> {
    if (this.store === null) {
      throw new Error('设置存储未初始化');
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
