// createEngine/startListening 参数组装。接口逐一镜像系统 speechRecognizer 的
// CreateEngineParams / StartParams / AudioInfo；不 import @ohos/@kit 类型（node 单测可直接加载），
// AsrSession 负责镜像 → 真实类型逐字段转换。
import { AsrDefaults, clampVadEnd } from './AsrConfig';

// 镜像 CreateEngineParams（language 仅 zh-CN；online=1 离线；extraParams 全量显式）。
export interface AsrCreateEngineParams {
  language: string;
  online: number;
  extraParams: Record<string, Object>;
}

// 镜像 AudioInfo（四字段均必填，仅支持 16000/1/16）。
export interface AsrAudioInfo {
  audioType: string;
  sampleRate: number;
  soundChannel: number;
  sampleBit: number;
}

// 镜像 StartParams（sessionId 限字母/数字/下划线/短横线；audioInfo 必填）。
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

// 启动识别参数（实时录音识别路径，无需 writeAudio）。三参数必须显式全量：
// recognitionMode 显式 0（默认 1 是写音频流模式，不喂流即不可用）；maxAudioDuration 显式 60000（默认 20000）；
// vadEnd 经 clampVadEnd 显式传入（默认 800ms 静音即判停），非法值回退 3000。
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
