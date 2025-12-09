# Logging Middleware Example

Comprehensive logging solution for AI SDK operations with multiple output formats and strategies.

## Features

This example demonstrates:

- ✅ Console logging (verbose and concise modes)
- ✅ JSON file logging for persistence
- ✅ Structured log entries with metadata
- ✅ Multi-logger support (log to multiple destinations)
- ✅ Request/response tracking
- ✅ Token usage monitoring
- ✅ Duration measurement
- ✅ Error logging
- ✅ Production-ready patterns

## Why Logging Matters

Logging AI operations is crucial for:

1. **Debugging**: Understand what prompts and responses occurred
2. **Monitoring**: Track token usage and costs
3. **Performance**: Measure response times
4. **Auditing**: Maintain records of AI interactions
5. **Analysis**: Identify patterns and optimize prompts
6. **Compliance**: Meet regulatory requirements

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Log Entry Structure

Each log entry contains:

```go
type LogEntry struct {
    Timestamp    time.Time              // When the request occurred
    Provider     string                 // AI provider (openai, anthropic, etc.)
    Model        string                 // Model name (gpt-4, claude-3, etc.)
    Prompt       string                 // Input prompt
    Response     string                 // Generated response
    InputTokens  int                    // Tokens in prompt
    OutputTokens int                    // Tokens in response
    TotalTokens  int                    // Total tokens used
    Duration     time.Duration          // Request duration
    FinishReason types.FinishReason     // Why generation stopped
    Error        string                 // Error message if failed
    Extra        map[string]interface{} // Additional metadata
}
```

## Logging Strategies

### 1. Console Logger (Verbose)

Detailed human-readable output:

```
[2024-12-08T10:30:45Z] openai/gpt-4
Prompt: What is the capital of France?
Response: The capital of France is Paris.
Tokens: 12 in, 8 out, 20 total
Duration: 1.234s
```

### 2. Console Logger (Concise)

Brief status updates:

```
[10:30:45] ✓ gpt-4: 20 tokens in 1.2s
[10:30:47] ✓ gpt-4: 35 tokens in 1.5s
[10:30:50] ✗ gpt-4: error in 0.5s
```

### 3. JSON Logger

Structured logs for analysis:

```json
{
  "timestamp": "2024-12-08T10:30:45Z",
  "provider": "openai",
  "model": "gpt-4",
  "prompt": "What is the capital of France?",
  "response": "The capital of France is Paris.",
  "inputTokens": 12,
  "outputTokens": 8,
  "totalTokens": 20,
  "duration": 1234000000,
  "finishReason": "stop"
}
```

### 4. Multi-Logger

Log to multiple destinations simultaneously:

```go
multiLogger := &MultiLogger{
    loggers: []Logger{
        &ConsoleLogger{Verbose: false},  // Console for monitoring
        jsonLogger,                       // File for persistence
    },
}
```

## Code Highlights

### Basic Logging Middleware

```go
type LoggingMiddleware struct {
    logger Logger
}

func (m *LoggingMiddleware) GenerateText(
    ctx context.Context,
    model provider.LanguageModel,
    prompt string,
    opts ai.GenerateTextOptions,
) (ai.GenerateTextResult, error) {
    start := time.Now()

    // Execute request
    result, err := ai.GenerateText(ctx, opts)

    // Log the operation
    m.logger.Log(LogEntry{
        Timestamp:    start,
        Duration:     time.Since(start),
        Model:        model.ModelID(),
        Prompt:       prompt,
        Response:     result.Text,
        TotalTokens:  result.Usage.TotalTokens,
        Error:        errToString(err),
    })

    return result, err
}
```

### Custom Logger Implementation

```go
type CustomLogger struct {
    // Your fields
}

func (l *CustomLogger) Log(entry LogEntry) {
    // Your logging logic
}

func (l *CustomLogger) Flush() error {
    // Flush any buffers
    return nil
}
```

## Use Cases

### 1. Development Debugging

Use verbose console logging during development:

```go
logger := &ConsoleLogger{Verbose: true}
middleware := NewLoggingMiddleware(logger)
```

### 2. Production Monitoring

Use concise console + JSON file logging:

```go
jsonLogger, _ := NewJSONLogger("logs/ai-requests.jsonl")
multiLogger := &MultiLogger{
    loggers: []Logger{
        &ConsoleLogger{Verbose: false},
        jsonLogger,
    },
}
```

### 3. Cost Tracking

Analyze JSON logs to calculate costs:

```bash
# Count total tokens used
jq -s 'map(.totalTokens) | add' ai-requests.jsonl

# Calculate cost (GPT-4: $0.03/1K input, $0.06/1K output)
jq -s 'map(.inputTokens * 0.03 / 1000 + .outputTokens * 0.06 / 1000) | add' ai-requests.jsonl
```

### 4. Performance Analysis

Find slow requests:

```bash
# Requests taking > 2 seconds
jq 'select(.duration > 2000000000)' ai-requests.jsonl

# Average response time
jq -s 'map(.duration) | add / length / 1000000000' ai-requests.jsonl
```

### 5. Error Analysis

Track errors:

