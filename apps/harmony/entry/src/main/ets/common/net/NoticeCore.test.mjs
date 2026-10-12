// TEXT 未上屏原因码 → 文案资源键单测：每种已知原因各归其键，未知/越界回落通用键。
import assert from 'node:assert';
import { TextFailReason, textFailToastKey } from './NoticeCore.ets';

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

test('未知原因（0）回落通用文案', () => {
  assert.equal(textFailToastKey(TextFailReason.UNKNOWN), 'notice_text_failed');
});

test('剪贴板非文本（1）', () => {
  assert.equal(textFailToastKey(TextFailReason.NON_TEXT), 'notice_text_failed_nontext');
});

test('剪贴板工具不可用（2）', () => {
  assert.equal(textFailToastKey(TextFailReason.UNAVAILABLE), 'notice_text_failed_unavailable');
});

test('未知数值（99/-1）回落通用文案', () => {
  assert.equal(textFailToastKey(99), 'notice_text_failed');
  assert.equal(textFailToastKey(-1), 'notice_text_failed');
});

finish();
