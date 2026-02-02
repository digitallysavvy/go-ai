# xAI Provider Tools

This package provides tools for xAI (Grok) integration, including provider-executed tools for vector search and MCP server integration.

## Overview

The xAI provider supports two specialized provider-executed tools:

1. **FileSearch** - Search vector stores for relevant information (RAG applications)
2. **MCPServer** - Connect to remote Model Context Protocol (MCP) servers

These tools are executed by xAI's servers (not locally), enabling powerful integrations without requiring local implementation.

## FileSearch Tool

The FileSearch tool enables Retrieval Augmented Generation (RAG) by searching vector stores for relevant information.

### Features

- Search across multiple vector stores
- Configurable maximum number of results
- Returns file IDs, filenames, relevance scores, and text snippets
- Provider-executed (no local implementation needed)

### Usage

#### Basic Example

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// Create a FileSearch tool
tool := xai.FileSearch(xai.FileSearchConfig{
    VectorStoreIDs: []string{"vs_123", "vs_456"},
    MaxNumResults:  5,
})
```

#### With Defaults

```go
// Use default settings (max 10 results)
tool := xai.FileSearchWithDefaults([]string{"vs_123"})
```

#### Complete Example

```go
package main

import (
    "context"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
    // Create provider
    provider := xai.New(xai.Config{
        APIKey: "your-api-key",
    })

    model, _ := provider.LanguageModel("grok-beta")

    // Create FileSearch tool
    fileSearchTool := xai.FileSearch(xai.FileSearchConfig{
        VectorStoreIDs: []string{"vs_abc123"},
        MaxNumResults:  5,
    })

    // Use with GenerateText
    result, err := ai.GenerateText(context.Background(), model, &ai.GenerateTextOptions{
        Prompt: types.Prompt{
            Messages: []types.Message{
                {
                    Role: "user",
                    Content: []types.ContentPart{
                        {Type: "text", Text: "What are the main features?"},
                    },
                },
            },
        },
        Tools: []types.Tool{fileSearchTool},
        ToolChoice: types.AutoToolChoice(),
    })

    if err != nil {
        log.Fatal(err)
    }

    log.Println(result.Text)
}
```

### Configuration

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `VectorStoreIDs` | `[]string` | List of vector store IDs to search | Required |
| `MaxNumResults` | `int` | Maximum number of results to return | 10 |

### Response Format

The tool returns search results including:
- File IDs and filenames
- Relevance scores
- Matching text snippets
- Query information

---

## MCPServer Tool

The MCPServer tool enables integration with remote Model Context Protocol (MCP) servers, allowing the model to use external tools and services.

### Features

- Connect to remote MCP-compatible servers
- Filter tools with AllowedTools
- Custom headers for API versioning
- Authorization support
- Provider-executed (no local MCP client needed)

### Usage

#### Full Configuration

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// Create an MCP Server tool with full configuration
tool := xai.MCPServer(xai.MCPServerConfig{
    ServerURL:     "https://api.example.com/mcp",
    AllowedTools:  []string{"search", "summarize", "translate"},
    Authorization: "Bearer your-token",
    Headers: map[string]string{
        "X-API-Version": "v1",
    },
})
```

#### Simple Configuration

```go
// Simple MCP server (allows all tools)
tool := xai.MCPServerSimple("https://api.example.com/mcp")
```

#### With Authentication

```go
// MCP server with authentication
tool := xai.MCPServerWithAuth(
    "https://api.example.com/mcp",
    "Bearer your-token",
)
```

#### Complete Example

```go
package main

import (
    "context"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
    // Create provider
    provider := xai.New(xai.Config{
        APIKey: "your-api-key",
    })

    model, _ := provider.LanguageModel("grok-beta")

    // Create MCP Server tool
    mcpTool := xai.MCPServer(xai.MCPServerConfig{
        ServerURL:     "https://api.example.com/mcp",
        AllowedTools:  []string{"search", "summarize"},
        Authorization: "Bearer token",
        Headers: map[string]string{
            "X-API-Version": "v1",
        },
    })

    // Use with GenerateText
    result, err := ai.GenerateText(context.Background(), model, &ai.GenerateTextOptions{
        Prompt: types.Prompt{
            Messages: []types.Message{
                {
                    Role: "user",
                    Content: []types.ContentPart{
                        {Type: "text", Text: "Search for Go best practices"},
                    },
                },
            },
        },
        Tools: []types.Tool{mcpTool},
        ToolChoice: types.AutoToolChoice(),
    })

    if err != nil {
        log.Fatal(err)
    }

    log.Println(result.Text)
}
```

### Configuration

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `ServerURL` | `string` | URL of the MCP server | Required |
| `AllowedTools` | `[]string` | List of allowed tool names | All tools allowed |
| `Authorization` | `string` | Authorization token | None |
| `Headers` | `map[string]string` | Custom HTTP headers | None |

### MCP Protocol

The Model Context Protocol (MCP) is a standard for connecting language models to external tools and data sources. MCP servers expose tools that can be called by the model.

