// OneShotWaiter 单测：先到先得、迟到丢弃、超时收口。
import assert from 'node:assert';
import { OneShotWaiter } from './OneShotWaiter.ets';

let caseNo = 0;
let failed = 0;
const jobs = [];

function test(name, fn) {
  jobs.push(Promise.resolve().then(() => fn()).then(
    () => {
      caseNo += 1;
      console.log(`PASS ${String(caseNo).padStart(2, '0')} ${name}`);
    },
    (e) => {
      caseNo += 1;
      failed += 1;
      console.log(`FAIL ${String(caseNo).padStart(2, '0')} ${name} :: ${e && e.message ? e.message : e}`);
    },
  ));
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

test('deliver 先于超时 → 取到投递值', async () => {
  const w = new OneShotWaiter();
  const p = w.wait(200, 'timeout');
  w.deliver('reply');
  assert.equal(await p, 'reply');
});

test('超时先到 → 取到超时值', async () => {
  const w = new OneShotWaiter();
  const t0 = Date.now();
  assert.equal(await w.wait(20, 'timeout'), 'timeout');
  assert.ok(Date.now() - t0 >= 15);
});

test('settle 后迟到的 deliver 丢弃（不抛、不改结果）', async () => {
  const w = new OneShotWaiter();
  const p = w.wait(200, 'timeout');
  w.deliver('first');
  w.deliver('late'); // 第二次投递必须无效果
  assert.equal(await p, 'first');
  await sleep(30);
  w.deliver('even-later'); // settle 后再投递不抛出
});

test('超时 settle 后的 deliver 同样丢弃', async () => {
  const w = new OneShotWaiter();
  const p = w.wait(10, 'timeout');
  await sleep(30);
  w.deliver('late');
  assert.equal(await p, 'timeout');
});

test('同一实例二次 wait 不再接受 deliver（旧 settle 生效）', async () => {
  const w = new OneShotWaiter();
  const p1 = w.wait(10, 't1');
  await sleep(25);
  w.deliver('ignored');
  assert.equal(await p1, 't1');
});

await Promise.all(jobs);
if (failed > 0) {
  console.log(`\n${failed}/${caseNo} 例失败`);
  process.exit(1);
}
console.log(`\n本文件 ${caseNo} 例全过`);
