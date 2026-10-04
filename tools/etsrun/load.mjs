// 共享 .ets 加载库：把 ArkTS 源码目录树复制到 os.tmpdir() 下 mkdtemp 的临时目录，
// 供 node（type-stripping）直接运行。复制时 .ets 改名为 .ts，相对导入补 .ts 扩展名。
// 零第三方依赖。调用方用 try/finally 调 unloadEtsTree 清理，不残留。
import { cpSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { extname, join } from 'node:path';

// 参与加载的文本源类型；其中 .ets 复制时改名为 .ts。
const TEXT_EXTS = new Set(['.ets', '.ts', '.mjs']);

// 匹配静态 `from '...'` 与动态 `import('...')` 中的相对路径导入。
const REL_IMPORT_RE = /(from\s+|import\s*\(\s*)(')(\.[^']*)(')/g;

// 相对导入补 .ts：'.ets' 后缀一律改 '.ts'（测试文件的写法）；
// 无扩展名的相对导入仅在 .ets 源内补全（ArkTS 风格），其余文件原样保留。
function rewriteImports(code, isEtsSource) {
  return code.replace(REL_IMPORT_RE, (_m, lead, q1, spec, q2) => {
    if (spec.endsWith('.ets')) {
      return `${lead}${q1}${spec.slice(0, -4)}.ts${q2}`;
    }
    if (isEtsSource) {
      return `${lead}${q1}${spec}.ts${q2}`;
    }
    return `${lead}${q1}${spec}${q2}`;
  });
}

function rewriteTree(dir) {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) {
      rewriteTree(p);
      continue;
    }
    const ext = extname(entry);
    if (!TEXT_EXTS.has(ext)) {
      continue;
    }
    const isEts = ext === '.ets';
    const out = rewriteImports(readFileSync(p, 'utf8'), isEts);
    if (isEts) {
      rmSync(p);
      writeFileSync(p.slice(0, -4) + '.ts', out);
    } else {
      writeFileSync(p, out);
    }
  }
}

// 复制 sourceDir 整棵树到临时目录并完成改写，返回临时根路径。
export function loadEtsTree(sourceDir, tmpPrefix) {
  const tmpRoot = mkdtempSync(join(tmpdir(), tmpPrefix));
  cpSync(sourceDir, tmpRoot, { recursive: true });
  rewriteTree(tmpRoot);
  return tmpRoot;
}

// 递归删除 loadEtsTree 产生的临时目录（finally 中调用）。
export function unloadEtsTree(tmpRoot) {
  rmSync(tmpRoot, { recursive: true, force: true });
}
