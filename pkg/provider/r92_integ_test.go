package provider

import (
	"encoding/json"
	"testing"
)

// TS RealtimeUsage token counts are JS numbers: a fractional wire value is
// kept, not truncated (provb r92 known-difference proposal, fixed instead:
// the token fields are new in v0.6).
func TestR92integRealtimeUsageTokensKeepJSNumbers(t *testing.T) {
	var u RealtimeUsage
	if err := json.Unmarshal([]byte(`{"inputTokens":10.5,"outputTokens":3,"cachedInputAudioTokens":0.25}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.InputTokens == nil || *u.InputTokens != 10.5 || u.CachedInputAudioTokens == nil || *u.CachedInputAudioTokens != 0.25 {
		t.Fatalf("usage = %+v", u)
	}
	b, err := json.Marshal(u)
	if err != nil || string(b) != `{"inputTokens":10.5,"outputTokens":3,"cachedInputAudioTokens":0.25}` {
		t.Fatalf("marshal = %s %v", b, err)
	}
}
