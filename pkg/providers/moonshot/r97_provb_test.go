package moonshot

import (
	"net/http"
	"testing"
)

// TestR97provbCacheWriteTokensBeyondInt64: TS parseTokenCountHeader reads a
// digit run with Number(), so a count beyond int64 is the JS number (1e20
// serializes as 100000000000000000000); Go's ParseInt failed and dropped it.
func TestR97provbCacheWriteTokensBeyondInt64(t *testing.T) {
	raw, ok := cacheWriteTokensByTTL(http.Header{"Msh-Usage-Cache-Write-Tokens-5m": {"100000000000000000000"}})
	if !ok || string(raw) != `{"ttl1h":0,"ttl5m":100000000000000000000}` {
		t.Fatalf("cacheWriteTokensByTTL = %s, %v", raw, ok)
	}
	raw, ok = cacheWriteTokensByTTL(http.Header{"Msh-Usage-Cache-Write-Tokens-1h": {"7"}})
	if !ok || string(raw) != `{"ttl1h":7,"ttl5m":0}` {
		t.Fatalf("cacheWriteTokensByTTL = %s, %v", raw, ok)
	}
}
