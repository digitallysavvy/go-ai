import Link from '@docusaurus/Link';
import CodeBlock from '@theme/CodeBlock';
import { useRef, useState, type KeyboardEvent } from 'react';
import { SHOWCASE } from './snippets';
import styles from './landing.module.css';

export default function Showcase() {
  const [active, setActive] = useState(0);
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const current = SHOWCASE[active];

  const focusTab = (index: number) => {
    const next = (index + SHOWCASE.length) % SHOWCASE.length;
    setActive(next);
    tabRefs.current[next]?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    switch (e.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        e.preventDefault();
        focusTab(index + 1);
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        e.preventDefault();
        focusTab(index - 1);
        break;
      case 'Home':
        e.preventDefault();
        focusTab(0);
        break;
      case 'End':
        e.preventDefault();
        focusTab(SHOWCASE.length - 1);
        break;
      default:
    }
  };

  return (
    <div className={styles.showcase}>
      <div className={styles.rail} role="tablist" aria-label="Examples">
        {SHOWCASE.map((s, i) => (
          <button
            key={s.id}
            ref={(el) => {
              tabRefs.current[i] = el;
            }}
            type="button"
            role="tab"
            id={`showcase-tab-${s.id}`}
            aria-selected={i === active}
            aria-controls={`showcase-panel-${s.id}`}
            tabIndex={i === active ? 0 : -1}
            className={styles.tab}
            onClick={() => setActive(i)}
            onKeyDown={(e) => onKeyDown(e, i)}
          >
            <span className={styles.tabPkg}>{s.pkg}</span>
            {s.symbol}
          </button>
        ))}
      </div>

      <div
        key={current.id}
        role="tabpanel"
        id={`showcase-panel-${current.id}`}
        aria-labelledby={`showcase-tab-${current.id}`}
        className={styles.panel}
      >
        <div className={styles.code}>
          <CodeBlock language="go" title={current.file}>
            {current.code}
          </CodeBlock>
        </div>
        <div className={styles.panelFoot}>
          <p className={styles.panelSummary}>{current.summary}</p>
          <Link to={current.docs} className={styles.panelLink}>
            {current.docsLabel} docs
            <span aria-hidden="true"> →</span>
          </Link>
        </div>
      </div>
    </div>
  );
}
