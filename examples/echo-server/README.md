# Echo Server Example

Production-ready HTTP server using the [Echo](https://echo.labstack.com/) web framework with AI capabilities.

## Features

This example demonstrates:

- ✅ Echo framework integration with Go AI SDK
- ✅ Built-in middleware (Logger, Recover, CORS, RequestID)
- ✅ Custom error handling with request tracking
- ✅ Request binding and validation
- ✅ Server-Sent Events (SSE) for streaming
- ✅ Tool calling with multiple tools
- ✅ Clean routing with Echo's router
- ✅ Production-ready patterns

## Prerequisites

- Go 1.21 or higher
- OpenAI API key
- Echo framework (`go get github.com/labstack/echo/v4`)

## Setup

1. Install dependencies:
```bash
go get github.com/labstack/echo/v4
go mod tidy
```

2. Set your API key:
```bash
export OPENAI_API_KEY=sk-...
```

3. Run the server:
```bash
go run main.go
```

The server will start on port 8080 (or PORT environment variable).

## API Endpoints

### POST /generate

Generate text completion.

**Request:**
```json
{
  "prompt": "What is the capital of France?",
  "system": "You are a helpful assistant",
  "maxTokens": 100,
  "temperature": 0.7
}
```

**Response:**
```json
{
  "text": "The capital of France is Paris.",
  "usage": {
    "inputTokens": 12,
    "outputTokens": 8,
    "totalTokens": 20
  },
  "finishReason": "stop"
}
```

### POST /stream

Stream text completion with Server-Sent Events.

**Request:**
```json
{
  "prompt": "Write a short poem about coding",
  "temperature": 0.8
}
```

**Response (SSE):**
```
event: start
data:

event: text
data: In lines of code

event: text
data:  we weave our dreams

event: done
data: {"inputTokens":8,"outputTokens":45,"totalTokens":53}
```

### POST /tools

Execute queries with tool calling.

**Request:**
```json
{
  "query": "What's 45 multiplied by 23?",
  "maxSteps": 5
}
```

**Response:**
```json
{
  "result": "45 multiplied by 23 equals 1,035.",
  "steps": [
    {
      "toolCalls": [...],
      "toolResults": [...]
    }
  ],
  "usage": {
    "totalTokens": 156
  }
}
```

### GET /health

Health check endpoint with request ID.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": 1703001234,
  "model": "gpt-4",
  "requestId": "abc123"
}
```

## Code Highlights

### Echo Framework Setup

```go
e := echo.New()
e.HideBanner = true

// Built-in middleware
e.Use(middleware.Logger())
e.Use(middleware.Recover())
e.Use(middleware.CORS())
e.Use(middleware.RequestID())

// Custom error handler
e.HTTPErrorHandler = customHTTPErrorHandler
```

### Request Binding

Echo provides automatic JSON binding with validation:

```go
var req GenerateRequest
if err := c.Bind(&req); err != nil {
    return echo.NewHTTPError(http.StatusBadRequest, err.Error())
}

if err := c.Validate(&req); err != nil {
    return echo.NewHTTPError(http.StatusBadRequest, err.Error())
}
```

### Custom Error Handler

Track errors with request IDs:

```go
func customHTTPErrorHandler(err error, c echo.Context) {
    code := http.StatusInternalServerError
    message := err.Error()

    if he, ok := err.(*echo.HTTPError); ok {
        code = he.Code
        if msg, ok := he.Message.(string); ok {
            message = msg
        }
    }

    if !c.Response().Committed {
        c.JSON(code, map[string]interface{}{
            "error":     message,
            "requestId": c.Response().Header().Get(echo.HeaderXRequestID),
            "timestamp": time.Now().Unix(),
        })
    }
}
```

### SSE Streaming

```go
c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
c.Response().Header().Set("Cache-Control", "no-cache")

stream, _ := ai.StreamText(ctx, opts)

