// speechRecognizer 会话封装（PRD §4.5）：生命周期严格按 createEngine → setListener →
// startListening → finish/cancel → shutdown（hms-ai-speechrecognizer 各接口“为了确保事件回调，
// 请优先调用 setListener”约束）。引擎实例跨按住复用（错误码 1002200001 官方处理步骤：
// 相同创建参数复用引擎，避免用完立即销毁），仅页面最终离开时 shutdown 释放。
// 麦克风仅按住期间占用：recognitionMode=0 实时录音模式，finish/cancel 即停止录音。
// 错误经 asrErrorKey（AsrConfig 纯函数）映射为文案键后上抛，本文件不做 UI 语义决策。
import { speechRecognizer } from '@kit.CoreSpeechKit';
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { AsrDefaults, asrErrorKey } from './AsrConfig';
import { buildCreateEngineParams, buildStartParams } from './AsrParamBuilder';

const TAG: string = 'PadLink.Asr';
const DOMAIN: number = 0x5050;

// 识别事件回调（text 为本拍最优结果；isFinal=子句最终，isLast=整轮最后一句，均现查文档字段名）。
// 回调走函数字段（同 Transport.onPacket 风格），null = 未挂接。
// 单实例会话：一次 begin → （finish | cancel）为一轮按住；begin 可多轮复用同一引擎。
export class AsrSession {
  public onStarted: (() => void) | null = null;
  public onResult: ((text: string, isFinal: boolean, isLast: boolean) => void) | null = null;
  public onComplete: (() => void) | null = null;
  public onError: ((errKey: string, code: number, message: string) => void) | null = null;

  private engine: speechRecognizer.SpeechRecognitionEngine | null = null;
  private creating: Promise<void> | null = null;
  private listening: boolean = false;
  private readonly sessionId: string;
  private readonly listener: speechRecognizer.RecognitionListener;

  constructor() {
    this.sessionId = `padlink-${new Date().getTime()}`; // 字符集限字母/数字/下划线/短横线
    const session: AsrSession = this;
    this.listener = {
      onStart(sessionId: string, eventMessage: string) {
        session.listening = true;
        hilog.info(DOMAIN, TAG, 'onStart %{public}s', eventMessage);
        if (session.onStarted !== null) {
          session.onStarted();
        }
      },
      onEvent(sessionId: string, eventCode: number, eventMessage: string) {
        hilog.debug(DOMAIN, TAG, 'onEvent %{public}d %{public}s', eventCode, eventMessage);
      },
      onResult(sessionId: string, result: speechRecognizer.SpeechRecognitionResult) {
        if (session.onResult !== null) {
          session.onResult(result.result, result.isFinal, result.isLast);
        }
      },
      onComplete(sessionId: string, eventMessage: string) {
        session.listening = false;
        if (session.onComplete !== null) {
          session.onComplete();
        }
      },
      onError(sessionId: string, errorCode: number, errorMessage: string) {
        session.listening = false;
        hilog.warn(DOMAIN, TAG, 'onError %{public}d %{public}s', errorCode, errorMessage);
        if (session.onError !== null) {
          session.onError(asrErrorKey(errorCode), errorCode, errorMessage);
        }
      },
    };
  }

  // 开始一轮识别：确保引擎就绪后 startListening。已在识别中时幂等返回。
  public async begin(): Promise<void> {
    if (this.listening) {
      return;
    }
    await this.ensureEngine();
    const mirror = buildStartParams(this.sessionId, AsrDefaults.VAD_END_DEFAULT_MS);
    const extra: Record<string, Object> = mirror.extraParams;
    const params: speechRecognizer.StartParams = {
      sessionId: mirror.sessionId,
      audioInfo: {
        audioType: mirror.audioInfo.audioType,
        sampleRate: mirror.audioInfo.sampleRate,
        soundChannel: mirror.audioInfo.soundChannel,
        sampleBit: mirror.audioInfo.sampleBit,
      },
      extraParams: extra,
    };
    const engine: speechRecognizer.SpeechRecognitionEngine | null = this.engine;
    if (engine === null) {
      throw new Error('ASR engine missing after ensureEngine');
    }
    engine.startListening(params); // 启动失败经 onError（1002200002 等）回调上抛
  }

  // 松手：结束录音并等待最终结果（isLast 经 onResult 回来）。
  public finish(): void {
    if (!this.listening) {
      return;
    }
    const engine: speechRecognizer.SpeechRecognitionEngine | null = this.engine;
    if (engine === null) {
      return;
    }
    this.listening = false;
    engine.finish(this.sessionId);
  }

  // 取消本轮：丢弃在途识别（幂等，未在识别中为 no-op）。
  public cancel(): void {
    if (!this.listening) {
      return;
    }
    const engine: speechRecognizer.SpeechRecognitionEngine | null = this.engine;
    if (engine === null) {
      return;
    }
    this.listening = false;
    engine.cancel(this.sessionId);
  }

  // 最终释放（页面离开/应用收尾）：shutdown 后引擎引用清空，下次 begin 自动重建。
  public shutdown(): void {
    this.cancel();
    const engine: speechRecognizer.SpeechRecognitionEngine | null = this.engine;
    this.engine = null;
    if (engine !== null) {
      engine.shutdown();
    }
  }

  // 引擎懒创建 + setListener（顺序：createEngine → setListener，均先于任何 startListening）。
  private ensureEngine(): Promise<void> {
    if (this.engine !== null) {
      return Promise.resolve();
    }
    if (this.creating !== null) {
      return this.creating;
    }
    const mirror = buildCreateEngineParams();
    const extra: Record<string, Object> = mirror.extraParams;
    const params: speechRecognizer.CreateEngineParams = {
      language: mirror.language,
      online: mirror.online,
      extraParams: extra,
    };
    const session: AsrSession = this;
    this.creating = new Promise<void>((resolve: () => void, reject: (e: BusinessError) => void) => {
      speechRecognizer.createEngine(params, (err: BusinessError,
        engine: speechRecognizer.SpeechRecognitionEngine) => {
        session.creating = null;
        if (err !== undefined && err !== null && err.code > 0) {
          hilog.error(DOMAIN, TAG, 'createEngine fail %{public}d %{public}s', err.code, err.message);
          reject(err);
          return;
        }
        session.engine = engine;
        engine.setListener(session.listener);
        hilog.info(DOMAIN, TAG, 'engine ready');
        resolve();
      });
    });
    return this.creating;
  }
}
