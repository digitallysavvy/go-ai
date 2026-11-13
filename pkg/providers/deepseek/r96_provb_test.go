package deepseek

import "testing"

// TestR96provbItemMetadataJSONStringify: DeepSeek Responses item metadata
// is written like JSON.stringify: <, > and & stay literal (main ruling 4,
// write side).
func TestR96provbItemMetadataJSONStringify(t *testing.T) {
	if raw := deepseekItemIDMetadata("deepseek", "<a&b>"); string(raw) != `{"deepseek":{"itemId":"<a&b>"}}` {
		t.Fatalf("metadata = %s", raw)
	}
}
