# Anthropic Prompt Caching

Demonstrates Anthropic's prompt caching feature for cost and latency reduction.

## Features

- **Automatic caching** of large system prompts
- **Document context caching** for RAG applications
- **Cost savings** up to 90% on cached content
- **Latency reduction** up to 85% on cached content
- **5-minute cache TTL**

## How It Works

Anthropic automatically caches:
- System messages
- Long context in messages
- Tool definitions

Cache hits reduce:
- **Cost**: $0.30 per 1M cached tokens (vs $3.00 regular)
- **Latency**: ~85% faster for cached content

## Prerequisites

- Anthropic API key
- Claude 3 or newer (Sonnet, Opus, Haiku)

## Setup

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

## Running

```bash
cd examples/providers/anthropic/caching
go run main.go
```

## Use Cases

1. **Large System Prompts** - Cache detailed instructions
2. **RAG Applications** - Cache document context
3. **Multi-turn Conversations** - Cache conversation history
4. **Batch Processing** - Reuse context across queries

## Best Practices

1. **Minimum 1024 tokens** for caching to be effective
2. **Reuse prompts** within 5-minute window
3. **Structure prompts** with cacheable prefix
4. **Monitor cache hits** in usage metrics

## Documentation

- [Anthropic Prompt Caching](https://docs.anthropic.com/claude/docs/prompt-caching)
- [Go AI SDK Docs](../../../../docs)
