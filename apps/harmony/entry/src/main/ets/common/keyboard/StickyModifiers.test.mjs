// StickyModifiers 粘滞修饰键状态机单测：
// 粘滞点亮/取消、自动复位、组合序列顺序精确断言（Shift+Ctrl+a 的 6 条 down/up）、
// 多修饰叠加、纯修饰点按不发键、符号层临时 Shift。
// 运行：node tools/etstest/run.mjs keyboard
// 说明：本目录仅此一个测试文件，runner 内联（harness 按目录加载，不能跨目录复用 gesture/testlib.mjs）。
import assert from 'node:assert';
import { StickyModifiers, StickyMod } from './StickyModifiers.ets';

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

// 文件末尾调用：有失败以退出码 1 结束（供 harness 判定）。
function finish() {
  if (failed > 0) {
    console.log(`\n${failed}/${caseNo} 例失败`);
    process.exit(1);
  }
  console.log(`\n本文件 ${caseNo} 例全过`);
}

// 序列投影成 [hid, down] 对，便于整体精确断言。
function proj(seq) {
  return seq.map((s) => [s.hid, s.down]);
}

test('纯修饰点按不发键：toggle 只翻状态、不产出序列', () => {
  const sm = new StickyModifiers();
  assert.equal(sm.toggle(StickyMod.SHIFT), true);
  assert.equal(sm.isLatched(StickyMod.SHIFT), true);
  assert.equal(sm.anyLatched(), true);
  assert.equal(sm.toggle(StickyMod.SHIFT), false);
  assert.equal(sm.isLatched(StickyMod.SHIFT), false);
  assert.equal(sm.anyLatched(), false);
  assert.deepEqual(sm.latchedUsages(), []);
});

test('粘滞点亮保持：点 Shift 后状态保持，直至非修饰键触发', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SHIFT);
  sm.toggle(StickyMod.CTRL);
  sm.toggle(StickyMod.SHIFT); // 取消 Shift，Ctrl 仍保持
  assert.equal(sm.isLatched(StickyMod.SHIFT), false);
  assert.equal(sm.isLatched(StickyMod.CTRL), true);
  assert.deepEqual(sm.latchedUsages(), [0xE0]);
});

test('组合序列顺序精确断言：Shift+Ctrl+a 共 6 条 down/up', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SHIFT);
  sm.toggle(StickyMod.CTRL); // 先 Shift 后 Ctrl，但发送序固定
  const seq = sm.takeSequence(0x04); // 'a'
  assert.equal(seq.length, 6);
  assert.deepEqual(proj(seq), [
    [0xE1, true], // Shift down（固定序第一位）
    [0xE0, true], // Ctrl down
    [0x04, true], // a down
    [0x04, false], // a up
    [0xE0, false], // Ctrl up（逆序）
    [0xE1, false], // Shift up（逆序）
  ]);
});

test('多修饰叠加：Ctrl+Alt+Super+Shift 固定序 down、逆序 up', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SUPER);
  sm.toggle(StickyMod.ALT);
  sm.toggle(StickyMod.CTRL);
  sm.toggle(StickyMod.SHIFT); // 乱序点亮
  const seq = sm.takeSequence(0x06); // 'c'
  assert.equal(seq.length, 10);
  assert.deepEqual(proj(seq), [
    [0xE1, true], [0xE0, true], [0xE2, true], [0xE3, true], // Shift Ctrl Alt Super down（固定序）
    [0x06, true], [0x06, false],
    [0xE3, false], [0xE2, false], [0xE0, false], [0xE1, false], // 逆序 up
  ]);
});

test('非修饰键触发后粘滞态全部自动复位', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SHIFT);
  sm.toggle(StickyMod.ALT);
  const seq = sm.takeSequence(0x04);
  assert.equal(seq.length, 6);
  assert.equal(sm.anyLatched(), false);
  assert.equal(sm.isLatched(StickyMod.SHIFT), false);
  assert.equal(sm.isLatched(StickyMod.ALT), false);
  // 复位后再次按键：仅主键 down/up 两条
  assert.deepEqual(proj(sm.takeSequence(0x04)), [[0x04, true], [0x04, false]]);
});

test('latchedUsages 固定序：乱序点亮仍按 Shift→Ctrl→Alt→Super 输出', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SUPER);
  sm.toggle(StickyMod.SHIFT);
  assert.deepEqual(sm.latchedUsages(), [0xE1, 0xE3]);
});

test('clear() 显式复位全部粘滞态', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.CTRL);
  sm.clear();
  assert.equal(sm.anyLatched(), false);
});

test('符号层临时 Shift（extraShift）：无粘滞时合成 4 条序列', () => {
  const sm = new StickyModifiers();
  const seq = sm.takeSequence(0x1E, true); // '!' = Shift+1
  assert.deepEqual(proj(seq), [
    [0xE1, true], [0x1E, true], [0x1E, false], [0xE1, false],
  ]);
  assert.equal(sm.anyLatched(), false); // 临时 Shift 不留粘滞
});

test('extraShift 与已点亮的 Shift 不重复：仍只发一次 Shift down/up', () => {
  const sm = new StickyModifiers();
  sm.toggle(StickyMod.SHIFT);
  const seq = sm.takeSequence(0x1E, true);
  assert.equal(seq.length, 4);
  assert.deepEqual(proj(seq), [
    [0xE1, true], [0x1E, true], [0x1E, false], [0xE1, false],
  ]);
  assert.equal(sm.anyLatched(), false);
});

finish();
