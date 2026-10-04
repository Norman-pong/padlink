// PairingCore 纯函数单测：NAK reason→资源键全映射、确认码校验、PAIR_REQ payload 构造、
// 设备名 64B 截断（CJK 不劈开码点）、PAIR_OK/PAIR_NAK payload 解析。
import assert from 'node:assert';
import { PairNak, PairPolicy, nakReasonKey, validatePairCode, parsePairNakReason,
  pairOkToken, buildPairReqInitJson, buildPairReqCodeBytes, sanitizeDeviceName } from './PairingCore.ets';

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

test('NAK reason 0..4 → 各自资源键', () => {
  assert.equal(nakReasonKey(PairNak.WRONG_CODE), 'pair_nak_wrong_code');
  assert.equal(nakReasonKey(PairNak.EXPIRED), 'pair_nak_expired');
  assert.equal(nakReasonKey(PairNak.TOO_MANY), 'pair_nak_too_many');
  assert.equal(nakReasonKey(PairNak.NO_SESSION), 'pair_nak_no_session');
  assert.equal(nakReasonKey(PairNak.FULL), 'pair_nak_full');
});

test('NAK 未知 reason（-1/5/255）→ 兜底键', () => {
  assert.equal(nakReasonKey(-1), 'pair_nak_unknown');
  assert.equal(nakReasonKey(5), 'pair_nak_unknown');
  assert.equal(nakReasonKey(255), 'pair_nak_unknown');
});

test('确认码校验：4 位 ASCII 数字通过', () => {
  assert.equal(validatePairCode('0000'), true);
  assert.equal(validatePairCode('1234'), true);
  assert.equal(validatePairCode('9999'), true);
});

test('确认码校验：长度/字符不合规拒绝', () => {
  assert.equal(validatePairCode(''), false);
  assert.equal(validatePairCode('123'), false);
  assert.equal(validatePairCode('12345'), false);
  assert.equal(validatePairCode('12a4'), false);
  assert.equal(validatePairCode('12 4'), false);
  assert.equal(validatePairCode('１２３４'), false); // 全角数字非 ASCII
  assert.equal(validatePairCode('+123'), false);
});

test('parsePairNakReason：1B payload 取值，形态非法返回 -1', () => {
  assert.equal(parsePairNakReason(Uint8Array.of(0).buffer), 0);
  assert.equal(parsePairNakReason(Uint8Array.of(4).buffer), 4);
  assert.equal(parsePairNakReason(new ArrayBuffer(0)), -1);
  assert.equal(parsePairNakReason(Uint8Array.of(0, 0).buffer), -1);
});

test('pairOkToken：32B 取副本，形态非法返回 null', () => {
  assert.equal(pairOkToken(new ArrayBuffer(31)), null);
  assert.equal(pairOkToken(new ArrayBuffer(33)), null);
  const ok = pairOkToken(new ArrayBuffer(32));
  assert.ok(ok !== null);
  assert.equal(ok.length, 32);
  const src = Uint8Array.from({ length: 32 }, (_, i) => i);
  const tok = pairOkToken(src.buffer);
  assert.ok(tok !== null);
  src[0] = 0xff; // 副本不受源后续改动影响
  assert.equal(tok[0], 0);
});

test('buildPairReqInitJson：JSON 形态与 daemon 约定一致（首个字符 {）', () => {
  const json = buildPairReqInitJson('口袋键鼠');
  assert.ok(json.startsWith('{"name":"'));
  assert.ok(json.endsWith('"}'));
  const parsed = JSON.parse(json);
  assert.equal(parsed.name, '口袋键鼠');
});

test('buildPairReqInitJson：名字按 64 字节截断（CJK 3B/字）', () => {
  const json = buildPairReqInitJson('一二三四五六七八九十'.repeat(10)); // 100 字 × 3B = 300B
  const parsed = JSON.parse(json);
  assert.ok(Buffer.byteLength(parsed.name, 'utf8') <= PairPolicy.NAME_MAX_BYTES);
  assert.equal(Buffer.byteLength(parsed.name, 'utf8'), 63); // 21 字 × 3B = 63B，第 22 字放不下
});

test('buildPairReqInitJson：ASCII 名字截断到 64B', () => {
  const parsed = JSON.parse(buildPairReqInitJson('x'.repeat(100)));
  assert.equal(parsed.name.length, 64);
});

test('buildPairReqInitJson：引号/反斜杠转义后仍为合法 JSON 且体积不超 64B（解码后）', () => {
  const parsed = JSON.parse(buildPairReqInitJson('a"b\\c'));
  assert.equal(parsed.name, 'a"b\\c');
  assert.ok(Buffer.byteLength(parsed.name, 'utf8') <= PairPolicy.NAME_MAX_BYTES);
});

test('buildPairReqInitJson：空白裁剪', () => {
  const parsed = JSON.parse(buildPairReqInitJson('  我的手机  '));
  assert.equal(parsed.name, '我的手机');
});

test('sanitizeDeviceName：emoji（代理对）不劈开', () => {
  // 20 个 4B emoji + 更多内容：64B 容纳 16 个，第 17 个整体放不下
  const s = sanitizeDeviceName('😀'.repeat(30));
  assert.ok(Buffer.byteLength(s, 'utf8') <= 64);
  assert.equal(Buffer.byteLength(s, 'utf8'), 64);
  assert.equal(Array.from(s).length, 16); // 无孤立代理
});

test('sanitizeDeviceName：孤立代理按 3B（U+FFFD 体积）计入', () => {
  // 高代理孤立：\uD83D 后无低位代理 → 按 3B 计
  const s = sanitizeDeviceName('\uD83D' + 'y'.repeat(100));
  assert.equal(s, '\uD83D' + 'y'.repeat(61));
  assert.ok(Buffer.byteLength(s, 'utf8') <= 64);
});

test('buildPairReqCodeBytes：4B ASCII 数字；非法返回 null', () => {
  const ok = buildPairReqCodeBytes('1234');
  assert.ok(ok !== null);
  assert.equal(ok.length, 4);
  assert.equal(String.fromCharCode(...ok), '1234');
  assert.equal(buildPairReqCodeBytes('12 4'), null);
  assert.equal(buildPairReqCodeBytes('123'), null);
});

finish();
