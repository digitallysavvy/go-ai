// Generates identity_fixtures.json: bootstrap / sandbox / prepared-sandbox
// identities and skill hashes computed with the exact TypeScript algorithms of
// @ai-sdk/harness at ai@7.0.113 (function bodies copied verbatim from
// src/agent/internal/bootstrap-recipe.ts, src/agent/internal/sandbox-bootstrap.ts,
// src/agent/prepare-sandbox-for-harness.ts and src/utils/write-skills.ts).
//
// Regenerate: node pkg/harness/testdata/gen_identity_fixtures.mjs > pkg/harness/testdata/identity_fixtures.json
import { createHash } from 'node:crypto';
import { posix } from 'node:path';

const harnessStateDirectoryPath = ({ sandboxHomeDir }) =>
  posix.join(sandboxHomeDir, '.ai-sdk-harness');

async function digest16(pushAll) {
  const encoder = new TextEncoder();
  const chunks = [];
  const pushString = value => {
    chunks.push(encoder.encode(value));
    chunks.push(encoder.encode('\0'));
  };
  pushAll(pushString);
  const totalLength = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const buffer = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    buffer.set(chunk, offset);
    offset += chunk.length;
  }
  const digest = await crypto.subtle.digest('SHA-256', buffer);
  const bytes = new Uint8Array(digest);
  let hex = '';
  for (let i = 0; i < 8; i++) hex += bytes[i].toString(16).padStart(2, '0');
  return hex;
}

const hashHarnessBootstrap = recipe =>
  digest16(pushString => {
    pushString(harnessStateDirectoryPath({ sandboxHomeDir: '$HOME' }));
    pushString(recipe.harnessId);
    pushString(recipe.bootstrapDir);
    const sortedFiles = [...recipe.files].sort((a, b) =>
      a.path.localeCompare(b.path),
    );
    for (const file of sortedFiles) {
      pushString(file.path);
      pushString(file.content);
    }
    pushString(JSON.stringify(recipe.commands));
    pushString(String(1));
  });

const hashSandboxBootstrapIdentity = ({ recipeIdentity, bootstrapHash, workDir }) =>
  digest16(pushString => {
    pushString(String(1));
    pushString(recipeIdentity ?? '');
    pushString(bootstrapHash ?? '');
    pushString(workDir ?? '');
  });

const resolvePreparedSandboxIdentity = ({ recipeIdentities, bootstrapHash, workDir }) =>
  digest16(pushString => {
    const entries = Object.entries(recipeIdentities).sort(([a], [b]) =>
      a.localeCompare(b),
    );
    pushString(String(1));
    pushString(workDir ?? '');
    pushString(bootstrapHash ?? '');
    for (const [harnessId, identity] of entries) {
      pushString(harnessId);
      pushString(identity);
    }
  });

function skillHash(skill, trailingNewline = false) {
  const content = `---\nname: ${skill.name}\ndescription: ${skill.description}\n---\n\n${skill.content}`;
  const files = new Map();
  files.set('SKILL.md', trailingNewline ? `${content}\n` : content);
  for (const file of skill.files ?? []) files.set(posix.normalize(file.path), file.content);
  const projected = Array.from(files, ([path, content]) => ({ path, content })).sort(
    (a, b) => a.path.localeCompare(b.path),
  );
  const hash = createHash('sha256');
  for (const file of projected) {
    hash.update(String(Buffer.byteLength(file.path)));
    hash.update(':');
    hash.update(file.path);
    hash.update(String(Buffer.byteLength(file.content)));
    hash.update(':');
    hash.update(file.content);
  }
  return hash.digest('hex');
}

const recipes = [
  {
    harnessId: 'demo',
    bootstrapDir: '/tmp/harness/demo',
    files: [
      { path: '/tmp/harness/demo/a.txt', content: 'one' },
      { path: '/tmp/harness/demo/b.txt', content: 'two' },
    ],
    commands: [{ command: 'echo first' }, { command: 'echo second' }],
  },
  {
    harnessId: 'claude-code',
    bootstrapDir: '.harness-bootstrap/claude-code',
    files: [
      { path: '.harness-bootstrap/claude-code/pnpm-workspace.yaml', content: 'onlyBuiltDependencies:\n  - x\n' },
      { path: '.harness-bootstrap/claude-code/package.json', content: '{"name":"bridge","dependencies":{"ws":"8.21.0"}}' },
      { path: '.harness-bootstrap/claude-code/bridge.mjs', content: 'console.log("héllo   <&>");\n' },
      { path: '.harness-bootstrap/claude-code/Bridge.mjs', content: 'upper' },
      { path: '.harness-bootstrap/claude-code/_hidden', content: '' },
      { path: '.harness-bootstrap/claude-code/pnpm-lock.yaml', content: 'lockfileVersion: 9.0\n' },
    ],
    commands: [
      { command: 'pnpm install --frozen-lockfile --prod "<&>"   \'q\' \\ \t\n\u0001' },
    ],
  },
  { harnessId: 'empty', bootstrapDir: '', files: [], commands: [] },
];

const out = { recipes: [], sandboxIdentities: [], preparedIdentities: [], skills: [] };
for (const recipe of recipes) {
  out.recipes.push({ recipe, identity: await hashHarnessBootstrap(recipe) });
}
const recipeIdentity = out.recipes[1].identity;
for (const input of [
  { recipeIdentity, bootstrapHash: 'repo-v1', workDir: 'repo' },
  { recipeIdentity, workDir: 'repo/' },
  { bootstrapHash: 'repo-v1' },
]) {
  out.sandboxIdentities.push({ input, identity: await hashSandboxBootstrapIdentity(input) });
}
for (const input of [
  { recipeIdentities: { beta: 'bbbb', alpha: 'aaaa', Alpha: 'AAAA', _x: 'xx' }, bootstrapHash: 'h', workDir: 'repo' },
  { recipeIdentities: {}, bootstrapHash: 'only-hash' },
]) {
  out.preparedIdentities.push({ input, identity: await resolvePreparedSandboxIdentity(input) });
}
for (const skill of [
  { name: 'demo', description: 'Demo skill.', content: 'Use reference.md.', files: [{ path: 'reference.md', content: '# Reference' }] },
  { name: 'ünï', description: 'D', content: 'C', files: [{ path: 'b/Z.md', content: 'z' }, { path: 'b/a.md', content: 'ä' }, { path: 'A.md', content: '' }] },
]) {
  out.skills.push({ skill, hash: skillHash(skill) });
}
console.log(JSON.stringify(out, null, 2));