for chunk := range stream.Chunks() {
    sendSSE(c.Response(), "text", chunk.Text)
    c.Response().Flush()
}
```

## Available Tools

### Calculator Tool

Performs basic arithmetic (add, subtract, multiply, divide):

```bash
curl -X POST http://localhost:8080/tools \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Calculate 156 divided by 12"
  }'
```

### Time Tool

Gets current time in any timezone:

```bash
curl -X POST http://localhost:8080/tools \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What time is it in Tokyo?",
    "system": "You are a helpful time assistant"
  }'
```

## Testing the Server

### Generate Text

```bash
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Explain quantum computing in one sentence",
    "maxTokens": 50
  }'
```

### Stream Response

```bash
curl -X POST http://localhost:8080/stream \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Write a haiku about mountains",
    "temperature": 0.9
  }'
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Echo vs Other Frameworks

### Echo Advantages

- **Performance**: One of the fastest Go web frameworks
- **Middleware**: Rich built-in middleware collection
- **Request ID**: Built-in request tracking
- **Error Handling**: Centralized error handler
- **Binding**: Automatic JSON/form binding
- **Validation**: Built-in validator support
- **Routing**: Powerful routing with parameter support

### When to Use Echo

- Need high performance HTTP server
- Want comprehensive middleware out of the box
- Building production REST APIs
- Require advanced routing features
- Need request tracking and logging

### Echo vs net/http

net/http is more minimal and requires manual middleware. Echo provides batteries-included approach.

### Echo vs Gin

Both are high-performance. Echo has more middleware options and better error handling patterns. Gin has slightly better performance in benchmarks.

## Production Considerations

### Graceful Shutdown

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

go func() {
    if err := e.Start(":8080"); err != nil && err != http.ErrServerClosed {
        e.Logger.Fatal(err)
    }
}()

<-ctx.Done()
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := e.Shutdown(ctx); err != nil {
    e.Logger.Fatal(err)
}
```

### Custom Validator

Add struct validation:

```go
type CustomValidator struct {
    validator *validator.Validate
}

func (cv *CustomValidator) Validate(i interface{}) error {
    return cv.validator.Struct(i)
}

e.Validator = &CustomValidator{validator: validator.New()}
```

### Rate Limiting

Add rate limiting middleware:

```go
e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))
```

### Authentication

Add JWT authentication:

```go
e.Use(middleware.JWT([]byte("secret")))
```

## Use Cases

### REST API Server

Echo is perfect for building REST APIs with multiple endpoints, middleware chains, and structured responses.

### Microservices

Use Echo for microservices that need HTTP endpoints with built-in observability (logging, metrics, tracing).

### AI-Powered APIs

Combine Echo's performance with AI capabilities for production-grade AI APIs.

### WebSocket Server

Echo supports WebSockets for real-time AI streaming beyond SSE.

## Troubleshooting

### Port Already in Use

Change the port:
```bash
PORT=3000 go run main.go
```

### CORS Issues

The example includes CORS middleware. For specific origins:

```go
e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
    AllowOrigins: []string{"https://example.com"},
    AllowMethods: []string{http.MethodGet, http.MethodPost},
}))
```

### Request Validation Errors

Make sure request fields match the struct tags:
- `json` tag defines JSON field name
- `validate:"required"` enforces required fields

### SSE Not Working

Ensure headers are set before writing:
- Content-Type: text/event-stream
- Cache-Control: no-cache
- Connection: keep-alive

## Next Steps

- Add more middleware (rate limiting, authentication)
- Implement WebSocket streaming
- Add OpenTelemetry for observability
- Create additional tool implementations
- Add database integration
- Implement caching layer

## Related Examples

- [http-server](../http-server) - Standard net/http implementation
- [gin-server](../gin-server) - Gin framework example
- [middleware](../middleware) - Middleware patterns

## Resources

- [Echo Documentation](https://echo.labstack.com/guide/)
- [Echo Middleware](https://echo.labstack.com/middleware/)
- [Go AI SDK Documentation](../../../docs)
- [OpenAI API](https://platform.openai.com/docs)
