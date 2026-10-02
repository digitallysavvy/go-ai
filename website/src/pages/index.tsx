import Head from '@docusaurus/Head';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import CodeBlock from '@theme/CodeBlock';
import Layout from '@theme/Layout';

import { FAQ } from '../components/landing/faq';
import GitHubIcon from '../components/landing/GitHubIcon';
import InstallCommand from '../components/landing/InstallCommand';
import Showcase from '../components/landing/Showcase';
import { PACKAGE_ROWS } from '../components/landing/packages';
import { PROVIDER_COUNT, PROVIDER_PACKAGES, providerHref } from '../components/landing/providers';
import { UI_STREAM_SNIPPET } from '../components/landing/snippets';
import styles from '../components/landing/landing.module.css';
import pageStyles from './index.module.css';

const GITHUB_URL = 'https://github.com/digitallysavvy/go-ai';

function symbolKind(symbol: string): 'func' | 'type' | 'pkg' {
  if (symbol === 'bridges') return 'pkg';
  if (/^[a-z]+\.[A-Z]/.test(symbol) && !symbol.endsWith('New')) return 'type';
  if (/^(AgentConfig|StopWhen)$/.test(symbol)) return 'type';
  return 'func';
}

function Hero() {
  const markSrc = useBaseUrl('/img/landing/mark.png');
  return (
    <section className={styles.hero}>
      <div className={styles.heroGridBg} aria-hidden="true" />
      <div className={`${styles.container} ${styles.heroGrid}`}>
        <div className={styles.heroCopy}>
          <img
            src={markSrc}
            className={`${styles.mark} ${styles.rise}`}
            width={353}
            height={274}
            alt=""
            decoding="async"
          />
          <p className={`${styles.heroMeta} ${styles.rise}`} style={{ ['--i' as string]: 1 }}>
            <span>github.com/digitallysavvy/go-ai</span>
            <span>Apache-2.0</span>
          </p>
          <h1 className={`${styles.h1} ${styles.rise}`} style={{ ['--i' as string]: 2 }}>
            The AI SDK,
            <br />
            in <em>Go</em>
            <span className={styles.caret} aria-hidden="true" />
          </h1>
          <p className={`${styles.heroSub} ${styles.rise}`} style={{ ['--i' as string]: 3 }}>
            Feature parity with Vercel&rsquo;s AI SDK, in idiomatic Go. Generate, stream, call tools,
            run agents and serve chat UIs across {PROVIDER_COUNT} providers.
          </p>
          <div className={styles.rise} style={{ ['--i' as string]: 4 }}>
            <InstallCommand />
          </div>
          <div className={`${styles.heroActions} ${styles.rise}`} style={{ ['--i' as string]: 5 }}>
            <Link to="/docs/getting-started" className={styles.btnPrimary}>
              Get started
            </Link>
            <a href={GITHUB_URL} className={styles.btnSecondary} target="_blank" rel="noopener noreferrer">
              <GitHubIcon />
              GitHub
            </a>
          </div>
        </div>

        <div className={styles.rise} style={{ ['--i' as string]: 3 }}>
          <Showcase />
        </div>
      </div>
    </section>
  );
}

