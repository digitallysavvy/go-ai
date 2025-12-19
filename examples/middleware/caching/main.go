package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// CacheEntry represents a cached response
type CacheEntry struct {
	Result    *ai.GenerateTextResult
	Timestamp time.Time
	ExpiresAt time.Time
	Hits      int
}

// Cache interface for different caching strategies
type Cache interface {
	Get(key string) (*CacheEntry, bool)
	Set(key string, entry *CacheEntry)
	Delete(key string)
	Clear()
	Stats() CacheStats
}

// CacheStats tracks cache performance
type CacheStats struct {
	Hits             int
	Misses           int
	Evictions        int
	TotalTokensSaved int
	CostSaved        float64
}

// MemoryCache implements in-memory caching with TTL
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	stats   CacheStats
	ttl     time.Duration
	maxSize int
}

func NewMemoryCache(ttl time.Duration, maxSize int) *MemoryCache {
	cache := &MemoryCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

func (c *MemoryCache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		c.stats.Misses++
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		c.stats.Misses++
		return nil, false
	}

	// Update hit count
	entry.Hits++
	c.stats.Hits++
	c.stats.TotalTokensSaved += entry.Result.Usage.TotalTokens

	// Estimate cost saved (GPT-4 pricing: $0.03 input, $0.06 output per 1K tokens)
	costSaved := float64(entry.Result.Usage.InputTokens)*0.03/1000 +
		float64(entry.Result.Usage.OutputTokens)*0.06/1000
	c.stats.CostSaved += costSaved

	return entry, true
}

func (c *MemoryCache) Set(key string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if cache is full
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	entry.ExpiresAt = time.Now().Add(c.ttl)
	c.entries[key] = entry
}

func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

func (c *MemoryCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats
}

func (c *MemoryCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.entries {
		if oldestKey == "" || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.stats.Evictions++
	}
}

func (c *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.ExpiresAt) {
				delete(c.entries, key)
				c.stats.Evictions++
			}
		}
		c.mu.Unlock()
	}
}

// CachingMiddleware wraps AI operations with caching
type CachingMiddleware struct {
	cache     Cache
	keyFunc   func(ai.GenerateTextOptions) string
	cacheable func(ai.GenerateTextOptions) bool
}

func NewCachingMiddleware(cache Cache) *CachingMiddleware {
	return &CachingMiddleware{
		cache:     cache,
		keyFunc:   defaultKeyFunc,
		cacheable: defaultCacheable,
	}
}

func (m *CachingMiddleware) GenerateText(
	ctx context.Context,
	opts ai.GenerateTextOptions,
) (*ai.GenerateTextResult, error) {
	// Check if this request is cacheable
	if !m.cacheable(opts) {
		return ai.GenerateText(ctx, opts)
	}

	// Generate cache key
	key := m.keyFunc(opts)

	// Check cache
	if entry, hit := m.cache.Get(key); hit {
		fmt.Printf("  ✓ Cache HIT for key: %s\n", key[:12]+"...")
		return entry.Result, nil
	}

	fmt.Printf("  ✗ Cache MISS for key: %s\n", key[:12]+"...")

	// Cache miss - make actual request
	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Store in cache
	m.cache.Set(key, &CacheEntry{
		Result:    result,
		Timestamp: time.Now(),
		Hits:      0,
	})

	return result, nil
}

