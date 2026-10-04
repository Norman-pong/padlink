// createEngine/startListening 参数组装（PRD §4.5）。纯数据结构：接口逐一镜像现场文档
// hms-ai-speechrecognizer 的 CreateEngineParams / StartParams / AudioInfo 字段，
// 不 import @ohos/@kit 类型（node 单测直接可加载）；AsrSession 负责镜像 → 真实类型逐字段转换。
// 三陷阱参数（recognitionMode/maxAudioDuration/vadEnd）在此显式落参，注释逐条给出文档出处。
import { AsrDefaults, clampVadEnd } from './AsrConfig';

// 镜像 CreateEngineParams（language 仅 zh-CN；online=1 离线；extraParams 可选字段全量显式）。
export interface AsrCreateEngineParams {
  language: string;
  online: number;
  extraParams: Record<string, Object>;
}

// 镜像 AudioInfo（audioType/sampleRate/soundChannel/sampleBit 均必填，仅支持 16000/1/16）。
export interface AsrAudioInfo {
  audioType: string;
  sampleRate: number;
  soundChannel: number;
  sampleBit: number;
}

// 镜像 StartParams（sessionId 字符集：字母/数字/下划线/短横线；audioInfo 必填）。
export interface AsrStartParams {
  sessionId: string;
  audioInfo: AsrAudioInfo;
  extraParams: Record<string, Object>;
}

// 创建引擎参数：zh-CN + 离线 + 短语音模式（听写单次 ≤60s）。
export function buildCreateEngineParams(): AsrCreateEngineParams {
  const extraParams: Record<string, Object> = {
    'locate': AsrDefaults.LOCATE,
    'recognizerMode': AsrDefaults.RECOGNIZER_MODE,
  };
  const params: AsrCreateEngineParams = {
    language: AsrDefaults.LANGUAGE,
    online: AsrDefaults.ONLINE_OFFLINE,
    extraParams: extraParams,
  };
  return params;
}

// 启动识别参数（实时录音识别路径，无需 writeAudio）：
// - recognitionMode 显式 0：默认 1 是写音频流模式，不喂流则功能不可用（陷阱 1）；
// - maxAudioDuration 显式 60000：默认仅 20000ms（陷阱 2）；
// - vadEnd 显式传入并经 clampVadEnd 校验：默认 800ms 静音即判停（陷阱 3），非法值回退 3000。
export function buildStartParams(sessionId: string, vadEndMs: number): AsrStartParams {
  const extraParams: Record<string, Object> = {
    'recognitionMode': AsrDefaults.RECOGNITION_MODE_REALTIME,
    'vadEnd': clampVadEnd(vadEndMs),
    'maxAudioDuration': AsrDefaults.MAX_AUDIO_DURATION_MS,
  };
  const audioInfo: AsrAudioInfo = {
    audioType: AsrDefaults.AUDIO_TYPE,
    sampleRate: AsrDefaults.SAMPLE_RATE,
    soundChannel: AsrDefaults.SOUND_CHANNEL,
    sampleBit: AsrDefaults.SAMPLE_BIT,
  };
  const params: AsrStartParams = {
    sessionId: sessionId,
    audioInfo: audioInfo,
    extraParams: extraParams,
  };
  return params;
}