Key concepts:
- **Server**: HTTP endpoint implementing MCP
- **Tools**: Functions exposed by the server
- **Authorization**: Optional authentication
- **Headers**: Custom metadata for requests

---

## Provider-Executed Tools

Both FileSearch and MCPServer are **provider-executed tools**, meaning:

1. **No Local Implementation**: You don't need to implement the Execute function
2. **Server-Side Execution**: xAI's servers handle execution
3. **Automatic Results**: Results are returned through the normal tool flow
4. **Error Handling**: Provider errors are handled by the SDK

### How It Works

```go
tool := xai.FileSearch(xai.FileSearchConfig{
    VectorStoreIDs: []string{"vs_123"},
    MaxNumResults:  5,
})

// The tool has ProviderExecuted = true
fmt.Println(tool.ProviderExecuted) // true

// When used with a model, xAI's servers execute the tool
// You receive the results automatically in the response
```

### Error Handling

Provider-executed tools may fail if:
- Vector store doesn't exist
- MCP server is unreachable
- Authorization fails
- Tool is not available

The SDK handles these errors automatically and provides clear error messages.

---

## Examples

Complete working examples are available in:

- **FileSearch**: `examples/providers/xai/file-search/main.go`
- **MCP Server**: `examples/providers/xai/mcp-server/main.go`

### Running Examples

#### FileSearch Example

```bash
export XAI_API_KEY="your-api-key"
export XAI_VECTOR_STORE_ID="vs_abc123"
cd examples/providers/xai/file-search
go run main.go
```

#### MCP Server Example

```bash
export XAI_API_KEY="your-api-key"
export MCP_SERVER_URL="https://api.example.com/mcp"
export MCP_AUTHORIZATION="Bearer your-token"  # Optional
cd examples/providers/xai/mcp-server
go run main.go
```

---

## Best Practices

### FileSearch

1. **Vector Store Setup**: Ensure vector stores are created and populated before use
2. **Result Limits**: Set appropriate MaxNumResults based on your use case
3. **Multiple Stores**: Search across multiple stores for broader coverage
4. **Query Design**: Craft clear, specific queries for better results

### MCPServer

1. **Tool Filtering**: Use AllowedTools to restrict available tools
2. **Authentication**: Secure MCP servers with proper authorization
3. **Headers**: Use custom headers for versioning and metadata
4. **Error Handling**: Handle MCP server errors gracefully
5. **Testing**: Test MCP integration thoroughly before production

---

## Troubleshooting

### Common Issues

**FileSearch returns no results**
- Verify vector store IDs are correct
- Check that vector stores contain relevant data
- Try increasing MaxNumResults
- Review query formulation

**MCP Server connection fails**
- Verify server URL is correct and accessible
- Check authorization credentials
- Ensure server implements MCP protocol correctly
- Review server logs for errors

**Tool not being called**
- Verify tool is included in Tools array
- Check ToolChoice setting (use AutoToolChoice for optional)
- Review model's decision-making context
- Ensure tool description is clear

---

## Related Documentation

- [xAI API Documentation](https://x.ai/api/docs)
- [Model Context Protocol (MCP)](https://modelcontextprotocol.io)
- [Deferrable Tools](../../../docs/DEFERRABLE_TOOLS.md)
- [Tool Usage Guide](../../../docs/TOOLS.md)

---

## API Reference

### FileSearch

```go
func FileSearch(config FileSearchConfig) types.Tool
```

Creates a provider-executed tool for searching vector stores.

**Parameters:**
- `config FileSearchConfig` - Configuration for the tool

**Returns:**
- `types.Tool` - Provider-executed tool with ProviderExecuted=true

---

### FileSearchWithDefaults

```go
func FileSearchWithDefaults(vectorStoreIDs []string) types.Tool
```

Creates a FileSearch tool with default settings (MaxNumResults=10).

**Parameters:**
- `vectorStoreIDs []string` - Vector store IDs to search

**Returns:**
- `types.Tool` - Provider-executed tool

---

### MCPServer

```go
func MCPServer(config MCPServerConfig) types.Tool
```

Creates a provider-executed tool for MCP server integration.

**Parameters:**
- `config MCPServerConfig` - Configuration for the tool

**Returns:**
- `types.Tool` - Provider-executed tool with ProviderExecuted=true

---

### MCPServerSimple

```go
func MCPServerSimple(serverURL string) types.Tool
```

Creates an MCP Server tool with minimal configuration.

**Parameters:**
- `serverURL string` - URL of the MCP server

**Returns:**
- `types.Tool` - Provider-executed tool

---

### MCPServerWithAuth

```go
func MCPServerWithAuth(serverURL, authorization string) types.Tool
```

Creates an MCP Server tool with authentication.

**Parameters:**
- `serverURL string` - URL of the MCP server
- `authorization string` - Authorization token

**Returns:**
- `types.Tool` - Provider-executed tool

---

## Version History

- **v6.0.61**: Initial implementation of FileSearch and MCPServer tools
- Aligned with TypeScript AI SDK v3.0.40

---

## License

MIT License - See LICENSE file for details
