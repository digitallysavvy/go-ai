package anthropic

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getBatchHeaders is combineHeaders(config headers, call headers) and
// getFromApi then lower-cases the keys in insertion order, so a call header
// replaces a provider header of the same name whatever the spelling, on the
// results download as on every other batch request.
func TestR99provaBatchResultsCallHeaderWinsAnyCase(t *testing.T) {
	for _, tc := range []struct{ configKey, callKey string }{
		{"x-custom", "X-Custom"},
		{"X-Custom", "x-custom"},
		{"anthropic-version", "Anthropic-Version"},
	} {
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		var mu sync.Mutex
		var got []string
		mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":1,"expired":0},"created_at":"c","expires_at":"e","results_url":"` + srv.URL + `/results"}`))
		})
		mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			got = r.Header.Values(tc.callKey)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"custom_id":"x","result":{"type":"canceled"}}`))
		})
		b := New(Config{APIKey: "k", BaseURL: srv.URL, Headers: map[string]string{tc.configKey: "config"}}).ExperimentalBatch()
		stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1", Headers: map[string]string{tc.callKey: "call"}})
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		_ = stream.Close()
		srv.Close()
		mu.Lock()
		if len(got) != 1 || got[0] != "call" {
			t.Errorf("config %q / call %q: results request %s = %v, want only the call value", tc.configKey, tc.callKey, tc.callKey, got)
		}
		mu.Unlock()
	}
}

// TS sends a caller's Accept header as given (it sets none of its own); the
// stream's Go-only Accept: text/event-stream applies only when the call
// sets none, whatever the spelling.
func TestR99provaStreamCallerAcceptWins(t *testing.T) {
	for _, key := range []string{"Accept", "accept", "ACCEPT"} {
		var mu sync.Mutex
		var got []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			got = r.Header.Values("Accept")
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		}))
		lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
		if err != nil {
			t.Fatal(err)
		}
		s, err := lm.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{key: "application/x-custom"}})
		if err == nil {
			_ = s.Close()
		}
		srv.Close()
		mu.Lock()
		if len(got) != 1 || got[0] != "application/x-custom" {
			t.Errorf("call header %q: Accept = %v, want only the caller's", key, got)
		}
		mu.Unlock()
	}
}

// TS reads a content_block_delta index with z.number(); the "integer fields"
// known difference truncates a fractional stream block index in Go, so the
// delta is still delivered instead of failing the stream.
func TestR99provaFractionalDeltaIndexTruncated(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0.5,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	s := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	text := ""
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream failed: %v", err)
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
	}
	if text != "hi" {
		t.Fatalf("text = %q, want hi", text)
	}
}
