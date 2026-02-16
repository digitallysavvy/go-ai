# Timeout Configuration Example

This example demonstrates how to use the enhanced timeout system in the Go-AI SDK.

## Features Demonstrated

1. **Total Timeout** - Set an overall timeout for the entire operation
2. **Per-Step Timeout** - Set a timeout for each generation step (useful for multi-step operations with tools)
3. **Per-Chunk Timeout** - Set a timeout for receiving each chunk in streaming operations
4. **Combined Timeouts** - Use multiple timeout types together
5. **Builder Pattern** - Use the builder pattern for creating timeout configurations

## Timeout Types

### Total Timeout
Sets an overall timeout for the entire operation. If the operation exceeds this duration, it will fail.

```go
total := 30 * time.Second
timeout := &ai.TimeoutConfig{
    Total: &total,
}
```

### Per-Step Timeout
Sets a timeout for each generation step. Particularly useful for:
- Multi-step agent operations
- Tool calling loops
- Operations that iterate multiple times

```go
perStep := 10 * time.Second
timeout := &ai.TimeoutConfig{
    PerStep: &perStep,
}
```

### Per-Chunk Timeout
Sets a timeout for receiving each chunk in streaming operations. Helps detect:
- Stalled streams
- Network issues
- Slow responses

```go
perChunk := 5 * time.Second
timeout := &ai.TimeoutConfig{
    PerChunk: &perChunk,
}
```

## Builder Pattern

The timeout configuration supports a fluent builder pattern:

```go
timeout := (&ai.TimeoutConfig{}).
    WithTotal(30 * time.Second).
    WithPerStep(10 * time.Second).
    WithPerChunk(5 * time.Second)
```

## Running the Example

1. Set your OpenAI API key:
```bash
export OPENAI_API_KEY="your-api-key-here"
```

2. Run the example:
```bash
go run main.go
```

## Key Concepts

- **Timeout Priority**: When multiple timeouts are set, they all apply. The operation will fail if ANY timeout is exceeded.
- **Context Cancellation**: Timeouts work by creating context.WithTimeout contexts, so they respect Go's standard cancellation patterns.
- **Error Handling**: When a timeout occurs, you'll receive a `context.DeadlineExceeded` error.

## Use Cases

1. **Production Reliability**: Set reasonable timeouts to prevent operations from hanging indefinitely
2. **Cost Control**: Limit how long expensive operations can run
3. **User Experience**: Ensure responsive applications by setting appropriate timeouts
4. **Debug Streaming**: Use per-chunk timeouts to detect slow or stalled streams early
