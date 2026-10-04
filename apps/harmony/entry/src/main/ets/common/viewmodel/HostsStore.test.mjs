// HostsStore 纯逻辑单测：hosts_json 序列化/解析（非法 JSON/畸形条目/重复/超限）、
// upsert 去重与置默认、上限 4 拒绝、默认顺延、重命名/设默认/删除边界、名称 64B 校验。
import assert from 'node:assert';
import { HostInfo, HostsPolicy, HostsFullError,
  HostsStore, parseHosts, validateHostName } from './HostsStore.ets';

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

// 内存字符串存取（模拟 preferences 两键）。
function memKv() {
  const map = new Map();
  return {
    getString: (k) => (map.has(k) ? map.get(k) : ''),
    putString: (k, v) => {
      map.set(k, v);
    },
    raw: () => map,
  };
}

function makeStore() {
  const kv = memKv();
  return { store: new HostsStore(kv), kv };
}

function host(ip, port = 53021, name = '', tokenHex = 'aabb') {
  const h = new HostInfo();
  h.ip = ip;
  h.port = port;
  h.name = name;
  h.tokenHex = tokenHex;
  return h;
}

test('上限常量与 daemon 侧一致（DefaultMaxClients=4、MaxNameLen=64B）', () => {
  assert.equal(HostsPolicy.LIMIT, 4);
  assert.equal(HostsPolicy.NAME_MAX_BYTES, 64);
  assert.equal(HostsPolicy.KEY_HOSTS, 'hosts_json');
  assert.equal(HostsPolicy.KEY_DEFAULT, 'default_ip');
});

test('parseHosts：空串/非法 JSON/非数组 → 空列表不抛', () => {
  assert.deepEqual(parseHosts(''), []);
  assert.deepEqual(parseHosts('not-json{'), []);
  assert.deepEqual(parseHosts('{"ip":"1.2.3.4"}'), []); // 对象非数组
  assert.deepEqual(parseHosts('null'), []);
});

test('parseHosts：畸形条目跳过（缺 IP/端口越界/非整数），合法条目保留', () => {
  const json = JSON.stringify([
    { name: 'bad1', port: 53021, tokenHex: 'aa' }, // 缺 ip
    { name: 'bad2', ip: '1.1.1.1', port: 0, tokenHex: 'aa' }, // 端口越界
    { name: 'bad3', ip: '1.1.1.1', port: 53021.5, tokenHex: 'aa' }, // 非整数
    { name: 'ok', ip: '192.168.1.10', port: 53021, tokenHex: 'aabb' },
    null,
    'junk',
  ]);
  const hosts = parseHosts(json);
  assert.equal(hosts.length, 1);
  assert.equal(hosts[0].ip, '192.168.1.10');
  assert.equal(hosts[0].port, 53021);
  assert.equal(hosts[0].name, 'ok');
  assert.equal(hosts[0].tokenHex, 'aabb');
});

test('parseHosts：重复 ip+port 保留首个；超出上限 4 截断', () => {
  const json = JSON.stringify([
    { ip: '10.0.0.1', port: 53021, name: 'first' },
    { ip: '10.0.0.1', port: 53021, name: 'dup' },
    { ip: '10.0.0.2', port: 53021, name: 'h2' },
    { ip: '10.0.0.3', port: 53021, name: 'h3' },
    { ip: '10.0.0.4', port: 53021, name: 'h4' },
    { ip: '10.0.0.5', port: 53021, name: 'h5' },
  ]);
  const hosts = parseHosts(json);
  assert.equal(hosts.length, 4);
  assert.equal(hosts[0].name, 'first');
  assert.equal(hosts[3].name, 'h4');
});

test('序列化往返：操作后持久化键内容可由新实例恢复（列表+默认）', () => {
  const { store, kv } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'work'));
  store.upsert(host('10.0.0.2', 53021, 'home'));
  const restored = new HostsStore(kv);
  assert.equal(restored.listHosts().length, 2);
  assert.equal(restored.getDefault().ip, '10.0.0.2');
  assert.equal(kv.raw().get('default_ip'), '10.0.0.2');
});

test('默认回退：default_ip 未设 → 首个；列表空 → null', () => {
  const kv = memKv();
  kv.putString(HostsPolicy.KEY_HOSTS, JSON.stringify([{ ip: '10.0.0.9', port: 53021, name: '' }]));
  const s = new HostsStore(kv);
  assert.equal(s.getDefault().ip, '10.0.0.9'); // 无 default_ip 键
  assert.equal(new HostsStore(memKv()).getDefault(), null);
});

test('upsert 新增：插入列表并置为默认', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'a'));
  store.upsert(host('10.0.0.2', 53021, 'b'));
  assert.equal(store.listHosts().length, 2);
  assert.equal(store.getDefault().ip, '10.0.0.2');
});

test('upsert 同 ip+port：原位更新（不新增），置默认并刷新 name/token', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'old', '0011'));
  store.upsert(host('10.0.0.2', 53021, 'b'));
  store.upsert(host('10.0.0.1', 53021, 'new', '2233'));
  assert.equal(store.listHosts().length, 2);
  const updated = store.listHosts()[0];
  assert.equal(updated.name, 'new');
  assert.equal(updated.tokenHex, '2233');
  assert.equal(store.getDefault().ip, '10.0.0.1');
});

