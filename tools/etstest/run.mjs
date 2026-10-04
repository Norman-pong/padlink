#!/usr/bin/env node
// PadLink ArkTS 纯逻辑层单测 harness（零依赖，node >= 26 直接运行）。
// 用共享库 tools/etsrun/load.mjs 把 apps/harmony/entry/src/main/ets/common/<目录>
// 复制到临时目录（.ets→.ts、相对导入补 .ts 扩展名），自动发现树内 *.test.mjs / *.test.ts，
// 逐个用 node 子进程执行（相互隔离）。全过退出码 0，任一失败退出码 1。
// 用法：node tools/etstest/run.mjs [common 下子目录名，默认 gesture]
import { spawnSync } from 'node:child_process';
import { readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadEtsTree, unloadEtsTree } from '../etsrun/load.mjs';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const commonDir = join(repoRoot, 'apps', 'harmony', 'entry', 'src', 'main', 'ets', 'common');
const dirArg = process.argv[2] ?? 'gesture';
const sourceDir = join(commonDir, dirArg);

function findTestFiles(dir, out) {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) {
      findTestFiles(p, out);
    } else if (entry.endsWith('.test.mjs') || entry.endsWith('.test.ts')) {
      out.push(p);
    }
  }
  return out;
}

const tmpRoot = loadEtsTree(sourceDir, 'padlink-etstest-');
try {
  const tests = findTestFiles(tmpRoot, []).sort();
  if (tests.length === 0) {
    console.error(`未发现 *.test.mjs / *.test.ts（common/${dirArg}）`);
    process.exit(1);
  }
  let failedFiles = 0;
  let totalCases = 0;
  for (const t of tests) {
    const label = relative(tmpRoot, t);
    const res = spawnSync(process.execPath, [t], { encoding: 'utf8' });
    const out = res.stdout ?? '';
    process.stdout.write(out);
    const cases = (out.match(/^PASS /gm) ?? []).length;
    totalCases += cases;
    // 防御：测试文件漏调 finish() 时失败会被吞掉，这里按失败统计行兜底
    const failedCases = (out.match(/^(\d+)\/\d+ 例失败$/m) ?? [])[1];
    if (res.status !== 0 || failedCases !== undefined) {
      failedFiles += 1;
      process.stderr.write(res.stderr ?? '');
      console.log(`FAIL 文件 ${label}（退出码 ${res.status}）`);
    } else {
      console.log(`PASS 文件 ${label}（${cases} 例）`);
    }
  }
  if (failedFiles > 0) {
    console.log(`\n${failedFiles}/${tests.length} 个测试文件失败，共 ${totalCases} 例`);
    process.exit(1);
  }
  console.log(`\n全部 ${tests.length} 个测试文件 ${totalCases} 例通过`);
} finally {
  unloadEtsTree(tmpRoot);
}
