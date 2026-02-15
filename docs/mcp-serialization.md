# MCP Tool Serialization

The Model Context Protocol (MCP) allows servers to expose tools that can be used by AI models. The Go-AI SDK provides methods to retrieve tool definitions in a format that can be stored, transmitted, or cached.

## GetSerializableTools()

The `GetSerializableTools()` method returns tool definitions from an MCP server in a JSON-serializable format with full pagination support.

### Method Signature

```go
func (c *MCPClient) GetSerializableTools(ctx context.Context) (*ListToolsResult, error)
```

### Return Type

```go
type ListToolsResult struct {
    Tools      []MCPTool `json:"tools"`
    NextCursor string    `json:"nextCursor,omitempty"`
}

type MCPTool struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    InputSchema map[string]interface{} `json:"inputSchema"`
}
```

## Basic Usage

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/mcp"
)

func main() {
    // Create MCP client
    transport := mcp.NewStdioTransport(mcp.StdioConfig{
        Command: "npx",
        Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
    })

    client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{
        ClientName:    "my-app",
        ClientVersion: "1.0.0",
    })

    // Connect to server
    ctx := context.Background()
    if err := client.Connect(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Get serializable tools
    tools, err := client.GetSerializableTools(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Tools can now be serialized and stored
    data, _ := json.MarshalIndent(tools, "", "  ")
    fmt.Printf("Serialized tools:\n%s\n", string(data))
}
```

## Storing Tool Definitions

Tool definitions can be cached to avoid repeated server queries:

```go
import (
    "encoding/json"
    "os"
)

// Cache tools to file
func cacheTools(tools *mcp.ListToolsResult, filename string) error {
    data, err := json.Marshal(tools)
    if err != nil {
        return err
    }
    return os.WriteFile(filename, data, 0644)
}

// Load tools from cache
func loadCachedTools(filename string) (*mcp.ListToolsResult, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, err
    }

    var tools mcp.ListToolsResult
    if err := json.Unmarshal(data, &tools); err != nil {
        return nil, err
    }

    return &tools, nil
}

// Usage
tools, err := client.GetSerializableTools(ctx)
if err != nil {
    log.Fatal(err)
}

// Cache for later use
if err := cacheTools(tools, "mcp-tools-cache.json"); err != nil {
    log.Printf("Warning: Failed to cache tools: %v", err)
}
```

## Transmitting Tool Definitions

Tool definitions can be sent over the network:

```go
import (
    "bytes"
    "encoding/json"
    "net/http"
)

// Send tools to remote service
func sendToolsToService(tools *mcp.ListToolsResult, url string) error {
    data, err := json.Marshal(tools)
    if err != nil {
        return err
    }

    resp, err := http.Post(url, "application/json", bytes.NewReader(data))
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return nil
}

// Usage
tools, err := client.GetSerializableTools(ctx)
if err != nil {
    log.Fatal(err)
}

err = sendToolsToService(tools, "https://api.example.com/mcp-tools")
if err != nil {
    log.Printf("Failed to send tools: %v", err)
}
```

## Pagination Support

When MCP servers have many tools, results may be paginated:

```go
func getAllTools(client *mcp.MCPClient, ctx context.Context) ([]mcp.MCPTool, error) {
    var allTools []mcp.MCPTool

    // Get first page
    result, err := client.GetSerializableTools(ctx)
    if err != nil {
        return nil, err
    }

    allTools = append(allTools, result.Tools...)

    // Handle pagination if present
    // Note: Current implementation doesn't support cursor-based pagination yet
    // This is a placeholder for future enhancement
    if result.NextCursor != "" {
        log.Printf("More tools available (cursor: %s)", result.NextCursor)
        // TODO: Implement pagination when available in SDK
    }

    return allTools, nil
}
```

## Comparison: ListTools vs GetSerializableTools

The SDK provides two methods for retrieving tools:

| Method | Return Type | Use Case |
|--------|-------------|----------|
| `ListTools()` | `[]MCPTool` | Convenience method for immediate use |
| `GetSerializableTools()` | `*ListToolsResult` | Full result with pagination support for storage/transmission |

```go
// Convenience method - just the tools
tools, err := client.ListTools(ctx)
// Returns: []MCPTool

// Serialization method - complete result
result, err := client.GetSerializableTools(ctx)
// Returns: *ListToolsResult with Tools + NextCursor
```

## Use Cases

### 1. Tool Registry Service

Build a centralized registry of available tools:

```go
type ToolRegistry struct {
    tools map[string]*mcp.ListToolsResult
}

func (r *ToolRegistry) RegisterServer(serverName string, tools *mcp.ListToolsResult) {
    r.tools[serverName] = tools
}

func (r *ToolRegistry) GetAllTools() []*mcp.ListToolsResult {
    var all []*mcp.ListToolsResult
    for _, tools := range r.tools {
        all = append(all, tools)
    }
    return all
}
```

### 2. Offline Tool Discovery

Cache tools for offline browsing:

```go
func discoverAndCache(servers []string) error {
    for _, server := range servers {
        client := connectToServer(server)
        tools, err := client.GetSerializableTools(ctx)
        if err != nil {
            log.Printf("Failed to get tools from %s: %v", server, err)
            continue
        }

        filename := fmt.Sprintf("tools-%s.json", server)
        if err := cacheTools(tools, filename); err != nil {
            log.Printf("Failed to cache tools for %s: %v", server, err)
        }
    }
    return nil
}
```

### 3. Tool Versioning

Track tool changes over time:

```go
type ToolSnapshot struct {
    Timestamp time.Time              `json:"timestamp"`
    Tools     *mcp.ListToolsResult   `json:"tools"`
}

func snapshotTools(client *mcp.MCPClient) (*ToolSnapshot, error) {
    tools, err := client.GetSerializableTools(context.Background())
    if err != nil {
        return nil, err
    }

    return &ToolSnapshot{
        Timestamp: time.Now(),
        Tools:     tools,
    }, nil
}
```

## Best Practices

1. **Cache tool definitions**: Avoid repeated queries by caching results
2. **Handle pagination**: Check for `NextCursor` and implement pagination when needed
3. **Validate after deserialization**: Ensure tools loaded from cache are still valid
4. **Version your cache**: Include timestamps or version numbers in cached data
5. **Implement TTL**: Refresh cached tools periodically to catch updates

## Error Handling

```go
tools, err := client.GetSerializableTools(ctx)
if err != nil {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        log.Fatal("Timeout getting tools")
    case errors.Is(err, mcp.ErrNotInitialized):
        log.Fatal("Client not initialized")
    default:
        log.Fatalf("Failed to get tools: %v", err)
    }
}
```

## See Also

- [MCP Integration Guide](./mcp-integration.md)
- [MCP Client API](./mcp-client.md)
- [Tool Execution](./tool-execution.md)
