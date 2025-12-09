# Gin Server Example

Production-ready HTTP server using the Gin web framework with AI capabilities.

## Features Demonstrated

- **Gin framework** integration
- **JSON request binding** and validation
- **Middleware** (CORS, logging)
- **SSE streaming** with Gin
- **Agent endpoints** with tools
- **Error handling** with proper HTTP codes
- **Type-safe requests/responses**
- **Health check endpoint**

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Installation

The example will automatically download Gin when you run it:

```bash
cd examples/gin-server
go get github.com/gin-gonic/gin
go run main.go
```

## Running the Server

```bash
go run main.go
```

You should see:

```
🚀 Gin server starting on port 8080
Available endpoints:
  POST /chat   - Chat completion
  POST /stream - Streaming SSE
  POST /agent  - Agent with tools
  GET  /health - Health check
```

## API Endpoints

### POST /chat - Chat Completion

Send a message and get a complete response.

**Request:**

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Explain quantum computing",
    "temperature": 0.7,
    "maxTokens": 200
  }'
```

**Response:**

```json
{
  "response": "Quantum computing is...",
  "usage": {
    "inputTokens": 5,
    "outputTokens": 195,
    "totalTokens": 200
  }
}
```

### POST /stream - Streaming with SSE

Stream responses in real-time.

**Request:**

```bash
curl -X POST http://localhost:8080/stream \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Write a story about AI",
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

...

event: done
data: {"totalTokens":150}
```

### POST /agent - Agent with Tools

Use an AI agent with tool-calling capabilities.

**Request:**

```bash
curl -X POST http://localhost:8080/agent \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is the weather in Boston and search for best restaurants there?",
    "maxSteps": 5
  }'
```

**Response:**

```json
{
  "result": "The weather in Boston is sunny, 72°F. Here are the top restaurants...",
  "steps": [
    {
      "stepNumber": 1,
      "toolCalls": [
        {
          "toolName": "get_weather",
          "args": {"location": "Boston, MA"}
        }
      ]
    },
    {
      "stepNumber": 2,
      "toolCalls": [
        {
          "toolName": "search",
          "args": {"query": "best restaurants Boston"}
        }
      ]
    }
  ],
  "toolCalls": [...],
  "usage": {
    "totalTokens": 450
  }
}
```

### GET /health - Health Check

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

## Gin Features Used

### Request Binding

```go
type ChatRequest struct {
    Message     string   `json:"message" binding:"required"`
    Temperature *float64 `json:"temperature"`
}

var req ChatRequest
if err := c.ShouldBindJSON(&req); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
}
```

### Middleware

```go
func corsMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
        c.Next()
    }
}

r.Use(corsMiddleware())
```

### JSON Responses

```go
c.JSON(http.StatusOK, gin.H{
    "message": "Success",
    "data": result,
})
```

### SSE Streaming

```go
c.Writer.Header().Set("Content-Type", "text/event-stream")
c.Writer.Header().Set("Cache-Control", "no-cache")

for chunk := range stream.Chunks() {
    fmt.Fprintf(c.Writer, "data: %s\n\n", chunk.Text)
    c.Writer.Flush()
}
```

## Advantages of Gin

1. **Performance**: Faster than standard net/http
2. **Routing**: Clean and intuitive route definitions
3. **Middleware**: Easy to add cross-cutting concerns
4. **Binding**: Automatic JSON/form binding with validation
5. **Error handling**: Built-in error handling
6. **Testing**: Easy to test with httptest

## Production Deployment

### With Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
```

### Environment Variables

```bash
# Required
export OPENAI_API_KEY=sk-...

# Optional
export PORT=8080
export GIN_MODE=release  # production mode
```

### Reverse Proxy (Nginx)

```nginx
server {
    listen 80;
    server_name api.example.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;

        # For SSE
        proxy_buffering off;
        proxy_read_timeout 86400;
    }
}
```

## Adding Custom Middleware

### Logging Middleware

```go
func loggingMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        c.Next()

        duration := time.Since(start)
        log.Printf("%s %s %d %v",
            c.Request.Method,
            c.Request.URL.Path,
            c.Writer.Status(),
            duration)
    }
}

r.Use(loggingMiddleware())
```

### Rate Limiting

```go
func rateLimitMiddleware() gin.HandlerFunc {
    limiter := rate.NewLimiter(10, 100)  // 10 req/sec, burst 100

    return func(c *gin.Context) {
        if !limiter.Allow() {
            c.JSON(http.StatusTooManyRequests, gin.H{
                "error": "Rate limit exceeded",
            })
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### Authentication

```go
func authMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")

        if token == "" {
            c.JSON(http.StatusUnauthorized, gin.H{
                "error": "Missing authorization token",
            })
            c.Abort()
            return
        }

        // Validate token
        // ...

        c.Next()
    }
}

// Apply to specific routes
authorized := r.Group("/")
authorized.Use(authMiddleware())
{
    authorized.POST("/chat", handleChat)
}
```

## Testing

```go
func TestChatEndpoint(t *testing.T) {
    r := setupRouter()

    w := httptest.NewRecorder()
    req, _ := http.NewRequest("POST", "/chat", strings.NewReader(`{
        "message": "Hello"
    }`))
    req.Header.Set("Content-Type", "application/json")

    r.ServeHTTP(w, req)

    assert.Equal(t, 200, w.Code)

    var response ChatResponse
    json.Unmarshal(w.Body.Bytes(), &response)
    assert.NotEmpty(t, response.Response)
}
```

## Performance Tips

1. **Use release mode**: `gin.SetMode(gin.ReleaseMode)`
2. **Enable gzip**: `r.Use(gzip.Gzip(gzip.DefaultCompression))`
3. **Connection pooling**: Reuse HTTP clients
4. **Caching**: Cache frequent responses
5. **Horizontal scaling**: Run multiple instances behind load balancer

## Comparison: Gin vs net/http

| Feature | net/http | Gin |
|---------|----------|-----|
| Routing | Manual | Built-in router |
| Middleware | Manual | Built-in support |
| JSON binding | Manual | Automatic |
| Performance | Fast | Faster (40x) |
| Learning curve | Low | Low-Medium |

## Next Steps

- **[http-server](../http-server)** - Compare with standard net/http
- **[echo-server](../echo-server)** - Try Echo framework
- **[middleware](../middleware)** - Advanced middleware patterns
- **[agents](../agents)** - Build complex agents

## Documentation

- [Gin Documentation](https://gin-gonic.com/docs/)
- [Go AI SDK Docs](../../docs)
