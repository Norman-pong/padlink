// 发版门禁：app.json5 的 versionName/versionCode 必须与 git tag 一致（由 release.yml 调用）。
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const tag = process.argv[2] ?? '';
if (!/^v\d+\.\d+\.\d+$/.test(tag)) {
  console.error(`tag 格式应为 vX.Y.Z，实际 "${tag}"`);
  process.exit(1);
}
const appJson = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '..', 'apps', 'harmony', 'AppScope', 'app.json5'),
  'utf8',
);
const name = appJson.match(/"versionName"\s*:\s*"([^"]+)"/)?.[1];
const code = Number(appJson.match(/"versionCode"\s*:\s*(\d+)/)?.[1]);
const expectName = tag.slice(1);
const [major, minor, patch] = expectName.split('.').map(Number);
const expectCode = major * 1_000_000 + minor * 10_000 + patch * 100;
if (name !== expectName || code !== expectCode) {
  console.error(`app.json5 与 tag 不一致：versionName=${name}（期望 ${expectName}）versionCode=${code}（期望 ${expectCode}）`);
  process.exit(1);
}
console.log(`版本一致：${name} / ${code}`);
