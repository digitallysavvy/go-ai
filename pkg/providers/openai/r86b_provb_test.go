package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86bprovbRealtimeBearerTokenJSRegexp pins the realtime WebSocket auth
// to TS getOpenAIRealtimeConnection's /^bearer\s+(.+)$/i (V061 item 25): JS \s
// includes U+00A0, U+2003, U+FEFF and \v; JS . excludes U+2028 and \r. Each
// want was printed by Node 24.
func TestR86bprovbRealtimeBearerTokenJSRegexp(t *testing.T) {
	for _, tc := range []struct {
		value, token string
		ok           bool
	}{
		{"Bearer tok", "tok", true},
		{"bearer\ttok", "tok", true},
		{"Bearer\u00a0tok", "tok", true},
		{"Bearer\u2003tok", "tok", true},
		{"Bearer\u000btok", "tok", true},
		{"Bearer\ufefftok", "tok", true},
		{"Bearer\u0085tok", "", false},
		{"Bearer tok\u2028x", "", false},
		{"Bearer tok\rx", "", false},
		{"Bearer  ", " ", true},
		{"Bearer \u2028", "", false},
		{"Bearer", "", false},
		{"Bearer ", "", false},
		{"BEARER  a b ", "a b ", true},
		{"Bearer \u00a0 tok", "tok", true},
	} {
		protocols, headers := openAIRealtimeWSAuth(map[string]string{"Authorization": tc.value, "X": "y"})
		if !tc.ok {
			if len(protocols) != 1 || headers["Authorization"] != tc.value {
				t.Errorf("%q: protocols %q, headers %v; want no token, header kept", tc.value, protocols, headers)
			}
			continue
		}
		if len(protocols) != 2 || protocols[1] != "openai-insecure-api-key."+tc.token {
			t.Errorf("%q: protocols %q, want token %q", tc.value, protocols, tc.token)
		}
		if _, kept := headers["Authorization"]; kept || headers["X"] != "y" {
			t.Errorf("%q: headers %v", tc.value, headers)
		}
	}
}

// TestR86bprovbCompatProviderOptionsNameJSTrim pins TS
// providerOptionsName = provider.split('.')[0].trim() (JS trim: U+FEFF is
// trimmed, U+0085 is not) on the OpenAI-compatible chat and embedding paths;
// the embedding path did not trim at all.
func TestR86bprovbCompatProviderOptionsNameJSTrim(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	emb := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", Name: " deepinfra .embedding", OpenAICompatible: true}), "e")
	_, _ = emb.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"deepinfra": map[string]interface{}{"dimensions": float64(512)}},
	})
	if body["dimensions"] != float64(512) {
		t.Fatalf("embedding body = %#v, want dimensions 512 from providerOptions.deepinfra", body)
	}
	body = nil
	if _, err := emb.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"deepinfra": map[string]interface{}{"dimensions": "x"}},
	}); err == nil || body != nil {
		t.Fatalf("invalid providerOptions.deepinfra.dimensions: err %v, body %#v; want a validation error before the request", err, body)
	}

	for _, tc := range []struct {
		name, key string
		want      bool
	}{
		{"\ufeffacme.chat", "acme", true},
		{"acme\u0085.chat", "acme", false},
		{"acme\u0085.chat", "acme\u0085", true},
	} {
		chat := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", Name: tc.name, OpenAICompatible: true}), "m")
		_, _ = chat.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{tc.key: map[string]interface{}{"user": "u1"}},
		})
		if got := body["user"] == "u1"; got != tc.want {
			t.Errorf("provider %q, options key %q: user sent = %v, want %v (body %#v)", tc.name, tc.key, got, tc.want, body)
		}
	}
}
