// EchoGuard 纯逻辑单测：token 会话拒收未封签 ECHO，其余放行。
import assert from 'node:assert';
import { shouldDropUnsealed } from './EchoGuard.ets';
import { PacketType, PacketFlag } from '../proto/PacketType.ets';

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

const ECHO = PacketType.ECHO;
const AUTH = PacketFlag.AUTH;

test('token 会话：未封签 ECHO 拒收', () => {
  assert.equal(shouldDropUnsealed(ECHO, 0, true), true);
});

test('token 会话：封签 ECHO 放行（交验签）', () => {
  assert.equal(shouldDropUnsealed(ECHO, AUTH, true), false);
});

test('token 会话：未封签 HELLO / ERR / NOTICE 不拒收（协议本就不封签）', () => {
  assert.equal(shouldDropUnsealed(PacketType.HELLO, 0, true), false);
  assert.equal(shouldDropUnsealed(PacketType.ERR, 0, true), false);
  assert.equal(shouldDropUnsealed(PacketType.NOTICE, 0, true), false);
});

test('无 token 会话（配对流）：一律放行（PAIR_OK 由 PairingModel 端到端自证）', () => {
  assert.equal(shouldDropUnsealed(ECHO, 0, false), false);
  assert.equal(shouldDropUnsealed(ECHO, AUTH, false), false);
});

finish();
