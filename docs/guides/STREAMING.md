# Streaming in Go AI SDK

The Go AI SDK uses a `Next()`-based pattern for streaming responses. This provides better type safety, error handling, and follows Go idioms.

## Basic Pattern

```go
import (
    "context"
    "fmt"

    "github.com/digitallysavvy/go-ai/pkg/provider"
)

func streamExample(model provider.LanguageModel) error {
    // Start streaming
    stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
        Prompt: types.Prompt{
            Text: "Tell me a story",
        },
    })
    if err != nil {
        return err
    }
    defer stream.Close()

    // Process chunks using Next()
    for {
        chunk, err := stream.Next()
        if err != nil {
            // Check for end of stream
            if err.Error() == "EOF" {
                break
            }
            return err
        }

        switch chunk.Type {
        case provider.ChunkTypeText:
            fmt.Print(chunk.Text)
        case provider.ChunkTypeFinish:
            fmt.Printf("\nFinish reason: %s\n", chunk.FinishReason)
            fmt.Printf("Total tokens: %d\n", chunk.Usage.GetTotalTokens())
        }
    }

    return nil
}
```

## Why Next() Instead of io.Reader?

### Type Safety

**Next() provides typed chunks:**
```go
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    // chunk is typed as *provider.StreamChunk
    // No parsing or type casting needed
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}
```

**io.Reader requires manual parsing:**
```go
buf := make([]byte, 4096)
for {
    n, err := stream.Read(buf)
    // Now you need to parse buf[:n] to determine chunk type
    // Error-prone and complex
}
```

### Error Handling

**Next() provides clear error semantics:**
```go
chunk, err := stream.Next()
if err != nil {
    if err.Error() == "EOF" {
        // Stream completed successfully
        break
    }
    // Actual error occurred
    return err
}
```

**io.Reader mixes errors with data:**
```go
n, err := stream.Read(buf)
if err != nil {
    // Did we get partial data? Should we process it?
    // Error handling is ambiguous
}
```

### Tool Calls and Structured Data

**Next() handles complex types naturally:**
```go
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    switch chunk.Type {
    case provider.ChunkTypeText:
        fmt.Print(chunk.Text)
    case provider.ChunkTypeToolCall:
        result := executeTool(chunk.ToolCall)
        // Tool calls are typed structs
    case provider.ChunkTypeReasoning:
        fmt.Printf("[Thinking: %s]\n", chunk.Text)
    }
}
```

**io.Reader would require custom serialization:**
```go
// How do you represent tool calls in []byte?
// Would need JSON encoding/decoding, framing, etc.
```

## Stream Chunk Types

The SDK supports several chunk types:

```go
const (
    ChunkTypeText       ChunkType = "text"       // Text content
    ChunkTypeToolCall   ChunkType = "tool-call"  // Function/tool invocation
    ChunkTypeToolResult ChunkType = "tool-result" // Tool execution result
    ChunkTypeFinish     ChunkType = "finish"     // Stream completion
    ChunkTypeError      ChunkType = "error"      // Error occurred
    ChunkTypeReasoning  ChunkType = "reasoning"  // Reasoning/thinking content
    ChunkTypeUsage      ChunkType = "usage"      // Token usage information
)
```

## Advanced Patterns

### Accumulating Text

```go
var fullText strings.Builder

for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    if chunk.Type == provider.ChunkTypeText {
        fullText.WriteString(chunk.Text)
        fmt.Print(chunk.Text)  // Also print as it arrives
    }
}

fmt.Printf("\nFull response: %s\n", fullText.String())
```

### Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

stream, err := model.DoStream(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{
        Text: "Long response...",
    },
})
if err != nil {
    return err
}
defer stream.Close()

