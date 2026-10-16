// 发版门禁（双轨版本）：daemon 与 APP 独立发版、独立版本号（对照台账 docs/RELEASES.md）。
// - daemon 发布（vX.Y.Z tag）：不要求与 APP 版本相等——APP 独立迭代；只校验 app.json5
//   内部一致性（versionCode = major×1e6 + minor×1e4 + patch×1e2），并打印记录用的版本行。
// - APP 发布（app-vX.Y.Z tag）：app.json5 的 versionName/versionCode 必须与 tag 一致。
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const tag = process.argv[2] ?? '';
const appJson = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '..', 'apps', 'harmony', 'AppScope', 'app.json5'),
  'utf8',
);
const name = appJson.match(/"versionName"\s*:\s*"([^"]+)"/)?.[1];
const code = Number(appJson.match(/"versionCode"\s*:\s*(\d+)/)?.[1]);

// 两轨共用：app.json5 内部一致性（versionName 形态 + versionCode 公式）。
const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(name ?? '');
const expectCode = m ? Number(m[1]) * 1_000_000 + Number(m[2]) * 10_000 + Number(m[3]) * 100 : NaN;
if (!m || code !== expectCode) {
  console.error(`app.json5 内部不一致：versionName=${name}（应为 X.Y.Z）versionCode=${code}（期望 ${expectCode}）`);
  process.exit(1);
}

if (tag.startsWith('app-v')) {
  const expectName = tag.slice('app-v'.length);
  if (name !== expectName) {
    console.error(`app.json5 与 tag 不一致：versionName=${name}（期望 ${expectName}）`);
    process.exit(1);
  }
  console.log(`APP 版本一致：${name} / ${code}`);
} else if (/^v\d+\.\d+\.\d+$/.test(tag)) {
  console.log(`daemon 发布 ${tag.slice(1)}；APP 版本 ${name}（${code}）独立记录于 docs/RELEASES.md`);
} else {
  console.error(`tag 格式应为 vX.Y.Z（daemon）或 app-vX.Y.Z（APP），实际 "${tag}"`);
  process.exit(1);
}
