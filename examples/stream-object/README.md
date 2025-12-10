# Stream Object Example

Demonstrates streaming structured object generation for real-time updates.

## Features

- **Real-time streaming** of structured data
- **Progressive rendering** as objects build
- **Partial object access** during generation
- **Array streaming** for lists
- **Live progress updates**

## Prerequisites

- Go 1.21+
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running

```bash
cd examples/stream-object
go run main.go
```

## Use Cases

1. **Progressive UI Updates** - Show data as it generates
2. **Long Lists** - Stream arrays of items
3. **Real-time Dashboards** - Update metrics live
4. **Interactive Forms** - Fill fields progressively
5. **Live Previews** - Show content before completion

## Key Concepts

### Streaming vs Non-Streaming

**Non-streaming:**
```go
result, _ := ai.GenerateObject(ctx, options)
// Wait for complete object
fmt.Println(result.Object)
```

**Streaming:**
```go
stream, _ := ai.StreamObject(ctx, options)
for partial := range stream.PartialObjects() {
    // Get partial object as it builds
    fmt.Println(partial)
}
finalObject := stream.FinalObject()
```

## Benefits

- **Better UX**: Users see progress immediately
- **Faster perceived performance**: Content appears faster
- **Cancellation**: Can stop generation early
- **Feedback**: Show progress indicators

## Documentation

- [Go AI SDK Docs](../../docs)
- [OpenAI Streaming](https://platform.openai.com/docs/api-reference/streaming)
