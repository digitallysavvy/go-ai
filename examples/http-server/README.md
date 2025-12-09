# HTTP Server Example

A production-ready HTTP server demonstrating how to build AI-powered REST APIs using the Go AI SDK with standard `net/http`.

## Features Demonstrated

- **Basic text generation** via REST API
- **Server-Sent Events (SSE)** for streaming responses
- **Tool calling** with custom function execution
- **CORS support** for cross-origin requests
- **Error handling** and proper HTTP status codes
- **Health check endpoint** for monitoring
- **Request/response validation**

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

1. **Set your API key:**

```bash
export OPENAI_API_KEY=sk-...
```

2. **Optional: Set custom port (defaults to 8080):**

```bash
export PORT=3000
```

## Running the Server

```bash
cd examples/http-server
go run main.go
```

You should see:

```
🚀 HTTP server starting on port 8080
Available endpoints:
  POST /generate - Generate text completion
  POST /stream   - Stream text completion (SSE)
  POST /tools    - Generate with tool calling
  GET  /health   - Health check
```

## API Endpoints

### GET / - API Information

Returns information about available endpoints.

```bash
curl http://localhost:8080/
```

### POST /generate - Basic Text Generation

Generate a complete text response.

**Request:**

```bash
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Explain quantum computing in simple terms",
    "temperature": 0.7,
    "maxTokens": 200
  }'
```

**Response:**

```json
{
  "text": "Quantum computing is a type of computing that...",
  "usage": {
    "inputTokens": 12,
    "outputTokens": 185,
    "totalTokens": 197
  },
  "finishReason": "stop"
}
```

### POST /stream - Streaming Text Generation (SSE)

Stream text generation in real-time using Server-Sent Events.

**Request:**

```bash
curl -X POST http://localhost:8080/stream \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Write a short story about a robot",
    "temperature": 0.9
  }'
```

**Response Stream:**

```
event: start
data:

event: text
data: Once

event: text
data:  upon

event: text
data:  a

event: text
data:  time

...

event: done
data: {"inputTokens":10,"outputTokens":150,"totalTokens":160}
```

**JavaScript client example:**

```javascript
const eventSource = new EventSource('http://localhost:8080/stream', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ prompt: 'Write a poem' })
});

eventSource.addEventListener('text', (e) => {
  console.log('Text:', e.data);
});

eventSource.addEventListener('done', (e) => {
  console.log('Usage:', JSON.parse(e.data));
  eventSource.close();
});

eventSource.addEventListener('error', (e) => {
  console.error('Error:', e.data);
  eventSource.close();
});
```

### POST /tools - Generate with Tool Calling

Generate text with access to tool functions.

**Request:**

```bash
curl -X POST http://localhost:8080/tools \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "What is the weather in San Francisco?",
    "system": "You are a helpful weather assistant"
  }'
```

**Response:**

```json
{
  "text": "The current weather in San Francisco is sunny with a temperature of 72°F and humidity at 65%.",
  "usage": {
    "inputTokens": 45,
    "outputTokens": 32,
    "totalTokens": 77
  },
  "finishReason": "stop",
  "steps": [
    {
      "type": "tool-call",
      "toolName": "get_weather",
      "toolCallId": "call_abc123",
      "args": {
        "location": "San Francisco, CA",
        "unit": "fahrenheit"
      }
    },
    {
      "type": "tool-result",
      "toolName": "get_weather",
      "result": {
        "location": "San Francisco, CA",
        "temperature": 72,
        "unit": "fahrenheit",
        "condition": "sunny",
        "humidity": 65
      }
    }
  ]
}
```

### GET /health - Health Check

Check if the server is running and healthy.

```bash
curl http://localhost:8080/health
```

**Response:**

```json
{
  "status": "healthy",
  "timestamp": 1702345678,
  "model": "gpt-4"
}
```

## Request Schema

### GenerateRequest

```typescript
{
  "prompt": string,           // Required: The user prompt
  "system": string,           // Optional: System message
  "maxTokens": number,        // Optional: Max tokens to generate
  "temperature": number,      // Optional: 0.0 to 2.0
  "tools": string[],          // Optional: Tool names to enable
  "extra": object             // Optional: Provider-specific options
}
```

