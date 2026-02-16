package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"golang.org/x/time/rate"
)

// RateLimiter interface for different rate limiting strategies
type RateLimiter interface {
	Allow() bool
	Wait(ctx context.Context) error
	Stats() RateLimitStats
}

// RateLimitStats tracks rate limiter performance
type RateLimitStats struct {
	Allowed    int
	Throttled  int
	WaitTime   time.Duration
	TotalCalls int
}

// TokenBucketLimiter implements token bucket algorithm
type TokenBucketLimiter struct {
	limiter *rate.Limiter
	mu      sync.Mutex
	stats   RateLimitStats
}

func NewTokenBucketLimiter(requestsPerSecond float64, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), burst),
	}
}

func (tbl *TokenBucketLimiter) Allow() bool {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	tbl.stats.TotalCalls++

	if tbl.limiter.Allow() {
		tbl.stats.Allowed++
		return true
	}

	tbl.stats.Throttled++
	return false
}

func (tbl *TokenBucketLimiter) Wait(ctx context.Context) error {
	tbl.mu.Lock()
	tbl.stats.TotalCalls++
	tbl.mu.Unlock()

	start := time.Now()
	err := tbl.limiter.Wait(ctx)

	tbl.mu.Lock()
	tbl.stats.WaitTime += time.Since(start)
	if err == nil {
		tbl.stats.Allowed++
	}
	tbl.mu.Unlock()

	return err
}

func (tbl *TokenBucketLimiter) Stats() RateLimitStats {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()
	return tbl.stats
}

// SlidingWindowLimiter tracks requests in a sliding time window
type SlidingWindowLimiter struct {
	mu          sync.Mutex
	requests    []time.Time
	maxRequests int
	window      time.Duration
	stats       RateLimitStats
}

func NewSlidingWindowLimiter(maxRequests int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		requests:    make([]time.Time, 0),
		maxRequests: maxRequests,
		window:      window,
	}
}

func (swl *SlidingWindowLimiter) Allow() bool {
	swl.mu.Lock()
	defer swl.mu.Unlock()

	swl.stats.TotalCalls++
	now := time.Now()

	// Remove old requests outside the window
	cutoff := now.Add(-swl.window)
	validRequests := make([]time.Time, 0)
	for _, req := range swl.requests {
		if req.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}
	swl.requests = validRequests

	// Check if we're under the limit
	if len(swl.requests) < swl.maxRequests {
		swl.requests = append(swl.requests, now)
		swl.stats.Allowed++
		return true
	}

	swl.stats.Throttled++
	return false
}

