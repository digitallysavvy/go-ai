# Tool Result Content Arrays

This guide explains how to use the new content array feature for tool results, which allows you to return rich, structured content from tools including text, images, files, and provider-specific content like tool references.

## Overview

The content array feature provides a more flexible way to return tool results compared to simple string or JSON values. It enables:

- **Multiple content blocks** in a single tool result
- **Mixed content types** (text, images, files)
- **Provider-specific features** like Anthropic's tool-reference
- **Backward compatibility** with existing simple results

## Basic Usage

### Simple Text Result (Old Style - Still Supported)

```go
// Old style - still works for simple text results
result := types.SimpleTextResult("call_123", "search", "Found 3 results")
```

### Content Blocks (New Style - Recommended)

```go
// New style - supports rich content
result := types.ContentResult("call_123", "search",
    types.TextContentBlock{Text: "Search results:"},
    types.TextContentBlock{Text: "Result 1: ..."},
    types.TextContentBlock{Text: "Result 2: ..."},
)
```

## Content Block Types

### TextContentBlock

Simple text content:

```go
types.TextContentBlock{
    Text: "This is a text block",
}
```

### ImageContentBlock

Images with base64 data:

```go
imageData := []byte{...} // Image bytes
types.ImageContentBlock{
    Data:      imageData,
    MediaType: "image/png",
}
```

### FileContentBlock

Files like PDFs or documents:

```go
pdfData := []byte{...} // PDF bytes
types.FileContentBlock{
    Data:      pdfData,
    MediaType: "application/pdf",
    Filename:  "report.pdf",
}
```

### CustomContentBlock

Provider-specific content (like tool-reference for Anthropic):

```go
// Using Anthropic's ToolReference helper
anthropic.ToolReference("calculator")

// Or manually:
types.CustomContentBlock{
    ProviderOptions: map[string]interface{}{
        "anthropic": map[string]interface{}{
            "type":     "tool-reference",
            "toolName": "calculator",
        },
    },
}
```

## Complete Examples

### Example 1: Simple Text Blocks

```go
package main

import (
    "context"
    "fmt"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func searchTool() types.Tool {
    return types.Tool{
        Name:        "search",
        Description: "Search for information",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "query": map[string]interface{}{
                    "type":        "string",
                    "description": "Search query",
                },
            },
            "required": []string{"query"},
        },
        Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
            query := input["query"].(string)

            // Return result using content blocks
            return types.ContentResult(opts.ToolCallID, "search",
                types.TextContentBlock{Text: fmt.Sprintf("Searching for: %s", query)},
                types.TextContentBlock{Text: "Found 3 results:"},
                types.TextContentBlock{Text: "1. First result"},
                types.TextContentBlock{Text: "2. Second result"},
                types.TextContentBlock{Text: "3. Third result"},
            ), nil
        },
    }
}
```

### Example 2: Mixed Content (Text + Images)

```go
func analyzeTool() types.Tool {
    return types.Tool{
        Name:        "analyze_image",
        Description: "Analyze an image and return results",
        Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
            // Perform analysis...
            chartData := generateChart() // Returns []byte

            return types.ContentResult(opts.ToolCallID, "analyze_image",
                types.TextContentBlock{Text: "Analysis complete:"},
                types.TextContentBlock{Text: "Found 5 objects in the image"},
                types.ImageContentBlock{
                    Data:      chartData,
                    MediaType: "image/png",
                },
                types.TextContentBlock{Text: "Confidence: 95%"},
            ), nil
        },
    }
}
```

### Example 3: Tool References (Anthropic)

```go
func toolSearchTool() types.Tool {
    return types.Tool{
        Name:        "search_tools",
        Description: "Search for available tools",
        Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
            query := input["query"].(string)

            // Search for tools matching the query
            matchingTools := []string{"add", "subtract", "multiply", "divide"}

            // Build content blocks with tool references
            blocks := []types.ToolResultContentBlock{
                types.TextContentBlock{Text: fmt.Sprintf("Found %d math tools:", len(matchingTools))},
            }

            // Add tool reference for each matching tool
            for _, toolName := range matchingTools {
                blocks = append(blocks, anthropic.ToolReference(toolName))
            }

            return types.ContentResult(opts.ToolCallID, "search_tools", blocks...), nil
        },
    }
}
```

## Error Handling

Use `ErrorResult` for tool execution errors:

```go
func riskyTool() types.Tool {
    return types.Tool{
        Name: "risky_operation",
        Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
            if err := performOperation(); err != nil {
                return types.ErrorResult(opts.ToolCallID, "risky_operation", err.Error()), nil
            }
            return types.ContentResult(opts.ToolCallID, "risky_operation",
                types.TextContentBlock{Text: "Operation completed successfully"},
            ), nil
        },
    }
}
```

## Migration from Old Style

### Before (Old Style)

```go
Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
    result := "Found 3 results"
    return types.SimpleTextResult(opts.ToolCallID, "search", result), nil
}
```

### After (New Style)

```go
Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
    return types.ContentResult(opts.ToolCallID, "search",
        types.TextContentBlock{Text: "Found 3 results"},
    ), nil
}
```

## Provider Compatibility

| Feature | Anthropic | OpenAI | Google |
|---------|-----------|--------|--------|
| Text blocks | ✅ | ✅ | ✅ |
| Image blocks | ✅ | ✅ | ✅ |
| File blocks | ✅ | ❌ | ❌ |
| Tool references | ✅ | ❌ | ❌ |

- **Anthropic**: Full support for all content block types
- **OpenAI**: Converts to text representation (images shown as base64)
- **Google**: Converts to text representation

## Best Practices

1. **Use content blocks for rich results**: When your tool returns multiple pieces of information or mixed content types, use content blocks instead of concatenating strings.

2. **Keep text blocks focused**: Each text block should contain one logical piece of information rather than a large wall of text.

3. **Include context**: When returning images or files, include a text block before them explaining what they contain.

4. **Error handling**: Use `ErrorResult()` for errors rather than returning error text as content.

5. **Backward compatibility**: Old-style results (`SimpleTextResult`, `SimpleJSONResult`) still work if you have existing tools.

## See Also

- [Tool Reference Guide](./tool-reference.md) - Anthropic-specific tool-reference feature
- [Tool Implementation Guide](./tools.md) - General tool development guide
- [Anthropic Provider Documentation](../providers/anthropic.md) - Anthropic-specific features
