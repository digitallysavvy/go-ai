// @ts-check
/**
 * Publishes a markdown copy of every doc page for agents and LLM tools.
 *
 * After the site is built, each doc at `/docs/<path>` gets a sibling
 * `/docs/<path>.md` (GitHub Pages serves it as a static file, so appending
 * `.md` to any doc URL returns its markdown). It also writes:
 *   - `/llms.txt`      — an index of every page's markdown URL (llmstxt.org),
 *                        with a "Start here" block and an "Optional" section
 *   - `/llms-full.txt` — every page concatenated into one file
 *   - `/llms-core.txt` — the core sections only (no providers, migrations or
 *                        troubleshooting), capped at CORE_MAX_BYTES
 *   - `/docs/<section>/llms.txt` — an index per docs section
 *   - `/agents.md`     — a short orientation file for coding agents
 *   - `/sitemap.md`    — every page with type, summary and prerequisites
 *
 * Nothing here lists pages by hand: everything derives from the docs tree and
 * front matter, so pages added or moved by other contributors appear
 * automatically.
 *
 * The markdown comes from the doc source, not the rendered HTML: frontmatter
 * is replaced by a `# title` heading, MDX-only syntax is flattened to plain
 * markdown, and links to other docs are rewritten to their `.md` URLs.
 * Relative links to repo files outside `docs/` become GitHub URLs.
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


const REPO_URL = 'https://github.com/digitallysavvy/go-ai';
const CORE_MAX_BYTES = 300 * 1024;
/** Sections that make up llms-core.txt, matched on the section slug. */
const CORE_SECTION_RE =
  /^(introduction|getting-started|foundations|(ai-sdk-)?core|agents|reference)$/;
/** Page types that go under "Optional" in llms.txt and are left out of llms-core.txt. */
const OPTIONAL_TYPES = new Set(['provider', 'migration', 'troubleshooting']);

/**
 * "Start here" entries. Each matches a doc permalink, and is skipped when no
 * page matches, so renamed or missing pages never break the build.
 * @type {{label: string, re: RegExp}[]}
 */
const START_HERE = [
  { label: 'Quick start', re: /\/getting-started\/(go|golang|quick-?start)$/ },
  { label: 'Generate text', re: /\/generating-text$/ },
  { label: 'Structured output', re: /\/generating-structured-data$/ },
  { label: 'Tools and tool calling', re: /\/tools-and-tool-calling$/ },
  { label: 'Use MCP tools', re: /\/mcp-tools$/ },
  { label: 'Build an agent', re: /\/agents\/(building-agents|overview)$/ },
  { label: 'Serve a useChat endpoint', re: /\/build-a-chat-app\/serve-usechat-from-go$/ },
  { label: 'Tool approval', re: /\/build-a-chat-app\/tool-approval$/ },
  { label: 'Claude Code and Codex harness', re: /\/build-a-chat-app\/coding-agents-harness$/ },
  { label: 'Use Go AI SDK with coding agents', re: /using-go-ai-with-coding-agents$/ },
  { label: 'Error handling', re: /\/error-handling$/ },
];

/** @param {string} dir a directory name such as `03-ai-sdk-core` */
const dirSlug = (dir) => dir.replace(/^\d+-/, '');

