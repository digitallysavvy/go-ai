package harness

import "testing"

// zod `.optional()` rejects an explicit null at every nesting level of the
// finish reason and usage objects.
func TestDecodeStreamPartRejectsNestedNullOptionals(t *testing.T) {
	for _, in := range []string{
		`{"type":"finish","finishReason":{"unified":"stop","raw":null},"totalUsage":{"inputTokens":{},"outputTokens":{}}}`,
		`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{"total":null},"outputTokens":{}}}`,
		`{"type":"finish-step","finishReason":{"unified":"stop"},"usage":{"inputTokens":{},"outputTokens":{"reasoning":null}}}`,
		`{"type":"finish-step","finishReason":{"unified":"stop"},"usage":{"inputTokens":{},"outputTokens":{},"raw":null}}`,
	} {
		if _, err := DecodeStreamPart([]byte(in)); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}
