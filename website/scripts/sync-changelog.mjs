// Publishes the repository's CHANGELOG.md as a docs page.
//
// Runs before `npm start` and `npm run build`. It reads ../CHANGELOG.md and
// writes ../docs/12-changelog/index.md (ignored by git), so the page can never
// drift from the root file. Relative links are rewritten to GitHub URLs
// because the target files are not part of the site.
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const source = join(root, 'CHANGELOG.md');
const outDir = join(root, 'docs', '12-changelog');
const repo = 'https://github.com/digitallysavvy/go-ai/blob/main/';

let body = readFileSync(source, 'utf8');

// Inline links such as [text](release_notes/FILE.md) become absolute.
body = body.replace(/\]\((?!https?:|#|mailto:)([^)\s]+)\)/g, (_m, target) => `](${repo}${target.replace(/^\.?\//, '')})`);

const frontmatter = [
  '---',
  'title: Changelog',
  'description: Release history of the Go AI SDK, generated from CHANGELOG.md in the repository.',
  '---',
  '',
  '<!-- Generated from CHANGELOG.md by website/scripts/sync-changelog.mjs. Do not edit. -->',
  '',
  '',
].join('\n');

mkdirSync(outDir, { recursive: true });
writeFileSync(join(outDir, 'index.md'), frontmatter + body);
console.log(`Wrote ${join('docs', '12-changelog', 'index.md')} from CHANGELOG.md`);