/** @param {string} dir */
function sectionTitle(dir) {
  return dirSlug(dir)
    .replace(/-/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
    .replace(/\b(Ai|Sdk|Api|Mcp)\b/g, (w) => w.toUpperCase());
}

/**
 * guide / reference / provider / migration / troubleshooting / recipe, from
 * the page's location in the docs tree.
 * @param {string} rel path relative to docs/
 */
function pageType(rel) {
  const segs = rel.split(path.sep);
  const dirs = segs.slice(0, -1).map(dirSlug);
  const file = dirSlug(segs[segs.length - 1]);
  const inDir = (/** @type {RegExp} */ re) => dirs.some((d) => re.test(d));
  if (inDir(/^providers?$/)) return 'provider';
  if (inDir(/^reference$/)) return 'reference';
  if (inDir(/^migration/) || /migration/.test(file)) return 'migration';
  if (inDir(/^troubleshooting$/)) return 'troubleshooting';
  if (inDir(/^(recipes?|cookbook)$/)) return 'recipe';
  return 'guide';
}

/** Reads the `## Prerequisites` list of a page as one short line, if present. */
function prerequisites(body, frontMatter) {
  if (frontMatter && frontMatter.prerequisites) {
    return [].concat(frontMatter.prerequisites).join('; ');
  }
  const m = body.match(/^##\s+Prerequisites\s*\n([\s\S]*?)(?=^#{1,6}\s|(?![\s\S]))/m);
  if (!m) return '';
  const items = m[1]
    .split('\n')
    .filter((l) => /^\s*([-*]|\d+\.)\s/.test(l))
    .map((l) => l.replace(/^\s*([-*]|\d+\.)\s+/, '').replace(/\*\*/g, '').trim());
  const text = items.length ? items.join('; ') : (m[1].trim().split('\n')[0] || '');
  const plain = text.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1').replace(/`/g, '');
  return plain.length > 200 ? plain.slice(0, 197) + '...' : plain;
}

/** First paragraph of a body, as a fallback summary. */
function firstParagraph(body) {
  for (const block of body.split(/\n\s*\n/)) {
    const t = block.trim();
    if (!t || /^(#|```|~~~|<|import |export |\||[-*] )/.test(t)) continue;
    const plain = t.replace(/\s+/g, ' ').replace(/\[([^\]]+)\]\([^)]*\)/g, '$1');
    return plain.length > 200 ? plain.slice(0, 197) + '...' : plain;
  }
  return '';
}

/** Module path and Go version from the repo's go.mod. */
function readGoMod(repoRoot) {
  try {
    const mod = fs.readFileSync(path.join(repoRoot, 'go.mod'), 'utf8');
    const modulePath = (mod.match(/^module\s+(\S+)/m) || [])[1] || 'github.com/digitallysavvy/go-ai';
    const goFull = (mod.match(/^go\s+(\S+)/m) || [])[1] || '';
    const goVersion = goFull.split('.').slice(0, 2).join('.');
    return { modulePath, goVersion };
  } catch {
    return { modulePath: 'github.com/digitallysavvy/go-ai', goVersion: '' };
  }
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

      const repoRoot = path.resolve(siteDir, '..');
      const docsRoot = path.join(repoRoot, 'docs');
      const isInside = (/** @type {string} */ root, /** @type {string} */ p) => {
        const r = path.relative(root, p);
        return r !== '' && !r.startsWith('..') && !path.isAbsolute(r);
      };

      /**
       * GitHub URL for a file or directory in the repo, or null if the path
       * is outside the repo.
       * @param {string} resolved absolute path
       * @param {string} frag
       */
      function repoLink(resolved, frag) {
        if (!isInside(repoRoot, resolved)) return null;
        const rel = path.relative(repoRoot, resolved).split(path.sep).join('/');
        let kind = 'blob';
        try {
          if (fs.statSync(resolved).isDirectory()) kind = 'tree';
        } catch {
          /* missing target: still point at GitHub rather than a dead relative path */
        }
        return `${REPO_URL}/${kind}/main/${rel}${frag}`;
      }

      /**
       * Rewrites a link target: links to other docs become their markdown
       * URLs, and relative links to repo files (outside docs/, or files in
       * docs/ that are not doc pages) become GitHub URLs. Returns null for
       * anything else.
       * @param {string} target
       * @param {string} fromFile
       */
      function rewriteTarget(target, fromFile) {
        if (/^[a-z][a-z0-9+.-]*:|^#|^\/\//i.test(target)) return null;
        const [p, hash = ''] = target.split('#');
        const frag = hash ? '#' + hash : '';
        if (!p) return null;
        if (p.startsWith('/')) {
          const abs = p.startsWith('/docs/')
            ? baseUrl + p.slice(1)
            : p.startsWith(docsRoute)
              ? p
              : null;
          return abs ? origin + mdPath(abs.replace(/\.mdx?$/, '')) + frag : null;
        }
        const resolved = path.resolve(path.dirname(fromFile), p);
        if (/\.mdx?$/.test(p)) {
          const doc = bySource.get(resolved);
          if (doc) return origin + mdPath(doc.permalink) + frag;
          return fs.existsSync(resolved) || !isInside(docsRoot, resolved)
            ? repoLink(resolved, frag)
            : null;
        }
        if (!isInside(docsRoot, resolved)) return repoLink(resolved, frag);
        // Inside docs/: a bare repo path such as `pkg/ai/generate.go` or an
        // existing non-page file.
        if (!fs.existsSync(resolved)) {
          return /^(\.\/)?(pkg|examples|cmd|website|skills|\.github)\//.test(p)
            ? repoLink(path.resolve(repoRoot, p), frag)
            : null;
        }
        return fs.statSync(resolved).isDirectory() ? null : repoLink(resolved, frag);
      }

      const { modulePath, goVersion } = readGoMod(repoRoot);
      const goLine = goVersion ? `Go ${goVersion} or later` : 'a current Go release';
      const llmsUrl = `${origin}${baseUrl}llms.txt`;

      const pages = [];
      for (const doc of docs) {
        const raw = fs.readFileSync(doc.sourceFile, 'utf8');
        let content = outsideCode(stripFrontmatter(raw), (text) =>
          flattenMdx(text).replace(
            /(\]\()([^)\s]+)(\))/g,
            (m, open, target, close) => {
              const rewritten = rewriteTarget(target, doc.sourceFile);
              return rewritten ? open + rewritten + close : m;
            },
          ),
        ).trim();
        // The header below carries the title, so drop a leading H1.
        content = content.replace(/^#[ \t]+[^\n]*\n+/, '').trim();
        const rel = path.relative(docsRoot, doc.sourceFile);
        const dirs = rel.split(path.sep).slice(0, -1);
        const type = pageType(rel);
        const summary = doc.description || firstParagraph(content);
        const url = origin + mdPath(doc.permalink);
        const canonical = origin + doc.permalink;
        const header =
          `# ${doc.title}\n\n` +
          (doc.description ? `> ${doc.description}\n\n` : '') +
          `Canonical URL: ${canonical}\n` +
          `Documentation index: ${llmsUrl}\n\n`;
        const body = header + content;
        const outRel = mdPath(doc.permalink).slice(baseUrl.length);
        const out = path.join(outDir, outRel);
        fs.mkdirSync(path.dirname(out), { recursive: true });
        fs.writeFileSync(out, body + '\n');
        pages.push({
          doc,
          url,
          body,
          type,
          summary,
          prereq: prerequisites(content, doc.frontMatter),
          dir: dirs[0] || '',
          slug: dirs[0] ? dirSlug(dirs[0]) : '',
        });
      }

      // Group by section slug (so 04-advanced and 06-advanced merge), in
      // source order.
      /** @type {Map<string, {slug: string, title: string, pages: any[]}>} */
      const sections = new Map();
      for (const pg of pages) {
        if (!sections.has(pg.slug)) {
          sections.set(pg.slug, {
            slug: pg.slug,
            title: pg.dir ? sectionTitle(pg.dir) : 'General',
            pages: [],
          });
        }
        /** @type {any} */ (sections.get(pg.slug)).pages.push(pg);
      }
      // Section overview (shortest permalink) first, then source order.
      const overviewFirst = (/** @type {any[]} */ list) => {
        if (list.length < 2) return list.slice();
        const head = list.reduce((x, y) =>
          y.doc.permalink.length < x.doc.permalink.length ? y : x,
        );
        return [head, ...list.filter((p) => p !== head)];
      };
      const coreBlurb =
        `${siteConfig.title} is a Go toolkit for building AI applications and agents: ` +
        'text and structured-output generation, streaming, tools, MCP, agents, ' +
        'embeddings and many model providers behind one interface. It tracks the ' +
        "Vercel AI SDK (TypeScript) for server-side features.";

      // ---- core bundle (computed first so llms.txt can report its size)
      const corePages = [];
      const entry = (/** @type {any} */ pg) =>
        `- [${pg.doc.title}](${pg.url})${pg.summary ? ': ' + pg.summary : ''}\n`;
      for (const sec of sections.values()) {
        if (!sec.slug || !CORE_SECTION_RE.test(sec.slug)) continue;
        for (const pg of overviewFirst(sec.pages)) {
          if (!OPTIONAL_TYPES.has(pg.type)) corePages.push(pg);
        }
      }
      // Core pages total well over the cap, so the bundle holds the "Start
      // here" pages first, then fills the rest of the budget with the
      // smallest pages (most coverage per byte). Everything left out is
      // listed as a link at the end.
      const startUrls = new Set();
      for (const { re } of START_HERE) {
        const hit = corePages.find((pg) => re.test(pg.doc.permalink));
        if (hit) startUrls.add(hit.url);
      }
      for (const pg of corePages) {
        if (pg.type === 'recipe' || (pg.doc.frontMatter && pg.doc.frontMatter.llms_start_here)) {
          startUrls.add(pg.url);
        }
      }
      const coreHead =
        `# ${siteConfig.title}: core documentation\n\n> ${coreBlurb}\n\n` +
        `Module: ${modulePath}. Install: go get ${modulePath}@latest. ${goLine}.\n` +
        `This file holds the most useful pages from the introduction, getting started, foundations, ` +
        `core, agents and reference sections, in full. Pages that did not fit are listed at the end ` +
        `as links. Providers, migration guides and troubleshooting are not included; find them in ` +
        `${llmsUrl}. Orientation: ${origin}${baseUrl}agents.md\n\n---\n\n`;
      const chunkOf = (/** @type {any} */ pg) => `<!-- Source: ${pg.url} -->\n\n${pg.body}\n\n---\n\n`;
      const sizeOf = (/** @type {string} */ t) => Buffer.byteLength(t);
      // Reserve room for the "left out" index: its size if every page were
      // left out, an upper bound that keeps the file under the cap.
      const omittedHeader = '## Pages not included here\n\nFetch these individually as markdown.\n\n';
      const reserve = sizeOf(omittedHeader) + corePages.reduce((n, pg) => n + sizeOf(entry(pg)), 0);
      let used = sizeOf(coreHead) + reserve;
      const included = new Set();
      const bySizeAsc = [...corePages].sort((x, y) => x.body.length - y.body.length);
      for (const pg of corePages.filter((p) => startUrls.has(p.url)).concat(bySizeAsc)) {
        if (included.has(pg)) continue;
        const n = sizeOf(chunkOf(pg));
        if (used + n > CORE_MAX_BYTES) continue;
        included.add(pg);
        used += n;
      }
      let core = coreHead;
      const omitted = [];
      for (const pg of corePages) {
        if (included.has(pg)) core += chunkOf(pg);
        else omitted.push(pg);
      }
      if (omitted.length) {
        core += omittedHeader + omitted.map(entry).join('');
      }
      fs.writeFileSync(path.join(outDir, 'llms-core.txt'), core);
      const coreKB = (Buffer.byteLength(core) / 1024).toFixed(0);

      // ---- llms.txt
      const startHere = [];
      const seen = new Set();
      const addStart = (/** @type {any} */ pg, /** @type {string} */ label) => {
        if (!pg || seen.has(pg.url)) return;
        seen.add(pg.url);
        startHere.push(`- [${label || pg.doc.title}](${pg.url})${pg.summary ? ': ' + pg.summary : ''}\n`);
      };
      for (const { label, re } of START_HERE) {
        addStart(pages.find((pg) => re.test(pg.doc.permalink) && !OPTIONAL_TYPES.has(pg.type)), label);
      }
      for (const pg of pages) {
        if (pg.type === 'recipe' || (pg.doc.frontMatter && pg.doc.frontMatter.llms_start_here)) {
          addStart(pg);
        }
      }

      let index = `# ${siteConfig.title}\n\n> ${coreBlurb}\n\n`;
      index += `- Module: \`${modulePath}\`\n`;
      index += `- Install: \`go get ${modulePath}@latest\`\n`;
      index += `- Go version: ${goLine}\n`;
      index += `- Agent orientation (short, read first): ${origin}${baseUrl}agents.md\n`;
      index += `- Every page with its type and summary: ${origin}${baseUrl}sitemap.md\n`;
      index += `- Every page is also markdown: append \`.md\` to its URL\n`;
      index += `- Core docs in one file (about ${coreKB} KB): ${origin}${baseUrl}llms-core.txt\n`;
      index += `- All docs in one file (large): ${origin}${baseUrl}llms-full.txt\n`;
      index += `- Source: ${REPO_URL}. Reference app: https://github.com/digitallysavvy/go-ai-shipyard\n`;
      if (startHere.length) index += `\n## Start here\n\n${startHere.join('')}`;
      for (const sec of sections.values()) {
        const list = overviewFirst(sec.pages.filter((pg) => !OPTIONAL_TYPES.has(pg.type)));
        if (!list.length) continue;
        index += `\n## ${sec.title}\n\n` + list.map(entry).join('');
      }
      const optional = [
        ['provider', 'Providers'],
        ['migration', 'Migration guides'],
        ['troubleshooting', 'Troubleshooting'],
      ];
      let optionalText = '';
      for (const [type, title] of optional) {
        const list = pages.filter((pg) => pg.type === type);
        if (list.length) optionalText += `\n### ${title}\n\n` + list.map(entry).join('');
      }
      if (optionalText) index += `\n## Optional\n\nSafe to skip unless the task involves a specific provider, an upgrade or an error.\n${optionalText}`;
      fs.writeFileSync(path.join(outDir, 'llms.txt'), index);

      fs.writeFileSync(
        path.join(outDir, 'llms-full.txt'),
        pages.map((pg) => `<!-- Source: ${pg.url} -->\n\n${pg.body}\n`).join('\n---\n\n'),
      );

      // ---- per-section indexes: /docs/<section>/llms.txt, named after the
      // URL segment the section's pages actually live under.
      /** @type {Map<string, {title: string, pages: any[]}>} */
      const byUrlSeg = new Map();
      for (const sec of sections.values()) {
        if (!sec.slug) continue;
        const counts = new Map();
        for (const pg of sec.pages) {
          const seg = pg.doc.permalink.slice(docsRoute.length).split('/')[0];
          if (seg && pg.doc.permalink.length > docsRoute.length - 1) counts.set(seg, (counts.get(seg) || 0) + 1);
        }
        const seg = [...counts.entries()].sort((x, y) => y[1] - x[1])[0]?.[0] || sec.slug;
        if (!byUrlSeg.has(seg)) byUrlSeg.set(seg, { title: sec.title, pages: [] });
        /** @type {any} */ (byUrlSeg.get(seg)).pages.push(...sec.pages);
      }
      for (const [seg, sec] of byUrlSeg) {
        let txt = `# ${siteConfig.title}: ${sec.title}\n\n> Pages in the ${sec.title} section. ` +
          `Full index: ${llmsUrl}\n\n`;
        txt += overviewFirst(sec.pages).map(entry).join('');
        const dest = path.join(outDir, 'docs', seg, 'llms.txt');
        fs.mkdirSync(path.dirname(dest), { recursive: true });
        fs.writeFileSync(dest, txt);
      }

      // ---- sitemap.md
      let sitemap = `# ${siteConfig.title} sitemap\n\n> Every documentation page with its type, a summary and prerequisites where the page states them.\n\n`;
      sitemap += `Types: guide, reference, provider, migration, troubleshooting, recipe. ` +
        `Fetch a page as markdown by appending \`.md\` to its URL. Index: ${llmsUrl}\n`;
      for (const sec of sections.values()) {
        sitemap += `\n## ${sec.title}\n\n`;
        for (const pg of overviewFirst(sec.pages)) {
          sitemap += `- [${pg.doc.title}](${pg.url}) (${pg.type})${pg.summary ? ': ' + pg.summary : ''}`;
          if (pg.prereq) sitemap += ` Prerequisites: ${pg.prereq}`;
          sitemap += '\n';
        }
      }
      fs.writeFileSync(path.join(outDir, 'sitemap.md'), sitemap);

      // ---- agents.md
      /** @param {RegExp} re */
      const find = (re) => pages.find((pg) => re.test(pg.doc.permalink));
      /** @param {string} label @param {RegExp} re */
      const link = (label, re) => {
        const pg = find(re);
        return pg ? `- [${label}](${pg.url})\n` : '';
      };
      const agentGuide = find(/using-go-ai-with-coding-agents$/);
      fs.writeFileSync(
        path.join(outDir, 'agents.md'),
        agentsMd({ origin: origin + baseUrl.replace(/\/$/, ''), modulePath, goLine, link, agentGuide }),
      );
    },
  };
};

