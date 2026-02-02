package anthropic

// CacheControl represents Anthropic's cache control configuration for tool definitions.
// When set on a tool, it enables prompt caching for that tool definition, reducing costs
// for requests that reuse the same tools.
//
// See: https://docs.anthropic.com/claude/docs/prompt-caching
type CacheControl struct {
	// Type is always "ephemeral" for prompt caching
	Type string `json:"type"`

	// TTL specifies the cache duration (optional)
	// Supported values: "5m" (5 minutes), "1h" (1 hour)
	// If omitted, defaults to "5m"
	TTL string `json:"ttl,omitempty"`
}

// ToolOptions contains Anthropic-specific options for tool definitions.
// These options are passed via Tool.ProviderOptions to enable provider-specific features.
//
// Example:
//
//	tool := types.Tool{
//	    Name:        "get_weather",
//	    Description: "Get weather information",
//	    Parameters:  schema,
//	    ProviderOptions: &anthropic.ToolOptions{
//	        CacheControl: &anthropic.CacheControl{
//	            Type: "ephemeral",
//	            TTL:  "5m",
//	        },
//	    },
//	}
type ToolOptions struct {
	// CacheControl enables prompt caching for this tool definition
	// When set, the tool definition will be cached for reuse across requests
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// WithCacheControl is a helper to create ToolOptions with cache control enabled.
//
// Parameters:
//   - ttl: Optional TTL ("5m" or "1h"). If empty, defaults to "5m"
//
// Returns:
//   - ToolOptions configured with cache control
//
// Example:
//
//	// Default 5-minute cache
//	options := anthropic.WithCacheControl("")
//
//	// 1-hour cache
//	options := anthropic.WithCacheControl("1h")
func WithCacheControl(ttl string) *ToolOptions {
	cacheControl := &CacheControl{
		Type: "ephemeral",
	}

	if ttl != "" {
		cacheControl.TTL = ttl
	}

	return &ToolOptions{
		CacheControl: cacheControl,
	}
}

// WithToolCache is a convenience function to enable caching on a tool.
//
// Parameters:
//   - ttl: Optional TTL ("5m" or "1h"). If empty, defaults to "5m"
//
// Returns:
//   - ToolOptions configured with ephemeral cache control
//
// Example:
//
//	tool.ProviderOptions = anthropic.WithToolCache("5m")
func WithToolCache(ttl string) *ToolOptions {
	return WithCacheControl(ttl)
}
