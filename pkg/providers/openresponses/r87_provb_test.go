package openresponses

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r87provbStream(t *testing.T, events ...string) ([]*provider.StreamChunk, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Test", "1")
		for _, e := range events {
			_, _ = w.Write([]byte("data: " + e + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "requested-model")
	s, err := m.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
		out = append(out, c)
	}
}

// TS 4b16915083 (open-responses-language-model.test.ts "should forward
// server response metadata from the response.created event"): the
// response.created event enqueues response-metadata with the server id,
// created_at * 1000 and the served model id.
func TestR87provbResponseCreatedEmitsResponseMetadata(t *testing.T) {
	chunks, err := r87provbStream(t,
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_123","object":"response","created_at":1750000000,"status":"in_progress","model":"server-reported-model-id","output":[]}}`,
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var metas []*provider.ResponseMetadata
	var order []string
	for _, c := range chunks {
		order = append(order, string(c.Type))
		if c.Type == provider.ChunkTypeResponseMetadata {
			metas = append(metas, c.ResponseMetadata)
		}
	}
	// Go's leading header chunk (WithResponseMetadata), then the TS part.
	if len(metas) != 2 {
		t.Fatalf("response-metadata chunks = %d, want 2 (order %v)", len(metas), order)
	}
	got := metas[1]
	if got.ID != "resp_123" || got.ModelID != "server-reported-model-id" || !got.Timestamp.Equal(time.Unix(1750000000, 0)) {
		t.Fatalf("metadata = %+v", got)
	}
	if got.Headers["X-Test"] != "1" {
		t.Fatalf("headers = %v, want the response headers kept", got.Headers)
	}
	if have := strings.Join(order[:4], ","); have != "stream-start,response-metadata,response-metadata,text-start" {
		t.Fatalf("order = %s", have)
	}
}

// created_at absent is an Invalid Date in TS (no timestamp); null is the
// epoch (null * 1000 === 0); a missing response is the TypeError reading
// chunk.response.id, which errors the stream.
func TestR87provbResponseCreatedEdgeCases(t *testing.T) {
	chunks, err := r87provbStream(t, `{"type":"response.created","response":{"id":"r","model":"m"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var last *provider.ResponseMetadata
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata {
			last = c.ResponseMetadata
		}
	}
	if last == nil || last.ID != "r" || !last.Timestamp.IsZero() {
		t.Fatalf("absent created_at: %+v", last)
	}

	chunks, err = r87provbStream(t, `{"type":"response.created","response":{"id":"r","model":"m","created_at":null}}`)
	if err != nil {
		t.Fatal(err)
	}
	last = nil
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata {
			last = c.ResponseMetadata
		}
	}
	if last == nil || !last.Timestamp.Equal(time.UnixMilli(0)) {
		t.Fatalf("null created_at: %+v", last)
	}

	for raw, want := range map[string]string{
		`{"type":"response.created"}`:                 "Cannot read properties of undefined (reading 'id')",
		`{"type":"response.created","response":null}`: "Cannot read properties of null (reading 'id')",
	} {
		_, err := r87provbStream(t, raw)
		if err == nil || err.Error() != want {
			t.Fatalf("%s: err = %v, want %q", raw, err, want)
		}
	}
}
