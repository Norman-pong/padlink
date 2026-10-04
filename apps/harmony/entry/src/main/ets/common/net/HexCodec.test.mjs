// HexCodec 纯函数单测：hex↔bytes 往返、非法输入、大小写兼容。
import assert from 'node:assert';
import { hexToBytes, bytesToHex } from './HexCodec.ets';

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

test('合法 hex 解码（64 字符 = 32B token）', () => {
  const hex = '00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff';
  const out = hexToBytes(hex);
  assert.ok(out !== null);
  assert.equal(out.length, 32);
  assert.equal(out[0], 0x00);
  assert.equal(out[1], 0x11);
  assert.equal(out[31], 0xff);
});

test('大写 hex 兼容', () => {
  const out = hexToBytes('ABCD');
  assert.ok(out !== null);
  assert.equal(out[0], 0xab);
  assert.equal(out[1], 0xcd);
});

test('空串与奇数长 → null', () => {
  assert.equal(hexToBytes(''), null);
  assert.equal(hexToBytes('abc'), null);
});

test('非 hex 字符 → null', () => {
  assert.equal(hexToBytes('zz'), null);
  assert.equal(hexToBytes('12 4'), null);
  assert.equal(hexToBytes('0x12'), null);
});

test('bytesToHex 输出小写并解码往返一致', () => {
  const bytes = new Uint8Array([0x00, 0x0f, 0xa0, 0xff]);
  const hex = bytesToHex(bytes);
  assert.equal(hex, '000fa0ff');
  const back = hexToBytes(hex);
  assert.ok(back !== null);
  assert.deepEqual(Array.from(back), Array.from(bytes));
});

finish();
