# AI Gateway Provider

The AI Gateway provider enables unified access to multiple LLM providers through Vercel's AI Gateway service. It supports model routing, zero data retention, and provider-specific search tools.

## Features

- **Unified Model Access**: Access models from multiple providers through a single interface
- **Zero Data Retention**: Optional mode that prevents request logging
- **Model Routing**: Intelligent routing to different model providers
- **Search Tools**: Built-in Parallel Search and Perplexity Search tools
- **Automatic Failover**: Gateway handles provider failover automatically
- **Usage Tracking**: Track API usage and credits

## Installation

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/gateway"
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/gateway"
)

func main() {
    // Create gateway provider
    provider, err := gateway.New(gateway.Config{
        APIKey: "your-api-key",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Create language model
    model, err := provider.LanguageModel("openai/gpt-4")
    if err != nil {
        log.Fatal(err)
    }

    // Generate text
    result, err := ai.GenerateText(context.Background(), model, ai.GenerateTextOptions{
        Prompt: "What is the capital of France?",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

### Zero Data Retention

```go
provider, err := gateway.New(gateway.Config{
    APIKey:            "your-api-key",
    ZeroDataRetention: true, // Enable zero data retention
})
```

### Get Available Models

```go
metadata, err := provider.GetAvailableModels(context.Background())
if err != nil {
    log.Fatal(err)
}

for _, provider := range metadata.Providers {
    fmt.Printf("Provider: %s\n", provider.Name)
    for _, model := range provider.Models {
        fmt.Printf("  - %s (%s)\n", model.Name, model.ID)
    }
}
```

### Check Credits

```go
credits, err := provider.GetCredits(context.Background())
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Available Credits: %d\n", credits.Available)
fmt.Printf("Used Credits: %d\n", credits.Used)
```

## Provider-Executed Tools

The Gateway provider includes two powerful search tools that are executed server-side by the gateway.

### Parallel Search

Search the web using Parallel AI's Search API for LLM-optimized excerpts.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/gateway"
    "github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func main() {
    provider, _ := gateway.New(gateway.Config{APIKey: "your-api-key"})
    model, _ := provider.LanguageModel("openai/gpt-4")

    // Create parallel search tool
    parallelSearch := tools.NewParallelSearch(tools.ParallelSearchConfig{
        Mode:       "one-shot", // or "agentic"
        MaxResults: 10,
        SourcePolicy: &tools.ParallelSearchSourcePolicy{
            IncludeDomains: []string{"wikipedia.org", "nature.com"},
            AfterDate:      "2024-01-01",
        },
    })

    result, err := ai.GenerateText(context.Background(), model, ai.GenerateTextOptions{
        Prompt: "Search for the latest developments in quantum computing",
        Tools: []types.Tool{
            parallelSearch.ToTool(),
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

#### Parallel Search Modes

- **one-shot**: Comprehensive results with longer excerpts for single-response answers
- **agentic**: Concise, token-efficient results for multi-step workflows

### Perplexity Search

Search using Perplexity's API for real-time information and news.

```go
// Create perplexity search tool
perplexitySearch := tools.NewPerplexitySearch(tools.PerplexitySearchConfig{
    MaxResults:       10,
    MaxTokensPerPage: 2048,
    Country:          "US",
    SearchDomainFilter: []string{"nature.com", "science.org"},
    SearchRecencyFilter: "week",
})

result, err := ai.GenerateText(context.Background(), model, ai.GenerateTextOptions{
    Prompt: "What are the latest AI research papers?",
    Tools: []types.Tool{
        perplexitySearch.ToTool(),
    },
})
```

## Configuration Options

### Provider Config

- `APIKey` (string): AI Gateway API key (or set `AI_GATEWAY_API_KEY` env var)
- `BaseURL` (string): Gateway API base URL (default: `https://ai-gateway.vercel.sh/v3/ai`)
- `Headers` (map[string]string): Custom headers
- `MetadataCacheRefreshMillis` (int64): Metadata cache refresh interval in milliseconds (default: 300000)
- `HTTPClient` (*http.Client): Custom HTTP client
- `ZeroDataRetention` (bool): Enable zero data retention mode

### Parallel Search Config

- `Mode` (string): "one-shot" or "agentic"
- `MaxResults` (int): Maximum results (1-20, default: 10)
- `SourcePolicy` (*ParallelSearchSourcePolicy): Domain and date filtering
- `Excerpts` (*ParallelSearchExcerpts): Excerpt length configuration
- `FetchPolicy` (*ParallelSearchFetchPolicy): Content freshness settings

### Perplexity Search Config

- `MaxResults` (int): Maximum results (1-20, default: 10)
- `MaxTokensPerPage` (int): Tokens per page (256-2048, default: 2048)
- `MaxTokens` (int): Total tokens across results (max: 1000000)
- `Country` (string): ISO 3166-1 country code
- `SearchDomainFilter` ([]string): Include/exclude domains
- `SearchLanguageFilter` ([]string): ISO 639-1 language codes
- `SearchRecencyFilter` (string): "day", "week", "month", or "year"

## Environment Variables

- `AI_GATEWAY_API_KEY`: API key for authentication
- `VERCEL_DEPLOYMENT_ID`: Vercel deployment ID (for observability)
- `VERCEL_ENV`: Vercel environment (for observability)
- `VERCEL_REGION`: Vercel region (for observability)

## Model IDs

Model IDs use the format `provider/model`. Examples:

- `openai/gpt-4`
- `anthropic/claude-3-opus-20240229`
- `google/gemini-pro`
- `mistral/mistral-large-latest`

Check available models using `provider.GetAvailableModels()`.

## Error Handling

The gateway provider returns standard provider errors:

- `AuthenticationError`: Invalid or missing API key
- `RateLimitError`: Rate limit exceeded
- `InvalidRequestError`: Invalid request parameters
- `APIError`: General API errors

```go
import "github.com/digitallysavvy/go-ai/pkg/provider/errors"

result, err := ai.GenerateText(ctx, model, opts)
if err != nil {
    switch e := err.(type) {
    case *errors.AuthenticationError:
        log.Printf("Authentication failed: %v", e)
    case *errors.RateLimitError:
        log.Printf("Rate limit exceeded: %v", e)
    default:
        log.Printf("Error: %v", err)
    }
}
```

## Examples

See the `examples/providers/gateway/` directory for complete examples:

- `basic/`: Basic gateway usage
- `parallel-search/`: Using Parallel Search tool
- `zero-retention/`: Zero data retention mode
- `model-routing/`: Model routing and failover

## License

Apache-2.0
