package openresponses

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading;
// frames after it count and the finish part comes once, at the end of the
// body (r78 provb change request, coordinator ruling a).
func TestR78integopenresponsesReadsPastDone(t *testing.T) {
	body := `data: {"type":"response.output_text.delta","delta":"Hello"}` + "\n\n" +
		"data: [DONE]\n\n" +
		`data: {"type":"response.output_text.delta","delta":" world"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}` + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil)
	defer func() { _ = s.Close() }()
	var text strings.Builder
	var finishes []*provider.StreamChunk
	var last *provider.StreamChunk
	for i := 0; i < 1000; i++ {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("stream error: %v", err)
			}
			break
		}
		last = c
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		case provider.ChunkTypeError:
			t.Fatalf("unexpected error part: %+v", c)
		}
	}
	if text.String() != "Hello world" {
		t.Fatalf("text = %q, want %q", text.String(), "Hello world")
	}
	if len(finishes) != 1 || last != finishes[0] {
		t.Fatalf("want exactly one finish part, last; got %d, last %+v", len(finishes), last)
	}
	if finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %q", finishes[0].FinishReason)
	}
}

// TS doGenerate: timestamp new Date(response.created_at * 1000), so a present
// created_at of 0 is the epoch; an absent one has no Go timestamp (r78 provb
// change request: BuildResponseMetadata treats 0 as absent).
func TestR78integopenresponsesCreatedAtZeroIsEpoch(t *testing.T) {
	for _, tc := range []struct {
		name, createdAt string
		want            time.Time
	}{
		{"zero", `"created_at":0,`, time.Unix(0, 0)},
		{"absent", ``, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1",` + tc.createdAt + `"model":"m","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
			}))
			defer srv.Close()
			m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
			res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) || res.ResponseMetadata.Timestamp.IsZero() != tc.want.IsZero() {
				t.Fatalf("timestamp = %v, want %v", res.ResponseMetadata, tc.want)
			}
		})
	}
}