// Stream will automatically stop when context is cancelled
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    fmt.Print(chunk.Text)
}
```

### Processing Tool Calls

```go
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    switch chunk.Type {
    case provider.ChunkTypeText:
        fmt.Print(chunk.Text)

    case provider.ChunkTypeToolCall:
        // Execute the tool
        result, err := executeToolCall(chunk.ToolCall)
        if err != nil {
            fmt.Printf("Tool error: %v\n", err)
            continue
        }
        fmt.Printf("Tool %s returned: %v\n", chunk.ToolCall.ToolName, result)

    case provider.ChunkTypeFinish:
        fmt.Printf("\nCompleted: %s\n", chunk.FinishReason)
    }
}
```

### Handling Reasoning/Thinking Tokens

Some models (like o1) emit reasoning tokens:

```go
var reasoning strings.Builder
var response strings.Builder

for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    switch chunk.Type {
    case provider.ChunkTypeReasoning:
        reasoning.WriteString(chunk.Text)
        fmt.Printf("[Thinking] %s", chunk.Text)

    case provider.ChunkTypeText:
        response.WriteString(chunk.Text)
        fmt.Print(chunk.Text)

    case provider.ChunkTypeFinish:
        if chunk.Usage.OutputDetails != nil &&
           chunk.Usage.OutputDetails.ReasoningTokens != nil {
            fmt.Printf("\nReasoning tokens: %d\n",
                *chunk.Usage.OutputDetails.ReasoningTokens)
        }
    }
}

fmt.Printf("\n\nThinking: %s\n", reasoning.String())
fmt.Printf("Response: %s\n", response.String())
```

### Concurrent Processing

```go
stream, err := model.DoStream(ctx, options)
if err != nil {
    return err
}
defer stream.Close()

// Channel for processing chunks concurrently
chunks := make(chan *provider.StreamChunk, 10)

// Start processor goroutines
var wg sync.WaitGroup
for i := 0; i < 3; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        for chunk := range chunks {
            processChunk(chunk)
        }
    }()
}

// Read from stream and send to channel
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }
    chunks <- chunk
}

close(chunks)
wg.Wait()
```

## Error Handling Best Practices

### Always Check for EOF

```go
chunk, err := stream.Next()
if err != nil {
    if err.Error() == "EOF" {
        // Normal stream completion
        break
    }
    // Actual error
    return fmt.Errorf("stream error: %w", err)
}
```

### Always Close Streams

```go
stream, err := model.DoStream(ctx, options)
if err != nil {
    return err
}
defer stream.Close()  // Always close, even on error

for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err  // defer will still call Close()
    }

    // Process chunk
}
```

### Check Stream.Err()

```go
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    // Process chunk
}

// Check for any lingering errors
if err := stream.Err(); err != nil {
    return fmt.Errorf("stream ended with error: %w", err)
}
```

## Migration from io.Reader Pattern

If you were expecting an `io.Reader` interface:

**DON'T:**
```go
// This pattern is not supported
buf := make([]byte, 4096)
n, err := stream.Read(buf)
```

**DO:**
```go
// Use the Next() method instead
chunk, err := stream.Next()
if err != nil {
    if err.Error() == "EOF" {
        break
    }
    return err
}

if chunk.Type == provider.ChunkTypeText {
    fmt.Print(chunk.Text)
}
```

## Why Not Both Patterns?

Supporting both `io.Reader` and `Next()` would require:

1. **Dual maintenance**: Two streaming implementations per provider
2. **Complexity**: Converting typed chunks to/from byte streams
3. **Performance overhead**: Serialization/deserialization
4. **Type information loss**: Can't represent complex types in []byte
5. **Inconsistent experience**: Users would be confused about which to use

The `Next()` pattern is superior for this use case, so that's what we support exclusively.

## Performance Considerations

### Buffering

The SDK handles buffering internally. You don't need to add additional buffering unless you have specific requirements:

```go
// SDK handles buffering internally - this is sufficient
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    process(chunk)
}
```

### Memory Usage

Chunks are processed one at a time, keeping memory usage low:

```go
// This uses minimal memory - only one chunk at a time
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    // Chunk is garbage collected after this iteration
    fmt.Print(chunk.Text)
}
```

If you need to accumulate the entire response, use a strings.Builder:

```go
var builder strings.Builder
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    if chunk.Type == provider.ChunkTypeText {
        builder.WriteString(chunk.Text)
    }
}

