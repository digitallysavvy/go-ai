package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling r78 b (xAI twin): TS reports service_tier, prompt_cache_key and
// safety_identifier when `!= null`, so a present empty string is reported
// in providerMetadata.xai; null is not.
func TestR78provbXAIResponsesEmptyEchoesReported(t *testing.T) {
	cases := map[string]struct {
		fields string
		want   map[string]interface{}
	}{
		"empty": {`"service_tier":"","prompt_cache_key":"","safety_identifier":""`, map[string]interface{}{"serviceTier": "", "promptCacheKey": "", "safetyIdentifier": ""}},
		"null":  {`"service_tier":null,"prompt_cache_key":null,"safety_identifier":null`, nil},
	}
	check := func(t *testing.T, pm map[string]interface{}, want map[string]interface{}) {
		t.Helper()
		for _, k := range []string{"serviceTier", "promptCacheKey", "safetyIdentifier"} {
			got, has := pm[k]
			w, wantHas := want[k]
			if has != wantHas || got != w {
				t.Fatalf("%s = %v (present %v), want %v (present %v); meta %v", k, got, has, w, wantHas, pm)
			}
		}
	}
	for name, tc := range cases {
		t.Run("generate/"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"grok-4",` + tc.fields + `,"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			}))
			defer srv.Close()
			m := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-4")
			r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			pm, _ := r.ProviderMetadata["xai"].(map[string]interface{})
			check(t, pm, tc.want)
		})
		t.Run("stream/"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":1,"status":"in_progress","model":"grok-4","output":[]}}` + "\n\n" +
					`data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"grok-4",` + tc.fields + `,"output":[]}}` + "\n\n"))
			}))
			defer srv.Close()
			m := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-4")
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			var meta map[string]map[string]interface{}
			finished := false
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeFinish {
					finished = true
					if len(c.ProviderMetadata) > 0 {
						if err := json.Unmarshal(c.ProviderMetadata, &meta); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if !finished {
				t.Fatal("no finish part")
			}
			check(t, meta["xai"], tc.want)
		})
	}
}

// TS xai-batch.ts convertTextBatchResponse: serviceTier when
// `response.service_tier != null`, so a present "" is reported.
func TestR78provbXAIBatchEmptyServiceTier(t *testing.T) {
	var resp xaiBatchTextResponseWire
	if err := json.Unmarshal([]byte(`{"id":"b1","created":1,"model":"grok-4","service_tier":"","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	r, berr := convertXAIBatchTextResponse(resp)
	if berr != nil {
		t.Fatal(berr)
	}
	pm, _ := r.ProviderMetadata["xai"].(map[string]interface{})
	if got, ok := pm["serviceTier"]; !ok || got != "" {
		t.Fatalf("serviceTier = %#v (present %v), want \"\"", got, ok)
	}
}

// TS resolves a provider tool's name with tools.find, so the first tool
// with the id names its output items.
func TestR78provbXAIProviderToolNameFirstWins(t *testing.T) {
	names := resolveProviderToolNames([]types.Tool{
		{Type: "provider", ProviderID: "xai.web_search", Name: "first"},
		{Type: "provider", ProviderID: "xai.web_search", Name: "second"},
	})
	if names["xai.web_search"] != "first" {
		t.Fatalf("names = %v, want the first tool's name", names)
	}
}
