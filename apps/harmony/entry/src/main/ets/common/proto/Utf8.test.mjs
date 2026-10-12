// Utf8 纯函数单测：utf8ByteLen 与 utf8Encode/utf8DecodeStrict 的体积与往返一致性。
import assert from 'node:assert';
import { utf8ByteLen, utf8Encode, utf8DecodeStrict } from './Utf8.ets';

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

test('ASCII / CJK / emoji 的字节数', () => {
  assert.equal(utf8ByteLen(''), 0);
  assert.equal(utf8ByteLen('abc'), 3);
  assert.equal(utf8ByteLen('你好'), 6);
  assert.equal(utf8ByteLen('a😀b'), 1 + 4 + 1);
});

test('孤立代理按 3B（U+FFFD 体积）', () => {
  assert.equal(utf8ByteLen('\ud800'), 3);
  assert.equal(utf8ByteLen('\udc00'), 3);
  assert.equal(utf8ByteLen('a\ud800b'), 5);
});

test('utf8ByteLen 与 utf8Encode 输出长度一致', () => {
  for (const s of ['abc', 'PadLink 你好', 'a😀b\ud800', 'ßüö']) {
    assert.equal(utf8ByteLen(s), utf8Encode(s).length, s);
  }
});

test('合法文本 encode→decode 严格往返', () => {
  const s = 'PadLink 你好 😀';
  assert.equal(utf8DecodeStrict(utf8Encode(s)), s);
});

test('非法字节 decode 返回 null', () => {
  assert.equal(utf8DecodeStrict(new Uint8Array([0xff])), null);
  assert.equal(utf8DecodeStrict(new Uint8Array([0xe4, 0xbd])), null); // 截断
});

finish();