fullText := builder.String()
```

## Provider-Specific Notes

### OpenAI

- Supports all chunk types including reasoning tokens (o1 models)
- Tool calls are streamed incrementally

### Anthropic

- Supports thinking blocks via ChunkTypeReasoning
- Provides context management information in finish chunks

### Google (Vertex AI & Generative AI)

- Streams via Server-Sent Events (SSE)
- Supports tool calls and multi-modal inputs
- Vertex AI supports Google Cloud Storage URLs

### Bedrock

- Different stream format per model provider (Claude, Titan, etc.)
- SDK normalizes to consistent chunk format

## Common Patterns

### Stream to HTTP Response

```go
func handleStream(w http.ResponseWriter, r *http.Request) {
    stream, err := model.DoStream(r.Context(), options)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    defer stream.Close()

    // Set headers for streaming
    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
    w.Header().Set("X-Content-Type-Options", "nosniff")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming not supported", http.StatusInternalServerError)
        return
    }

    for {
        chunk, err := stream.Next()
        if err != nil {
            if err.Error() == "EOF" {
                break
            }
            fmt.Fprintf(w, "Error: %v\n", err)
            return
        }

        if chunk.Type == provider.ChunkTypeText {
            fmt.Fprint(w, chunk.Text)
            flusher.Flush()
        }
    }
}
```

### Stream with Progress Updates

```go
func streamWithProgress(model provider.LanguageModel) error {
    stream, err := model.DoStream(ctx, options)
    if err != nil {
        return err
    }
    defer stream.Close()

    chunkCount := 0
    for {
        chunk, err := stream.Next()
        if err != nil {
            if err.Error() == "EOF" {
                break
            }
            return err
        }

        chunkCount++

        switch chunk.Type {
        case provider.ChunkTypeText:
            fmt.Print(chunk.Text)
        case provider.ChunkTypeFinish:
            fmt.Printf("\n\n[Received %d chunks]\n", chunkCount)
        }
    }

    return nil
}
```

## Testing Streams

### Mock Stream for Testing

```go
type mockStream struct {
    chunks []*provider.StreamChunk
    index  int
}

func (m *mockStream) Next() (*provider.StreamChunk, error) {
    if m.index >= len(m.chunks) {
        return nil, io.EOF
    }
    chunk := m.chunks[m.index]
    m.index++
    return chunk, nil
}

func (m *mockStream) Err() error {
    return nil
}

func (m *mockStream) Close() error {
    return nil
}

func TestStreamProcessing(t *testing.T) {
    stream := &mockStream{
        chunks: []*provider.StreamChunk{
            {Type: provider.ChunkTypeText, Text: "Hello"},
            {Type: provider.ChunkTypeText, Text: " World"},
            {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
        },
    }

    var result strings.Builder
    for {
        chunk, err := stream.Next()
        if err != nil {
            if err.Error() == "EOF" {
                break
            }
            t.Fatal(err)
        }

        if chunk.Type == provider.ChunkTypeText {
            result.WriteString(chunk.Text)
        }
    }

    if result.String() != "Hello World" {
        t.Errorf("Expected 'Hello World', got '%s'", result.String())
    }
}
```

## Resources

- [Provider Documentation](../../pkg/providers/)
- [Language Model Interface](../../pkg/provider/language_model.go)
- [Examples](../../examples/)

## Summary

The Go AI SDK uses a `Next()`-based streaming pattern that:

- ✅ Provides strong type safety
- ✅ Handles complex chunk types naturally
- ✅ Follows Go idioms and conventions
- ✅ Simplifies error handling
- ✅ Supports tool calls and structured data
- ✅ Enables concurrent processing

The `io.Reader` pattern is intentionally not supported to maintain simplicity and type safety. Use `Next()` for all streaming operations.
