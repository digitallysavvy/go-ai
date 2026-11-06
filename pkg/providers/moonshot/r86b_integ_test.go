package moonshot

import (
	"net/http"
	"testing"
)

// cr-r86b-tools-to-provb 2: TS reads the msh-usage-cache-write-tokens-*
// headers through response headers (Latin-1 strings) and tests
// /^\d+$/.test(value.trim()): a raw 0xA0 byte is U+00A0 and trims away.
func TestR86bintegCacheWriteHeadersReadAsFetch(t *testing.T) {
	raw, ok := cacheWriteTokensByTTL(http.Header{"Msh-Usage-Cache-Write-Tokens-5m": {"\xa012"}})
	if !ok || string(raw) != `{"ttl1h":0,"ttl5m":12}` {
		t.Errorf("cacheWriteTokensByTTL = %s, %v; want ttl5m 12", raw, ok)
	}
}
