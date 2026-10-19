import assert from 'node:assert/strict';
import { TouchpadModel } from './TouchpadModel.ets';
import { PacketType } from '../proto/PacketType.ets';
import { test, finish } from '../gesture/testlib.mjs';

// Node 无 ArkUI 运行时；这里只替代事件枚举与传输出口，执行真实 ViewModel/手势引擎。
globalThis.TouchType = { Down: 0, Up: 1, Move: 2, Cancel: 3 };

function point(id, x, y = 0) {
  return { id, x, y };
}

function event(type, ms, touches, history = [], changedTouches = []) {
  return { type, timestamp: ms * 1e6, touches, changedTouches, getHistoricalPoints: () => history };
}

function historical(ms, p) {
  return { timestamp: ms * 1e6, touchObject: p };
}

function setup() {
  const model = new TouchpadModel();
  const sent = [];
  model.bindTransport({
    sendMove: (dx, dy) => sent.push({ dx, dy }),
    sendScroll: (dy) => sent.push({ scroll: dy }),
    sendReliable: (pkt) => { sent.push({ type: pkt.type, down: pkt.button?.down }); return Promise.resolve(true); },
    nextSeq: () => 0,
  });
  model.onTouch(event(TouchType.Down, 1000, [point(1, 0)]));
  model.onTouch(event(TouchType.Move, 1100, [point(1, 12)]));
  sent.length = 0;
  return { model, sent };
}

test('非均匀历史时间戳：合并帧与逐点上报产生相同光标命令', () => {
  const batched = setup();
  const separate = setup();
  batched.model.onTouch(event(TouchType.Move, 1200, [point(1, 30)], [
    historical(1170, point(1, 18)), historical(1180, point(1, 24)),
  ]));
  for (const [ms, x] of [[1170, 18], [1180, 24], [1200, 30]]) {
    separate.model.onTouch(event(TouchType.Move, ms, [point(1, x)]));
  }
  assert.deepEqual(batched.sent, separate.sent);
  batched.model.cancelGesture();
  separate.model.cancelGesture();
});

test('当前点与历史点完全重复，不重复注入也不改变后续加速', () => {
  const repeated = setup();
  const separate = setup();
  repeated.model.onTouch(event(TouchType.Move, 1200, [point(1, 24)], [
    historical(1100, point(1, 12)), historical(1200, point(1, 24)),
  ]));
  separate.model.onTouch(event(TouchType.Move, 1200, [point(1, 24)]));
  assert.deepEqual(repeated.sent, separate.sent);
  repeated.model.cancelGesture();
  separate.model.cancelGesture();
});

test('旧历史点不回退活跃手指的位置与时间', () => {
  const stale = setup();
  const separate = setup();
  stale.model.onTouch(event(TouchType.Move, 1200, [point(1, 24)], [historical(1090, point(1, 5))]));
  separate.model.onTouch(event(TouchType.Move, 1200, [point(1, 24)]));
  assert.deepEqual(stale.sent, separate.sent);
  stale.model.cancelGesture();
  separate.model.cancelGesture();
});

test('未知指历史点不会阻止当前点升格为 DOWN，UP 后可复用手指 id', () => {
  const model = new TouchpadModel();
  model.onTouch(event(TouchType.Move, 1000, [point(1, 0)], [historical(990, point(1, -1))]));
  assert.equal(model.currentState(), 1);
  model.onTouch(event(TouchType.Up, 1100, [], [], [point(1, 0)]));
  assert.equal(model.currentState(), 0);
  model.onTouch(event(TouchType.Down, 1200, [point(1, 0)]));
  assert.equal(model.currentState(), 1);
  model.cancelGesture();
  assert.equal(model.currentState(), 0);
});

test('CANCEL 释放拖拽按键并清理活跃手指，后续历史点不产生残留移动', () => {
  const model = new TouchpadModel();
  const buttons = [];
  model.bindTransport({
    sendMove: () => {}, sendScroll: () => {}, nextSeq: () => 0,
    sendReliable: (pkt) => { buttons.push(pkt); return Promise.resolve(true); },
  });
  model.onTouch(event(TouchType.Down, 1000, [point(1, 0)]));
  model.onTouch(event(TouchType.Move, 1310, [point(1, 0.5)]));
  model.onTouch(event(TouchType.Cancel, 1320, []));
  assert.equal(model.currentState(), 0);
  assert.equal(buttons.length, 2);
  assert.ok(buttons.every((pkt) => pkt.type === PacketType.BUTTON));
  assert.deepEqual(buttons.map((pkt) => pkt.button.down), [true, false]);
  model.onTouch(event(TouchType.Down, 1400, [point(1, 0)], [historical(1315, point(1, 10))]));
  assert.equal(model.currentState(), 1);
  model.cancelGesture();
});

test('页面取消沿用触摸时钟，后续采样率继续按系统启动时间统计', () => {
  const model = new TouchpadModel();
  model.onTouch(event(TouchType.Down, 1000, [point(1, 0)]));
  model.cancelGesture();
  model.onTouch(event(TouchType.Down, 2000, [point(1, 0)]));
  assert.equal(model.getSampleRate(), 2);
  model.cancelGesture();
});

test('窗口切换取消后丢弃旧触点余帧，直到新的 DOWN 才恢复输入', () => {
  const { model, sent } = setup();
  model.cancelGesture();
  sent.length = 0;
  model.onTouch(event(TouchType.Move, 1200, [point(1, 500)], [historical(1190, point(1, 400))]));
  model.onTouch(event(TouchType.Up, 1210, [], [], [point(1, 500)]));
  assert.deepEqual(sent, []);
  assert.equal(model.currentState(), 0);
  model.onTouch(event(TouchType.Down, 1300, [point(1, 0)]));
  model.onTouch(event(TouchType.Move, 1400, [point(1, 20)]));
  assert.ok(sent.some((packet) => packet.dx > 0));
  model.cancelGesture();
});

finish();
