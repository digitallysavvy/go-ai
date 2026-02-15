# Tool Reference (Anthropic)

Tool references are an Anthropic-specific feature that allows you to reference previously defined tools without sending the full tool definition again. This reduces token usage and improves performance in conversations where tools have already been defined.

## Overview

When using features like tool search or tool discovery, instead of returning full tool definitions (which can be hundreds of tokens each), you can return tool references that simply point to tools by name. Claude will use the tools that were already defined earlier in the conversation.

## Benefits

- **Reduced token usage**: Reference a tool with ~10 tokens instead of 100+ tokens for a full definition
- **Better performance**: Faster API calls with smaller payloads
- **Cleaner responses**: Tool search results are more concise and easier to read

## Basic Usage

### Creating a Tool Reference

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"

// Create a tool reference
ref := anthropic.ToolReference("calculator")
```

### Using in Tool Results

```go
func toolSearchExecute(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
    query := input["query"].(string)

    // Search for matching tools
    matchingTools := searchTools(query) // Returns []string of tool names

    // Build result with tool references
    blocks := []types.ToolResultContentBlock{
        types.TextContentBlock{Text: fmt.Sprintf("Found %d tools:", len(matchingTools))},
    }

    for _, toolName := range matchingTools {
        blocks = append(blocks, anthropic.ToolReference(toolName))
    }

    return types.ContentResult(opts.ToolCallID, "search_tools", blocks...), nil
}
```

## Complete Example

Here's a complete example of a tool search system using tool references:

```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/client"
)

// Define your actual tools
func mathTools() []types.Tool {
    return []types.Tool{
        {
            Name:        "add",
            Description: "Add two numbers",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "a": map[string]interface{}{"type": "number"},
                    "b": map[string]interface{}{"type": "number"},
                },
            },
            Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
                a := input["a"].(float64)
                b := input["b"].(float64)
                return types.SimpleTextResult(opts.ToolCallID, "add", fmt.Sprintf("%.2f", a+b)), nil
            },
        },
        {
            Name:        "multiply",
            Description: "Multiply two numbers",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "a": map[string]interface{}{"type": "number"},
                    "b": map[string]interface{}{"type": "number"},
                },
            },
            Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
                a := input["a"].(float64)
                b := input["b"].(float64)
                return types.SimpleTextResult(opts.ToolCallID, "multiply", fmt.Sprintf("%.2f", a*b)), nil
            },
        },
        {
            Name:        "divide",
            Description: "Divide two numbers",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "a": map[string]interface{}{"type": "number"},
                    "b": map[string]interface{}{"type": "number"},
                },
            },
            Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
                a := input["a"].(float64)
                b := input["b"].(float64)
                if b == 0 {
                    return types.ErrorResult(opts.ToolCallID, "divide", "Division by zero"), nil
                }
                return types.SimpleTextResult(opts.ToolCallID, "divide", fmt.Sprintf("%.2f", a/b)), nil
            },
        },
    }
}

// Tool search tool that returns references
func searchToolsTool(availableTools []types.Tool) types.Tool {
    return types.Tool{
        Name:        "search_tools",
        Description: "Search for available tools by keyword",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "query": map[string]interface{}{
                    "type":        "string",
                    "description": "Search query (e.g., 'math', 'calculation')",
                },
            },
            "required": []string{"query"},
        },
        Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
            query := strings.ToLower(input["query"].(string))

            // Search for tools matching the query
            var matchingTools []string
            for _, tool := range availableTools {
                if strings.Contains(strings.ToLower(tool.Name), query) ||
                    strings.Contains(strings.ToLower(tool.Description), query) {
                    matchingTools = append(matchingTools, tool.Name)
                }
            }

            if len(matchingTools) == 0 {
                return types.ContentResult(opts.ToolCallID, "search_tools",
                    types.TextContentBlock{Text: "No tools found matching your query."},
                ), nil
            }

            // Build result with tool references
            blocks := []types.ToolResultContentBlock{
                types.TextContentBlock{
                    Text: fmt.Sprintf("Found %d tool(s) matching '%s':", len(matchingTools), input["query"]),
                },
            }

            // Add a tool reference for each matching tool
            for _, toolName := range matchingTools {
                blocks = append(blocks, anthropic.ToolReference(toolName))
            }

            return types.ContentResult(opts.ToolCallID, "search_tools", blocks...), nil
        },
    }
}

