package responses

import (
	"encoding/json"
	"math"
	"testing"
)

// TestR99provbUsagePlatformInt: a fractional-path Responses usage count
// beyond the platform int is clamped, not an implementation-defined
// conversion (MinInt64 on amd64, wrapped on 32-bit targets).
func TestR99provbUsagePlatformInt(t *testing.T) {
	var u ResponsesAPIUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":1e19,"output_tokens":2.5,"input_tokens_details":{"cached_tokens":1e19}}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != math.MaxInt || u.OutputTokens != 2 || u.InputTokensDetails == nil || u.InputTokensDetails.CachedTokens != math.MaxInt {
		t.Errorf("usage = %+v", u)
	}
}
