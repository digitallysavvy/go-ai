// Generates identity_fixtures.json: the TS `hashHarnessBootstrap` identity
// (see ai@7.0.113 packages/harness/src/agent/internal/bootstrap-recipe.ts,
// function body copied verbatim, same as
// pkg/harness/testdata/gen_identity_fixtures.mjs) computed over the REAL
// bootstrap recipe each adapter's own bootstrap.ts builds -- using the exact
// bytes embedded in ../<adapter>/*, which are byte-for-byte the same as the
// TS adapters' `dist/bridge/*` (see VERSIONS.json). This is the drift test's
// cross-language half: it proves that a Go host, given these embedded bytes,
// computes the identical 16-hex bootstrap identity a TS host resuming the
// same sandbox would compute for a session it created (or vice versa).
//
// Sources for the per-adapter file lists / bootstrapDir / commands (verbatim
// transcription, not re-derived):
//   - claude-code: packages/harness-claude-code/src/claude-code-bootstrap.ts
//   - codex:       packages/harness-codex/src/codex-bootstrap.ts
//   - opencode:    packages/harness-opencode/src/opencode-bootstrap.ts
//   - deepagents:  packages/harness-deepagents/src/deepagents-bootstrap.ts
//
// Regenerate: node pkg/harness/bridges/testdata/gen_bridge_identity_fixtures.mjs \
//   > pkg/harness/bridges/testdata/identity_fixtures.json
import { readFile } from 'node:fs/promises';
import { posix } from 'node:path';
import { fileURLToPath } from 'node:url';

const bridgesDir = posix.dirname(posix.dirname(fileURLToPath(import.meta.url).replaceAll('\\', '/')));

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

async function readEmbedded(adapterDir, name) {
  return readFile(posix.join(bridgesDir, adapterDir, name), 'utf8');
}

// Verbatim from packages/harness-deepagents/src/deepagents-bootstrap.ts.
function installRipgrepCommand() {
  const v = '14.1.1';
  const shaArm =
    'c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f';
  const shaX64 =
    '4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e';
  return [
    'command -v rg >/dev/null 2>&1 || {',
    'case "$(uname -m)" in',
    `aarch64) a=aarch64-unknown-linux-gnu; sha=${shaArm} ;;`,
    `*) a=x86_64-unknown-linux-musl; sha=${shaX64} ;;`,
    'esac;',
    `f=/tmp/ripgrep-${v}.tar.gz;`,
    `curl -fsSL "https://github.com/BurntSushi/ripgrep/releases/download/${v}/ripgrep-${v}-$a.tar.gz" -o "$f"`,
    '&& echo "$sha  $f" | sha256sum -c -',
    '&& tar xzf "$f" -C /tmp',
    `&& mv "/tmp/ripgrep-${v}-$a/rg" /usr/local/bin/rg && chmod +x /usr/local/bin/rg;`,
    '}',
  ].join(' ');
}

async function buildRecipe({ adapterDir, harnessId, bootstrapDir, fileNames, commands }) {
  const files = await Promise.all(
    fileNames.map(async name => ({
      path: `${bootstrapDir}/${name}`,
      content: await readEmbedded(adapterDir, name),
    })),
  );
  return { harnessId, bootstrapDir, files, commands };
}

const adapters = {
  claudecode: {
    adapterDir: 'claudecode',
    harnessId: 'claude-code',
    bootstrapDir: '.harness-bootstrap/claude-code',
    fileNames: ['package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml', 'bridge.mjs'],
    commands: [
      { command: 'pnpm install --frozen-lockfile --store-dir .pnpm-store' },
      { command: './node_modules/.bin/claude --version' },
    ],
  },
  codex: {
    adapterDir: 'codex',
    harnessId: 'codex',
    bootstrapDir: '.harness-bootstrap/codex',
    fileNames: ['package.json', 'pnpm-lock.yaml', 'bridge.mjs'],
    commands: [
      { command: 'pnpm install --frozen-lockfile --store-dir .pnpm-store' },
    ],
  },
  opencode: {
    adapterDir: 'opencode',
    harnessId: 'opencode',
    bootstrapDir: '.harness-bootstrap/opencode',
    fileNames: ['package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml', 'bridge.mjs', 'host-tool-mcp.mjs'],
    commands: [
      { command: 'pnpm install --frozen-lockfile --store-dir .pnpm-store' },
      { command: './node_modules/.bin/opencode --version' },
    ],
  },
  deepagents: {
    adapterDir: 'deepagents',
    harnessId: 'deepagents',
    bootstrapDir: '.harness-bootstrap/deepagents',
    fileNames: ['bridge.mjs', 'package.json', 'pnpm-lock.yaml'],
    commands: [
      { command: installRipgrepCommand() },
      { command: 'pnpm install --frozen-lockfile --store-dir .pnpm-store' },
    ],
  },
};

const out = {};
for (const [key, spec] of Object.entries(adapters)) {
  const recipe = await buildRecipe(spec);
  const identity = await hashHarnessBootstrap(recipe);
  out[key] = {
    harnessId: spec.harnessId,
    bootstrapDir: spec.bootstrapDir,
    fileNames: spec.fileNames,
    commands: spec.commands,
    identity,
  };
}

process.stdout.write(JSON.stringify(out, null, 2) + '\n');
