// 滚动列表页标题栏样式单源（D5，硬约束 5/19）：discover/settings/debug 三页同参同光源，
// 改一处全生效。配方 =
// ① 滚动渐显模糊：GRADIENT_BLUR + 行程 0→20vp（官方 FAQ 推荐值，faqs-arkui-1095）；
// ② blurStrategy ENABLE：项目实证档——官方默认 ADAPTIVE 按系统三档分档，部分设备落
//    无模糊档永不显影（ui-design-navigation-dynamic-blur + ArkFin 实证 lore）；
// ③ 沉浸材质恒启用：hdsMaterial 路径（systemMaterialEffect）模拟器+真机均显影，
//    档位恒 ADAPTIVE（硬约束 8：低算力设备强制 EXQUISITE/GENTLE 会卡顿发热），
//    与 GRADIENT_BLUR 配合即官方完整配方（ui-design-hdsnavigation SystemMaterialParams）。
// originalStyle/scrollEffectStyle 不显式配置：GRADIENT_BLUR + enableScrollEffect(true) 下
// 框架默认 original 透明、滚动后 comp_background_gray（ui-design-hdsnavigation
// HdsNavigationBackgroundStyle），显式覆盖反而与默认滚动联动语义纠缠。
// 分隔线保持默认 AUTO（随滚动跟手显示，配合滚动模糊；恒透明三件套的 divider OFF 属另一形态）。
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
