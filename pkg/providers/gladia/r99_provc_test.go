package gladia

import "testing"

// TS getFromApi({credentialedOrigin}) withholds every caller and provider
// header (the provider's user agent included) from another origin.
func TestR99provcPollHeadersCrossOrigin(t *testing.T) {
	m := &TranscriptionModel{provider: New(Config{APIKey: "secret", BaseURL: "https://api.gladia.io/v2"}), modelID: "default"}
	if h := m.pollHeaders("https://results.example.com/r", "https://api.gladia.io/v2", map[string]string{"X-A": "b"}); h != nil {
		t.Fatalf("headers = %v, want none", h)
	}
	if h := m.pollHeaders("https://api.gladia.io/v2/pre-recorded/1", "https://api.gladia.io/v2", nil); h["x-gladia-key"] != "secret" {
		t.Fatalf("same origin headers = %v", h)
	}
}
