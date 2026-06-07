import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import { useState } from 'react';
import styles from './index.module.css';

const QUICK_START_CODE = `package main

import (
    "context"
    "fmt"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    ctx := context.Background()

    provider := openai.New(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })
    model, _ := provider.LanguageModel("gpt-4o")

    result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Why is Go great for AI applications?",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Text)
}`;

const FEATURES = [
  {
    icon: '⚡',
    title: '26+ Providers',
    desc: 'Switch between OpenAI, Anthropic, Google, AWS Bedrock, Groq, and 21 more with zero code changes.',
  },
  {
    icon: '🔒',
    title: 'Type Safe',
    desc: "Leverage Go's strong typing system. Catch errors at compile time, not in production.",
  },
  {
    icon: '🌊',
    title: 'Streaming Native',
    desc: 'First-class streaming via Go channels with automatic backpressure and context cancellation.',
  },
  {
    icon: '🤖',
    title: 'Agent Framework',
    desc: 'Build autonomous agents with tool calling, multi-step reasoning, and workflow orchestration.',
  },
  {
    icon: '🧩',
    title: 'Middleware System',
    desc: 'Wrap models with caching, rate limiting, logging, and custom logic using composable middleware.',
  },
  {
    icon: '📊',
    title: 'Observability',
    desc: 'Built-in OpenTelemetry integration with spans, metrics, and structured event callbacks.',
  },
  {
    icon: '🗄️',
    title: 'Structured Output',
    desc: 'Generate validated JSON objects from any model using Go structs and JSON schema.',
  },
  {
    icon: '🎨',
    title: 'Multimodal',
    desc: 'Text, image generation, speech synthesis, transcription, video, and embeddings in one SDK.',
  },
  {
    icon: '🚀',
    title: 'Production Ready',
    desc: 'Battle-tested error handling, retry logic, and deployment patterns for Go services.',
  },
];

const PROVIDERS = [
  'OpenAI', 'Anthropic', 'Google', 'Azure', 'AWS Bedrock', 'Mistral',
  'Cohere', 'Groq', 'DeepSeek', 'xAI', 'Perplexity', 'Together AI',
  'Fireworks', 'Replicate', 'Hugging Face', 'Ollama', 'ElevenLabs',
  'Deepgram', 'AssemblyAI', 'Stability AI', 'Fal.ai', 'Cerebras',
  'DeepInfra', 'Alibaba', 'KlingAI', 'Prodia', 'Voyage', 'Cloudflare',
];

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <button onClick={copy} className={styles.copyIcon} title="Copy to clipboard" style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}>
      {copied ? '✓' : '⎘'}
    </button>
  );
}

export default function Home() {
  const { siteConfig } = useDocusaurusContext();

  return (
    <Layout title={siteConfig.title} description={siteConfig.tagline} noFooter={false}>
      {/* Hero */}
      <section className={styles.hero}>
        <div className={styles.heroBg} />
        <div className={styles.heroGrid}>
          <div>
            <div className={styles.heroBadges}>
              <span className={styles.badge}>Go 1.21+</span>
              <span className={styles.badge}>26+ Providers</span>
              <span className={styles.badge}>Apache 2.0</span>
            </div>

            <h1 className={styles.heroTitle}>
              Build AI apps in{' '}
              <span className={styles.heroAccent}>Go</span>
              <br />
              the right way.
            </h1>

            <p className={styles.heroSubtitle}>
              A unified, production-grade SDK for integrating 26+ AI providers.
              Same API. Any provider. Pure Go.
            </p>

            <div className={styles.heroCtas}>
              <Link to="/docs/getting-started" className={styles.ctaPrimary}>
                Get Started →
              </Link>
              <a
                href="https://github.com/digitallysavvy/go-ai"
                className={styles.ctaSecondary}
                target="_blank"
                rel="noopener noreferrer"
              >
                View on GitHub
              </a>
            </div>

            <div className={styles.installBox}>
              <span className={styles.installLabel}>$</span>
              <code className={styles.installCmd}>
                go get github.com/digitallysavvy/go-ai
              </code>
              <CopyButton text="go get github.com/digitallysavvy/go-ai" />
            </div>
          </div>

          <div className={styles.heroCode}>
            <div className={styles.codePanel}>
              <div className={styles.codePanelHeader}>
                <span className={`${styles.dot} ${styles.dotRed}`} />
                <span className={`${styles.dot} ${styles.dotYellow}`} />
                <span className={`${styles.dot} ${styles.dotGreen}`} />
                <span className={styles.codePanelFilename}>main.go</span>
              </div>
              <div className={styles.codePanelBody}>
                <pre>{QUICK_START_CODE}</pre>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Stats */}
      <div className={styles.stats}>
        <div className={styles.stat}>
          <span className={styles.statValue}>26+</span>
          <span className={styles.statLabel}>AI Providers</span>
        </div>
        <div className={styles.stat}>
          <span className={styles.statValue}>1:1</span>
          <span className={styles.statLabel}>Vercel AI SDK Parity</span>
        </div>
        <div className={styles.stat}>
          <span className={styles.statValue}>Go</span>
          <span className={styles.statLabel}>Native Concurrency</span>
        </div>
        <div className={styles.stat}>
          <span className={styles.statValue}>0</span>
          <span className={styles.statLabel}>Runtime Dependencies</span>
        </div>
      </div>

      {/* Features */}
      <section className={styles.features}>
        <div className={styles.sectionHeader}>
          <p className={styles.sectionLabel}>Why Go AI SDK</p>
          <h2 className={styles.sectionTitle}>Everything you need for AI in Go</h2>
          <p className={styles.sectionSubtitle}>
            From simple text generation to complex multi-agent workflows — built
            for production from day one.
          </p>
        </div>
        <div className={styles.featuresGrid}>
          {FEATURES.map((f) => (
            <div key={f.title} className={styles.featureCard}>
              <span className={styles.featureIcon}>{f.icon}</span>
              <h3 className={styles.featureTitle}>{f.title}</h3>
              <p className={styles.featureDesc}>{f.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Providers */}
      <section className={styles.providers}>
        <div className={styles.providersInner}>
          <div className={styles.sectionHeader}>
            <p className={styles.sectionLabel}>Providers</p>
            <h2 className={styles.sectionTitle}>One API. Every provider.</h2>
            <p className={styles.sectionSubtitle}>
              Switch providers by changing a single line. Your business logic
              never changes.
            </p>
          </div>
          <div className={styles.providersList}>
            {PROVIDERS.map((p) => (
              <span key={p} className={styles.providerPill}>
                {p}
              </span>
            ))}
          </div>
        </div>
      </section>
    </Layout>
  );
}