test('upsert 满 4 台新增 → HostsFullError；更新已有不受限', () => {
  const { store } = makeStore();
  for (let i = 1; i <= 4; i++) {
    store.upsert(host(`10.0.0.${i}`, 53021, `h${i}`));
  }
  assert.throws(() => store.upsert(host('10.0.0.5', 53021, 'h5')), HostsFullError);
  assert.throws(() => store.upsert(host('10.0.0.5', 11111, 'h5x')), HostsFullError);
  store.upsert(host('10.0.0.4', 53021, 'refreshed')); // 已存在：更新放行
  assert.equal(store.listHosts().length, 4);
  assert.equal(store.listHosts()[3].name, 'refreshed');
});

test('upsert 空 IP 拒绝（fail fast）', () => {
  const { store } = makeStore();
  assert.throws(() => store.upsert(host('')));
});

test('renameHost：成功改名并持久化；输入去首尾空白', () => {
  const { store, kv } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'old'));
  assert.equal(store.renameHost('10.0.0.1', 53021, '  工作本  '), true);
  assert.equal(new HostsStore(kv).listHosts()[0].name, '工作本');
});

test('renameHost：目标不存在 → false；名称为空/超 64B → false', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'old'));
  assert.equal(store.renameHost('10.9.9.9', 53021, 'x'), false);
  assert.equal(store.renameHost('10.0.0.1', 53021, '   '), false);
  assert.equal(store.renameHost('10.0.0.1', 53021, '一'.repeat(22)), false); // 66B > 64B
  assert.equal(store.listHosts()[0].name, 'old'); // 失败不改写
});

test('validateHostName：空拒绝、trim、CJK 字节边界（63B 过/66B 拒）、emoji 64B 边界', () => {
  assert.equal(validateHostName(''), '');
  assert.equal(validateHostName('   '), '');
  assert.equal(validateHostName('  my host  '), 'my host');
  assert.equal(validateHostName('一二三四五六七八九十'.repeat(2) + '一'), '一二三四五六七八九十'.repeat(2) + '一'); // 21×3=63B
  assert.equal(validateHostName('一二三四五六七八九十'.repeat(2) + '二二'), ''); // 22×3=66B
  assert.equal(validateHostName('x'.repeat(64)).length, 64);
  assert.equal(validateHostName('x'.repeat(65)), '');
  assert.equal(Buffer.byteLength(validateHostName('😀'.repeat(16)), 'utf8'), 64); // 16×4B
});

test('removeHost：删除成功；删默认顺延首个；删至空默认清空', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'a'));
  store.upsert(host('10.0.0.2', 53021, 'b'));
  store.upsert(host('10.0.0.3', 53021, 'c'));
  store.setDefault('10.0.0.1', 53021);
  assert.equal(store.removeHost('10.0.0.1', 53021), true);
  assert.equal(store.getDefault().ip, '10.0.0.2'); // 顺延列表首个
  assert.equal(store.removeHost('10.0.0.2', 53021), true);
  assert.equal(store.getDefault().ip, '10.0.0.3');
  assert.equal(store.removeHost('10.0.0.3', 53021), true);
  assert.equal(store.getDefault(), null);
  assert.deepEqual(store.listHosts(), []);
});

test('removeHost：目标不存在 → false', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'a'));
  assert.equal(store.removeHost('10.9.9.9', 53021), false);
  assert.equal(store.removeHost('10.0.0.1', 11111), false); // 同 IP 不同端口
  assert.equal(store.listHosts().length, 1);
});

test('setDefault：切换默认并持久化；目标不存在 → false', () => {
  const { store, kv } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'a'));
  store.upsert(host('10.0.0.2', 53021, 'b'));
  assert.equal(store.setDefault('10.0.0.1', 53021), true);
  assert.equal(kv.raw().get('default_ip'), '10.0.0.1');
  assert.equal(new HostsStore(kv).getDefault().ip, '10.0.0.1');
  assert.equal(store.setDefault('10.9.9.9', 53021), false);
  assert.equal(kv.raw().get('default_ip'), '10.0.0.1');
});

test('default_ip 失效（指向已删主机）→ 构造时回退首个', () => {
  const kv = memKv();
  kv.putString(HostsPolicy.KEY_HOSTS,
    JSON.stringify([{ ip: '10.0.0.1', port: 53021, name: 'a' }, { ip: '10.0.0.2', port: 53021, name: 'b' }]));
  kv.putString(HostsPolicy.KEY_DEFAULT, '10.9.9.9');
  assert.equal(new HostsStore(kv).getDefault().ip, '10.0.0.1');
});

test('listHosts 返回快照：外部改动不影响内部列表', () => {
  const { store } = makeStore();
  store.upsert(host('10.0.0.1', 53021, 'a'));
  const snap = store.listHosts();
  snap.push(host('10.9.9.9', 53021, 'ghost'));
  assert.equal(store.listHosts().length, 1);
});

finish();
