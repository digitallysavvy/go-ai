package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94integBatchTimestampsISOString: TS convertUnixTimestamp
// (openai-batch.ts) is toISOString after a range guard: expanded years
// outside 0..9999, and an out-of-range time left out (cr-r94-carry-to-provb).
func TestR94integBatchTimestampsISOString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/files":
			_, _ = w.Write([]byte(`{"id":"file-input","object":"file","filename":"batch.jsonl","purpose":"batch","expires_at":253402300800}`))
		case "/batches":
			_, _ = w.Write([]byte(`{"id":"batch_123","status":"validating","created_at":-62198755200,"expires_at":10000000000000,"request_counts":{"total":1,"completed":0,"failed":0}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	result, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{openaiTextBatchRequest("a", "gpt-5.6", "Hi")},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	meta, _ := result.ProviderMetadata["openai"].(map[string]interface{})
	if meta["inputFileExpiresAt"] != "+010000-01-01T00:00:00.000Z" {
		t.Errorf("inputFileExpiresAt = %v", meta["inputFileExpiresAt"])
	}
	if result.CreatedAt != "-000001-01-01T00:00:00.000Z" {
		t.Errorf("CreatedAt = %q", result.CreatedAt)
	}
	if result.ExpiresAt != "" {
		t.Errorf("ExpiresAt = %q, want it left out (Invalid Date)", result.ExpiresAt)
	}
}

// TestR94integLiveURLProtocolSetter: TS sets url.protocol (not the raw
// scheme): a port equal to the new scheme's default is dropped and a
// non-special scheme is left as is. Expected from Node 24.
func TestR94integLiveURLProtocolSetter(t *testing.T) {
	for base, want := range map[string]string{
		"ws://h:443/v1":   "wss://h/v1/live/sessions",
		"foo://h/v1":      "foo://h/v1/live/sessions",
		"http://h:443/v1": "ws://h:443/v1/live/sessions",
	} {
		live, err := NewRealtimeModelLive(New(Config{APIKey: "k", BaseURL: base}), "gpt-live-1").GetServerWebSocketConfig()
		if err != nil || live.URL != want {
			t.Errorf("base %s: live url = %q, %v; want %s", base, live.URL, err, want)
		}
	}
}
