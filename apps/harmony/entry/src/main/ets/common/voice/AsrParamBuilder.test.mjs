// AsrParamBuilder 纯逻辑单测：CreateEngineParams / StartParams 三陷阱参数精确值、
// audioInfo 固定规格、vadEnd 钳制落参、sessionId 透传。
import assert from 'node:assert';
import { buildCreateEngineParams, buildStartParams } from './AsrParamBuilder.ets';

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

// ---- CreateEngineParams ----

test('createEngine：zh-CN + 离线 + locate/recognizerMode 显式', () => {
  const p = buildCreateEngineParams();
  assert.equal(p.language, 'zh-CN');
  assert.equal(p.online, 1);
  assert.equal(p.extraParams['locate'], 'CN');
  assert.equal(p.extraParams['recognizerMode'], 'short');
});

// ---- StartParams 三陷阱精确值 ----

test('startListening：recognitionMode 显式 0（陷阱 1：默认 1 为写流模式）', () => {
  const p = buildStartParams('sid-1', 3000);
  assert.equal(p.extraParams['recognitionMode'], 0);
  assert.notEqual(p.extraParams['recognitionMode'], 1);
});

test('startListening：maxAudioDuration 精确 60000（陷阱 2：默认 20000）', () => {
  const p = buildStartParams('sid-1', 3000);
  assert.equal(p.extraParams['maxAudioDuration'], 60000);
});

test('startListening：vadEnd 精确 3000（陷阱 3：默认 800ms 即判停）', () => {
  const p = buildStartParams('sid-1', 3000);
  assert.equal(p.extraParams['vadEnd'], 3000);
});

test('startListening：vadEnd 非法值经钳制回退 3000', () => {
  assert.equal(buildStartParams('sid', 800).extraParams['vadEnd'], 800); // 范围内保留
  assert.equal(buildStartParams('sid', 100).extraParams['vadEnd'], 3000);
  assert.equal(buildStartParams('sid', 50001).extraParams['vadEnd'], 3000);
  assert.equal(buildStartParams('sid', NaN).extraParams['vadEnd'], 3000);
});

// ---- StartParams 其余必填字段 ----

test('startListening：audioInfo 固定 pcm/16000/单声道/16bit', () => {
  const p = buildStartParams('sid', 3000);
  assert.equal(p.audioInfo.audioType, 'pcm');
  assert.equal(p.audioInfo.sampleRate, 16000);
  assert.equal(p.audioInfo.soundChannel, 1);
  assert.equal(p.audioInfo.sampleBit, 16);
});

test('startListening：sessionId 透传', () => {
  assert.equal(buildStartParams('padlink-1728000000000', 3000).sessionId, 'padlink-1728000000000');
  assert.equal(buildStartParams('a_1-b', 3000).sessionId, 'a_1-b');
});

finish();
