// GestureEngine 状态机单测：轻点/双指轻点边界、长按拖拽全流程、第二指插入取消、
// 双指滚动方向与冲账、三指切换与节流、CANCEL 复位、慢速亚像素累积。
import assert from 'node:assert';
import { GestureCommandType, GestureEngine, GestureStateName, HidButton } from './GestureEngine.ets';
import { defaultGestureConfig } from './GestureConfig.ets';
import { TouchKind } from './TouchSample.ets';
import { cancel, down, feedAll, finish, move, test, up } from './testlib.mjs';

const IDLE = GestureStateName.IDLE;
const BUTTON = GestureCommandType.BUTTON;
const CURSOR = GestureCommandType.CURSOR_MOVE;
const SCROLL = GestureCommandType.SCROLL;
const SWITCH = GestureCommandType.SWITCH_TAB;

function newEngine() {
  return new GestureEngine(defaultGestureConfig());
}

function isButton(c, btn, isDown) {
  return c.type === BUTTON && c.btn === btn && c.down === isDown;
}

test('单指轻点 149ms → 左键 down+up 成对，状态归 IDLE', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), up(1, 0, 0, 149)]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.LEFT, true));
  assert.ok(isButton(cmds[1], HidButton.LEFT, false));
  assert.equal(eng.currentState(), IDLE);
});

test('单指轻点 151ms → 无命令（超轻点窗口，未成长按）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), up(1, 0, 0, 151)]);
  assert.deepEqual(cmds, []);
  assert.equal(eng.currentState(), IDLE);
});

test('150..300ms 之间抬起（200ms）→ 无命令死区', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), up(1, 0, 0, 200)]);
  assert.deepEqual(cmds, []);
  assert.equal(eng.currentState(), IDLE);
});

test('轻点位移 7.9vp ≤ 8 → 仍是轻点', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), move(1, 7.9, 0, 100), up(1, 7.9, 0, 120)]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.LEFT, true));
  assert.ok(isButton(cmds[1], HidButton.LEFT, false));
  assert.equal(eng.currentState(), IDLE);
});

test('位移 8.1vp > 8 → 轻点出局转光标，抬起无按键命令', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), move(1, 8.1, 0, 100), up(1, 8.1, 0, 120)]);
  assert.ok(cmds.some((c) => c.type === CURSOR), '应有光标命令');
  assert.equal(cmds.filter((c) => c.type === BUTTON).length, 0, '不应有按键命令');
  assert.equal(eng.currentState(), IDLE);
});

test('双指轻点按下时间差 39ms ≤ 40 → 右键 down+up', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 20, 0, 39),
    up(1, 0, 0, 100),
    up(2, 20, 0, 110),
  ]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.RIGHT, true));
  assert.ok(isButton(cmds[1], HidButton.RIGHT, false));
  assert.equal(eng.currentState(), IDLE);
});

test('双指按下时间差 41ms > 40 → 无任何命令', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 20, 0, 41),
    up(1, 0, 0, 100),
    up(2, 20, 0, 110),
  ]);
  assert.deepEqual(cmds, []);
  assert.equal(eng.currentState(), IDLE);
});

test('长按拖拽全流程命令序列精确断言：down→hold→move×2→up', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    move(1, 0.5, 0, 310), // 0.5vp ≤8 且 310ms ≥300 → 长按成立，发左键 down
    move(1, 5.5, 0, 318), // d=5vp/8ms → gain≈1.6375 → dx=round(5*1.2*1.6375)=10
    move(1, 6.5, 0, 326), // d=1vp 慢速 gain 1 → eff=1.2-0.175=1.025 → dx=1（残差累计）
    up(1, 6.5, 0, 330),
  ]);
  assert.deepEqual(cmds, [
    { type: BUTTON, dx: 0, dy: 0, btn: HidButton.LEFT, down: true, dyHiRes: 0, direction: 0 },
    { type: CURSOR, dx: 10, dy: 0, btn: 0, down: false, dyHiRes: 0, direction: 0 },
    { type: CURSOR, dx: 1, dy: 0, btn: 0, down: false, dyHiRes: 0, direction: 0 },
    { type: BUTTON, dx: 0, dy: 0, btn: HidButton.LEFT, down: false, dyHiRes: 0, direction: 0 },
  ]);
  assert.equal(eng.currentState(), IDLE);
});

test('长按后原地抬起 350ms → 左键 down+up（无位移的拖拽即点即释）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), up(1, 0, 0, 350)]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.LEFT, true));
  assert.ok(isButton(cmds[1], HidButton.LEFT, false));
  assert.equal(eng.currentState(), IDLE);
});

test('按下未超 300ms 时光标窗口内移动不发光标（轻点候选阶段静默）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [down(1, 0, 0, 0), move(1, 3, 0, 50)]);
  assert.deepEqual(cmds, []);
});

