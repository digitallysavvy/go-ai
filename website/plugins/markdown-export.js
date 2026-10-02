// @ts-check
/**
 * Publishes a markdown copy of every doc page for agents and LLM tools.
 *
 * After the site is built, each doc at `/docs/<path>` gets a sibling
 * `/docs/<path>.md` (GitHub Pages serves it as a static file, so appending
 * `.md` to any doc URL returns its markdown). It also writes:
 *   - `/llms.txt`      — an index of every page's markdown URL (llmstxt.org)
 *   - `/llms-full.txt` — every page concatenated into one file
 *
 * The markdown comes from the doc source, not the rendered HTML: frontmatter
 * is replaced by a `# title` heading, MDX-only syntax is flattened to plain
 * markdown, and links to other docs are rewritten to their `.md` URLs.
 */
const fs = require('fs');
const path = require('path');

/** @param {string} s */
function stripFrontmatter(s) {
  const m = s.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/);
  return m ? s.slice(m[0].length) : s;
}

/**
 * Applies `fn` only to text outside fenced code blocks, so code samples
 * (Go imports, generics like `<T>`) are left untouched.
 * @param {string} s
 * @param {(text: string) => string} fn
 */
function outsideCode(s, fn) {
  const parts = s.split(/(^(?:```|~~~)[^\n]*\n[\s\S]*?^(?:```|~~~)[ \t]*$)/m);
  return parts.map((p, i) => (i % 2 === 1 ? p : fn(p))).join('');
}

/** `<Note [type="…"]>…</Note>` (the only custom MDX component) → a blockquote. */
function flattenMdx(text) {
  return text
    .replace(/<Note(?:\s+type="(\w+)")?\s*>\s*([\s\S]*?)\s*<\/Note>/g, (_, type, body) =>
      (`**${type ? type[0].toUpperCase() + type.slice(1) : 'Note'}:** ` +
        body.trim().replace(/^[ \t]+/gm, ''))
        .split('\n')
        .map((l) => (l ? '> ' + l : '>'))
        .join('\n'),
    )
    .replace(/^\s*(import|export)\s.+$/gm, '');
}

/**
 * @param {import('@docusaurus/types').LoadContext} context
 * @returns {import('@docusaurus/types').Plugin}
 */
module.exports = function markdownExport(context) {
  const { siteConfig, siteDir } = context;
  const origin = siteConfig.url.replace(/\/$/, '');
  const baseUrl = siteConfig.baseUrl;

  return {
    name: 'markdown-export',
    async postBuild({ outDir, plugins }) {
      const docsPlugin = plugins.find(
        (p) => p.name === 'docusaurus-plugin-content-docs',
      );
      if (!docsPlugin) throw new Error('markdown-export: docs plugin not found');
      /** @type {any} */
      const content = docsPlugin.content;
      const docs = content.loadedVersions[0].docs
        .filter((d) => !d.draft && !d.unlisted)
        .map((d) => ({
          ...d,
          sourceFile: path.resolve(siteDir, d.source.replace(/^@site\//, '')),
        }))
        .sort((a, b) => a.sourceFile.localeCompare(b.sourceFile));

      /** @param {string} permalink */
      const mdPath = (permalink) => permalink.replace(/\/$/, '') + '.md';
      const bySource = new Map(docs.map((d) => [d.sourceFile, d]));
      const docsRoute = baseUrl + 'docs/';

      /**
       * Rewrites a link target to the markdown URL of the doc it points at;
       * returns null for anything that isn't a doc link.
       * @param {string} target
       * @param {string} fromFile
       */
      function rewriteTarget(target, fromFile) {
        if (/^[a-z]+:|^#|^\/\//i.test(target)) return null;
        const [p, hash = ''] = target.split('#');
        const frag = hash ? '#' + hash : '';
        if (/\.mdx?$/.test(p)) {
          const doc = bySource.get(path.resolve(path.dirname(fromFile), p));
          return doc ? origin + mdPath(doc.permalink) + frag : null;
        }
        const abs = p.startsWith('/docs/')
          ? baseUrl + p.slice(1)
          : p.startsWith(docsRoute)
            ? p
            : null;
        if (!abs) return null;
        return origin + mdPath(abs) + frag;
      }

      const pages = [];
      for (const doc of docs) {
        const raw = fs.readFileSync(doc.sourceFile, 'utf8');
        let body = outsideCode(stripFrontmatter(raw), (text) =>
          flattenMdx(text).replace(
            /(\]\()([^)\s]+)(\))/g,
            (m, open, target, close) => {
              const rewritten = rewriteTarget(target, doc.sourceFile);
              return rewritten ? open + rewritten + close : m;
            },
          ),
        ).trim();
        if (!/^#\s/m.test(body.split('\n').find((l) => l.trim()) || '')) {
          const desc = doc.description ? `\n\n> ${doc.description}` : '';
          body = `# ${doc.title}${desc}\n\n${body}`;
        }
        const url = origin + mdPath(doc.permalink);
        const rel = mdPath(doc.permalink).slice(baseUrl.length);
        const out = path.join(outDir, rel);
        fs.mkdirSync(path.dirname(out), { recursive: true });
        fs.writeFileSync(out, body + '\n');
        pages.push({ doc, url, body });
      }

      // llms.txt: grouped by top-level docs section, in source order.
      const sections = new Map();
      for (const pg of pages) {
        const rel = path.relative(path.resolve(siteDir, '../docs'), pg.doc.sourceFile);
        const top = rel.includes(path.sep) ? rel.split(path.sep)[0] : '';
        const name = top
          ? top
              .replace(/^\d+-/, '')
              .replace(/-/g, ' ')
              .replace(/\b\w/g, (c) => c.toUpperCase())
              .replace(/\b(Ai|Sdk|Api|Mcp)\b/g, (w) => w.toUpperCase())
          : 'General';
        if (!sections.has(name)) sections.set(name, []);
        sections.get(name).push(pg);
      }
      let index = `# ${siteConfig.title}\n\n> ${siteConfig.tagline}\n\n`;
      index +=
        'Every page is also available as markdown by appending `.md` to its URL. ' +
        `The full documentation in one file: ${origin}${baseUrl}llms-full.txt\n`;
      for (const [name, list] of sections) {
        // Section overview (shortest permalink) first, then source order.
        const head = list.reduce((x, y) => (y.doc.permalink.length < x.doc.permalink.length ? y : x));
        list.splice(list.indexOf(head), 1);
        list.unshift(head);
        index += `\n## ${name}\n\n`;
        for (const pg of list) {
          const desc = pg.doc.description ? `: ${pg.doc.description}` : '';
          index += `- [${pg.doc.title}](${pg.url})${desc}\n`;
        }
      }
      fs.writeFileSync(path.join(outDir, 'llms.txt'), index);
      fs.writeFileSync(
        path.join(outDir, 'llms-full.txt'),
        pages.map((pg) => `<!-- Source: ${pg.url} -->\n\n${pg.body}\n`).join('\n---\n\n'),
      );
    },
  };
};