/**
 * Text of /agents.md. Signatures here are checked against pkg/ when this
 * file changes; keep the code small and compile-checked.
 */
function agentsMd({ origin, modulePath, goLine, link, agentGuide }) {
  const mod = modulePath;
  return `# Go AI SDK

> Go toolkit for AI applications and agents: text and object generation, streaming, tools, MCP, agents and many model providers behind one interface. Port of the Vercel AI SDK (TypeScript).

- Module: \`${mod}\` (packages under \`${mod}/pkg/...\`)
- Install: \`go get ${mod}@latest\`
- Go version: ${goLine}
- Docs: ${origin}/docs; append \`.md\` to any page URL for markdown

## The six calls you will use most

\`\`\`go
import (
    "${mod}/pkg/agent"
    "${mod}/pkg/ai"
    "${mod}/pkg/provider/types"
    "${mod}/pkg/providers/anthropic"
    "${mod}/pkg/providers/openai"
    "${mod}/pkg/schema"
)

// 1. Provider and model. Take model IDs from the provider's constants.
prov := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
model, err := prov.LanguageModel(anthropic.ClaudeSonnet5_5)

// 2. Generate text.
res, err := ai.GenerateText(ctx, ai.GenerateTextOptions{Model: model, Prompt: "Hello"})
fmt.Println(res.Text)

// 3. Stream text. Use Chunks(); the stream is not an io.Reader.
stream, err := ai.StreamText(ctx, ai.StreamTextOptions{Model: model, Prompt: "Hello"})
for c := range stream.Chunks() { fmt.Print(c.Text) }
err = stream.Err()

// 4. Structured output into a struct.
err = ai.GenerateObjectInto(ctx, ai.GenerateObjectOptions{
    Model: model, Prompt: "A pancake recipe",
    Schema: schema.NewSimpleStructSchema(reflect.TypeOf(Recipe{})),
}, &recipe)

// 5. Tools and an agent loop. Set ToolApproval to gate a tool.
tool := types.Tool{
    Name: "weather", Description: "Current weather", ToolApproval: true,
    Parameters: map[string]interface{}{"type": "object"},
    Execute: func(ctx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
        return "sunny", nil
    },
}
a := agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model, Tools: []types.Tool{tool}, StopWhen: []ai.StopCondition{ai.StepCountIs(5)},
})
out, err := a.Execute(ctx, "Weather in Oslo?") // out.Text

// 6. Embeddings.
emb, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).EmbeddingModel(openai.ModelTextEmbedding3Small)
er, err := ai.Embed(ctx, ai.EmbedOptions{Model: emb, Input: "hello"}) // er.Embedding
\`\`\`

## Gotchas

- \`LanguageModel(id)\` returns \`(model, error)\`; check the error.
- Streams are \`provider.TextStream\` (\`Next\`, \`Err\`, \`Close\`) or \`Chunks()\`. They do not implement \`io.Reader\`.
- Provider option and metadata keys are camelCase, as in the TypeScript SDK.
- Use \`ToolApproval\` on a tool. \`NeedsApproval\` is deprecated.
- \`GenerateText\` and \`StreamText\` run one step unless you set \`StopWhen\` (for example \`ai.StepCountIs(5)\`). A tool-loop agent defaults to 20 steps.
- For a useChat endpoint, \`ai.PipeUIMessageStreamToResponse\` and \`agent.PipeAgentUIStreamFromUIMessagesToResponse\` set the status and stream headers on an \`http.ResponseWriter\`. Do not set them yourself.
- \`OnStepFinish\` has different signatures in \`ai\` and \`agent\`. Check the godoc.

## Where to read next

- [llms.txt](${origin}/llms.txt): index with a "Start here" block
- [llms-core.txt](${origin}/llms-core.txt): the core pages in one file
- [sitemap.md](${origin}/sitemap.md): every page with type and summary
${link('Quick start', /\/getting-started\/(go|golang|quick-?start)$/)}${link('Generating text', /\/generating-text$/)}${link('Structured data', /\/generating-structured-data$/)}${link('Tools and tool calling', /\/tools-and-tool-calling$/)}${link('Building agents', /\/agents\/building-agents$/)}${agentGuide ? `- [Use Go AI SDK with coding agents](${agentGuide.url})\n` : ''}- Reference app (useChat frontend, Go backend, approval-gated tool, harness): https://github.com/digitallysavvy/go-ai-shipyard
`;
}