test('第二指插入取消单指判定：无左键、无右键、全程无 cursor 泄漏', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    move(1, 3, 0, 50), // 单指判定中
    down(2, 30, 0, 100), // 时间差 100ms > 40 → 双指轻点也无资格
    move(2, 33, 0, 120),
    up(1, 3, 0, 130), // 若无插入本可判轻点
    up(2, 33, 0, 140),
  ]);
  assert.equal(cmds.filter((c) => c.type === BUTTON).length, 0, '不应有按键命令');
  assert.equal(cmds.filter((c) => c.type === CURSOR).length, 0, '不应有光标命令泄漏');
  assert.equal(eng.currentState(), IDLE);
});

test('第二指插入长按拖拽：左键保持，全部抬起才释放', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    move(1, 0.5, 0, 310), // 长按成立
    down(2, 40, 0, 320), // 插入，按键保持，无新命令
    up(1, 0.5, 0, 330), // 主指抬起，余一指 → 仍不释放
    up(2, 40, 0, 340), // 全部抬起 → 释放
  ]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.LEFT, true));
  assert.ok(isButton(cmds[1], HidButton.LEFT, false));
  assert.equal(eng.currentState(), IDLE);
});

test('双指滚动自然方向：手指下滑 +10vp → HI_RES +100（自然滚动）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 10, 200), // 轻点窗口已过 → 判滚
    move(2, 50, 10, 205),
  ]);
  assert.equal(cmds.length, 2);
  assert.equal(cmds[0].type, SCROLL);
  assert.equal(cmds[0].dyHiRes, 100);
  assert.equal(cmds[1].dyHiRes, 100);
});

test('非自然滚动取反：手指下滑 +10vp → HI_RES -100', () => {
  const cfg = defaultGestureConfig();
  cfg.naturalScroll = false;
  const eng = new GestureEngine(cfg);
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 10, 200),
    move(2, 50, 10, 205),
  ]);
  assert.equal(cmds.length, 2);
  assert.equal(cmds[0].dyHiRes, -100);
  assert.equal(cmds[1].dyHiRes, -100);
});

test('滚动冲账：尾样余量由最后一条 Scroll 带出，不丢尾', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 0.15, 200), // 1.5 单位 → trunc 发 1，余 0.5
    up(1, 0, 0.15, 210),
    up(2, 50, 0, 215), // 末指抬起 → 冲账 round(0.5)=1
  ]);
  assert.deepEqual(
    cmds.filter((c) => c.type === SCROLL).map((c) => c.dyHiRes),
    [1, 1],
  );
  assert.equal(eng.currentState(), IDLE);
});

test('滚动总量守恒：全程 Scroll 之和 = round(总位移×单位换算)', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 0.25, 200),
    move(1, 0, 0.5, 210),
    move(1, 0, 0.75, 220),
    up(1, 0, 0.75, 230),
    up(2, 50, 0, 240),
  ]);
  const total = cmds.filter((c) => c.type === SCROLL).reduce((acc, c) => acc + c.dyHiRes, 0);
  assert.equal(total, Math.round(0.75 * 10)); // 8 = 2+3+2+冲账1
  assert.equal(cmds[cmds.length - 1].type, SCROLL, '最后一条应是冲账 Scroll');
});

test('滚动中一指抬起 → 另一指继续到抬起', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 10, 200), // Scroll(100)
    up(1, 0, 10, 210),
    move(2, 50, 10, 230), // 余指继续（y 0→10）→ Scroll(100)
    up(2, 50, 10, 240), // 冲账 0 → 无命令
  ]);
  const scrolls = cmds.filter((c) => c.type === SCROLL).map((c) => c.dyHiRes);
  assert.deepEqual(scrolls, [100, 100]);
  assert.equal(eng.currentState(), IDLE);
});

test('三指水平同向 ≥60vp → SwitchTab(+1) 恰一次', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 10, 0, 5),
    down(3, 20, 0, 10),
    move(1, 70, 0, 50),
    move(2, 80, 0, 55),
    move(3, 90, 0, 60), // 第三指到位 → 触发
    up(1, 70, 0, 70),
    up(2, 80, 0, 75),
    up(3, 90, 0, 80),
  ]);
  assert.equal(cmds.length, 1);
  assert.equal(cmds[0].type, SWITCH);
  assert.equal(cmds[0].direction, 1);
  assert.equal(eng.currentState(), IDLE);
});

test('三指反向水平位移 → SwitchTab(-1)', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 100, 0, 0),
    down(2, 110, 0, 5),
    down(3, 120, 0, 10),
    move(1, 30, 0, 50),
    move(2, 40, 0, 55),
    move(3, 50, 0, 60),
  ]);
  assert.equal(cmds.length, 1);
  assert.equal(cmds[0].type, SWITCH);
  assert.equal(cmds[0].direction, -1);
});

test('三指水平但位移 59vp < 60 → 不触发', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 10, 0, 5),
    down(3, 20, 0, 10),
    move(1, 59, 0, 50),
    move(2, 69, 0, 55),
    move(3, 79, 0, 60),
  ]);
  assert.deepEqual(cmds, []);
});

