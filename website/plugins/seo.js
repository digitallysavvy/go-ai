// @ts-check
/**
 * Search and agent-facing extras that need the built site:
 *
 *   - A per-page Open Graph image for every doc, at
 *     `img/og/<doc path>.png` (see `ogImagePath` in src/theme/seo.ts, which
 *     the doc pages use for their og:image / twitter:image tags).
 *   - `robots.txt`, allowing all crawlers (including AI crawlers) and
 *     pointing at the sitemap.
 *
 * Images are rendered at build time with satori (layout → SVG) and resvg
 * (SVG → PNG), so GitHub Pages serves them as plain static files.
 */
const fs = require('fs');
const path = require('path');

const WIDTH = 1200;
const HEIGHT = 630;
const COLORS = {
  bg: '#0f141b',
  grid: '#17202b',
  text: '#ffffff',
  muted: '#9aa7b6',
  accent: '#00acd7',
};

/** @param {string} pkgFile */
function font(pkgFile) {
  return fs.readFileSync(require.resolve(pkgFile));
}

/** `ai-sdk-core` → `AI SDK Core` */
function sectionLabel(slug) {
  return slug
    .split('-')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
    .replace(/\b(Ai|Sdk|Api|Mcp|Ui)\b/g, (w) => w.toUpperCase());
}

/** Plain-object element tree for satori (no JSX in a CommonJS plugin). */
function h(type, style, ...children) {
  const flat = children.flat().filter((c) => c !== null && c !== undefined && c !== false);
  return {
    type,
    props: { style, children: flat.length === 1 ? flat[0] : flat },
  };
}

function card({ section, title, description, host, logoDataUrl }) {
  const titleSize = title.length <= 24 ? 76 : title.length <= 44 ? 64 : 52;
  return h(
    'div',
    {
      width: WIDTH,
      height: HEIGHT,
      display: 'flex',
      flexDirection: 'column',
      justifyContent: 'space-between',
      padding: '64px 72px',
      backgroundColor: COLORS.bg,
      backgroundImage: `linear-gradient(${COLORS.grid} 1px, transparent 1px), linear-gradient(90deg, ${COLORS.grid} 1px, transparent 1px)`,
      backgroundSize: '48px 48px',
      fontFamily: 'Inter',
      color: COLORS.text,
    },
    h(
      'div',
      { display: 'flex', flexDirection: 'column' },
      section
        ? h(
            'div',
            {
              display: 'flex',
              fontFamily: 'JetBrains Mono',
              fontSize: 26,
              color: COLORS.accent,
              marginBottom: 24,
            },
            `docs / ${section}`,
          )
        : null,
      h(
        'div',
        {
          display: 'block',
          fontSize: titleSize,
          fontWeight: 800,
          lineHeight: 1.1,
          letterSpacing: -1,
          lineClamp: 3,
        },
        title,
      ),
      description
        ? h(
            'div',
            {
              display: 'block',
              marginTop: 28,
              fontSize: 30,
              lineHeight: 1.4,
              color: COLORS.muted,
              lineClamp: 2,
            },
            description,
          )
        : null,
    ),
    h(
      'div',
      { display: 'flex', alignItems: 'center', justifyContent: 'space-between' },
      h(
        'div',
        { display: 'flex', alignItems: 'center' },
        { type: 'img', props: { src: logoDataUrl, width: 116, height: 90, style: { marginRight: 22 } } },
        h('div', { display: 'flex', fontSize: 36, fontWeight: 800 }, 'Go AI SDK'),
      ),
      h('div', { display: 'flex', fontFamily: 'JetBrains Mono', fontSize: 24, color: COLORS.muted }, host),
    ),
  );
}

/**
 * @param {import('@docusaurus/types').LoadContext} context
 * @returns {import('@docusaurus/types').Plugin}
 */
module.exports = function seo(context) {
  const { siteConfig, siteDir } = context;
  const baseUrl = siteConfig.baseUrl;
  const siteRoot = siteConfig.url.replace(/\/$/, '') + baseUrl;

  return {
    name: 'seo',
    async postBuild({ outDir, plugins }) {
      fs.writeFileSync(
        path.join(outDir, 'robots.txt'),
        [
          '# All crawlers, including AI crawlers, are welcome.',
          '# Agent-friendly docs: every page is available as markdown by',
          `# appending .md to its URL; the index is ${siteRoot}llms.txt`,
          'User-agent: *',
          'Allow: /',
          '',
          `Sitemap: ${siteRoot}sitemap.xml`,
          '',
        ].join('\n'),
      );

      const docsPlugin = plugins.find((p) => p.name === 'docusaurus-plugin-content-docs');
      if (!docsPlugin) throw new Error('seo: docs plugin not found');
      /** @type {any} */
      const content = docsPlugin.content;
      const docs = content.loadedVersions[0].docs.filter((d) => !d.draft && !d.unlisted);

      const { default: satori } = await import('satori');
      const { Resvg } = await import('@resvg/resvg-js');
      const fonts = [
        { name: 'Inter', data: font('@fontsource/inter/files/inter-latin-400-normal.woff'), weight: 400, style: 'normal' },
        { name: 'Inter', data: font('@fontsource/inter/files/inter-latin-800-normal.woff'), weight: 800, style: 'normal' },
        {
          name: 'JetBrains Mono',
          data: font('@fontsource/jetbrains-mono/files/jetbrains-mono-latin-500-normal.woff'),
          weight: 500,
          style: 'normal',
        },
      ];
      const logo = fs.readFileSync(path.join(siteDir, 'static/img/logo-mark.png'));
      const logoDataUrl = `data:image/png;base64,${logo.toString('base64')}`;
      const host = siteConfig.url.replace(/^https?:\/\//, '') + baseUrl.replace(/\/$/, '');

      const started = Date.now();
      for (const doc of docs) {
        // e.g. docs/foundations/tools; section index pages end in "/".
        const rel = doc.permalink.replace(/\/$/, '').slice(baseUrl.length);
        const parts = rel.split('/');
        const section = parts.length > 2 ? sectionLabel(parts[1]) : '';
        const tree = card({
          section,
          title: doc.title,
          description: doc.description,
          host,
          logoDataUrl,
        });
        const svg = await satori(/** @type {any} */ (tree), { width: WIDTH, height: HEIGHT, fonts });
        // satori has already turned all text into paths, so skip resvg's
        // system font scan (it costs about a second per image).
        const png = new Resvg(svg, {
          fitTo: { mode: 'width', value: WIDTH },
          font: { loadSystemFonts: false },
        })
          .render()
          .asPng();
        const out = path.join(outDir, 'img/og', `${rel}.png`);
        fs.mkdirSync(path.dirname(out), { recursive: true });
        fs.writeFileSync(out, png);
      }
      console.log(`[seo] ${docs.length} Open Graph images in ${((Date.now() - started) / 1000).toFixed(1)}s`);
    },
  };
};
