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
}

// NewCacheControlValidator creates a new validator for a single request.
func NewCacheControlValidator() *CacheControlValidator {
	return &CacheControlValidator{}
}

// GetCacheControl returns an ephemeral cache marker if the breakpoint limit has not
// been exceeded, or nil if the limit (4) would be exceeded. A warning is added on
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