function Packages() {
  return (
    <section className={styles.section} aria-labelledby="packages-title">
      <div className={styles.container}>
        <div className={styles.sectionHead}>
          <p className={styles.eyebrow}>go list ./pkg/...</p>
          <h2 id="packages-title" className={styles.h2}>
            Everything the AI SDK does, as Go packages.
          </h2>
          <p className={styles.lede}>
            The module is organised the way you will import it. Each row below is a package path and the
            exported names you will actually call.
          </p>
        </div>
        <div className={styles.pkgGrid}>
          {PACKAGE_ROWS.map((row) => (
            <article key={row.title} className={styles.pkgRow}>
              <span className={styles.pkgPath}>{row.path}</span>
              <h3 className={styles.pkgTitle}>
                <Link to={row.docs}>{row.title}</Link>
              </h3>
              <p className={styles.pkgDesc}>{row.desc}</p>
              <ul className={styles.pkgSyms}>
                {row.symbols.map((s) => (
                  <li key={s} data-kind={symbolKind(s)}>
                    {s}
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function Providers() {
  return (
    <section className={styles.section} aria-labelledby="providers-title">
      <div className={styles.container}>
        <div className={styles.provHead}>
          <div>
            <p className={styles.eyebrow}>pkg/providers</p>
            <h2 id="providers-title" className={styles.h2}>
              {PROVIDER_COUNT} provider packages.
            </h2>
          </div>
          <p className={styles.lede}>
            Every provider is its own package that returns the shared model interfaces. Swapping providers is
            a change to the import and the constructor; the calls stay the same.
          </p>
        </div>
        <ul className={styles.provGrid}>
          {PROVIDER_PACKAGES.map((p) =>
            p.docs ? (
              <li key={p.pkg}>
                <Link to={p.docs} className={styles.prov}>
                  {p.pkg}
                </Link>
              </li>
            ) : (
              <li key={p.pkg}>
                <a href={providerHref(p)} className={styles.prov} target="_blank" rel="noopener noreferrer">
                  {p.pkg}
                </a>
              </li>
            ),
          )}
        </ul>
        <p className={styles.provNote}>
          Names are the package directories under <code>pkg/providers</code>.{' '}
          <Link to="/docs/providers/overview">Provider overview</Link>
        </p>
      </div>
    </section>
  );
}

function UIStream() {
  return (
    <section className={styles.section} aria-labelledby="ui-title">
      <div className={`${styles.container} ${styles.uiGrid}`}>
        <div className={styles.uiProse}>
          <p className={styles.eyebrow}>pkg/ai · UIMessage</p>
          <h2 id="ui-title" className={styles.h2}>
            Serve the AI SDK UI from Go.
          </h2>
          <p>
            <code>ai.UIMessage</code> mirrors the TypeScript <code>UIMessage</code> shape, with the same JSON
            keys and part types, and <code>ai.PipeUIMessageStreamToResponse</code> writes the UI message
            stream as server-sent events. Point an AI SDK front end such as <code>useChat</code> at a Go
            route.
          </p>
          <ul>
            <li>
              <code>ConvertToModelMessages</code> turns incoming UI messages into model messages.
            </li>
            <li>
              <code>CreateUIMessageStreamResponse</code> builds a ready-made <code>*http.Response</code>{' '}
              when you are not inside a handler.
            </li>
          </ul>
          <Link to="/docs/reference/ai/stream-transport-helpers" className={styles.btnSecondary}>
            Stream transport helpers
          </Link>
        </div>
        <div className={`${styles.code} ${styles.codeSolo}`}>
          <CodeBlock language="go" title="server.go">
            {UI_STREAM_SNIPPET}
          </CodeBlock>
        </div>
      </div>
    </section>
  );
}

function Faq() {
  return (
    <section className={styles.section} aria-labelledby="faq-title">
      <div className={styles.container}>
        <div className={styles.sectionHead}>
          <p className={styles.eyebrow}>faq</p>
          <h2 id="faq-title" className={styles.h2}>
            Questions about the Go AI SDK
          </h2>
        </div>
        <div className={pageStyles.faq}>
          {FAQ.map(({ q, a }) => (
            <details key={q} className={pageStyles.faqItem}>
              <summary className={pageStyles.faqQ}>{q}</summary>
              <p className={pageStyles.faqA}>{a}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}

function FinalCta() {
  return (
    <section className={styles.cta} aria-labelledby="cta-title">
      <div className={`${styles.container} ${styles.ctaInner}`}>
        <h2 id="cta-title" className={styles.h2}>
          Start with one file.
        </h2>
        <p className={styles.lede}>
          Install the module, set a provider key, and run the first example from the getting-started guide.
        </p>
        <InstallCommand />
        <div className={styles.ctaActions}>
          <Link to="/docs/getting-started" className={styles.btnPrimary}>
            Get started
          </Link>
          <Link to="/docs/reference" className={styles.btnSecondary}>
            API reference
          </Link>
        </div>
      </div>
    </section>
  );
}

const PAGE_TITLE = 'Go AI SDK: build AI apps and agents in Go';
const PAGE_DESCRIPTION = `The open-source AI SDK for Go. Generate and stream text, call tools, run agents, use MCP and serve useChat UIs across ${PROVIDER_COUNT} providers, with parity with the TypeScript AI SDK.`;

function StructuredData() {
  const { siteConfig } = useDocusaurusContext();
  const home = siteConfig.url.replace(/\/$/, '') + siteConfig.baseUrl;
  const data = [
    {
      '@context': 'https://schema.org',
      '@type': 'WebSite',
      name: siteConfig.title,
      url: home,
      description: PAGE_DESCRIPTION,
    },
    {
      '@context': 'https://schema.org',
      '@type': 'SoftwareSourceCode',
      name: 'Go AI SDK',
      description: PAGE_DESCRIPTION,
      url: home,
      codeRepository: GITHUB_URL,
      programmingLanguage: { '@type': 'ComputerLanguage', name: 'Go', url: 'https://go.dev' },
      runtimePlatform: 'Go 1.26+',
      license: 'https://www.apache.org/licenses/LICENSE-2.0',
      keywords: 'Go, golang, AI SDK, LLM, AI agents, tool calling, MCP, OpenAI, Anthropic, Gemini',
      image: home + 'img/social-card.png',
    },
    {
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: FAQ.map(({ q, a }) => ({
        '@type': 'Question',
        name: q,
        acceptedAnswer: { '@type': 'Answer', text: a },
      })),
    },
  ];
  return (
    <Head>
      <title>{PAGE_TITLE}</title>
      <meta property="og:title" content={PAGE_TITLE} />
      <script type="application/ld+json">{JSON.stringify(data)}</script>
    </Head>
  );
}

export default function Home() {
  return (
    <Layout description={PAGE_DESCRIPTION}>
      <StructuredData />
      <main className={`${styles.page} ${pageStyles.main}`}>
        <Hero />
        <Packages />
        <Providers />
        <UIStream />
        <Faq />
        <FinalCta />
      </main>
    </Layout>
  );
}
