# MCP (Model Context Protocol) Package

This package provides a Go implementation of the Model Context Protocol (MCP) client for the Go-AI SDK.

## Overview

The MCP package allows you to connect to MCP servers and use their tools, resources, and prompts in your AI applications. MCP servers can expose various capabilities including:

- **Tools**: Functions that can be called by AI models
- **Resources**: Data sources that can be read
- **Prompts**: Reusable prompt templates

## Image Content Support

**Important:** This package properly handles image content from MCP tools to prevent token explosions.

### The Problem

When MCP servers return images in tool results, the raw base64 data could be treated as text, causing:
- Massive token usage (200K+ tokens for a single image)
- Context window overflow errors
- Expensive API calls
- Failed tool executions

### The Solution

The `ConvertMCPContentToAISDK` function properly converts MCP image content to the AI SDK's native image format:

```go
// MCP returns image as:
{
  "type": "image",
  "data": "iVBORw0KGgo...", // base64 data
  "mimeType": "image/png"
}

// Converted to AI SDK ImageContent:
types.ImageContent{
  Image: []byte{...},      // decoded bytes
  MimeType: "image/png",
}
```

This prevents the base64 string from being treated as text, reducing token usage from 200K+ to ~1K tokens per image.

### Supported Image Formats

The conversion supports all standard image data formats:

1. **Base64 data**: Raw base64-encoded image data
   ```go
   Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB..."
   ```

2. **Data URLs**: Complete data URL with MIME type
   ```go
   Data: "data:image/png;base64,iVBORw0KGgo..."
   ```

3. **HTTP/HTTPS URLs**: Remote image URLs
   ```go
   Data: "https://example.com/image.png"
   ```

## Usage

### Basic MCP Client Setup

```go
import (
    "context"
    "github.com/digitallysavvy/go-ai/pkg/mcp"
)

// Create stdio transport for local MCP server
transport := mcp.NewStdioTransport(mcp.StdioTransportConfig{
    Command: "node",
    Args:    []string{"server.mjs"},
})

// Create client
client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{
    ClientName:    "my-app",
    ClientVersion: "1.0.0",
})

// Connect
ctx := context.Background()
err := client.Connect(ctx)
if err != nil {
    panic(err)
}
defer client.Close()
```

### Using MCP Tools

```go
// Get tools from MCP server
converter := mcp.NewMCPToolConverter(client)
tools, err := converter.ConvertToGoAITools(ctx)
if err != nil {
    panic(err)
}

// Use tools with AI model
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  openai.GPT4Turbo(),
    Prompt: "Generate a chart of sales data",
    Tools:  tools, // MCP tools that may return images
})
```

### Handling Tool Results with Images

The package automatically converts image results:

```go
// MCP server returns tool result with image
toolResult := &mcp.CallToolResult{
    Content: []mcp.ToolResultContent{
        {
            Type: "text",
            Text: "Here is your chart:",
        },
        {
            Type:     "image",
            Data:     "iVBORw0KGgo...", // base64 PNG
            MimeType: "image/png",
        },
    },
}

// Automatically converted to AI SDK content parts
// Images are properly handled as bytes, not text
// Token usage: ~1K tokens instead of 200K+
```

## Migration Guide

This fix is **fully backward compatible**. No code changes are required:

- Text-only tool results work exactly as before
- Mixed text/image content is automatically handled
- No breaking API changes

Existing applications will automatically benefit from the fix with no migration needed.

## Performance Impact

### Token Savings

- **Before fix**: 200K+ tokens per image (treated as text)
- **After fix**: ~1K tokens per image (native image content)
- **Typical savings**: 100K-200K tokens per image

### Example Cost Savings

For GPT-4 Turbo:
- Input tokens: $0.01 per 1K tokens
- Before: 200K tokens × $0.01 / 1K = $2.00 per image
- After: 1K tokens × $0.01 / 1K = $0.01 per image
- **Savings: $1.99 per image (~99% cost reduction)**

## Transport Types

### Stdio Transport

For local MCP servers running as child processes:

```go
transport := mcp.NewStdioTransport(mcp.StdioTransportConfig{
    Command: "node",
    Args:    []string{"server.mjs"},
})
```

### HTTP Transport

For remote MCP servers over HTTP:

```go
transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
    URL:       "https://mcp.example.com",
    TimeoutMS: 30000,
})
```

### HTTP with OAuth

For authenticated MCP servers:

```go
transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
    URL:       "https://mcp.example.com",
    TimeoutMS: 30000,
    OAuth: &mcp.OAuthConfig{
        ClientID:     "your-client-id",
        ClientSecret: "your-client-secret",
        TokenURL:     "https://auth.example.com/token",
    },
})
```

## Testing

The package includes comprehensive tests for image content conversion:

```bash
go test ./pkg/mcp/... -v
```

Test coverage includes:
- Base64 image conversion
- Data URL image conversion
- HTTP/HTTPS URL handling
- Mixed text/image content
- Error handling for invalid data
- Token explosion prevention

## Known Scenarios

This fix specifically addresses MCP tools that return:
- Screenshots
- Generated charts and graphs
- Any image content in tool results
- Mixed text and image responses

## References

- [MCP Specification](https://spec.modelcontextprotocol.io/)
- [Go-AI SDK Documentation](https://github.com/digitallysavvy/go-ai)

## Version History

### v1.1.0 (2026-02-02)
- **Fixed**: Critical bug where MCP image content caused 200K+ token explosions
- **Added**: Proper image content conversion to prevent token waste
- **Added**: Support for all image formats (base64, data URLs, HTTP URLs)
- **Added**: Comprehensive test coverage for image handling
- Token usage for images reduced from 200K+ to ~1K tokens
