package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS google-batch.ts posts the resumable init with postJsonToApi, which
// sends {'Content-Type': 'application/json', ...headers}: a caller
// Content-Type replaces the JSON one.
func TestR99provgBatchInitCallerContentTypeWins(t *testing.T) {
	var initContentType []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
		initContentType = r.Header.Values("Content-Type")
		w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session-123")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/upload/v1beta/files/session-123", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"file":{"name":"files/batch-input"}}`))
	})
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	})

	b := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).ExperimentalBatch()
	longPrompt := strings.Repeat("a", googleBatchInlineCreationMaxBytes+1000)
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("big", "gemini-2.5-flash", longPrompt)},
		Headers:  map[string]string{"content-type": "application/vnd.custom+json"},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if len(initContentType) != 1 || initContentType[0] != "application/vnd.custom+json" {
		t.Fatalf("init content-type = %q, want the caller's", initContentType)
	}
}

// TS parseEvent falls back to {type: 'custom', rawType:
// String(Object.keys(data)[0])}: JS enumerates array-index keys first, a
// non-empty array or string has the key "0", and anything without keys is
// "undefined".
func TestR99provgRealtimeCustomRawTypeIsFirstJSKey(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-live-2.5-flash-preview")
	for raw, want := range map[string]string{
		`{"unknownEvent":{},"7":1,"2":true}`: "2",
		`{"zeta":1,"alpha":2}`:               "zeta",
		`{"a":1,"a":2}`:                      "a",
		`{}`:                                 "undefined",
		`["x"]`:                              "0",
		`[]`:                                 "undefined",
		`"text"`:                             "0",
		`""`:                                 "undefined",
		`5`:                                  "undefined",
		`true`:                               "undefined",
	} {
		events, err := m.ParseServerEvent(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if len(events) != 1 || events[0].Type != "custom" || events[0].RawType != want {
			t.Fatalf("%s: events = %+v, want custom rawType %q", raw, events, want)
		}
	}
}
