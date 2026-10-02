import Head from '@docusaurus/Head';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import { useDoc } from '@docusaurus/plugin-content-docs/client';
import Metadata from '@theme-original/DocItem/Metadata';
import type MetadataType from '@theme/DocItem/Metadata';
import type { WrapperProps } from '@docusaurus/types';
import type { ReactNode } from 'react';

import { absoluteUrl, markdownPath, ogImagePath } from '../../seo';

type Props = WrapperProps<typeof MetadataType>;

/**
 * Adds, on top of Docusaurus's own doc metadata: the page's generated Open
 * Graph image, a link to its markdown copy for agents, and schema.org
 * TechArticle structured data. (Docusaurus already emits BreadcrumbList.)
 */
export default function MetadataWrapper(props: Props): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  const { metadata, frontMatter } = useDoc();

  const url = absoluteUrl(siteConfig.url, metadata.permalink);
  const image = frontMatter.image
    ? undefined // an explicit frontmatter image wins (Docusaurus already emits it)
    : absoluteUrl(siteConfig.url, ogImagePath(metadata.permalink, siteConfig.baseUrl));
  const home = absoluteUrl(siteConfig.url, siteConfig.baseUrl);

  const structuredData = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    headline: metadata.title,
    description: metadata.description,
    url,
    ...(image ? { image } : {}),
    ...(metadata.lastUpdatedAt ? { dateModified: new Date(metadata.lastUpdatedAt).toISOString() } : {}),
    inLanguage: 'en',
    proficiencyLevel: 'Expert',
    isPartOf: { '@type': 'WebSite', name: siteConfig.title, url: home },
    about: {
      '@type': 'SoftwareSourceCode',
      name: 'Go AI SDK',
      codeRepository: 'https://github.com/digitallysavvy/go-ai',
      programmingLanguage: 'Go',
    },
  };

  return (
    <>
      <Metadata {...props} />
      <Head>
        <meta property="og:type" content="article" />
        {image && <meta property="og:image" content={image} />}
        {image && <meta name="twitter:image" content={image} />}
        {image && <meta property="og:image:width" content="1200" />}
        {image && <meta property="og:image:height" content="630" />}
        <link rel="alternate" type="text/markdown" href={markdownPath(metadata.permalink)} />
        <script type="application/ld+json">{JSON.stringify(structuredData)}</script>
      </Head>
    </>
  );
}
