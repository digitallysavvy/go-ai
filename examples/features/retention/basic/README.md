# Retention Settings - Basic Example

This example demonstrates how to use retention settings to control what data is retained from LLM requests/responses, reducing memory consumption.

## Overview

Retention settings allow you to exclude request and/or response bodies from results while maintaining all other metadata (usage, finish reason, etc.). This is particularly useful for:

- **Memory optimization**: Reduce memory consumption by 50-80% for image-heavy workloads
- **Privacy**: Avoid storing sensitive request/response data
- **Performance**: Lower memory pressure leads to better garbage collection

## Running the Example

```bash
export OPENAI_API_KEY=your-api-key
go run main.go
```

## What This Example Demonstrates

1. **Default behavior**: By default, all data is retained (backwards compatible)
2. **Full optimization**: Exclude both request and response bodies
3. **Partial optimization**: Exclude only the request body

## Key Concepts

### RetentionSettings

```go
retention := &types.RetentionSettings{
    RequestBody:  types.BoolPtr(false),  // Don't retain request
    ResponseBody: types.BoolPtr(false),  // Don't retain response
}
```

- `nil` = default (retain everything)
- `false` = exclude from result
- `true` = explicitly retain (same as nil)

### When to Use Retention Settings

- **Image processing**: When sending images in prompts
- **Large contexts**: When using large documents or code
- **Privacy-sensitive**: When handling confidential information
- **Long-running processes**: To reduce cumulative memory usage

### What's Still Available

Even with retention disabled, you still get:
- Generated text
- Token usage information
- Finish reason
- Tool calls and results
- Warnings
- All other metadata

## Memory Impact

For a typical image-based prompt:
- **With retention** (default): ~2-5 MB per request
- **Without retention**: ~0.1-0.5 MB per request
- **Savings**: 50-80% memory reduction

## Best Practices

1. **Enable by default in production** for memory-intensive apps
2. **Keep retention enabled in development** for debugging
3. **Use partial retention** if you need to debug requests but not responses
4. **Monitor memory usage** with runtime.MemStats to verify savings

## Related Documentation

- [Memory Optimization Guide](../../../../docs/06-advanced/11-memory-optimization.md)
- [Retention Settings API Reference](../../../../docs/07-reference/ai/retention-settings.md)
