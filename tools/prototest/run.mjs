#!/usr/bin/env node
// PadLink ArkTS 协议层对拍 harness（零依赖，node >= 26 直接运行）。
// 流程：用共享库 tools/etsrun/load.mjs 把 apps/harmony/entry/src/main/ets/common/proto/
// 复制到临时目录（.ets→.ts、相对导入补 .ts 扩展名），动态 import 后跑
// protocol/testvectors.json 全量黄金向量 + 追加负例。全过退出码 0，任一失败退出码 1。
import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { loadEtsTree, unloadEtsTree } from '../etsrun/load.mjs';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const protoDir = join(repoRoot, 'apps', 'harmony', 'entry', 'src', 'main', 'ets', 'common', 'proto');
const vectorsPath = join(repoRoot, 'protocol', 'testvectors.json');

let caseNo = 0;
let failures = 0;
function check(name, cond, detail) {
  caseNo += 1;
  const label = `PASS ${String(caseNo).padStart(2, '0')}`;
  if (cond) {
    console.log(`${label} ${name}`);
  } else {
    failures += 1;
    console.log(`FAIL ${String(caseNo).padStart(2, '0')} ${name}${detail ? ' :: ' + detail : ''}`);
  }
}

function bytesFromHex(hex) {
  return new Uint8Array(Buffer.from(hex, 'hex')).buffer;
}

function hexFromBytes(ab) {
  return Buffer.from(new Uint8Array(ab)).toString('hex');
}