```bash
# Count errors
jq 'select(.error != null and .error != "")' ai-requests.jsonl | wc -l

# Error messages
jq -r 'select(.error != null) | .error' ai-requests.jsonl
```

## Advanced Patterns

### Sampling

Log only a percentage of requests:

```go
type SamplingLogger struct {
    underlying Logger
    sampleRate float64
}

func (l *SamplingLogger) Log(entry LogEntry) {
    if rand.Float64() < l.sampleRate {
        l.underlying.Log(entry)
    }
}
```

### Filtering

Log only specific conditions:

```go
type FilteringLogger struct {
    underlying Logger
    minTokens  int
}

func (l *FilteringLogger) Log(entry LogEntry) {
    if entry.TotalTokens >= l.minTokens {
        l.underlying.Log(entry)
    }
}
```

### Async Logging

Buffer logs and write asynchronously:

```go
type AsyncLogger struct {
    underlying Logger
    buffer     chan LogEntry
}

func NewAsyncLogger(underlying Logger) *AsyncLogger {
    l := &AsyncLogger{
        underlying: underlying,
        buffer:     make(chan LogEntry, 100),
    }
    go l.worker()
    return l
}

func (l *AsyncLogger) worker() {
    for entry := range l.buffer {
        l.underlying.Log(entry)
    }
}

func (l *AsyncLogger) Log(entry LogEntry) {
    l.buffer <- entry
}
```

### Log Rotation

Rotate log files by size or time:

```go
type RotatingLogger struct {
    basename   string
    maxSize    int64
    currentFile *os.File
    currentSize int64
}

func (l *RotatingLogger) Log(entry LogEntry) {
    data, _ := json.Marshal(entry)

    if l.currentSize+int64(len(data)) > l.maxSize {
        l.rotate()
    }

    l.currentFile.Write(data)
    l.currentSize += int64(len(data))
}
```

## Integration with Observability

### OpenTelemetry

```go
import "go.opentelemetry.io/otel/trace"

type TracingLogger struct {
    underlying Logger
    tracer     trace.Tracer
}

func (l *TracingLogger) Log(entry LogEntry) {
    _, span := l.tracer.Start(ctx, "ai.generate")
    span.SetAttributes(
        attribute.String("model", entry.Model),
        attribute.Int("tokens", entry.TotalTokens),
    )
    defer span.End()

    l.underlying.Log(entry)
}
```

### Prometheus Metrics

```go
import "github.com/prometheus/client_golang/prometheus"

type MetricsLogger struct {
    underlying   Logger
    requestCount *prometheus.CounterVec
    tokenCount   *prometheus.CounterVec
    duration     *prometheus.HistogramVec
}

func (l *MetricsLogger) Log(entry LogEntry) {
    l.requestCount.WithLabelValues(entry.Model).Inc()
    l.tokenCount.WithLabelValues(entry.Model).Add(float64(entry.TotalTokens))
    l.duration.WithLabelValues(entry.Model).Observe(entry.Duration.Seconds())

    l.underlying.Log(entry)
}
```

## Testing

Run the example:

```bash
go run main.go
```

Expected output:
- Console logs showing requests
- `ai-requests.jsonl` file with structured logs
- Log preview at the end

## Log Analysis Tools

### View logs in real-time

```bash
tail -f ai-requests.jsonl | jq '.'
```

### Query specific model

```bash
jq 'select(.model == "gpt-4")' ai-requests.jsonl
```

### Group by model

```bash
jq -s 'group_by(.model) | map({model: .[0].model, count: length})' ai-requests.jsonl
```

### Token usage report

```bash
jq -s 'group_by(.model) | map({
  model: .[0].model,
  totalTokens: map(.totalTokens) | add,
  avgTokens: (map(.totalTokens) | add / length)
})' ai-requests.jsonl
```

## Best Practices

1. **Use structured logging**: JSON format enables powerful analysis
2. **Log metadata**: Include model, provider, timestamps
3. **Sanitize sensitive data**: Remove PII from logs
4. **Rotate logs**: Prevent disk space issues
5. **Async logging**: Don't block on I/O
6. **Sample in production**: Log percentage of requests to reduce volume
7. **Index logs**: Use tools like Elasticsearch for searching
8. **Set retention policies**: Delete old logs after period

## Troubleshooting

### Logs not appearing

- Check file permissions
- Verify logger is being called
- Call `Flush()` before program exits

### Large log files

- Implement log rotation
- Use sampling
- Compress old logs

### Slow performance

- Use async logging
- Buffer writes
- Reduce verbosity in production

## Next Steps

- Add log aggregation (ELK stack, Loki)
- Implement alert rules
- Create dashboard for monitoring
- Add structured error tracking
- Integrate with APM tools

## Related Examples

- [middleware](../../middleware) - Basic middleware patterns
- [http-server](../../http-server) - HTTP server with logging
- [gin-server](../../gin-server) - Gin framework logging

## Resources

- [Structured Logging Best Practices](https://www.datadoghq.com/blog/structured-logging/)
- [Go Logging Libraries](https://github.com/avelino/awesome-go#logging)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
