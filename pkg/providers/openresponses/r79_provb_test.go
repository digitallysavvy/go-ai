package openresponses

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate does not validate the body (jsonSchema only applies types) and
// sets timestamp new Date(response.created_at * 1000): a fractional
// created_at keeps its milliseconds, null is the epoch (null * 1000 is 0) and
// a non-numeric one is an Invalid Date; none of them fails the call.
func TestR79provbOpenResponsesCreatedAtTolerated(t *testing.T) {
	for _, tc := range []struct {
		name, createdAt string
		want            time.Time
		wantField       int64
	}{
		{"fractional", `"created_at":1700000000.75,`, time.UnixMilli(1700000000750), 1700000000},
		{"null", `"created_at":null,`, time.UnixMilli(0), 0},
		{"string", `"created_at":"soon",`, time.Time{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1",` + tc.createdAt + `"completed_at":1700000001.5,"model":"m","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
			}))
			defer srv.Close()
			m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
			res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			ts := res.ResponseMetadata.Timestamp
			if ts.IsZero() != tc.want.IsZero() || !ts.Equal(tc.want) {
				t.Fatalf("timestamp = %v, want %v", ts, tc.want)
			}
			if len(res.Content) == 0 {
				t.Fatal("no content decoded")
			}
		})
	}
}

// openResponsesResponseWire truncates fractional unix seconds and keeps every
// other field.
func TestR79provbOpenResponsesResponseWireFractional(t *testing.T) {
	var w openResponsesResponseWire
	if err := json.Unmarshal([]byte(`{"id":"r","created_at":12.9,"completed_at":-3.5,"status":"completed","model":"m","output":[]}`), &w); err != nil {
		t.Fatal(err)
	}
	r := w.response()
	if r.ID != "r" || r.Status != "completed" || r.Model != "m" || r.Output == nil {
		t.Fatalf("fields lost: %+v", r)
	}
	if r.CreatedAt != 12 || r.CompletedAt == nil || *r.CompletedAt != -3 {
		t.Fatalf("created_at %d completed_at %v", r.CreatedAt, r.CompletedAt)
	}
	var absent openResponsesResponseWire
	if err := json.Unmarshal([]byte(`{"id":"r","completed_at":null}`), &absent); err != nil || absent.response().CompletedAt != nil || absent.response().CreatedAt != 0 {
		t.Fatalf("absent: %+v %v", absent, err)
	}
}

// TS doStream parses events with z.any(): a response.completed event whose
// response has a fractional created_at still finishes with its usage.
func TestR79provbOpenResponsesStreamFractionalCreatedAt(t *testing.T) {
	body := `data: {"type":"response.created","response":{"id":"r","created_at":1700000000.5,"model":"m"}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","delta":"Hi"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"r","created_at":1700000000.5,"status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}` + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil)
	defer func() { _ = s.Close() }()
	var finish *provider.StreamChunk
	for i := 0; i < 100; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %+v", c)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || finish.Usage.OutputTokens == nil || *finish.Usage.OutputTokens != 2 {
		t.Fatalf("finish = %+v", finish)
	}
}

// TS custom_tool_call input: JSON.stringify(input), which keeps <, > and &
// literal (Go's json.Marshal escapes them as < etc.).
func TestR79provbOpenResponsesCustomToolInputNotHTMLEscaped(t *testing.T) {
	const want = `"a<b && c>d"`
	t.Run("generate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","created_at":1,"model":"m","output":[{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":"a<b && c>d"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
		}))
		defer srv.Close()
		m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
		res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].RawArguments != want {
			t.Fatalf("tool calls = %+v", res.ToolCalls)
		}
	})
	t.Run("stream", func(t *testing.T) {
		body := `data: {"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":""}}` + "\n\n" +
			`data: {"type":"response.output_item.done","item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":"a<b && c>d"}}` + "\n\n"
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil)
		defer func() { _ = s.Close() }()
		var call *types.ToolCall
		for i := 0; i < 100; i++ {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeToolCall {
				call = c.ToolCall
			}
		}
		if call == nil || call.RawArguments != want {
			t.Fatalf("tool call = %+v", call)
		}
	})
}
