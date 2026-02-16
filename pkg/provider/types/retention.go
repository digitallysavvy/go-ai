package types

// RetentionSettings controls what data is retained from LLM requests/responses.
// This is useful for reducing memory consumption and respecting privacy.
//
// Note: In the TypeScript SDK, this is called "experimental_include" with
// boolean flags. In Go, we use pointer booleans where nil = default (retain).
//
// Fields:
//   - RequestBody: nil = retain (default), false = exclude, true = explicitly retain
//   - ResponseBody: nil = retain (default), false = exclude, true = explicitly retain
//
// Example:
//
//	retention := &types.RetentionSettings{
//	    RequestBody:  BoolPtr(false),  // Don't retain request
//	    ResponseBody: BoolPtr(false),  // Don't retain response
//	}
type RetentionSettings struct {
	// RequestBody controls whether to retain the request body in results.
	// The request body can be large when sending images or files.
	// nil = default (retain), false = exclude, true = explicitly retain
	RequestBody *bool

	// ResponseBody controls whether to retain the response body in results.
	// The response body can be large for image/audio generation.
	// nil = default (retain), false = exclude, true = explicitly retain
	ResponseBody *bool
}

// ShouldRetainRequestBody returns whether to retain the request body.
// Returns true if RequestBody is nil or true (default behavior).
func (r *RetentionSettings) ShouldRetainRequestBody() bool {
	if r == nil || r.RequestBody == nil {
		return true // default: retain
	}
	return *r.RequestBody
}

// ShouldRetainResponseBody returns whether to retain the response body.
// Returns true if ResponseBody is nil or true (default behavior).
func (r *RetentionSettings) ShouldRetainResponseBody() bool {
	if r == nil || r.ResponseBody == nil {
		return true // default: retain
	}
	return *r.ResponseBody
}

// BoolPtr is a helper function for creating bool pointers.
// Useful when setting retention settings.
//
// Example:
//
//	retention := &types.RetentionSettings{
//	    RequestBody:  types.BoolPtr(false),
//	    ResponseBody: types.BoolPtr(false),
//	}
func BoolPtr(b bool) *bool {
	return &b
}
