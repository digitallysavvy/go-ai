import CopyButton from './CopyButton';
import { INSTALL_COMMAND } from './snippets';
import styles from './landing.module.css';

export default function InstallCommand({ className }: { className?: string }) {
  return (
    <div className={`${styles.install} ${className ?? ''}`}>
      <span className={styles.installPrompt} aria-hidden="true">
        $
      </span>
      <code className={styles.installCmd}>{INSTALL_COMMAND}</code>
      <CopyButton text={INSTALL_COMMAND} label="Copy install command" />
    </div>
  );
}
