// Capability rows for the "package index" section. Each row names the real
// package path and exported symbols that back the claim.

export type PackageRow = {
  path: string;
  title: string;
  desc: string;
  symbols: string[];
  docs: string;
};

export const PACKAGE_ROWS: PackageRow[] = [
  {
    path: 'pkg/ai',
    title: 'Text generation and streaming',
    desc: 'One call for a full result, or a channel of chunks while the model is still writing.',
    symbols: ['GenerateText', 'StreamText', 'Chunks()'],
    docs: '/docs/ai-sdk-core/generating-text',
  },
  {
    path: 'pkg/ai',
    title: 'Structured output',
    desc: 'Typed objects from a Go struct or a JSON schema, generated in one shot or streamed as they fill in.',
    symbols: ['GenerateObject', 'StreamObject', 'ObjectOutput[T]', 'SchemaFor[T]'],
    docs: '/docs/ai-sdk-core/generating-structured-data',
  },
  {
    path: 'pkg/agent',
    title: 'Tools and multi-step agents',
    desc: 'Tools are plain Go funcs with a schema. The tool loop runs them until your stop condition says otherwise.',
    symbols: ['NewToolLoopAgent', 'AgentConfig', 'StopWhen', 'StepCountIs'],
    docs: '/docs/agents/overview',
  },
  {
    path: 'pkg/ai',
    title: 'Embeddings and rerank',
    desc: 'Embed one value or a batch, and rerank documents against a query, through the same provider packages.',
    symbols: ['Embed', 'EmbedMany', 'Rerank'],
    docs: '/docs/ai-sdk-core/embeddings',
  },
  {
    path: 'pkg/ai',
    title: 'Image, speech, transcription, video',
    desc: 'Media generation uses the same options-struct shape as text, so the call site looks familiar.',
    symbols: ['GenerateImage', 'GenerateSpeech', 'Transcribe', 'GenerateVideo'],
    docs: '/docs/ai-sdk-core/image-generation',
  },
  {
    path: 'pkg/mcp',
    title: 'MCP client',
    desc: 'Connect to Model Context Protocol servers and expose their tools to the model.',
    symbols: ['NewMCPClient', 'NewHTTPTransport', 'ConvertToGoAITools'],
    docs: '/docs/ai-sdk-core/mcp-tools',
  },
  {
    path: 'pkg/middleware',
    title: 'Middleware',
    desc: 'Wrap a model or a whole provider to transform params, results and streams. Built-ins included.',
    symbols: ['WrapLanguageModel', 'WrapProvider', 'ExtractReasoningMiddleware', 'DefaultSettingsMiddleware'],
    docs: '/docs/ai-sdk-core/middleware',
  },
  {
    path: 'pkg/telemetry',
    title: 'Telemetry',
    desc: 'OpenTelemetry spans for generation calls, plus a registry for pluggable telemetry integrations.',
    symbols: ['GetTracer', 'RegisterTelemetryIntegration'],
    docs: '/docs/ai-sdk-core/telemetry',
  },
  {
    path: 'pkg/providers/gateway',
    title: 'AI Gateway',
    desc: 'Language, embedding, image, speech, transcription, video and reranking models through the AI Gateway.',
    symbols: ['gateway.New'],
    docs: '/docs/providers/gateway',
  },
  {
    path: 'pkg/harness',
    title: 'Coding-agent harnesses',
    desc: 'Drive coding agents from Go over the harness-v1 protocol, with bridges for Claude Code, Codex, OpenCode, GitHub Copilot and more.',
    symbols: ['harness.Session', 'bridges'],
    docs: '/docs/advanced/coding-agents',
  },
];
