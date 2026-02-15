package alibaba

// CacheStrategy represents the cache control strategy
type CacheStrategy string

const (
	// CacheStrategyDefault uses Alibaba's automatic caching
	CacheStrategyDefault CacheStrategy = "default"

	// CacheStrategyExplicit requires explicit cache marking
	CacheStrategyExplicit CacheStrategy = "explicit"
)

// CacheControl represents Alibaba's cache control structure
type CacheControl struct {
	Strategy CacheStrategy `json:"strategy"`
}

// GetCacheControl returns a cache control object for the given strategy
func GetCacheControl(strategy CacheStrategy) *CacheControl {
	if strategy == "" {
		strategy = CacheStrategyDefault
	}

	return &CacheControl{
		Strategy: strategy,
	}
}

// MessageCacheControl represents ephemeral cache marking for messages
type MessageCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// NewEphemeralCacheControl creates a cache control marker for explicit caching
func NewEphemeralCacheControl() *MessageCacheControl {
	return &MessageCacheControl{
		Type: "ephemeral",
	}
}
