// 发现浮层态规则单测：断开态可挂 DISCOVERING（幂等）、活动会话态拒绝、回收只认自写的态。
import assert from 'node:assert';
import { canEnterDiscoverOverlay, shouldExitDiscoverOverlay } from './DiscoverOverlay.ets';
import { ConnState } from './ConnState.ets';

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

test('断开态（IDLE/LOST）可挂发现浮层', () => {
  assert.equal(canEnterDiscoverOverlay(ConnState.IDLE), true);
  assert.equal(canEnterDiscoverOverlay(ConnState.LOST), true);
});

test('已在 DISCOVERING 可重入（幂等）', () => {
  assert.equal(canEnterDiscoverOverlay(ConnState.DISCOVERING), true);
});

test('活动会话态（PAIRING/CONNECTED/RECONNECTING）拒绝', () => {
  assert.equal(canEnterDiscoverOverlay(ConnState.PAIRING), false);
  assert.equal(canEnterDiscoverOverlay(ConnState.CONNECTED), false);
  assert.equal(canEnterDiscoverOverlay(ConnState.RECONNECTING), false);
});

test('回收只认自写的 DISCOVERING 态', () => {
  assert.equal(shouldExitDiscoverOverlay(ConnState.DISCOVERING), true);
});

test('非 DISCOVERING 态不回收（不误伤活动会话）', () => {
  assert.equal(shouldExitDiscoverOverlay(ConnState.IDLE), false);
  assert.equal(shouldExitDiscoverOverlay(ConnState.LOST), false);
  assert.equal(shouldExitDiscoverOverlay(ConnState.CONNECTED), false);
  assert.equal(shouldExitDiscoverOverlay(ConnState.RECONNECTING), false);
  assert.equal(shouldExitDiscoverOverlay(ConnState.PAIRING), false);
});

finish();
