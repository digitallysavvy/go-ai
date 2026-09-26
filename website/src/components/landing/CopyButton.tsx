import { useEffect, useRef, useState } from 'react';
import styles from './landing.module.css';

type Props = {
  text: string;
  label: string;
};

export default function CopyButton({ text, label }: Props) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // Clipboard unavailable (insecure context or denied); leave the text selectable.
    }
  };

  return (
    <button
      type="button"
      className={styles.copyBtn}
      onClick={copy}
      aria-label={copied ? 'Copied' : label}
      data-copied={copied ? 'true' : undefined}
    >
      {copied ? (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" fill="none">
          <path d="M3 8.5l3 3 7-7" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ) : (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" fill="none">
          <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.5" />
          <path d="M10.5 5.5V3.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2" stroke="currentColor" strokeWidth="1.5" />
        </svg>
      )}
      <span className={styles.srOnly} aria-live="polite">
        {copied ? 'Copied to clipboard' : ''}
      </span>
    </button>
  );
}
