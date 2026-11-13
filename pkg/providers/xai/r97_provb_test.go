package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97provbBatchFractionalUsageRawExact: TS convertXaiBatchTextUsage
// keeps `raw: usage` and the counts are z.number(), so a fractional count
// stays exact in Usage.Raw (Go's raw held the truncated retry value).
func TestR97provbBatchFractionalUsageRawExact(t *testing.T) {
	var resp xaiBatchTextResponseWire
	body := `{"choices":[{"message":{"role":"assistant","content":"hi"},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":1.5,"completion_tokens":2,"total_tokens":3.5}}`
	if err := providerutils.UnmarshalChunk([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result, berr := convertXAIBatchTextResponse(resp)
	if berr != nil {
		t.Fatal(berr)
	}
	if *result.Usage.InputTokens != 1 || result.Usage.Raw["prompt_tokens"] != 1.5 {
		t.Fatalf("input = %d, raw = %#v", *result.Usage.InputTokens, result.Usage.Raw)
	}
}
