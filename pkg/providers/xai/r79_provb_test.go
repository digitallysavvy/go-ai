package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var r79provbFracTime = time.UnixMilli(1700000000250)

// TS xai-responses-api.ts created_at: z.number().nullish() on the response
// and its stream events: a fractional value is accepted and the timestamp is
// new Date(created_at * 1000).
func TestR79provbXAIResponsesFractionalCreatedAt(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1700000000.25,"status":"completed","model":"grok-4","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
		}))
		defer srv.Close()
		m := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-4")
		r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		if !r.ResponseMetadata.Timestamp.Equal(r79provbFracTime) {
			t.Fatalf("timestamp = %v", r.ResponseMetadata.Timestamp)
		}
	})
	t.Run("stream", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":1700000000.25,"status":"in_progress","model":"grok-4","output":[]}}` + "\n\n" +
				`data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_1","object":"response","created_at":1700000000.25,"status":"completed","model":"grok-4","output":[]}}` + "\n\n"))
		}))
		defer srv.Close()
		m := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-4")
		s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		var meta *provider.ResponseMetadata
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				t.Fatalf("unexpected error part: %+v", c)
			}
			if c.Type == provider.ChunkTypeResponseMetadata && meta == nil {
				meta = c.ResponseMetadata
			}
		}
		if meta == nil || meta.ID != "resp_1" || meta.ModelID != "grok-4" || !meta.Timestamp.Equal(r79provbFracTime) {
			t.Fatalf("metadata = %+v", meta)
		}
	})
}

// TS xai-batch.ts: created is z.number().nullish() and the result's response
// is getResponseMetadata(response), an object even when every field is
// absent. Files metadata createdAt is the wire number.
func TestR79provbXAIBatchFilesFractional(t *testing.T) {
	var resp xaiBatchTextResponseWire
	if err := json.Unmarshal([]byte(`{"id":"b1","created":1700000000.25,"model":"grok-4","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	r, berr := convertXAIBatchTextResponse(resp)
	if berr != nil {
		t.Fatal(berr)
	}
	if !r.ResponseMetadata.Timestamp.Equal(r79provbFracTime) {
		t.Fatalf("timestamp = %v", r.ResponseMetadata.Timestamp)
	}
	var bare xaiBatchTextResponseWire
	if err := json.Unmarshal([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`), &bare); err != nil {
		t.Fatal(err)
	}
	r, berr = convertXAIBatchTextResponse(bare)
	if berr != nil {
		t.Fatal(berr)
	}
	if r.ResponseMetadata == nil || !r.ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("response metadata = %+v, want an empty object", r.ResponseMetadata)
	}

	var f xaiFileResponse
	if err := json.Unmarshal([]byte(`{"id":"f","created_at":1700000000.25,"expires_at":5}`), &f); err != nil {
		t.Fatal(err)
	}
	meta := xaiFileMetadata(f)
	if meta["createdAt"] != 1700000000.25 || meta["expiresAt"] != int64(5) {
		t.Fatalf("meta = %#v", meta)
	}
	if got := xaiUnixSecondsToTime(f.CreatedAt); got == nil || !got.Equal(r79provbFracTime) {
		t.Fatalf("createdAt = %v", got)
	}
}