## What You'll Learn

1. **HTTP API Design**: Build RESTful endpoints for AI operations
2. **SSE Streaming**: Implement Server-Sent Events for real-time streaming
3. **CORS Handling**: Enable cross-origin requests
4. **Error Handling**: Proper HTTP status codes and error responses
5. **Tool Integration**: Execute custom functions during generation
6. **Context Management**: Use context for timeouts and cancellation
7. **Health Monitoring**: Implement health check endpoints

## Code Highlights

### Setting Up the Server

```go
// Create provider and model once at startup
p := openai.New(openai.Config{
    APIKey: apiKey,
})
model, err := p.LanguageModel("gpt-4")

// Setup routes
mux := http.NewServeMux()
mux.HandleFunc("/generate", handleGenerate)
mux.HandleFunc("/stream", handleStream)
mux.HandleFunc("/tools", handleTools)

// Add CORS middleware
handler := corsMiddleware(mux)

http.ListenAndServe(":8080", handler)
```

### Streaming with SSE

```go
// Set SSE headers
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")

stream, _ := ai.StreamText(ctx, opts)
flusher := w.(http.Flusher)

for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Fprintf(w, "event: text\ndata: %s\n\n", chunk.Text)
        flusher.Flush()
    }
}
```

### Tool Calling

```go
weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get the current weather for a location",
    Parameters:  jsonSchema,
    Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
        location := params["location"].(string)
        // Call weather API
        return weatherData, nil
    },
}

result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:    model,
    Prompt:   "What's the weather?",
    Tools:    []types.Tool{weatherTool},
    MaxSteps: &maxSteps,
})
```

## Testing the Server

### Manual Testing

```bash
# Test basic generation
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Say hello"}'

# Test streaming
curl -X POST http://localhost:8080/stream \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Count to 10"}'

# Test tool calling
curl -X POST http://localhost:8080/tools \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Weather in Boston?"}'

# Test health
curl http://localhost:8080/health
```

### Load Testing with Apache Bench

```bash
# Test 100 requests with 10 concurrent connections
ab -n 100 -c 10 -p request.json -T application/json \
  http://localhost:8080/generate
```

## Production Considerations

### Security

1. **API Key Management**: Use environment variables, never hardcode
2. **Rate Limiting**: Add rate limiting middleware (see middleware examples)
3. **Authentication**: Add auth middleware for production
4. **Input Validation**: Validate all request parameters
5. **Error Messages**: Don't expose internal errors to clients

### Performance

1. **Connection Pooling**: Reuse HTTP clients
2. **Timeouts**: Set appropriate timeouts on contexts
3. **Buffering**: Use buffered responses where appropriate
4. **Caching**: Cache common responses (see caching middleware)

### Monitoring

1. **Logging**: Add structured logging (see logging middleware)
2. **Metrics**: Track request counts, latency, errors
3. **Health Checks**: Implement comprehensive health checks
4. **Tracing**: Add distributed tracing (see telemetry middleware)

## Next Steps

- **[gin-server](../gin-server)** - See Gin framework implementation
- **[middleware](../middleware)** - Add logging, caching, rate limiting
- **[generate-object](../generate-object)** - Learn structured output generation
- **[providers](../providers)** - Explore provider-specific features

## Troubleshooting

### Port Already in Use

If port 8080 is in use:

```bash
export PORT=3000
go run main.go
```

### Stream Not Flushing

If streaming doesn't work:
1. Check that your client supports SSE
2. Verify reverse proxy settings (disable buffering)
3. Ensure Content-Type is `text/event-stream`

### CORS Errors

If you get CORS errors:
1. Check that CORS middleware is applied
2. Verify allowed origins in corsMiddleware
3. Ensure preflight OPTIONS requests are handled

## Documentation

- [Go AI SDK Documentation](../../docs)
- [OpenAI API Reference](https://platform.openai.com/docs/api-reference)
- [Server-Sent Events Spec](https://html.spec.whatwg.org/multipage/server-sent-events.html)
