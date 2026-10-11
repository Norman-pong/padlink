// 语音识别参数常量与纯逻辑工具（不 import 任何 @ohos/@kit 类型，node 单测可直接加载）。
// 三参数陷阱——必须显式全量传参，否则默认值即错：
// 1. recognitionMode 不传默认 1（写音频流模式，须 writeAudio 喂流），实时录音识别必须显式 0；
// 2. maxAudioDuration 不传默认仅 20000ms；短语音模式上限 60000，听写取上限；
// 3. vadEnd 不传默认 800ms（静音 800ms 即判停，句间停顿会被截断）；可配 [500,10000]，显式取 3000。
// 识别结果：isFinal = 子句最终，isLast = 整轮最后一句；发送 TEXT 的判定条件为 isLast == true。

export class AsrDefaults {
  // 当前仅支持 zh-CN + 离线（online=1）。
  public static readonly LANGUAGE: string = 'zh-CN';
  public static readonly ONLINE_OFFLINE: number = 1;
  public static readonly LOCATE: string = 'CN';
  public static readonly RECOGNIZER_MODE: string = 'short';
  // 三陷阱参数（见文件头注释）。
  public static readonly RECOGNITION_MODE_REALTIME: number = 0;
  public static readonly MAX_AUDIO_DURATION_MS: number = 60000;
  public static readonly VAD_END_DEFAULT_MS: number = 3000;
  public static readonly VAD_END_MIN_MS: number = 500;
  public static readonly VAD_END_MAX_MS: number = 10000;
  // 四字段均为当前唯一支持值。
  public static readonly AUDIO_TYPE: string = 'pcm';
  public static readonly SAMPLE_RATE: number = 16000;
  public static readonly SOUND_CHANNEL: number = 1;
  public static readonly SAMPLE_BIT: number = 16;
}

// 范围内原值返回；NaN/Infinity/越界一律回退默认 3000（不给静默中间值）。
export function clampVadEnd(vadEndMs: number): number {
  if (typeof vadEndMs !== 'number' || !Number.isFinite(vadEndMs)) {
    return AsrDefaults.VAD_END_DEFAULT_MS;
  }
  if (vadEndMs < AsrDefaults.VAD_END_MIN_MS || vadEndMs > AsrDefaults.VAD_END_MAX_MS) {
    return AsrDefaults.VAD_END_DEFAULT_MS;
  }
  return vadEndMs;
}

// Core Speech 错误码 → 文案资源键。
// 1002200004/1002200005（finish/cancel 时无进行中任务）属正常伴随回包，由 VoiceModel 静默收口，不在此映射。
export function asrErrorKey(code: number): string {
  if (code === 1002200012) {
    return 'voice_err_mic_permission';
  }
  if (code === 1002200006) {
    return 'voice_err_busy';
  }
  if (code === 1002200001) {
    return 'voice_err_create_failed';
  }
  if (code === 1002200002) {
    return 'voice_err_start_failed';
  }
  if (code === 1002200003) {
    return 'voice_err_max_audio';
  }
  if (code === 1002200007 || code === 1002200008) {
    return 'voice_err_engine';
  }
  if (code === 1002200011) {
    return 'voice_err_recognize';
  }
  return 'voice_err_unknown';
}
