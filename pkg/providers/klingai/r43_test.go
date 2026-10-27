package klingai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getHeaders merges the provider-configured headers (and the SDK user-agent
// suffix) into every request, the status poll included; provider headers
// override the generated Authorization and per-call headers override both.
func TestR43_ProviderHeadersReachStartAndStatus(t *testing.T) {
	seen := map[string]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method] = r.Header.Clone()
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"processing"}}`))
	}))
	defer server.Close()
	prov, err := New(Config{APIKey: "key", BaseURL: server.URL, Headers: map[string]string{"X-Proxy": "p", "Authorization": "Custom abc"}})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := newVideoModel(prov, "kling-v2.6-t2v")
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1, Headers: map[string]string{"X-Call": "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(start.Operation)}); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		h := seen[method]
		if h.Get("X-Proxy") != "p" || h.Get("Authorization") != "Custom abc" || !strings.Contains(h.Get("User-Agent"), "klingai") {
			t.Fatalf("%s headers = %#v", method, h)
		}
	}
	if seen[http.MethodPost].Get("X-Call") != "c" {
		t.Fatalf("call header missing: %#v", seen[http.MethodPost])
	}
}
