// Accel 纯函数单测：加速曲线三段值/边界、toCounts 四舍五入与残差、方向无关性。
import assert from 'node:assert';
import { gain, toCounts } from './Accel.ets';
import { defaultGestureConfig } from './GestureConfig.ets';
import { finish, test } from './testlib.mjs';

const cfg = defaultGestureConfig();

test('增益第一段：v=0.1 ≤ v1 → 1.0', () => {
  assert.equal(gain(0.1, cfg), 1.0);
});

test('增益边界：v=v1 → 1.0（含边界）', () => {
  assert.equal(gain(0.2, cfg), 1.0);
});

test('增益中段：v=0.7 → 线性内插 ≈1.75', () => {
  const g = gain(0.7, cfg);
  assert.ok(Math.abs(g - 1.75) < 1e-9, `实得 ${g}`);
});

test('增益边界：v=v2 → maxGain（含边界）', () => {
  assert.equal(gain(1.2, cfg), cfg.accelMaxGain);
});

test('增益第三段：v=2.0 > v2 → 恒 maxGain=2.5', () => {
  assert.equal(gain(2.0, cfg), 2.5);
});

test('toCounts 四舍五入：d=1vp/4ms → dx=1，残差回传', () => {
  // speed=0.25 → gain=1.075 → eff=1.2*1.075=1.29 → round=1，残差 0.29
  const r = toCounts(1, 0, 4, cfg, 0, 0);
  assert.equal(r.dx, 1);
  assert.equal(r.dy, 0);
  assert.ok(Math.abs(r.resX - 0.29) < 1e-9, `resX 实得 ${r.resX}`);
});

test('toCounts 负方向：d=(0,-4)/10ms → dy=-6（速度取模，方向无关）', () => {
  // speed=0.4 → gain=1.3 → effY=-4*1.2*1.3=-6.24 → round=-6
  const r = toCounts(0, -4, 10, cfg, 0, 0);
  assert.equal(r.dy, -6);
  assert.equal(r.dx, 0);
  assert.ok(Math.abs(r.resY - -0.24) < 1e-9, `resY 实得 ${r.resY}`);
});

test('toCounts 残差入参参与累计：0.5vp+残差0.29 → 进位', () => {
  // eff=0.5*1.2*1.0+0.29=0.89 → round=1
  const r = toCounts(0.5, 0, 10, cfg, 0.29, 0);
  assert.equal(r.dx, 1);
});

test('toCounts 纯函数确定性：同入参两次调用结果一致', () => {
  const a = toCounts(3, 2, 5, cfg, 0.1, -0.2);
  const b = toCounts(3, 2, 5, cfg, 0.1, -0.2);
  assert.deepEqual(a, b);
});

test('toCounts dt≤0 按 1ms 计（不产生 NaN/Infinity）', () => {
  const r = toCounts(0, 0, 0, cfg, 0, 0);
  assert.equal(r.dx, 0);
  assert.equal(r.dy, 0);
  assert.ok(Number.isFinite(r.resX) && Number.isFinite(r.resY));
});

finish();
