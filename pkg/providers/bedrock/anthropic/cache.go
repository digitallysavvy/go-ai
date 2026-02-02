package anthropic

// BedrockCacheTTL represents cache duration options for Bedrock prompt caching.
//
// Supported values:
//   - "5m": 5-minute TTL (default, supported by all caching-capable models)
//   - "1h": 1-hour TTL (supported by Claude Opus 4.5, Claude Haiku 4.5, Claude Sonnet 4.5 v2)
//
// See: https://docs.aws.amazon.com/bedrock/latest/userguide/prompt-caching.html
type BedrockCacheTTL string

const (
	// CacheTTL5Minutes represents a 5-minute cache TTL (default)
	// Supported by all caching-capable Claude models on Bedrock
	CacheTTL5Minutes BedrockCacheTTL = "5m"

	// CacheTTL1Hour represents a 1-hour cache TTL
	// Supported by Claude 4.5 Sonnet v2, Claude 4.5 Opus, and Claude 4.5 Haiku
	// Model IDs:
	//   - us.anthropic.claude-4-5-sonnet-v2:0
	//   - us.anthropic.claude-4-5-opus-20250514:0
	//   - us.anthropic.claude-4-5-haiku-20250510:0
	CacheTTL1Hour BedrockCacheTTL = "1h"
)

// BedrockCachePoint marks a position for prompt caching with optional TTL.
// Cache points indicate to Bedrock where to cache content for reuse across requests.
//
// Cache points can be inserted:
//   - After system messages
//   - After tool definitions
//   - Within user/assistant message content
type BedrockCachePoint struct {
	// Type is always "default" for standard caching
	Type string `json:"type"`

	// TTL specifies the cache duration
	// If nil, uses the default 5-minute TTL
	TTL *BedrockCacheTTL `json:"ttl,omitempty"`
}

// CreateBedrockCachePoint creates a cache point with an optional TTL.
//
// Parameters:
//   - ttl: Optional cache TTL. If nil, uses default 5-minute TTL
//
// Returns:
//   - BedrockCachePoint configured with the specified TTL
//
// Example:
//
//	// Default 5-minute cache
//	cachePoint := CreateBedrockCachePoint(nil)
//
//	// 1-hour cache
//	ttl := CacheTTL1Hour
//	cachePoint := CreateBedrockCachePoint(&ttl)
func CreateBedrockCachePoint(ttl *BedrockCacheTTL) BedrockCachePoint {
	return BedrockCachePoint{
		Type: "default",
		TTL:  ttl,
	}
}

// CacheConfig specifies how and where to insert cache points in requests.
// This configuration is used to automatically insert cache points at strategic
// positions to optimize caching behavior.
type CacheConfig struct {
	// TTL for all cache points created by this configuration
	// If nil, uses default 5-minute TTL
	TTL *BedrockCacheTTL

	// CacheSystem enables automatic cache point insertion after system messages
	// Useful when you have large system prompts that don't change frequently
	CacheSystem bool

	// CacheTools enables automatic cache point insertion after tool definitions
	// Useful when you have many tools that remain constant across requests
	CacheTools bool

	// CacheMessageIndices specifies message indices where cache points should be inserted
	// Indices are 0-based and indicate positions after which to insert cache points
	// Useful for caching large documents or context within messages
	CacheMessageIndices []int
}

// WithCacheTTL returns a function that sets the cache TTL in CacheConfig
func WithCacheTTL(ttl BedrockCacheTTL) func(*CacheConfig) {
	return func(c *CacheConfig) {
		c.TTL = &ttl
	}
}

// WithSystemCache returns a function that enables system message caching
func WithSystemCache() func(*CacheConfig) {
	return func(c *CacheConfig) {
		c.CacheSystem = true
	}
}

// WithToolCache returns a function that enables tool caching
func WithToolCache() func(*CacheConfig) {
	return func(c *CacheConfig) {
		c.CacheTools = true
	}
}

// WithMessageCacheIndices returns a function that sets message cache indices
func WithMessageCacheIndices(indices ...int) func(*CacheConfig) {
	return func(c *CacheConfig) {
		c.CacheMessageIndices = indices
	}
}

// NewCacheConfig creates a new CacheConfig with the specified options
//
// Example:
//
//	config := NewCacheConfig(
//	    WithCacheTTL(CacheTTL1Hour),
//	    WithSystemCache(),
//	    WithToolCache(),
//	)
func NewCacheConfig(opts ...func(*CacheConfig)) *CacheConfig {
	config := &CacheConfig{}
	for _, opt := range opts {
		opt(config)
	}
	return config
}
