package huggingface

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var r79provbHFFracTime = time.UnixMilli(1700000000250)

// TS huggingface-responses-language-model.ts: created_at is z.number() and
// the timestamp new Date(created_at * 1000), in generate and on
// response.created; a fractional value keeps its milliseconds.
func TestR79provbHuggingFaceFractionalCreatedAt(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"r","model":"m","object":"response","created_at":1700000000.25,"status":"completed","error":null,"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto","tools":[],"temperature":1,"top_p":1,"usage":{"input_tokens":12,"output_tokens":25,"total_tokens":37},"output":[]}`))
		}))
		defer srv.Close()
		m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "m")
		r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		if !r.ResponseMetadata.Timestamp.Equal(r79provbHFFracTime) {
			t.Fatalf("timestamp = %v", r.ResponseMetadata.Timestamp)
		}
	})
	t.Run("stream", func(t *testing.T) {
		body := `data: {"type":"response.created","response":{"id":"resp_1","model":"m","created_at":1700000000.25}}` + "\n\n"
		s := newHFStream(io.NopCloser(strings.NewReader(body)), nil, "huggingface")
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
			if c.Type == provider.ChunkTypeResponseMetadata {
				meta = c.ResponseMetadata
			}
		}
		if meta == nil || !meta.Timestamp.Equal(r79provbHFFracTime) {
			t.Fatalf("metadata = %+v", meta)
		}
	})
}

// TS response.created timestamp: new Date(value.response.created_at * 1000)
// with no validation (the chunk union's loose fallback): absent is an
// Invalid Date (none), null is the epoch.
func TestR79provbHuggingFaceStreamCreatedAtPresence(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        time.Time
	}{
		{"absent", ``, time.Time{}},
		{"null", `,"created_at":null`, time.UnixMilli(0)},
		{"zero", `,"created_at":0`, time.UnixMilli(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `data: {"type":"response.created","response":{"id":"resp_1","model":"m"` + tc.field + `}}` + "\n\n"
			s := newHFStream(io.NopCloser(strings.NewReader(body)), nil, "huggingface")
			defer func() { _ = s.Close() }()
			var meta *provider.ResponseMetadata
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeResponseMetadata {
					meta = c.ResponseMetadata
				}
			}
			if meta == nil || meta.Timestamp.IsZero() != tc.want.IsZero() || !meta.Timestamp.Equal(tc.want) {
				t.Fatalf("metadata = %+v, want timestamp %v", meta, tc.want)
			}
		})
	}
}
