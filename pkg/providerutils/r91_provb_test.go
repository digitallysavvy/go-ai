package providerutils

import (
	"math"
	"strings"
	"testing"
)

// core2-03: TS createJsonLinesResponseHandler rejects a maxLineBytes that is
// not a positive safe integer (response-handler.ts:290, test :588 rejects
// Number.MAX_SAFE_INTEGER + 1), and math.MaxInt no longer overflows the
// scanner buffer size (makeslice panic).
func TestR91provbMaxLineBytesRange(t *testing.T) {
	if math.MaxInt == math.MaxInt64 {
		for _, v := range []int64{1 << 53, math.MaxInt64} {
			if _, err := (BatchResultDownloads{MaxLineBytes: int(v)}).ResolveMaxLineBytes(); err == nil || !strings.Contains(err.Error(), "maxLineBytes must be a positive safe integer.") {
				t.Fatalf("%d: err = %v", v, err)
			}
		}
		safe := int64(1<<53 - 1)
		if got, err := (BatchResultDownloads{MaxLineBytes: int(safe)}).ResolveMaxLineBytes(); err != nil || int64(got) != safe {
			t.Fatalf("MAX_SAFE_INTEGER: %d, %v", got, err)
		}
	}
	// MaxInt32 is a safe integer TS accepts; on 32-bit it is near MaxInt.
	for _, n := range []int{math.MaxInt32, math.MaxInt} {
		s := NewJSONLinesScanner(strings.NewReader("{\"a\":1}\n"), n)
		if !s.Scan() || s.Text() != `{"a":1}` {
			t.Fatalf("%d: scan failed: %v", n, s.Err())
		}
	}
}
