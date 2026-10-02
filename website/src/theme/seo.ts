/**
 * URL helpers shared by the doc-page wrappers. These must match the paths
 * the build plugins write: plugins/seo.js (Open Graph images) and
 * plugins/markdown-export.js (.md copies).
 */

/** Path of a doc's generated Open Graph image, e.g. /go-ai/img/og/docs/foundations/tools.png */
export function ogImagePath(permalink: string, baseUrl: string): string {
  return `${baseUrl}img/og/${permalink.replace(/\/$/, '').slice(baseUrl.length)}.png`;
}

/** Path of a doc's markdown copy, e.g. /go-ai/docs/foundations/tools.md */
export function markdownPath(permalink: string): string {
  return `${permalink.replace(/\/$/, '')}.md`;
}

/** Absolute URL for a site path. */
export function absoluteUrl(siteUrl: string, pathname: string): string {
  return siteUrl.replace(/\/$/, '') + pathname;
}
