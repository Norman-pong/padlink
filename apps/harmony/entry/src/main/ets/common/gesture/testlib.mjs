// 手势单测共用小工具（node:assert 风格，零依赖）。
// 事件类别取自源码 TouchSample.ets（harness 加载时改写为 .ts 导入）。
import { TouchKind } from './TouchSample.ets';

let caseNo = 0;
let failed = 0;

// 单例断言入口：fn 内用 assert 抛错即记失败。
export function test(name, fn) {
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
export function finish() {
  if (failed > 0) {
    console.log(`\n${failed}/${caseNo} 例失败`);
    process.exit(1);
  }
  console.log(`\n本文件 ${caseNo} 例全过`);
}

export function down(id, x, y, t) {
  return { pointerId: id, x: x, y: y, kind: TouchKind.DOWN, tMs: t };
}

export function move(id, x, y, t) {
  return { pointerId: id, x: x, y: y, kind: TouchKind.MOVE, tMs: t };
}

export function up(id, x, y, t) {
  return { pointerId: id, x: x, y: y, kind: TouchKind.UP, tMs: t };
}

export function cancel(id, x, y, t) {
  return { pointerId: id, x: x, y: y, kind: TouchKind.CANCEL, tMs: t };
}

// 依次喂样本，收集全部命令。
export function feedAll(engine, samples) {
  const out = [];
  for (const s of samples) {
    for (const c of engine.feed(s)) {
      out.push(c);
    }
  }
  return out;
}
