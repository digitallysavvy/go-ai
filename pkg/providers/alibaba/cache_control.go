package alibaba

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// maxCacheBreakpoints is the maximum number of cache markers Alibaba allows per request.
const maxCacheBreakpoints = 4

// MessageCacheControl represents ephemeral cache marking for messages.
// It serializes to {"type": "ephemeral"} on the wire.
type MessageCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// NewEphemeralCacheControl creates a cache control marker for explicit caching.
func NewEphemeralCacheControl() *MessageCacheControl {
	return &MessageCacheControl{Type: "ephemeral"}
}

// CacheControlValidator tracks how many cache breakpoints have been applied in a
// single request and enforces the provider limit of 4. It mirrors the TS
// CacheControlValidator from get-cache-control.ts.
type CacheControlValidator struct {
	breakpointCount int
	warnings        []types.Warning
	// perPart selects the TS behavior: cache markers come from each
	// message's / part's providerOptions.alibaba.cacheControl. The default
	// (NewCacheControlValidator) is the older Go call-level mode, where a
	// providerOptions.alibaba.cacheControl on the call marks every message.
	perPart bool
}

// NewPartCacheControlValidator creates a validator for TS-style per-message and
// per-part cache control (providerOptions.alibaba.cacheControl on a message,
// content part or tool result).
func NewPartCacheControlValidator() *CacheControlValidator {
	return &CacheControlValidator{perPart: true}
}

// GetCacheControlFromOptions mirrors TS CacheControlValidator.getCacheControl:
// it reads alibaba.cacheControl (or alibaba.cache_control) from the given
// provider options and passes the value through. A falsy value (null, false,
// 0, "") means no marker. Every marker beyond the fourth adds a warning.
func (v *CacheControlValidator) GetCacheControlFromOptions(providerOptions map[string]interface{}) interface{} {
	alibaba, _ := providerOptions["alibaba"].(map[string]interface{})
	value, ok := alibaba["cacheControl"]
	if !ok || value == nil {
		value = alibaba["cache_control"]
	}
	if !jsTruthy(value) {
		return nil
	}
	v.breakpointCount++
	if v.breakpointCount > maxCacheBreakpoints {
		v.warnings = append(v.warnings, types.Warning{
			Type:    "other",
			Message: "Max breakpoint limit exceeded. Only the last 4 cache markers will take effect.",
		})
	}
	return value
}

func jsTruthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	return true
}

// NewCacheControlValidator creates a new validator for a single request.
func NewCacheControlValidator() *CacheControlValidator {
	return &CacheControlValidator{}
}

// GetCacheControl returns an ephemeral cache marker if the breakpoint limit has not
// been exceeded, or nil if adding another block would pass the limit (4). A warning is added on
// the first call that exceeds the limit.
func (v *CacheControlValidator) GetCacheControl() *MessageCacheControl {
	v.breakpointCount++
	if v.breakpointCount > maxCacheBreakpoints {
		if v.breakpointCount == maxCacheBreakpoints+1 {
			// Emit the warning exactly once
			v.warnings = append(v.warnings, types.Warning{
				Type:    "other",
				Message: "Alibaba: max cache breakpoint limit (4) exceeded. Only the first 4 cache markers will take effect.",
			})
		}
		return nil
	}
	return NewEphemeralCacheControl()
}

// Warnings returns any warnings accumulated during cache control application.
func (v *CacheControlValidator) Warnings() []types.Warning {
	return v.warnings
}
