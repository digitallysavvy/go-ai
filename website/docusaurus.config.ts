import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Go AI SDK',
  tagline: 'Build production-grade AI applications in Go',
  favicon: 'img/favicon.png',

  url: 'https://digitallysavvy.github.io',
  baseUrl: '/go-ai/',
  organizationName: 'digitallysavvy',
  projectName: 'go-ai',
  trailingSlash: false,

  onBrokenLinks: 'throw',

  stylesheets: [
    {
      href: 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600;700&display=swap',
      type: 'text/css',
    },
  ],

  markdown: {
    format: 'detect',
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          // 'docs' prefix matches the /docs/... absolute links throughout the source content
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          numberPrefixParser: true,
          exclude: [
            'README.md',
            'CONTRIBUTING_DOCS.md',
            'DOCUMENTATION_STYLE_GUIDE.md',
            'QUALITY_TOOLS_QUICK_REFERENCE.md',
            '_templates/**',
            'scripts/**',
            'implementation/**',
          ],
          editUrl: 'https://github.com/digitallysavvy/go-ai/edit/main/docs/',
          showLastUpdateTime: true,
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        language: ['en'],
        highlightSearchTermsOnTargetPage: true,
        explicitSearchResultPath: true,
        docsRouteBasePath: 'docs',
        docsDir: '../docs',
        indexBlog: false,
      },
    ],
  ],

  themeConfig: {
    image: 'img/social-card.png',
    colorMode: {
      defaultMode: 'dark',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },
    announcementBar: {
      id: 'star-on-github',
      content:
        '⭐️ Go AI SDK is under active development — <a target="_blank" rel="noopener noreferrer" href="https://github.com/digitallysavvy/go-ai">star us on GitHub</a> and check the <a href="/go-ai/docs/migration-guides/from-v0.4-to-v0.5">latest release notes</a>.',
      backgroundColor: '#00acd7',
      textColor: '#04121a',
      isCloseable: true,
    },
    navbar: {
      title: 'Go AI SDK',
      logo: {
        alt: 'Go AI SDK',
        src: 'img/logo-mark.png',
        srcDark: 'img/logo-mark.png',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/docs/ai-sdk-core/overview',
          label: 'Core API',
          position: 'left',
        },
        {
          to: '/docs/providers/overview',
          label: 'Providers',
          position: 'left',
        },
        {
          href: 'https://pkg.go.dev/github.com/digitallysavvy/go-ai',
          label: 'pkg.go.dev',
          position: 'left',
        },
        {
          href: 'https://github.com/digitallysavvy/go-ai',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            { label: 'Getting Started', to: '/docs/getting-started' },
            { label: 'Foundations', to: '/docs/foundations/overview' },
            { label: 'Core API', to: '/docs/ai-sdk-core/overview' },
            { label: 'Agents', to: '/docs/agents/overview' },
            { label: 'Providers', to: '/docs/providers/overview' },
          ],
        },
        {
          title: 'Reference',
          items: [
            { label: 'API Reference', to: '/docs/reference' },
            { label: 'Migration Guides', to: '/docs/migration-guides/from-v0.4-to-v0.5' },
            { label: 'Troubleshooting', to: '/docs/troubleshooting' },
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/digitallysavvy/go-ai',
            },
            {
              label: 'Issues',
              href: 'https://github.com/digitallysavvy/go-ai/issues',
            },
            {
              label: 'Discussions',
              href: 'https://github.com/digitallysavvy/go-ai/discussions',
            },
            {
              label: 'pkg.go.dev',
              href: 'https://pkg.go.dev/github.com/digitallysavvy/go-ai',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Go AI SDK Contributors. Apache 2.0 License.`,
    },
    prism: {
      theme: prismThemes.vsDark,
      darkTheme: prismThemes.vsDark,
      additionalLanguages: ['go', 'bash', 'json', 'yaml', 'diff'],
      defaultLanguage: 'go',
    },
    docs: {
      sidebar: {
        hideable: true,
        autoCollapseCategories: true,
      },
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
