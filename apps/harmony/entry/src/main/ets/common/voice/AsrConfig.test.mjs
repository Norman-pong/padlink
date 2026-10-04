// AsrConfig 纯逻辑单测：vadEnd 范围校验（非法值回退默认 3000）、错误码 → 文案键全映射。
import assert from 'node:assert';
import { AsrDefaults, clampVadEnd, asrErrorKey } from './AsrConfig.ets';

let caseNo = 0;
let failed = 0;

function test(name, fn) {
  caseNo += 1;
  try {
    fn();
    console.log(`PASS ${String(caseNo).padStart(2, '0')} ${name}`);
  } catch (e) {
    failed += 1;
    console.log(`FAIL ${String(caseNo).padStart(2, '0')} ${name} :: ${e && e.message ? e.message : e}`);
  }
}

function finish() {
  if (failed > 0) {
    console.log(`\n${failed}/${caseNo} 例失败`);
    process.exit(1);
  }
  console.log(`\n本文件 ${caseNo} 例全过`);
}

// ---- 三陷阱常量精确值（PRD §4.5 口径） ----

test('三陷阱常量精确值', () => {
  assert.equal(AsrDefaults.RECOGNITION_MODE_REALTIME, 0);
  assert.equal(AsrDefaults.MAX_AUDIO_DURATION_MS, 60000);
  assert.equal(AsrDefaults.VAD_END_DEFAULT_MS, 3000);
});

test('范围边界常量与语言/音频规格', () => {
  assert.equal(AsrDefaults.VAD_END_MIN_MS, 500);
  assert.equal(AsrDefaults.VAD_END_MAX_MS, 10000);
  assert.equal(AsrDefaults.LANGUAGE, 'zh-CN');
  assert.equal(AsrDefaults.ONLINE_OFFLINE, 1);
  assert.equal(AsrDefaults.AUDIO_TYPE, 'pcm');
  assert.equal(AsrDefaults.SAMPLE_RATE, 16000);
  assert.equal(AsrDefaults.SOUND_CHANNEL, 1);
  assert.equal(AsrDefaults.SAMPLE_BIT, 16);
});

// ---- clampVadEnd ----

test('vadEnd 范围内原值返回', () => {
  assert.equal(clampVadEnd(3000), 3000);
  assert.equal(clampVadEnd(500), 500);
  assert.equal(clampVadEnd(10000), 10000);
  assert.equal(clampVadEnd(800), 800);
});

test('vadEnd 越界回退默认 3000', () => {
  assert.equal(clampVadEnd(499), 3000);
  assert.equal(clampVadEnd(10001), 3000);
  assert.equal(clampVadEnd(0), 3000);
  assert.equal(clampVadEnd(-3000), 3000);
  assert.equal(clampVadEnd(20000), 3000);
});

test('vadEnd 非法值（NaN/Infinity/非数字）回退默认 3000', () => {
  assert.equal(clampVadEnd(NaN), 3000);
  assert.equal(clampVadEnd(Infinity), 3000);
  assert.equal(clampVadEnd(-Infinity), 3000);
  assert.equal(clampVadEnd(undefined), 3000);
  assert.equal(clampVadEnd(null), 3000);
  assert.equal(clampVadEnd('3000'), 3000);
});

// ---- asrErrorKey（errorcode-corespeech 全集映射） ----

test('错误码映射：1002200012 → 权限拒绝键', () => {
  assert.equal(asrErrorKey(1002200012), 'voice_err_mic_permission');
});

test('错误码映射：busy/创建失败/启动失败/超长', () => {
  assert.equal(asrErrorKey(1002200006), 'voice_err_busy');
  assert.equal(asrErrorKey(1002200001), 'voice_err_create_failed');
  assert.equal(asrErrorKey(1002200002), 'voice_err_start_failed');
  assert.equal(asrErrorKey(1002200003), 'voice_err_max_audio');
});

test('错误码映射：引擎未初始化/已销毁同键，识别异常独立键', () => {
  assert.equal(asrErrorKey(1002200007), 'voice_err_engine');
  assert.equal(asrErrorKey(1002200008), 'voice_err_engine');
  assert.equal(asrErrorKey(1002200011), 'voice_err_recognize');
});

test('错误码映射：未知码（401/1002200009/0/负数）→ 兜底键', () => {
  assert.equal(asrErrorKey(401), 'voice_err_unknown');
  assert.equal(asrErrorKey(1002200009), 'voice_err_unknown');
  assert.equal(asrErrorKey(0), 'voice_err_unknown');
  assert.equal(asrErrorKey(-1), 'voice_err_unknown');
});

finish();
