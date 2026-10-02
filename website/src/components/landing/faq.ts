import { PROVIDER_COUNT } from './providers';

/**
 * Landing page FAQ. Rendered visibly and as schema.org FAQPage data, so the
 * text must stay plain (no markup) and accurate.
 */
export const FAQ: { q: string; a: string }[] = [
  {
    q: 'What is the Go AI SDK?',
    a: `The Go AI SDK is an open-source Go library for building AI applications and agents. One API covers text generation and streaming, structured output, tool calling, agents, embeddings, image and speech generation, and MCP, across ${PROVIDER_COUNT} model providers.`,
  },
  {
    q: 'How does it relate to the Vercel AI SDK?',
    a: 'It is a Go port of the TypeScript AI SDK from Vercel. It keeps the same concepts and names (GenerateText, StreamText, tools, UIMessage) in idiomatic Go and tracks TypeScript releases; the few intentional differences are documented in the migration guides.',
  },
  {
    q: 'Which model providers does it support?',
    a: `${PROVIDER_COUNT} provider packages, including OpenAI, Anthropic, Google Gemini, Google Vertex AI, Amazon Bedrock, Azure OpenAI, Mistral, xAI Grok, Groq, DeepSeek, Cohere, Together AI, Fireworks, Perplexity and Ollama. Each provider returns the same model interfaces, so switching providers only changes the constructor.`,
  },
  {
    q: 'Can a React or Next.js frontend using useChat talk to a Go backend?',
    a: 'Yes. The Go AI SDK speaks the same UI message stream protocol as the TypeScript SDK, so a useChat frontend pointed at a Go handler that calls ai.PipeUIMessageStreamToResponse works unmodified.',
  },
  {
    q: 'Does it support AI agents and the Model Context Protocol (MCP)?',
    a: 'Yes. pkg/agent runs multi-step tool-calling agents with stop conditions and approvals, and pkg/mcp connects to MCP servers and exposes their tools to any model.',
  },
  {
    q: 'What are the requirements and license?',
    a: 'Go 1.26 or later. Install with go get github.com/digitallysavvy/go-ai. The SDK is open source under the Apache 2.0 license.',
  },
  {
    q: 'Are the docs available for AI coding agents?',
    a: 'Yes. Every docs page is available as markdown by adding .md to its URL, llms.txt lists every page, and llms-full.txt contains the full documentation in one file.',
  },
];
