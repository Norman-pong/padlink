// HMAC-SHA256 提供方（cryptoFramework 同步接口）。
// 文档查证（js-apis-cryptoframework Mac 段 / crypto-compute-hmac）：
// createSymKeyGenerator('HMAC') + convertKeySync（API12+）、Mac.initSync/updateSync/doFinalSync（API12+）
// 均为同步 API，API 26 可用。故 HmacProvider.compute 可保持同步实现——
// Auth.seal 无需异步包装，common/proto 零改动，无需 FIFO/覆盖合并管线。
// 密钥为配对签发的 32 字节 token：构造时一次性 convertKeySync，compute 内每包独立三段式计算。
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import type { HmacProvider } from '../proto/Auth';

export class CryptoHmacProvider implements HmacProvider {
  private symKey: cryptoFramework.SymKey;

  constructor(token: Uint8Array) {
    const gen: cryptoFramework.SymKeyGenerator = cryptoFramework.createSymKeyGenerator('HMAC');
    const blob: cryptoFramework.DataBlob = { data: token };
    this.symKey = gen.convertKeySync(blob);
  }

  // 入参 = 零化 hmac 后的 19B 头 || payload；返回 8 字节截断 tag（hmac_trunc）。
  public compute(input: Uint8Array): Uint8Array {
    const mac: cryptoFramework.Mac = cryptoFramework.createMac('SHA256');
    mac.initSync(this.symKey);
    mac.updateSync({ data: input });
    const full: cryptoFramework.DataBlob = mac.doFinalSync();
    return full.data.slice(0, 8);
  }
}