const tmpRoot = loadEtsTree(protoDir, 'padlink-prototest-');
try {
  // 动态加载编解码模块与 node:crypto HMAC 实现（.ets 已由共享库转为 .ts）
  const mod = async (name) => import(pathToFileURL(join(tmpRoot, `${name}.ts`)).href);
  const PacketType = (await mod('PacketType')).PacketType;
  const Packet = await mod('Packet');
  const Codec = await mod('Codec');
  const Auth = await mod('Auth');

  class NodeHmacProvider {
    constructor(key) {
      this.key = key;
    }
    compute(input) {
      return createHmac('sha256', this.key).update(input).digest().subarray(0, 8);
    }
  }

  const vectors = JSON.parse(readFileSync(vectorsPath, 'utf8'));
  const token = Buffer.from(vectors.token_hex, 'hex');
  const provider = new NodeHmacProvider(token);

  const typeByName = {
    HELLO: PacketType.HELLO,
    DISCOVER_RESP: PacketType.DISCOVER_RESP,
    PAIR_REQ: PacketType.PAIR_REQ,
    PAIR_OK: PacketType.PAIR_OK,
    PAIR_NAK: PacketType.PAIR_NAK,
    MOVE: PacketType.MOVE,
    SCROLL: PacketType.SCROLL,
    BUTTON: PacketType.BUTTON,
    KEY: PacketType.KEY,
    TEXT: PacketType.TEXT,
    ECHO: PacketType.ECHO,
    BYE: PacketType.BYE,
    ERR: PacketType.ERR,
  };
  const errorByName = {
    bad_magic: Codec.DecodeError.BadMagic,
    bad_version: Codec.DecodeError.BadVersion,
    len_out_of_range: Codec.DecodeError.LenRange,
    truncated: Codec.DecodeError.Truncated,
    unknown_type: Codec.DecodeError.UnknownType,
    payload_len: Codec.DecodeError.PayloadLen,
    bad_utf8: Codec.DecodeError.BadUTF8,
    bad_hmac: Codec.DecodeError.BadHMAC,
  };

  // 按向量语义构造新包（与 Go 构造器对应），用于同语义重编 hex 比对
  function buildFromSemantics(v) {
    const s = v.payload_semantics;
    switch (v.type) {
      case 'MOVE':
        return Packet.newMove(v.seq, s.dx, s.dy);
      case 'SCROLL':
        return Packet.newScroll(v.seq, s.dy_hi_res);
      case 'BUTTON':
        return Packet.newButton(v.seq, s.btn, s.down);
      case 'KEY':
        return Packet.newKey(v.seq, Number.parseInt(s.hid_usage, 16), s.down);
      case 'TEXT':
        return Packet.newText(v.seq, s.text);
      case 'ECHO':
        return Packet.newEcho(v.seq, Number(s.ts_ms));
      default:
        return Packet.newRaw(typeByName[v.type], v.seq, new ArrayBuffer(0));
    }
  }

  // 断言解码后的语义字段
  function assertSemantics(v, pkt) {
    const s = v.payload_semantics;
    const diffs = [];
    const eq = (label, actual, expected) => {
      if (actual !== expected) {
        diffs.push(`${label}: 期望 ${expected}，实得 ${actual}`);
      }
    };
    switch (v.type) {
      case 'MOVE':
        eq('dx', pkt.move.dx, s.dx);
        eq('dy', pkt.move.dy, s.dy);
        break;
      case 'SCROLL':
        eq('dy_hi_res', pkt.scroll.dyHiRes, s.dy_hi_res);
        break;
      case 'BUTTON':
        eq('btn', pkt.button.btn, s.btn);
        eq('down', pkt.button.down, s.down);
        break;
      case 'KEY':
        eq('hid_usage', pkt.key.hidUsage, Number.parseInt(s.hid_usage, 16));
        eq('down', pkt.key.down, s.down);
        break;
      case 'TEXT':
        eq('text', pkt.text, s.text);
        break;
      case 'ECHO':
        eq('ts_ms', pkt.echo.tsMs, Number(s.ts_ms));
        break;
      default:
        break;
    }
    return diffs;
  }

  // 3a. valid 向量：decode 成功 + 语义字段 + 重编 hex 一致
  for (const v of vectors.vectors.filter((x) => x.valid)) {
    const ab = bytesFromHex(v.hex);
    const res = Codec.decode(ab);
    let detail = '';
    if (!res.ok) {
      detail = `decode 失败 kind=${res.kind} msg=${res.message}`;
    } else {
      const pkt = res.packet;
      const diffs = [];
      if (pkt.type !== typeByName[v.type]) {
        diffs.push(`type: 期望 ${v.type}(${typeByName[v.type]})，实得 ${pkt.type}`);
      }
      if (pkt.seq !== v.seq) {
        diffs.push(`seq: 期望 ${v.seq}，实得 ${pkt.seq}`);
      }
      diffs.push(...assertSemantics(v, pkt));
      const reencoded = hexFromBytes(Codec.encode(pkt));
      if (reencoded !== v.hex) {
        diffs.push(`重编 hex 不一致\n      期望 ${v.hex}\n      实得 ${reencoded}`);
      }
      if (!v.authenticated) {
        // 未认证向量：同语义重新构造后 encode 必须 hex 级一致；
        // 已认证向量（仅向量 2）由下方 seal 专项用例覆盖
        const rebuiltHex = hexFromBytes(Codec.encode(buildFromSemantics(v)));
        if (rebuiltHex !== v.hex) {
          diffs.push(`同语义重编 hex 不一致\n      期望 ${v.hex}\n      实得 ${rebuiltHex}`);
        }
      }
      if (diffs.length > 0) {
        detail = diffs.join('；');
      } else {
        check(`向量 ${v.id} valid ${v.type} decode+语义+回编一致`, true);
        continue;
      }
    }
    check(`向量 ${v.id} valid ${v.type} decode+语义+回编一致`, false, detail);
  }

  // 3b. 向量 2 专项：verifyHmac 真 + 同语义 seal 后逐字节复现（含 hmac）
  {
    const v = vectors.vectors.find((x) => x.id === 2);
    const pkt = Codec.decode(bytesFromHex(v.hex)).packet;
    check(
      '向量 2 verifyHmac(向量内嵌 hmac)=true',
      Auth.verifyHmac(pkt, provider) === true,
      `flags=${pkt.flags} hmac=${Buffer.from(pkt.hmac).toString('hex')}`,
    );
    const rebuilt = Packet.newMove(v.seq, v.payload_semantics.dx, v.payload_semantics.dy);
    Auth.seal(rebuilt, provider);
    const sealedHex = hexFromBytes(Codec.encode(rebuilt));
    check(
      '向量 2 同语义 seal 重编 hex 级一致（含 hmac 字段）',
      sealedHex === v.hex,
      `期望 ${v.hex}\n      实得 ${sealedHex}`,
    );
  }

  // 3c. HMAC 语义自证：独立用 node:crypto 按"flags 含 FlagAuth、11..18 清零"重算向量 2
  {
    const v = vectors.vectors.find((x) => x.id === 2);
    const raw = Buffer.from(v.hex, 'hex');
    const input = Buffer.from(raw);
    input[11] = 0;
    input[12] = 0;
    input[13] = 0;
    input[14] = 0;
    input[15] = 0;
    input[16] = 0;
    input[17] = 0;
    input[18] = 0;
    if ((input[4] & 0x01) !== 0x01) {
      check('向量 2 独立重算 hmac（flags 含 FlagAuth、11..18 清零）', false, 'flags 未置 FlagAuth，向量与 PROTOCOL.md §5 不符');
    } else {
      const expect = createHmac('sha256', token).update(input).digest().subarray(0, 8).toString('hex');
      const actual = raw.subarray(11, 19).toString('hex');
      check(
        '向量 2 独立重算 hmac 与内嵌值一致（防实现与向量双向都错）',
        expect === actual,
        `内嵌 ${actual}\n      重算 ${expect}`,
      );
    }
  }

  // 3d. invalid 向量：decode 失败且错误类别对应
  for (const v of vectors.vectors.filter((x) => !x.valid)) {
    const res = Codec.decode(bytesFromHex(v.hex));
    const expectedKind = errorByName[v.expect_error];
    check(
      `向量 ${v.id} invalid ${v.expect_error} → kind=${expectedKind}`,
      !res.ok && res.kind === expectedKind,
      res.ok ? `意外解码成功 type=${res.packet.type}` : `kind=${res.kind} msg=${res.message}`,
    );
  }

  // 3e. 追加负例
  const header = (type, flags, seq, plen) => {
    const b = Buffer.alloc(19);
    b[0] = 0x50;
    b[1] = 0x4c;
    b[2] = 1;
    b[3] = type;
    b[4] = flags;
    b.writeUInt16BE(seq, 5);
    b.writeUInt16BE(plen, 7);
    return b;
  };
  const hexOf = (buf) => buf.toString('hex');

  {
    // 未知 type 0xF0（plen=0，其余头字段合法）
    const res = Codec.decode(bytesFromHex(hexOf(header(0xf0, 0, 1, 0))));
    check('追加负例 未知 type 0xF0 → UnknownType', !res.ok && res.kind === Codec.DecodeError.UnknownType,
      res.ok ? '意外解码成功' : `kind=${res.kind}`);
  }
  {
    // ver=2
    const b = header(PacketType.MOVE, 0, 1, 0);
    b[2] = 2;
    const res = Codec.decode(bytesFromHex(hexOf(b)));
    check('追加负例 ver=2 → BadVersion', !res.ok && res.kind === Codec.DecodeError.BadVersion,
      res.ok ? '意外解码成功' : `kind=${res.kind}`);
  }
  const lenCases = [
    ['MOVE', PacketType.MOVE, 3],
    ['SCROLL', PacketType.SCROLL, 5],
    ['BUTTON', PacketType.BUTTON, 1],
    ['KEY', PacketType.KEY, 2],
    ['ECHO', PacketType.ECHO, 7],
  ];
  for (const [name, type, plen] of lenCases) {
    const b = Buffer.concat([header(type, 0, 1, plen), Buffer.alloc(plen)]);
    const res = Codec.decode(bytesFromHex(hexOf(b)));
    check(`追加负例 ${name} payload=${plen}B 长度不符 → PayloadLen`, !res.ok && res.kind === Codec.DecodeError.PayloadLen,
      res.ok ? '意外解码成功' : `kind=${res.kind} msg=${res.message}`);
  }
  {
    // TEXT 非法 UTF-8：单字节 0xFF
    const b = Buffer.concat([header(PacketType.TEXT, 0, 1, 1), Buffer.from([0xff])]);
    const res = Codec.decode(bytesFromHex(hexOf(b)));
    check('追加负例 TEXT 0xFF → BadUTF8', !res.ok && res.kind === Codec.DecodeError.BadUTF8,
      res.ok ? `意外解码成功 text=${JSON.stringify(res.packet.text)}` : `kind=${res.kind}`);
  }
  {
    // TEXT 超长编码 C0 80（必须拒绝）
    const b = Buffer.concat([header(PacketType.TEXT, 0, 1, 2), Buffer.from([0xc0, 0x80])]);
    const res = Codec.decode(bytesFromHex(hexOf(b)));
    check('追加负例 TEXT 超长编码 C0 80 → BadUTF8', !res.ok && res.kind === Codec.DecodeError.BadUTF8,
      res.ok ? `意外解码成功 text=${JSON.stringify(res.packet.text)}` : `kind=${res.kind}`);
  }
  {
    // TEXT 代理对码点 ED A0 80（U+D800，必须拒绝）
    const b = Buffer.concat([header(PacketType.TEXT, 0, 1, 3), Buffer.from([0xed, 0xa0, 0x80])]);
    const res = Codec.decode(bytesFromHex(hexOf(b)));
    check('追加负例 TEXT 代理对码点 ED A0 80 → BadUTF8', !res.ok && res.kind === Codec.DecodeError.BadUTF8,
      res.ok ? `意外解码成功 text=${JSON.stringify(res.packet.text)}` : `kind=${res.kind}`);
  }
  {
    // 向量 2 篡改 1 字节 hmac → verifyHmac=false
    const v = vectors.vectors.find((x) => x.id === 2);
    const b = Buffer.from(v.hex, 'hex');
    b[18] ^= 0xff;
    const pkt = Codec.decode(bytesFromHex(hexOf(b))).packet;
    check('追加负例 向量 2 篡改 hmac 1 字节 → verifyHmac=false', Auth.verifyHmac(pkt, provider) === false);
  }
  {
    // 未认证包（向量 1）调 verifyHmac → false（FlagAuth 未置位）
    const v = vectors.vectors.find((x) => x.id === 1);
    const pkt = Codec.decode(bytesFromHex(v.hex)).packet;
    check('追加负例 未置 FlagAuth 调 verifyHmac → false', Auth.verifyHmac(pkt, provider) === false);
  }
  {
    // 空输入 → Truncated
    const res = Codec.decode(new ArrayBuffer(0));
    check('追加负例 空输入 → Truncated', !res.ok && res.kind === Codec.DecodeError.Truncated, `kind=${res.kind}`);
  }
} finally {
  unloadEtsTree(tmpRoot);
}

if (failures > 0) {
  console.log(`\n${failures} 个用例失败，共 ${caseNo} 例`);
  process.exit(1);
}
console.log(`\n全部 ${caseNo} 例通过`);
