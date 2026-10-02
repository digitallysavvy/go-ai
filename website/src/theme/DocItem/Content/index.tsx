import Content from '@theme-original/DocItem/Content';
import type ContentType from '@theme/DocItem/Content';
import type { WrapperProps } from '@docusaurus/types';
import type { ReactNode } from 'react';

import PageActions from '../../../components/PageActions';

type Props = WrapperProps<typeof ContentType>;

/** Doc content with the "Copy page / Open in…" actions above it. */
export default function ContentWrapper(props: Props): ReactNode {
  return (
    <>
      <PageActions />
      <Content {...props} />
    </>
  );
}
