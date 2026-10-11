// 滚动列表页标题栏样式单源：discover/settings/debug 三页同参，改一处全生效。
// 三项必须同参：GRADIENT_BLUR（行程 0→20vp）、blurStrategy ENABLE（官方默认 ADAPTIVE 按系统
// 分档，部分设备落无模糊档永不显影）、材质档位 ADAPTIVE（强制 EXQUISITE/GENTLE 在低算力设备卡顿）。
import { ScrollEffectType, BlurStrategy, hdsMaterial } from '@kit.UIDesignKit';
import type { TitleBarStyleOptions } from '@kit.UIDesignKit';
import { LengthMetrics } from '@kit.ArkUI';

export function scrollBlurTitleBarStyle(): TitleBarStyleOptions {
  const style: TitleBarStyleOptions = {
    scrollEffectOpts: {
      enableScrollEffect: true,
      scrollEffectType: ScrollEffectType.GRADIENT_BLUR,
      blurEffectiveStartOffset: LengthMetrics.vp(0),
      blurEffectiveEndOffset: LengthMetrics.vp(20)
    },
    blurStrategy: BlurStrategy.ENABLE,
    systemMaterialEffect: {
      materialType: hdsMaterial.MaterialType.ADAPTIVE,
      materialLevel: hdsMaterial.MaterialLevel.ADAPTIVE
    }
  };
  return style;
}