func (swl *SlidingWindowLimiter) Wait(ctx context.Context) error {
	for {
		if swl.Allow() {
			return nil
		}

		// Calculate wait time
		swl.mu.Lock()
		if len(swl.requests) > 0 {
			oldestRequest := swl.requests[0]
			waitTime := swl.window - time.Since(oldestRequest)
			swl.mu.Unlock()

			if waitTime > 0 {
				select {
				case <-time.After(waitTime):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		} else {
			swl.mu.Unlock()
		}
	}
}

func (swl *SlidingWindowLimiter) Stats() RateLimitStats {
	swl.mu.Lock()
	defer swl.mu.Unlock()
	return swl.stats
}

// ConcurrentLimiter limits concurrent requests
type ConcurrentLimiter struct {
	mu            sync.Mutex
	semaphore     chan struct{}
	maxConcurrent int
	stats         RateLimitStats
	active        int
}

func NewConcurrentLimiter(maxConcurrent int) *ConcurrentLimiter {
	return &ConcurrentLimiter{
		semaphore:     make(chan struct{}, maxConcurrent),
		maxConcurrent: maxConcurrent,
	}
}

func (cl *ConcurrentLimiter) Acquire(ctx context.Context) error {
	cl.mu.Lock()
	cl.stats.TotalCalls++
	cl.mu.Unlock()

	select {
	case cl.semaphore <- struct{}{}:
		cl.mu.Lock()
		cl.active++
		cl.stats.Allowed++
		cl.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (cl *ConcurrentLimiter) Release() {
	<-cl.semaphore
	cl.mu.Lock()
	cl.active--
	cl.mu.Unlock()
}

func (cl *ConcurrentLimiter) Allow() bool {
	select {
	case cl.semaphore <- struct{}{}:
		cl.mu.Lock()
		cl.active++
		cl.stats.Allowed++
		cl.stats.TotalCalls++
		cl.mu.Unlock()
		return true
	default:
		cl.mu.Lock()
		cl.stats.Throttled++
		cl.stats.TotalCalls++
		cl.mu.Unlock()
		return false
	}
}

func (cl *ConcurrentLimiter) Wait(ctx context.Context) error {
	return cl.Acquire(ctx)
}

func (cl *ConcurrentLimiter) Stats() RateLimitStats {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.stats
}

// RateLimitingMiddleware wraps AI operations with rate limiting
type RateLimitingMiddleware struct {
	limiter RateLimiter
}

func NewRateLimitingMiddleware(limiter RateLimiter) *RateLimitingMiddleware {
	return &RateLimitingMiddleware{limiter: limiter}
}

func (m *RateLimitingMiddleware) GenerateText(
	ctx context.Context,
	opts ai.GenerateTextOptions,
) (*ai.GenerateTextResult, error) {
	// Wait for rate limiter
	if err := m.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}

	// Execute request
	return ai.GenerateText(ctx, opts)
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Rate Limiting Middleware Example ===")

	// Example 1: Token Bucket Rate Limiting
	fmt.Println("1. Token Bucket Rate Limiting (2 requests/sec, burst 3)")
	fmt.Println("   " + string(make([]byte, 60, 60)))

	limiter := NewTokenBucketLimiter(2.0, 3)
	middleware := NewRateLimitingMiddleware(limiter)

	// Burst of 5 requests
	fmt.Println("\n   Sending 5 rapid requests:")
	for i := 1; i <= 5; i++ {
		start := time.Now()
		result, err := middleware.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: fmt.Sprintf("Count to %d", i),
		})
		duration := time.Since(start)

		if err != nil {
			log.Printf("   Request %d failed: %v", i, err)
			continue
		}

		fmt.Printf("   Request %d: %dms (response: %s)\n",
			i, duration.Milliseconds(), truncate(result.Text, 30))
	}

	stats := limiter.Stats()
	fmt.Printf("\n   Stats: %d allowed, %d throttled, %.2fs total wait\n",
		stats.Allowed, stats.Throttled, stats.WaitTime.Seconds())

	// Example 2: Sliding Window Rate Limiting
	fmt.Println("\n2. Sliding Window (3 requests per 10 seconds)")
	fmt.Println("   " + string(make([]byte, 60, 60)))

	windowLimiter := NewSlidingWindowLimiter(3, 10*time.Second)
	windowMiddleware := NewRateLimitingMiddleware(windowLimiter)

	fmt.Println("\n   First 3 requests (should succeed):")
	for i := 1; i <= 3; i++ {
		reqStart := time.Now()
		_, err := windowMiddleware.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: fmt.Sprintf("What is %d + %d?", i, i),
		})
		reqDuration := time.Since(reqStart)

		if err != nil {
			log.Printf("   Request %d failed: %v", i, err)
			continue
		}

		fmt.Printf("   Request %d: %dms ✓\n", i, reqDuration.Milliseconds())
	}

	fmt.Println("\n   4th request (should wait ~10 seconds):")
	startTime := time.Now()
	_, err = windowMiddleware.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is 4 + 4?",
	})
	waitDuration := time.Since(startTime)

	if err != nil {
		log.Printf("   Request failed: %v", err)
	} else {
		fmt.Printf("   Request completed after waiting %ds ✓\n", int(waitDuration.Seconds()))
	}

	windowStats := windowLimiter.Stats()
	fmt.Printf("\n   Stats: %d allowed, %d throttled\n",
		windowStats.Allowed, windowStats.Throttled)

	// Example 3: Concurrent Request Limiting
	fmt.Println("\n3. Concurrent Request Limiting (max 2 concurrent)")
	fmt.Println("   " + string(make([]byte, 60, 60)))

	concurrentLimiter := NewConcurrentLimiter(2)
	var wg sync.WaitGroup

	fmt.Println("\n   Starting 5 concurrent requests:")
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()

			// Acquire slot
			if err := concurrentLimiter.Acquire(ctx); err != nil {
				fmt.Printf("   Request %d: failed to acquire ✗\n", requestID)
				return
			}
			defer concurrentLimiter.Release()

			start := time.Now()
			_, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
				Model:  model,
				Prompt: fmt.Sprintf("What is the number %d?", requestID),
			})
			duration := time.Since(start)

			if err != nil {
				fmt.Printf("   Request %d: error after %dms ✗\n",
					requestID, duration.Milliseconds())
			} else {
				fmt.Printf("   Request %d: completed in %dms ✓\n",
					requestID, duration.Milliseconds())
			}
		}(i)
	}

	wg.Wait()

	concurrentStats := concurrentLimiter.Stats()
	fmt.Printf("\n   Stats: %d allowed, %d throttled\n",
		concurrentStats.Allowed, concurrentStats.Throttled)

	// Example 4: Combined Rate Limiters
	fmt.Println("\n4. Combined Rate Limiters (token bucket + concurrent)")
	fmt.Println("   " + string(make([]byte, 60, 60)))

	combinedLimiter := NewCombinedLimiter(
		NewTokenBucketLimiter(1.0, 2), // 1 req/sec, burst 2
		NewConcurrentLimiter(2),       // max 2 concurrent
	)

	fmt.Println("\n   Sending 3 concurrent bursts:")
	var wg2 sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg2.Add(1)
		go func(requestID int) {
			defer wg2.Done()

			start := time.Now()
			if err := combinedLimiter.Wait(ctx); err != nil {
				fmt.Printf("   Request %d: rate limited ✗\n", requestID)
				return
			}

			_, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
				Model:  model,
				Prompt: fmt.Sprintf("Say hello %d", requestID),
			})

			if err != nil {
				fmt.Printf("   Request %d: error ✗\n", requestID)
			} else {
				fmt.Printf("   Request %d: completed in %dms ✓\n",
					requestID, time.Since(start).Milliseconds())
			}

			// Release concurrent limiter if it has Release method
			if cl, ok := combinedLimiter.limiters[1].(*ConcurrentLimiter); ok {
				cl.Release()
			}
		}(i)
	}

	wg2.Wait()

	fmt.Println("\n5. Final Summary")
	fmt.Println("   " + string(make([]byte, 60, 60)))
	fmt.Println("   Rate limiting helps:")
	fmt.Println("   • Stay within API rate limits")
	fmt.Println("   • Prevent cost overruns")
	fmt.Println("   • Handle traffic spikes gracefully")
	fmt.Println("   • Protect downstream services")
}

// CombinedLimiter combines multiple rate limiters
type CombinedLimiter struct {
	limiters []RateLimiter
}

func NewCombinedLimiter(limiters ...RateLimiter) *CombinedLimiter {
	return &CombinedLimiter{limiters: limiters}
}

func (cl *CombinedLimiter) Allow() bool {
	for _, limiter := range cl.limiters {
		if !limiter.Allow() {
			return false
		}
	}
	return true
}

func (cl *CombinedLimiter) Wait(ctx context.Context) error {
	for _, limiter := range cl.limiters {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (cl *CombinedLimiter) Stats() RateLimitStats {
	// Aggregate stats from all limiters
	var combined RateLimitStats
	for _, limiter := range cl.limiters {
		stats := limiter.Stats()
		combined.Allowed += stats.Allowed
		combined.Throttled += stats.Throttled
		combined.WaitTime += stats.WaitTime
		combined.TotalCalls += stats.TotalCalls
	}
	return combined
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
