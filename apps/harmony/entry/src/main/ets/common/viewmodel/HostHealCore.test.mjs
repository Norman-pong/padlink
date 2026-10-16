// HostHealCore 纯逻辑单测：候选判定（身份命中/地址变化/无身份）与迁移档案构造。
import assert from 'node:assert';
import { pickHealCandidate, buildMigratedHost } from './HostHealCore.ets';
import { HostInfo } from './HostsStore.ets';
import { DiscoveredHost } from '../net/DiscoverCore.ets';

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

const ID = '00112233445566778899aabbccddeeff';

function storedHost(ip = '192.168.1.10', hostId = ID) {
  const h = new HostInfo();
  h.name = 'office';
  h.ip = ip;
  h.port = 53021;
  h.tokenHex = 'aabb';
  h.hostId = hostId;
  return h;
}

function found(ip, hostId = ID, port = 53021) {
  const d = new DiscoveredHost();
  d.ip = ip;
  d.port = port;
  d.name = 'office';
  d.hostId = hostId;
  return d;
}

test('同 host_id 不同 IP → 命中候选', () => {
  const cand = pickHealCandidate([found('192.168.1.10'), found('10.0.0.5')], storedHost('192.168.1.10'));
  assert.ok(cand !== null);
  assert.equal(cand.ip, '10.0.0.5');
});

test('同 host_id 同 IP 不同端口 → 命中候选', () => {
  const cand = pickHealCandidate([found('192.168.1.10', ID, 53099)], storedHost('192.168.1.10'));
  assert.ok(cand !== null);
  assert.equal(cand.port, 53099);
});

test('档案无 hostId → 永不候选（旧档案走原 ip:port 路径）', () => {
  assert.equal(pickHealCandidate([found('10.0.0.5')], storedHost('192.168.1.10', '')), null);
});

test('host_id 不同 → 不候选', () => {
  assert.equal(pickHealCandidate([found('10.0.0.5', 'ff'.repeat(16))], storedHost()), null);
});

test('地址相同 → 不候选（无需迁移）', () => {
  assert.equal(pickHealCandidate([found('192.168.1.10')], storedHost('192.168.1.10')), null);
});

test('空发现列表 → 不候选', () => {
  assert.equal(pickHealCandidate([], storedHost()), null);
});

test('迁移档案：沿用身份与 token，仅换地址', () => {
  const m = buildMigratedHost(storedHost('192.168.1.10'), found('10.0.0.5', ID, 53099));
  assert.equal(m.ip, '10.0.0.5');
  assert.equal(m.port, 53099);
  assert.equal(m.hostId, ID);
  assert.equal(m.tokenHex, 'aabb');
  assert.equal(m.name, 'office');
});

test('迁移档案：档案名为空时取发现名', () => {
  const t = storedHost();
  t.name = '';
  const m = buildMigratedHost(t, found('10.0.0.5'));
  assert.equal(m.name, 'office');
});

finish();
