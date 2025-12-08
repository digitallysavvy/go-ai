# Comprehensive Example

This example showcases multiple features of the Go AI SDK in a single application, demonstrating how different capabilities can work together.

## Features Demonstrated

1. **Multiple Provider Support** - Use OpenAI, Anthropic, and Google models interchangeably
2. **Embeddings & Semantic Search** - Generate embeddings and find similar documents
3. **Autonomous Agents** - Multi-step reasoning with tool usage

## Prerequisites

- Go 1.21 or higher
- At least one API key (OpenAI recommended for full functionality)
  - OpenAI API key (for all features)
  - Anthropic API key (optional, for Claude models)
  - Google API key (optional, for Gemini models)

## Setup

1. Set your API keys:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...  # Optional
export GOOGLE_API_KEY=...            # Optional
```

2. Run the example:

```bash
go run main.go
```

## What You'll Learn

### Multi-Provider Architecture
- How to use multiple providers in the same application
- How to switch between models from different providers
- The unified API across all providers

### Embeddings and Semantic Search
- Creating embeddings with `EmbedMany()`
- Querying with `Embed()`
- Finding similar documents with `FindMostSimilar()`
- Building a simple semantic search system

### Agent Workflows
- Creating autonomous agents with `agent.NewToolLoopAgent()`
- Defining multiple tools for the agent
- Multi-step reasoning and tool orchestration
- Tracking agent execution steps

## Expected Output

The example will demonstrate:
1. Responses from multiple AI providers (if configured)
2. Semantic similarity scores for document search
3. Agent execution with tool calls for math and weather

## Notes

- The example will skip providers for which API keys are not set
- Each section is independent - failures in one won't affect others
- Tool executions in the agent example are simulated (no real API calls)
