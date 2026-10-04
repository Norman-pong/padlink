// 语音识别（PRD §4.5）参数常量与纯逻辑工具。本文件不 import 任何 @ohos/@kit 类型，
// 可被 node 单测直接加载（tools/etstest），AsrSession 负责与真实 API 类型的转换。
//
// 三处默认值陷阱（必须显式全量传参，出处为现场文档，见 docs/HARMONY-DOCIDS.md 三）：
// 1. recognitionMode：不传默认 1 = 写音频流模式（须 writeAudio 喂流，不喂则功能不可用）；
//    实时录音识别必须显式传 0。出处：hms-ai-speechrecognizer StartParams.extraParams 字段表。
// 2. maxAudioDuration：不传默认仅 20000ms；短语音模式可配范围 [20000,60000]，听写取上限 60000。
//    出处：同上字段表（“最大支持音频时长，不传参时默认20000ms”）。
// 3. vadEnd：不传默认 800ms（短语音），静音 800ms 即判停，听写时句间稍作停顿就会被截断；
//    可配范围 [500,10000]，显式取 3000。出处：同上字段表 + faqs-core-speech-12。
//
// 识别结果字段（现查 hms-ai-speechrecognizer SpeechRecognitionResult 字段表）：
// isFinal = 本条子句的最终结果；isLast = 整轮识别的最后一句；中间/最终结果经同一 onResult 回调返回。
// 发送 TEXT 的判定条件为 isLast == true。
// （MICROPHONE 权限名常量归 VoiceModel：SDK 要求 Permissions 字面量联合类型，纯逻辑文件不引 @kit。）

// 识别参数常量（readonly 常量类，同 ConnState/GestureStateName 风格）。
export class AsrDefaults {
  // CreateEngineParams：当前仅支持 zh-CN + 离线（online=1）。
  public static readonly LANGUAGE: string = 'zh-CN';
  public static readonly ONLINE_OFFLINE: number = 1;
  public static readonly LOCATE: string = 'CN';
  public static readonly RECOGNIZER_MODE: string = 'short';
  // StartParams.extraParams 三陷阱参数（见文件头注释）。
  public static readonly RECOGNITION_MODE_REALTIME: number = 0;
  public static readonly MAX_AUDIO_DURATION_MS: number = 60000;
  public static readonly VAD_END_DEFAULT_MS: number = 3000;
  public static readonly VAD_END_MIN_MS: number = 500;
  public static readonly VAD_END_MAX_MS: number = 10000;
  // AudioInfo：四字段均为当前唯一支持值（hms-ai-speechrecognizer AudioInfo 字段表）。
  public static readonly AUDIO_TYPE: string = 'pcm';
  public static readonly SAMPLE_RATE: number = 16000;
  public static readonly SOUND_CHANNEL: number = 1;
  public static readonly SAMPLE_BIT: number = 16;
}

// vadEnd 越界校验：范围内原值返回；NaN/Infinity/越界一律回退默认 3000（不给静默中间值）。
export function clampVadEnd(vadEndMs: number): number {
  if (typeof vadEndMs !== 'number' || !Number.isFinite(vadEndMs)) {
    return AsrDefaults.VAD_END_DEFAULT_MS;
  }
  if (vadEndMs < AsrDefaults.VAD_END_MIN_MS || vadEndMs > AsrDefaults.VAD_END_MAX_MS) {
    return AsrDefaults.VAD_END_DEFAULT_MS;
  }
  return vadEndMs;
}

// Core Speech 错误码 → 文案资源键（纯函数；出处 errorcode-corespeech 错误码全集）。
// 1002200004/1002200005（finish/cancel 时无进行中任务）不在此映射：属松手/取消的正常伴随
// 回包，由 VoiceModel 静默收口，不作为错误展示。
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
