// One entry per provider package directory under pkg/providers, excluding the
// two non-provider helpers in that directory (gemini: shared Google/Vertex
// implementation; youcom: tool factories). Keep alphabetical.

export type ProviderEntry = {
  pkg: string;
  /** Internal docs route, or undefined to fall back to pkg.go.dev. */
  docs?: string;
};

export const PROVIDER_PACKAGES: ProviderEntry[] = [
  { pkg: 'alibaba', docs: '/docs/providers/alibaba' },
  { pkg: 'anthropic', docs: '/docs/providers/anthropic' },
  { pkg: 'anthropicaws' },
  { pkg: 'assemblyai', docs: '/docs/providers/assemblyai' },
  { pkg: 'azure', docs: '/docs/providers/azure' },
  { pkg: 'baseten', docs: '/docs/providers/baseten' },
  { pkg: 'bedrock', docs: '/docs/providers/bedrock' },
  { pkg: 'bfl', docs: '/docs/providers/bfl' },
  { pkg: 'bytedance', docs: '/docs/providers/bytedance-video-generation' },
  { pkg: 'cartesia' },
  { pkg: 'cerebras', docs: '/docs/providers/cerebras' },
  { pkg: 'cohere', docs: '/docs/providers/cohere' },
  { pkg: 'deepgram', docs: '/docs/providers/deepgram' },
  { pkg: 'deepinfra', docs: '/docs/providers/deepinfra' },
  { pkg: 'deepseek', docs: '/docs/providers/deepseek' },
  { pkg: 'elevenlabs', docs: '/docs/providers/elevenlabs' },
  { pkg: 'fal', docs: '/docs/providers/fal' },
  { pkg: 'fireworks', docs: '/docs/providers/fireworks' },
  { pkg: 'fishaudio' },
  { pkg: 'gateway', docs: '/docs/providers/gateway' },
  { pkg: 'gladia', docs: '/docs/providers/gladia' },
  { pkg: 'gmicloud' },
  { pkg: 'google', docs: '/docs/providers/google' },
  { pkg: 'googlevertex', docs: '/docs/providers/google-vertex' },
  { pkg: 'groq', docs: '/docs/providers/groq' },
  { pkg: 'huggingface', docs: '/docs/providers/huggingface' },
  { pkg: 'hume' },
  { pkg: 'klingai', docs: '/docs/providers/klingai' },
  { pkg: 'lmnt', docs: '/docs/providers/lmnt' },
  { pkg: 'luma' },
  { pkg: 'minimax' },
  { pkg: 'mistral', docs: '/docs/providers/mistral' },
  { pkg: 'moonshot' },
  { pkg: 'ollama', docs: '/docs/providers/ollama' },
  { pkg: 'openai', docs: '/docs/providers/openai' },
  { pkg: 'openresponses', docs: '/docs/providers/openresponses' },
  { pkg: 'perplexity', docs: '/docs/providers/perplexity' },
  { pkg: 'prodia', docs: '/docs/providers/prodia' },
  { pkg: 'quiverai', docs: '/docs/providers/quiverai' },
  { pkg: 'replicate', docs: '/docs/providers/replicate' },
  { pkg: 'revai' },
  { pkg: 'stability', docs: '/docs/providers/stability' },
  { pkg: 'together', docs: '/docs/providers/together' },
  { pkg: 'typesafeai' },
  { pkg: 'vercel' },
  { pkg: 'voyage', docs: '/docs/providers/voyage' },
  { pkg: 'xai', docs: '/docs/providers/xai' },
  { pkg: 'zai' },
];

export const PROVIDER_COUNT = PROVIDER_PACKAGES.length;

export function providerHref(entry: ProviderEntry): string {
  return entry.docs ?? `https://pkg.go.dev/github.com/digitallysavvy/go-ai/pkg/providers/${entry.pkg}`;
}
