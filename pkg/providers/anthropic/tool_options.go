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
	// CacheControl enables prompt caching for this tool definition.
	// When set, the tool definition will be cached for reuse across requests.
	CacheControl *CacheControl `json:"cache_control,omitempty"`

	// EagerInputStreaming enables eager (larger-chunk) streaming of tool input deltas
	// for this custom function tool. When true, Anthropic streams tool input in larger
	// batches rather than byte-by-byte, improving streaming responsiveness.
	//
	// Only applies to custom function tools. Do NOT set on provider tools
	// (web_search_20260209, web_fetch_20260209, etc.).
	//
	// When enabled, the stream emits tool-input-start, tool-input-delta, and
	// tool-input-end chunks alongside the final tool-call chunk.
	EagerInputStreaming *bool `json:"eager_input_streaming,omitempty"`

	// DeferLoading marks this tool for deferred loading when used alongside a tool search tool.
	// When true, Claude does not load this tool's full definition upfront; instead it discovers
	// and loads the tool on demand via tool_search.
	//
	// Use with AnthropicTools.ToolSearchBm2520251119 or AnthropicTools.ToolSearchRegex20251119.
	// Serialized as defer_loading in the API request.
	DeferLoading *bool `json:"defer_loading,omitempty"`

	// AllowedCallers restricts which tool types can invoke this tool programmatically.
	// Valid values: "direct", "code_execution_20250825", "code_execution_20260120"
	//
	// When set, the anthropic-beta: advanced-tool-use-2025-11-20 header is automatically
	// injected. Serialized as allowed_callers in the API request.
	AllowedCallers []string `json:"allowed_callers,omitempty"`
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
