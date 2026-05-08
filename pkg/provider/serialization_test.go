package provider

import "testing"

func TestSerializableConfigDropsAuthHeadersAndHTTPClient(t *testing.T) {
	type cfg struct {
		APIKey     string
		BaseURL    string
		Headers    map[string]string
		HTTPClient interface{}
	}

	got := SerializableConfig(cfg{
		APIKey:     "secret",
		BaseURL:    "https://example.com",
		Headers:    map[string]string{"Authorization": "Bearer secret"},
		HTTPClient: "client",
	})

	if got["BaseURL"] != "https://example.com" {
		t.Fatalf("BaseURL not preserved: %#v", got)
	}
	for _, key := range []string{"APIKey", "Headers", "HTTPClient"} {
		if _, ok := got[key]; ok {
			t.Fatalf("%s should be removed from serialized config: %#v", key, got)
		}
	}
}
