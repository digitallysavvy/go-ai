# Web Search Agent Example

Demonstrates an AI agent with web search and content retrieval capabilities.

## Features

- **Web search** integration
- **Page content retrieval**
- **Fact checking** capabilities
- **Multi-source synthesis**
- **Source citation**

## Prerequisites

- Go 1.21+
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running

```bash
cd examples/agents/web-search-agent
go run main.go
```

## How It Works

The agent:
1. Receives a research query
2. Searches the web for relevant information
3. Retrieves detailed page content
4. Fact-checks important claims
5. Synthesizes information from multiple sources
6. Provides cited answer

## Available Tools

- **web_search**: Search for information
- **get_page_content**: Retrieve full page content
- **fact_check**: Verify claims

## Use Cases

1. **Research Assistance** - Answer research questions
2. **Fact Checking** - Verify information
3. **Content Aggregation** - Compile information from multiple sources
4. **News Analysis** - Get latest updates on topics

## Documentation

- [Go AI SDK Agents](../../../docs)