// defaultKeyFunc creates a cache key from request parameters
func defaultKeyFunc(opts ai.GenerateTextOptions) string {
	// Hash the prompt and key parameters
	h := sha256.New()
	h.Write([]byte(opts.Model.ModelID()))
	h.Write([]byte(opts.Prompt))
	h.Write([]byte(opts.System))

	if opts.Temperature != nil {
		h.Write([]byte(fmt.Sprintf("%f", *opts.Temperature)))
	}
	if opts.MaxTokens != nil {
		h.Write([]byte(fmt.Sprintf("%d", *opts.MaxTokens)))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// defaultCacheable determines if a request should be cached
func defaultCacheable(opts ai.GenerateTextOptions) bool {
	// Don't cache requests with tools (non-deterministic)
	if len(opts.Tools) > 0 {
		return false
	}

	// Don't cache streaming requests
	// (Note: In a real implementation, you'd check for streaming mode)

	// Cache requests with temperature 0 or 0.5 or lower (more deterministic)
	if opts.Temperature != nil && *opts.Temperature > 0.5 {
		return false
	}

	return true
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

	fmt.Println("=== Caching Middleware Example ===")

	// Example 1: Basic caching
	fmt.Println("1. Basic Caching (5 minute TTL)")
	fmt.Println("   " + string(make([]byte, 50, 50)))

	cache := NewMemoryCache(5*time.Minute, 100)
	middleware := NewCachingMiddleware(cache)

	temp := 0.0 // Deterministic for caching

	// First request (cache miss)
	fmt.Println("\n   First request:")
	result, err := middleware.GenerateText(ctx, ai.GenerateTextOptions{
		Model:       model,
		Prompt:      "What is the capital of France?",
		Temperature: &temp,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("   Response: %s\n", truncate(result.Text, 60))

	// Second request (cache hit)
	fmt.Println("\n   Second request (same prompt):")
	result, err = middleware.GenerateText(ctx, ai.GenerateTextOptions{
		Model:       model,
		Prompt:      "What is the capital of France?",
		Temperature: &temp,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("   Response: %s\n", truncate(result.Text, 60))

	// Example 2: Multiple requests
	fmt.Println("\n2. Multiple Requests with Caching")
	fmt.Println("   " + string(make([]byte, 50, 50)))

	questions := []string{
		"What is 2+2?",
		"Name the largest ocean",
		"What is 2+2?", // Repeat
		"Who invented the telephone?",
		"Name the largest ocean", // Repeat
		"What is the speed of light?",
		"What is 2+2?", // Repeat again
	}

	for i, question := range questions {
		fmt.Printf("\n   Request %d: %s\n", i+1, question)
		result, err = middleware.GenerateText(ctx, ai.GenerateTextOptions{
			Model:       model,
			Prompt:      question,
			Temperature: &temp,
		})
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}
		fmt.Printf("   Answer: %s\n", truncate(result.Text, 60))
		time.Sleep(200 * time.Millisecond)
	}

	// Show cache statistics
	stats := cache.Stats()
	fmt.Println("\n3. Cache Statistics")
	fmt.Println("   " + string(make([]byte, 50, 50)))
	fmt.Printf("   Cache Hits:          %d\n", stats.Hits)
	fmt.Printf("   Cache Misses:        %d\n", stats.Misses)
	fmt.Printf("   Hit Rate:            %.1f%%\n", float64(stats.Hits)/float64(stats.Hits+stats.Misses)*100)
	fmt.Printf("   Tokens Saved:        %d\n", stats.TotalTokensSaved)
	fmt.Printf("   Estimated Cost Saved: $%.4f\n", stats.CostSaved)

	// Example 4: Non-cacheable requests
	fmt.Println("\n4. Non-Cacheable Requests")
	fmt.Println("   " + string(make([]byte, 50, 50)))

	// High temperature (not cacheable)
	highTemp := 1.0
	fmt.Println("\n   High temperature request (temp=1.0):")
	result, err = middleware.GenerateText(ctx, ai.GenerateTextOptions{
		Model:       model,
		Prompt:      "Tell me a random fact",
		Temperature: &highTemp,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("   Response: %s\n", truncate(result.Text, 60))

	// With tools (not cacheable)
	fmt.Println("\n   Request with tools:")
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"temp": 72, "condition": "sunny"}, nil
		},
	}

	maxSteps := 3
	result, err = middleware.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather in Paris?",
		Tools:    []types.Tool{weatherTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("   Response: %s\n", truncate(result.Text, 60))

	// Final stats
	finalStats := cache.Stats()
	fmt.Println("\n5. Final Statistics")
	fmt.Println("   " + string(make([]byte, 50, 50)))
	fmt.Printf("   Total Requests:      %d\n", finalStats.Hits+finalStats.Misses)
	fmt.Printf("   Cache Hits:          %d\n", finalStats.Hits)
	fmt.Printf("   Cache Misses:        %d\n", finalStats.Misses)
	fmt.Printf("   Evictions:           %d\n", finalStats.Evictions)
	fmt.Printf("   Hit Rate:            %.1f%%\n", float64(finalStats.Hits)/float64(finalStats.Hits+finalStats.Misses)*100)
	fmt.Printf("   Tokens Saved:        %d\n", finalStats.TotalTokensSaved)
	fmt.Printf("   Estimated Cost Saved: $%.4f\n", finalStats.CostSaved)

	// Example 6: Cache inspection
	fmt.Println("\n6. Cache Management")
	fmt.Println("   " + string(make([]byte, 50, 50)))
	fmt.Printf("   Current cache size: %d entries\n", len(cache.entries))
	fmt.Println("   Clearing cache...")
	cache.Clear()
	fmt.Printf("   Cache size after clear: %d entries\n", len(cache.entries))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FileCacheadds persistence to the cache
type FileCache struct {
	memory   *MemoryCache
	filename string
}

func NewFileCache(filename string, ttl time.Duration, maxSize int) (*FileCache, error) {
	fc := &FileCache{
		memory:   NewMemoryCache(ttl, maxSize),
		filename: filename,
	}

	// Load existing cache from file
	if err := fc.load(); err != nil {
		log.Printf("Failed to load cache from file: %v", err)
	}

	return fc, nil
}

func (fc *FileCache) Get(key string) (*CacheEntry, bool) {
	return fc.memory.Get(key)
}

func (fc *FileCache) Set(key string, entry *CacheEntry) {
	fc.memory.Set(key, entry)
	fc.save()
}

func (fc *FileCache) Delete(key string) {
	fc.memory.Delete(key)
	fc.save()
}

func (fc *FileCache) Clear() {
	fc.memory.Clear()
	fc.save()
}

func (fc *FileCache) Stats() CacheStats {
	return fc.memory.Stats()
}

func (fc *FileCache) load() error {
	data, err := os.ReadFile(fc.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet
		}
		return err
	}

	return json.Unmarshal(data, &fc.memory.entries)
}

func (fc *FileCache) save() error {
	data, err := json.Marshal(fc.memory.entries)
	if err != nil {
		return err
	}

	return os.WriteFile(fc.filename, data, 0644)
}
