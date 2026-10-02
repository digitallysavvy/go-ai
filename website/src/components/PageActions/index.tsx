import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import { useDoc } from '@docusaurus/plugin-content-docs/client';
import { useState, type ReactNode } from 'react';

import { absoluteUrl, markdownPath } from '../../theme/seo';
import styles from './styles.module.css';

type CopyState = 'idle' | 'copied' | 'failed';

/**
 * Per-page actions for working with the docs in an AI tool: copy the page
 * as markdown, view the markdown, or open it in ChatGPT / Claude.
 */
export default function PageActions(): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  const { metadata } = useDoc();
  const [copy, setCopy] = useState<CopyState>('idle');

  const mdPath = markdownPath(metadata.permalink);
  const mdUrl = absoluteUrl(siteConfig.url, mdPath);
  const prompt = encodeURIComponent(
    `Read ${mdUrl} (Go AI SDK documentation) so I can ask questions about it.`,
  );

  async function copyPage() {
    try {
      const res = await fetch(mdPath);
      if (!res.ok) throw new Error(String(res.status));
      await navigator.clipboard.writeText(await res.text());
      setCopy('copied');
    } catch {
      setCopy('failed');
    }
    setTimeout(() => setCopy('idle'), 2000);
  }

  return (
    <div className={styles.actions} role="group" aria-label="Page actions">
      <button type="button" className={styles.action} onClick={copyPage}>
        {copy === 'copied' ? 'Copied' : copy === 'failed' ? 'Copy failed' : 'Copy page'}
      </button>
      <a className={styles.action} href={mdPath}>
        View as Markdown
      </a>
      <a
        className={styles.action}
        href={`https://chatgpt.com/?hints=search&q=${prompt}`}
        target="_blank"
        rel="noopener noreferrer"
      >
        Open in ChatGPT
      </a>
      <a className={styles.action} href={`https://claude.ai/new?q=${prompt}`} target="_blank" rel="noopener noreferrer">
        Open in Claude
      </a>
    </div>
  );
}
