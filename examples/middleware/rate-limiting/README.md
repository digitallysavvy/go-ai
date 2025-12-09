# Rate Limiting Middleware Example

Intelligent rate limiting for AI operations to prevent hitting API limits, control costs, and handle traffic spikes.

## Features

- ✅ Token bucket rate limiting (requests per second with burst)
- ✅ Sliding window rate limiting (requests per time window)
- ✅ Concurrent request limiting (max concurrent requests)
- ✅ Combined rate limiters (multiple strategies)
- ✅ Automatic request queuing and waiting
- ✅ Rate limit statistics tracking
- ✅ Thread-safe operations
- ✅ Context-aware cancellation

## Why Rate Limiting?

### Prevent API Errors
- Most AI providers have rate limits (requests per minute/hour)
- Exceeding limits results in 429 errors
- Rate limiting prevents these errors

### Control Costs
- Limit spending by capping request rates
- Prevent cost overruns from runaway processes
- Budget-friendly AI usage

### Handle Traffic Spikes
- Queue requests during high traffic
- Gracefully handle bursts
- Maintain service stability

## Rate Limiting Strategies

### 1. Token Bucket

Allows bursts but averages to a steady rate:

```go
limiter := NewTokenBucketLimiter(
    2.0,  // 2 requests per second
    3,    // burst of 3 requests
)
```

**Use when:** You want to allow short bursts but maintain average rate

### 2. Sliding Window

Fixed number of requests in a time window:

```go
limiter := NewSlidingWindowLimiter(
    10,             // 10 requests
    1*time.Minute,  // per minute
)
```

**Use when:** You need strict limits matching API provider quotas

### 3. Concurrent Limiter

Limits simultaneous requests:

```go
limiter := NewConcurrentLimiter(5) // max 5 concurrent
```

**Use when:** You want to control load on your system or downstream services

### 4. Combined Limiters

Apply multiple strategies:

```go
limiter := NewCombinedLimiter(
    NewTokenBucketLimiter(10.0, 20),  // 10/sec, burst 20
    NewConcurrentLimiter(5),          // max 5 concurrent
)
```

**Use when:** You need both rate and concurrency control

## Quick Start

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Code Examples

### Basic Usage

```go
limiter := NewTokenBucketLimiter(2.0, 5)
middleware := NewRateLimitingMiddleware(limiter)

result, err := middleware.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Your prompt here",
})
```

### With Statistics

```go
stats := limiter.Stats()
fmt.Printf("Allowed: %d, Throttled: %d\n", stats.Allowed, stats.Throttled)
fmt.Printf("Wait time: %v\n", stats.WaitTime)
```

### Concurrent Requests

```go
limiter := NewConcurrentLimiter(3)
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()

        if err := limiter.Acquire(ctx); err != nil {
            return
        }
        defer limiter.Release()

        // Make AI request
    }()
}

wg.Wait()
```

## API Provider Limits

### OpenAI (GPT-4)
- Tier 1: 500 requests/minute
- Tier 2: 5,000 requests/minute
- Tier 3: 10,000 requests/minute

```go
// For Tier 1
limiter := NewSlidingWindowLimiter(500, 1*time.Minute)
```

### Anthropic (Claude)
- Free: 5 requests/minute
- Build: 50 requests/minute
- Scale: 1,000 requests/minute

```go
// For Build tier
limiter := NewSlidingWindowLimiter(50, 1*time.Minute)
```

### Google (Gemini)
- Free: 15 requests/minute
- Pay-as-you-go: 300-1000 requests/minute

```go
// For free tier
limiter := NewSlidingWindowLimiter(15, 1*time.Minute)
```

## Best Practices

1. **Match provider limits**: Set rates slightly below API limits
2. **Use burst for UX**: Allow small bursts for better user experience
3. **Add concurrency limits**: Protect your own resources
4. **Monitor statistics**: Track throttling rates
5. **Handle errors gracefully**: Retry with backoff on rate limit errors
6. **Per-user rate limiting**: Different limits for different users

## Testing

The example demonstrates:
1. Token bucket with burst handling
2. Sliding window with wait times
3. Concurrent request limiting
4. Combined rate limiters

## Advanced Patterns

### Per-User Rate Limiting

```go
type UserRateLimiter struct {
    limiters map[string]RateLimiter
    mu       sync.RWMutex
}

func (url *UserRateLimiter) GetLimiter(userID string) RateLimiter {
    url.mu.RLock()
    limiter, exists := url.limiters[userID]
    url.mu.RUnlock()

    if !exists {
        limiter = NewTokenBucketLimiter(10, 20)
        url.mu.Lock()
        url.limiters[userID] = limiter
        url.mu.Unlock()
    }

    return limiter
}
```

### Adaptive Rate Limiting

```go
type AdaptiveRateLimiter struct {
    limiter   *rate.Limiter
    errorRate float64
}

func (arl *AdaptiveRateLimiter) AdjustRate(err error) {
    if isRateLimitError(err) {
        // Reduce rate by 50%
        currentRate := arl.limiter.Limit()
        arl.limiter.SetLimit(currentRate * 0.5)
    } else if arl.errorRate < 0.01 {
        // Increase rate by 10%
        currentRate := arl.limiter.Limit()
        arl.limiter.SetLimit(currentRate * 1.1)
    }
}
```

### Distributed Rate Limiting (Redis)

```go
import "github.com/go-redis/redis_rate/v10"

limiter := redis_rate.NewLimiter(redisClient)

res, err := limiter.Allow(ctx, "api:user:123", redis_rate.PerMinute(100))
if err != nil {
    panic(err)
}

if res.Allowed == 0 {
    // Rate limited
    return
}
```

## Troubleshooting

### Still Getting 429 Errors

- Rate limits may be set too high
- Check for rate limits on other dimensions (tokens/minute)
- Add buffer: use 80-90% of actual limit

### High Wait Times

- Increase burst size for token bucket
- Reduce request rate
- Add caching to reduce requests

### Memory Usage

- Use distributed rate limiting (Redis)
- Clean up old limiters for inactive users
- Set expiration on rate limit data

## Related Examples

- [caching](../caching) - Reduce requests through caching
- [logging](../logging) - Track rate limit events
- [middleware](../../middleware) - Basic middleware patterns

## Resources

- [Token Bucket Algorithm](https://en.wikipedia.org/wiki/Token_bucket)
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
- [OpenAI Rate Limits](https://platform.openai.com/docs/guides/rate-limits)
- [Anthropic Rate Limits](https://docs.anthropic.com/claude/reference/rate-limits)