func main() {
    ctx := context.Background()

    // Create Anthropic provider
    provider := anthropic.New(anthropic.Config{
        APIKey: "your-api-key",
    })

    // Create model
    model := provider.LanguageModel("claude-3-5-sonnet-20241022", nil)

    // Define all available tools (including search)
    mathToolsList := mathTools()
    allTools := append(mathToolsList, searchToolsTool(mathToolsList))

    // Example conversation
    messages := []types.Message{
        {
            Role: types.RoleUser,
            Content: []types.ContentPart{
                types.TextContent{Text: "Search for math tools"},
            },
        },
    }

    // Generate response
    result, err := model.DoGenerate(ctx, &client.GenerateOptions{
        Messages: messages,
        Tools:    allTools,
    })

    if err != nil {
        panic(err)
    }

    fmt.Printf("Response: %+v\n", result)

    // Claude may call search_tools and see tool references
    // Then it can call the actual tools (add, multiply, divide) directly
}
```

## Inspecting Tool References

You can detect and extract tool references from results:

### Check if a Block is a Tool Reference

```go
if customBlock, ok := block.(types.CustomContentBlock); ok {
    if toolName, isRef := anthropic.IsToolReference(customBlock); isRef {
        fmt.Printf("Found tool reference: %s\n", toolName)
    }
}
```

### Extract All Tool References from a Result

```go
result := types.ContentResult("call_123", "search_tools",
    types.TextContentBlock{Text: "Found tools:"},
    anthropic.ToolReference("add"),
    anthropic.ToolReference("multiply"),
)

toolNames := anthropic.ExtractToolReferences(result)
// toolNames = []string{"add", "multiply"}
```

## How It Works

### Without Tool References

```json
{
  "role": "tool",
  "content": "Found tools: add (adds two numbers), multiply (multiplies two numbers), divide (divides two numbers)"
}
```

Token count: ~30 tokens (plus more for full definitions if inline)

### With Tool References

```json
{
  "role": "tool",
  "content": [
    {"type": "text", "text": "Found 3 tools:"},
    {"type": "tool_reference", "tool_name": "add"},
    {"type": "tool_reference", "tool_name": "multiply"},
    {"type": "tool_reference", "tool_name": "divide"}
  ]
}
```

Token count: ~15 tokens (much more efficient!)

## Requirements

1. **Tools must be pre-defined**: The referenced tool must have been included in the `tools` parameter earlier in the conversation.

2. **Tool names must match exactly**: The tool name in the reference must exactly match the name of a defined tool.

3. **Anthropic only**: This is an Anthropic-specific feature and only works with Claude models via the Anthropic API.

## Use Cases

### 1. Tool Discovery

Allow the model to search through available tools and suggest relevant ones:

```go
searchToolsTool() // Returns tool references for matching tools
```

### 2. Tool Recommendations

Create a tool that recommends other tools based on the task:

```go
recommendToolsTool() // Suggests tools by returning references
```

### 3. Dynamic Tool Loading

Load tools dynamically based on context and return references to available tools:

```go
loadPluginTools() // Returns references to dynamically loaded tools
```

## Best Practices

1. **Use for search/discovery**: Tool references are ideal for tool search and discovery features.

2. **Include context**: Add a text block before tool references explaining what they are.

3. **Check tool availability**: Ensure referenced tools are actually defined in the conversation.

4. **Fallback handling**: Have a fallback for when tool references aren't supported (e.g., return tool descriptions as text).

## Limitations

- **Anthropic only**: Other providers don't support tool references
- **Pre-defined tools only**: Can only reference tools that were already provided
- **No cross-conversation**: References don't work across separate API calls

## See Also

- [Tool Result Content Arrays Guide](./tool-result-content-arrays.md) - General guide on content blocks
- [Anthropic Advanced Features](../providers/anthropic-advanced-features.md) - Other Anthropic-specific features
- [Tools Guide](./tools.md) - General tool development guide
