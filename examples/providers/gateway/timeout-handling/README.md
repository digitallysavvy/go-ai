# Gateway Timeout Handling Example

This example demonstrates proper timeout handling when using the AI Gateway provider.

## Overview

The Gateway SDK provides helpful timeout error detection and troubleshooting guidance. This example shows:

1. How to set appropriate timeouts for different operations
2. How to detect and handle timeout errors
3. Best practices for timeout configuration
4. Troubleshooting guidance from error messages

## Prerequisites

1. Set your AI Gateway API key:
```bash
export AI_GATEWAY_API_KEY="your-api-key-here"
```

2. Ensure you have credits available in your Gateway account

## Running the Example

```bash
go run main.go
```

## Examples Included

### Example 1: Text Generation with Short Timeout
Demonstrates timeout error handling by using an intentionally short timeout (100ms) for a text generation request. Shows how the SDK detects and reports timeout errors.

### Example 2: Text Generation with Appropriate Timeout
Uses a reasonable timeout (30 seconds) for a simple text generation request, showing successful execution.

### Example 3: Video Generation with Short Timeout
Demonstrates timeout handling for video generation using an intentionally short timeout (1 second). Video generation typically takes several minutes, so this will timeout.

### Example 4: Video Generation with Appropriate Timeout
Uses a production-appropriate timeout (10 minutes) for video generation.

## Understanding Timeout Errors

The Gateway SDK automatically detects timeout errors and provides helpful guidance:

```go
if gatewayerrors.IsGatewayTimeoutError(err) {
    // This is a client-side timeout
    // The error message includes troubleshooting steps
    fmt.Printf("Timeout error: %v\n", err)
}
```

## Timeout Error Message

When a timeout occurs, you'll see a helpful error message like:

```
Gateway request timed out: context deadline exceeded

This is a client-side timeout. To resolve this:
1. Increase your context timeout: ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
2. For video generation, consider using longer timeouts (e.g., 10 minutes)
3. Check your network connectivity
4. Verify the Gateway API is accessible
```

## Recommended Timeouts

### Text Generation
- **Simple prompts**: 30-60 seconds
- **Complex generation**: 2-5 minutes
- **Streaming**: 30 seconds per chunk

### Image Generation
- **Single image**: 1-2 minutes
- **Batch generation**: 3-5 minutes
- **High resolution**: 5-10 minutes

### Video Generation
- **Short videos (2-4s)**: 5-10 minutes
- **Longer videos**: 15-30 minutes
- **Multiple videos**: 20-40 minutes

### Embeddings
- **Single embedding**: 10-30 seconds
- **Batch embeddings**: 1-5 minutes

## Best Practices

### 1. Always Use Context Timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

result, err := ai.GenerateVideo(ctx, options)
```

### 2. Check for Specific Error Types

```go
if err != nil {
    if gatewayerrors.IsGatewayTimeoutError(err) {
        // Handle timeout specifically
        log.Printf("Operation timed out: %v", err)
        // Maybe retry or notify user
    } else {
        // Handle other errors
        log.Printf("Operation failed: %v", err)
    }
}
```

### 3. Implement Retry Logic

```go
maxRetries := 3
for i := 0; i < maxRetries; i++ {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    result, err := ai.GenerateText(ctx, model, options)
    cancel()

    if err == nil {
        return result, nil
    }

    if !gatewayerrors.IsGatewayTimeoutError(err) {
        // Non-timeout error, don't retry
        return nil, err
    }

    // Timeout error, retry with longer timeout
    log.Printf("Attempt %d timed out, retrying...", i+1)
}
```

### 4. Use Async Processing for Long Operations

For operations that may take a very long time, consider:

```go
// Start video generation in a goroutine
resultChan := make(chan *ai.GenerateVideoResult, 1)
errorChan := make(chan error, 1)

go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
    defer cancel()

    result, err := ai.GenerateVideo(ctx, options)
    if err != nil {
        errorChan <- err
        return
    }
    resultChan <- result
}()

// Wait for result or timeout
select {
case result := <-resultChan:
    fmt.Println("Video generated!")
case err := <-errorChan:
    fmt.Printf("Error: %v\n", err)
case <-time.After(30 * time.Minute):
    fmt.Println("Operation still running after 30 minutes")
}
```

## Production Considerations

1. **Monitoring**: Log timeout errors to track performance issues
2. **Alerting**: Set up alerts for high timeout rates
3. **Metrics**: Track average request durations
4. **User Experience**: Show progress indicators for long operations
5. **Graceful Degradation**: Provide fallbacks when timeouts occur

## Common Timeout Causes

1. **Network Issues**: Slow or unstable connection
2. **Provider Load**: High demand on the provider's service
3. **Complex Requests**: Large models or complex prompts
4. **Resource Limits**: Gateway or provider rate limiting
5. **Configuration**: Timeout set too short for the operation

## Troubleshooting

### Frequent Timeouts?

1. Check your network connection
2. Increase timeout duration
3. Verify Gateway API status
4. Check provider availability
5. Review request complexity

### Inconsistent Timeouts?

1. Monitor network latency
2. Check provider status pages
3. Review rate limiting
4. Consider peak usage times
5. Implement retry with backoff

## Related Examples

- `/examples/providers/gateway/basic` - Basic gateway usage
- `/examples/providers/gateway/video-generation` - Video generation
- `/examples/providers/gateway/zero-retention` - Zero data retention
