import MDXComponents from '@theme-original/MDXComponents';
import Admonition from '@theme/Admonition';
import type { ReactNode } from 'react';

function Note({ children }: { children: ReactNode }) {
  return <Admonition type="note">{children}</Admonition>;
}

export default {
  ...MDXComponents,
  Note,
};
