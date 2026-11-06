import { defineConfig } from 'vitest/config';
import fs from 'node:fs';
import path from 'node:path';
const W = path.resolve(import.meta.dirname, '..');
const pkgs = {};
for (const d of fs.readdirSync(path.join(W, 'packages'))) {
  const pj = path.join(W, 'packages', d, 'package.json');
  if (fs.existsSync(pj)) pkgs[JSON.parse(fs.readFileSync(pj, 'utf8')).name] = path.join(W, 'packages', d);
}
function resolveSrc(id) {
  if (id === '@ai-sdk/test-server' ) return path.join(W, 'rec/test-server/index.ts');
  if (id === '@ai-sdk/test-server/with-vitest') return path.join(W, 'rec/test-server/with-vitest.ts');
  const m = /^(@[^/]+\/[^/]+|[^@/][^/]*)(\/.*)?$/.exec(id);
  if (!m || !pkgs[m[1]]) return null;
  const sub = (m[2] || '').replace(/^\//, '');
  const base = path.join(pkgs[m[1]], 'src');
  const cands = sub ? [path.join(base, sub, 'index.ts'), path.join(base, sub + '.ts')] : [path.join(base, 'index.ts')];
  return cands.find(c => fs.existsSync(c)) ?? null;
}
export default defineConfig({
  plugins: [{
    name: 'rec', enforce: 'pre',
    resolveId(id) { return resolveSrc(id); },
    transform(code, id) {
      if (!/\.test\.tsx?$/.test(id)) return null;
      return code.replace(/\.(do(?:Generate|Stream|Embed|Rerank|Start|Status|Decide|GetBatchResults|StartBatch|GetBatchStatus|CancelBatch|CreateClientSecret|CreateWebRTCSession)|parseServerEvent|serializeClientEvent)\(/g, '.__cap_$1(');
    },
  }],
  esbuild: { tsconfigRaw: '{"compilerOptions":{"target":"ES2022","useDefineForClassFields":true}}' },
  define: { __PACKAGE_VERSION__: JSON.stringify('0.0.0-test') },
  test: { environment: 'node', include: ['**/*.test.ts'], setupFiles: [path.join(W, 'rec/setup.mjs')], reporters: ['dot'], testTimeout: 20000 },
});
