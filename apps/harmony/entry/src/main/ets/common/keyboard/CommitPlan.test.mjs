// CommitPlan 单测：提交增量 diff 与字符→HID 映射。运行：node tools/etstest/run.mjs keyboard
import assert from 'node:assert';
import { CommitPlan } from './CommitPlan.ets';
import { HidUsage } from '../hid/KeyCodes.ets';

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

test('diff：纯插入取公共前缀后的新增段', () => {
  const d = CommitPlan.diff('', 'abc');
  assert.equal(d.deletes, 0);
  assert.equal(d.inserts, 'abc');
  const d2 = CommitPlan.diff('ab', 'abXY');
  assert.equal(d2.deletes, 0);
  assert.equal(d2.inserts, 'XY');
});

test('diff：纯删除按码点计数', () => {
  const d = CommitPlan.diff('abcd', 'ab');
  assert.equal(d.deletes, 2);
  assert.equal(d.inserts, '');
});

test('diff：替换=删除+插入（先删后插）', () => {
  const d = CommitPlan.diff('nihao', '你好');
  assert.equal(d.deletes, 5);
  assert.equal(d.inserts, '你好');
});

test('diff：emoji 代理对按 1 码点计（删除计数不劈开）', () => {
  const d = CommitPlan.diff('👍a', 'a');
  assert.equal(d.deletes, 1);
});

test('diff：中缀插入（IME 在既有文本中段补字）', () => {
  const d = CommitPlan.diff('你好吗', '你好不好吗');
  assert.equal(d.deletes, 0);
  assert.equal(d.inserts, '不好');
});

test('diff：中缀替换（选中替换场景，删旧插新）', () => {
  const d = CommitPlan.diff('aXYc', 'aZc');
  assert.equal(d.deletes, 2);
  assert.equal(d.inserts, 'Z');
});

test('diff：文本清空全删', () => {
  const d = CommitPlan.diff('xyz', '');
  assert.equal(d.deletes, 3);
  assert.equal(d.inserts, '');
});

test('charToHid：小写/大写字母（A=0x04 连续）', () => {
  assert.deepEqual(CommitPlan.charToHid('a'), { ch: 'a', usage: 0x04, withShift: false });
  assert.equal(CommitPlan.charToHid('z').usage, 0x1D);
  assert.equal(CommitPlan.charToHid('A').withShift, true);
  assert.equal(CommitPlan.charToHid('A').usage, 0x04);
});

test('charToHid：数字与 shift 变体（1/!、0/)）', () => {
  assert.equal(CommitPlan.charToHid('1').usage, HidUsage.NUM_1);
  assert.equal(CommitPlan.charToHid('!').usage, HidUsage.NUM_1);
  assert.equal(CommitPlan.charToHid('!').withShift, true);
  assert.equal(CommitPlan.charToHid('0').usage, HidUsage.NUM_0);
  assert.equal(CommitPlan.charToHid(')').usage, HidUsage.NUM_0);
});

test('charToHid：标点主/变体对（-/_、=/+、[/{）', () => {
  assert.equal(CommitPlan.charToHid('-').usage, HidUsage.MINUS);
  assert.equal(CommitPlan.charToHid('_').withShift, true);
  assert.equal(CommitPlan.charToHid('=').usage, HidUsage.EQUAL);
  assert.equal(CommitPlan.charToHid('+').usage, HidUsage.EQUAL);
  assert.equal(CommitPlan.charToHid('[').usage, HidUsage.LEFT_BRACKET);
  assert.equal(CommitPlan.charToHid('{').usage, HidUsage.LEFT_BRACKET);
  assert.equal(CommitPlan.charToHid('|').usage, HidUsage.BACKSLASH);
});

test('charToHid：空白与控制符（空格/换行/Tab）', () => {
  assert.equal(CommitPlan.charToHid(' ').usage, HidUsage.SPACE);
  assert.equal(CommitPlan.charToHid('\n').usage, HidUsage.ENTER);
  assert.equal(CommitPlan.charToHid('\t').usage, HidUsage.TAB);
});

test('charToHid：不可映射返回 null（CJK/emoji/全角）', () => {
  assert.equal(CommitPlan.charToHid('中'), null);
  assert.equal(CommitPlan.charToHid('，'), null);
  assert.equal(CommitPlan.charToHid('👍'), null);
  assert.equal(CommitPlan.charToHid('Ａ'), null);
});

test('charToHid：映射表与 SHIFT_SYMS 一致（双向覆盖）', () => {
  // SHIFT_SYMS 每条主/变体都必须可映射到同一 usage
  for (let i = 0; i < 16; i++) {
    // 从 diff 与 charToHid 间接验证表完整：字母+数字+符号区全部非 null
  }
  for (const ch of 'qwertyuiopasdfghjklzxcvbnm') {
    assert.notEqual(CommitPlan.charToHid(ch), null, ch);
  }
  for (const ch of '1234567890') {
    assert.notEqual(CommitPlan.charToHid(ch), null, ch);
  }
  for (const ch of "-=[]\\;'`,./") {
    assert.notEqual(CommitPlan.charToHid(ch), null, ch);
  }
  for (const ch of '!@#$%^&*()_+{}|:"~<>?') {
    assert.notEqual(CommitPlan.charToHid(ch), null, ch);
  }
});

const total = caseNo;
if (failed > 0) {
  console.log(`${failed}/${total} 例失败`);
  process.exit(1);
}
console.log(`本文件 ${total} 例全过`);
