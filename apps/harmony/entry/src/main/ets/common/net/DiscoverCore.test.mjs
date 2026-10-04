// DiscoverCore 纯函数单测：DISCOVER_RESP JSON 解析（类型校验/端口界/非法输入）与按 IP 去重合并。
import assert from 'node:assert';
import { parseDiscoverRespJson, upsertHost, DiscoveredHost } from './DiscoverCore.ets';

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

test('合法 JSON 全字段解析 + 来源 ip 回填', () => {
  const h = parseDiscoverRespJson(
    '{"name":"办公桌","os":"linux","daemon":"0.3.0","ver":1,"paired":2,"port":53021}', '192.168.1.10');
  assert.ok(h !== null);
  assert.equal(h.name, '办公桌');
  assert.equal(h.os, 'linux');
  assert.equal(h.daemon, '0.3.0');
  assert.equal(h.ver, 1);
  assert.equal(h.paired, 2);
  assert.equal(h.port, 53021);
  assert.equal(h.ip, '192.168.1.10');
});

test('缺失可缺省字段（name/os/daemon/ver/paired）容错为默认值', () => {
  const h = parseDiscoverRespJson('{"port":53022}', '10.0.0.1');
  assert.ok(h !== null);
  assert.equal(h.name, '');
  assert.equal(h.os, '');
  assert.equal(h.daemon, '');
  assert.equal(h.ver, 0);
  assert.equal(h.paired, 0);
  assert.equal(h.port, 53022);
});

test('port 缺失 → null（连接目标必须存在）', () => {
  assert.equal(parseDiscoverRespJson('{"name":"x"}', '10.0.0.1'), null);
});

test('port 非数值 → null', () => {
  assert.equal(parseDiscoverRespJson('{"port":"53021"}', '10.0.0.1'), null);
});

test('port 越界（0 / 65536 / 非整数）→ null', () => {
  assert.equal(parseDiscoverRespJson('{"port":0}', '10.0.0.1'), null);
  assert.equal(parseDiscoverRespJson('{"port":65536}', '10.0.0.1'), null);
  assert.equal(parseDiscoverRespJson('{"port":53021.5}', '10.0.0.1'), null);
});

test('非对象 JSON（数组/数字/null）→ null', () => {
  assert.equal(parseDiscoverRespJson('[1,2]', '10.0.0.1'), null);
  assert.equal(parseDiscoverRespJson('42', '10.0.0.1'), null);
  assert.equal(parseDiscoverRespJson('null', '10.0.0.1'), null);
});

test('非法 JSON 文本 → null（不抛出）', () => {
  assert.equal(parseDiscoverRespJson('{"name":', '10.0.0.1'), null);
  assert.equal(parseDiscoverRespJson('', '10.0.0.1'), null);
});

test('字段类型错误（name 数值）→ 容错为默认值不崩', () => {
  const h = parseDiscoverRespJson('{"name":3,"port":53021}', '10.0.0.1');
  assert.ok(h !== null);
  assert.equal(h.name, '');
});

test('upsert 新 ip 追加并保持首现顺序', () => {
  const a = new DiscoveredHost(); a.ip = '10.0.0.1';
  const b = new DiscoveredHost(); b.ip = '10.0.0.2';
  const c = new DiscoveredHost(); c.ip = '10.0.0.3';
  const out = upsertHost(upsertHost(upsertHost([], a), b), c);
  assert.deepEqual(out.map((h) => h.ip), ['10.0.0.1', '10.0.0.2', '10.0.0.3']);
});

test('upsert 同 ip 覆盖原位不重复', () => {
  const a = new DiscoveredHost(); a.ip = '10.0.0.1'; a.paired = 0;
  const a2 = new DiscoveredHost(); a2.ip = '10.0.0.1'; a2.paired = 3;
  const b = new DiscoveredHost(); b.ip = '10.0.0.2';
  const out = upsertHost(upsertHost(upsertHost([], a), b), a2);
  assert.equal(out.length, 2);
  assert.equal(out[0].ip, '10.0.0.1');
  assert.equal(out[0].paired, 3);
  assert.equal(out[1].ip, '10.0.0.2');
});

test('upsert 不改入参数组（@Trace 重赋值语义）', () => {
  const a = new DiscoveredHost(); a.ip = '10.0.0.1';
  const src = [a];
  const out = upsertHost(src, a);
  assert.notEqual(out, src);
  assert.equal(src.length, 1);
});

finish();