test('三指垂直位移不触发（系统三指下滑为截屏，不拦截不响应）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 10, 0, 5),
    down(3, 20, 0, 10),
    move(1, 0, 70, 50),
    move(2, 10, 70, 55),
    move(3, 20, 70, 60),
  ]);
  assert.deepEqual(cmds, []);
  assert.equal(eng.currentState(), GestureStateName.THREE_PENDING);
});

test('三指切换 500ms 节流：窗口内二次触发忽略，窗口后可再触发', () => {
  const eng = newEngine();
  feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 10, 0, 5),
    down(3, 20, 0, 10),
    move(1, 70, 0, 50),
    move(2, 80, 0, 55),
    move(3, 90, 0, 60), // 第一次触发，lastSwitch=60
  ]);
  const again = feedAll(eng, [
    move(1, 140, 0, 100), // 100-60=40 < 500 → 忽略
    move(2, 150, 0, 105),
    move(3, 160, 0, 110),
    up(1, 140, 0, 120),
    up(2, 150, 0, 125),
    up(3, 160, 0, 130),
  ]);
  assert.deepEqual(again, []);
  const third = feedAll(eng, [
    down(1, 0, 0, 700),
    down(2, 10, 0, 705),
    down(3, 20, 0, 710),
    move(1, 70, 0, 740),
    move(2, 80, 0, 745),
    move(3, 90, 0, 750), // 750-60=690 ≥ 500 → 触发
  ]);
  assert.equal(third.length, 1);
  assert.equal(third[0].type, SWITCH);
  assert.equal(third[0].direction, 1);
});

test('CANCEL 拖拽：补发左键 up，后续 Move 忽略，随后新手势正常', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    move(1, 0.5, 0, 310), // 长按成立
    cancel(1, 0.5, 0, 320),
    move(1, 50, 0, 330), // 已复位：忽略
  ]);
  assert.equal(cmds.length, 2);
  assert.ok(isButton(cmds[0], HidButton.LEFT, true));
  assert.ok(isButton(cmds[1], HidButton.LEFT, false));
  assert.equal(eng.currentState(), IDLE);
  const tap = feedAll(eng, [down(1, 0, 0, 400), up(1, 0, 0, 450)]);
  assert.equal(tap.length, 2, '复位后轻点应正常');
});

test('CANCEL 滚动：冲账余量后归 IDLE，无残留命令', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    down(2, 50, 0, 10),
    move(1, 0, 10.05, 200), // 100.5 → 发 100 余 0.5
    cancel(1, 0, 10.05, 210), // 冲账 Scroll(1)
    move(2, 50, 30, 220), // 已复位：忽略
  ]);
  const scrolls = cmds.filter((c) => c.type === SCROLL).map((c) => c.dyHiRes);
  assert.deepEqual(scrolls, [100, 1]);
  assert.equal(cmds.filter((c) => c.type !== SCROLL).length, 0);
  assert.equal(eng.currentState(), IDLE);
});

test('多指抬起后光标漂移禁止：非活跃指 Move 直接忽略', () => {
  const eng = newEngine();
  feedAll(eng, [down(1, 0, 0, 0), move(1, 20, 0, 50), up(1, 20, 0, 60)]);
  const stale = feedAll(eng, [move(1, 100, 100, 999), move(2, -50, -50, 999)]);
  assert.deepEqual(stale, []);
  assert.equal(eng.currentState(), IDLE);
});

test('慢速亚像素累积：0.5vp/样本 × 4 样本 ≥ 2 计数（不丢慢速位移）', () => {
  const eng = newEngine();
  const cmds = feedAll(eng, [
    down(1, 0, 0, 0),
    move(1, 10, 0, 8), // 快速越界进 CURSOR：speed=1.25 ≥ v2 → dx=round(10*1.2*2.5)=30
    move(1, 10.5, 0, 16), // 0.6 → 1（残差 -0.4）
    move(1, 11, 0, 24), // 0.2 → 0
    move(1, 11.5, 0, 32), // 0.8 → 1（残差 -0.2）
    move(1, 12, 0, 40), // 0.4 → 0
  ]);
  assert.equal(cmds[0].dx, 30);
  const slow = cmds.slice(1);
  const sum = slow.reduce((acc, c) => acc + c.dx, 0);
  assert.ok(sum >= 2, `慢速 4 样本合计 ${sum} 计数，应 ≥2`);
  assert.ok(slow.every((c) => Math.abs(c.dx) <= 1), '每样本至多 1 计数');
});

test('配置注入：低灵敏度改变光标计数', () => {
  const cfg = defaultGestureConfig();
  cfg.sensitivity = 0.5;
  const eng = new GestureEngine(cfg);
  const cmds = feedAll(eng, [down(1, 0, 0, 0), move(1, 10, 0, 8)]);
  assert.equal(cmds.length, 1);
  assert.equal(cmds[0].dx, 13); // 10*0.5*2.5 = 12.5 → round 13
});

test('TouchKind 常量完备（DOWN/MOVE/UP/CANCEL 互异）', () => {
  const kinds = [TouchKind.DOWN, TouchKind.MOVE, TouchKind.UP, TouchKind.CANCEL];
  assert.equal(new Set(kinds).size, 4);
});

finish();
