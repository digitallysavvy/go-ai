# Deferrable Tools Example

This example demonstrates how to use provider-executed (deferrable) tools in the Go-AI SDK.

## What Are Deferrable Tools?

Deferrable tools are tools that are executed by the LLM provider (like Anthropic or xAI) rather than locally by your application. These tools provide capabilities that are integrated directly into the provider's infrastructure.

## Features Demonstrated

1. **Provider-Executed Tool**: Using Anthropic's `tool-search-bm25` to search for tools
2. **Mixed Tools**: Combining local tools (executed in your app) with provider-executed tools
3. **Error Handling**: Gracefully handling errors from provider-executed tools

## Prerequisites

- Go 1.21 or later
- Anthropic API key

## Setup

1. Get an API key from [Anthropic](https://console.anthropic.com/)

2. Set your API key as an environment variable:
   ```bash
   export ANTHROPIC_API_KEY=your_api_key_here
   ```

## Running the Example

```bash
cd examples/deferrable-tools
go run main.go
```

## Expected Output

The example will run three demonstrations:

### Example 1: Provider-Executed Tool
```
=== Example 1: Provider-Executed Tool (tool-search-bm25) ===
Response: I found several weather-related tools...
Steps: 2
Tool Results: 1

Tool: tool-search-bm25
  Provider-Executed: true
  Result: [search results]
```

### Example 2: Mixed Local and Provider Tools
```
=== Example 2: Mixed Local and Provider Tools ===
  [LOCAL] Executing get_weather for: San Francisco
Response: The weather in San Francisco is currently...
Tool Execution Summary:
  ✓ get_weather (local)
  ✓ web-search (provider-executed)

Total: 1 local, 1 provider-executed
```

### Example 3: Error Handling
```
=== Example 3: Error Handling ===
Response: I attempted to fetch the URL but encountered an error...
Tool Results:
Tool: web-fetch
  ⚠️  Error: [error details]
```

## Key Concepts

### Provider-Executed Tools

These tools are executed by the provider:
- **Anthropic**: tool-search-bm25, tool-search-regex, web-search, web-fetch, code-execution
- **xAI**: file-search, mcp-server

```go
toolSearch := types.Tool{
    Name:        "tool-search-bm25",
    Description: "Search for tools",
    Parameters:  /* schema */,
    // Note: No Execute function needed!
}
```

### Local Tools

These tools are executed in your application:

```go
weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get weather",
    Parameters:  /* schema */,
    Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
        // Your implementation
        return result, nil
    },
}
```

### Checking Tool Execution Type

```go
for _, tr := range result.ToolResults {
    if tr.ProviderExecuted {
        fmt.Println("Executed by provider")
    } else {
        fmt.Println("Executed locally")
    }
}
```

## Code Structure

- `runToolSearchExample()`: Demonstrates a simple provider-executed tool
- `runMixedToolsExample()`: Shows how to use both local and provider tools together
- `runErrorHandlingExample()`: Demonstrates error handling for provider tools

## Learn More

- [Deferrable Tools Documentation](../../docs/DEFERRABLE_TOOLS.md)
- [Anthropic Tools Documentation](https://docs.anthropic.com/en/docs/build-with-claude/tool-use)
- [TypeScript AI SDK](https://sdk.vercel.ai/docs/ai-sdk-core/tools-and-tool-calling)

## Troubleshooting

**Tool not being executed?**
- Ensure the tool name matches exactly (case-sensitive)
- Check that you're using a supported provider (Anthropic, xAI, etc.)

**Missing API key error?**
- Verify `ANTHROPIC_API_KEY` is set: `echo $ANTHROPIC_API_KEY`
- Make sure the API key is valid and has sufficient credits

**Timeout errors?**
- Provider-executed tools may take longer than local tools
- Consider increasing timeout settings if needed
