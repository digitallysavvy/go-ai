package alibaba

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading:
// frames after it still count and flush() (the finish part) runs at the end
// of the body (coordinator ruling r78 a).
func TestR78provbAlibabaReadsPastDone(t *testing.T) {
	frames := []string{
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var text strings.Builder
	var finishes []*provider.StreamChunk
	var last *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
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
	if len(finishes) != 1 || last != finishes[0] {
		t.Fatalf("want exactly one finish part, last; got %d finishes, last %+v", len(finishes), last)
	}
	if text.String() != "Hello world" || finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text.String(), finishes[0].FinishReason, "Hello world")
	}
}

// r78provbDropMetadata checks the stream carried exactly one
// response-metadata part (TS emits it for the first valid chunk even when
// the chunk has no id, model or created) and returns the other parts.
func r78provbDropMetadata(t *testing.T, chunks []*provider.StreamChunk) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	n := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata {
			n++
			continue
		}
		out = append(out, c)
	}
	if n != 1 {
		t.Fatalf("want exactly one response-metadata part, got %d", n)
	}
	return out
}

// Reviewer item 3 (r78): TS createLanguageModelResponseMetadata uses
// `created != null ? new Date(created * 1000)`, so created 0 is the epoch in
// generate and in the stream's response-metadata; an absent created is none.
func TestR78provbAlibabaCreatedZeroIsEpoch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		created string
		want    bool
	}{{"zero", `"created":0,`, true}, {"absent", ``, false}} {
		check := func(t *testing.T, ts time.Time) {
			t.Helper()
			if tc.want && (ts.IsZero() || !ts.Equal(time.Unix(0, 0))) {
				t.Fatalf("timestamp = %v, want the epoch", ts)
			}
			if !tc.want && !ts.IsZero() {
				t.Fatalf("timestamp = %v, want none", ts)
			}
		}
		t.Run("generate/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion",` + tc.created + `"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			if r.ResponseMetadata == nil {
				t.Fatal("no response metadata")
			}
			check(t, r.ResponseMetadata.Timestamp)
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk",` + tc.created + `"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
			if err != nil {
				t.Fatal(err)
			}
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
				if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata != nil && c.ResponseMetadata.ID == "c1" {
					meta = c.ResponseMetadata
				}
			}
			if meta == nil {
				t.Fatal("no response-metadata part")
			}
			check(t, meta.Timestamp)
		})
	}
}

func r78provbJSON(v interface{}) interface{} {
	b, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// TestR78provbTSCasesConvertMessages replays
// convert-to-alibaba-chat-messages.test.ts (TS convertToAlibabaChatMessages,
// extracted at e64ed82f27 with --expand-each --fn).
func TestR78provbTSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var validator *CacheControlValidator
		if v, ok := in["cacheControlValidator"]; ok && v != nil {
			validator = NewCacheControlValidator()
		}
		preserve, _ := in["preserveThinking"].(bool)
		out, err := ConvertToAlibabaChatMessagesWithError(msgs, validator, preserve)
		if err != nil {
			return nil, nil, err
		}
		if out == nil {
			return []interface{}{}, nil, nil
		}
		return r78provbJSON(out), nil, nil
	}, r78provbKeyOrderSkips)
}

// r78provbKeyOrderSkips: these TS tests stringify a json tool output
// ({temperature, condition}) in insertion order; the case file decodes it into
// a Go map, which marshals its keys sorted (a Go caller's struct value keeps
// its field order).
var r78provbKeyOrderSkips = map[string]string{
	"should convert tool results":                                                            r78provbKeyOrder,
	"should inject cache control into single-part tool message":                              r78provbKeyOrder,
	"should use part-level cache control for single-part tool message":                       r78provbKeyOrder,
	"should prefer part-level over message-level cache control for single-part tool message": r78provbKeyOrder,
	"should apply message-level cache control to last part of multi-part tool message":       r78provbKeyOrder,
	"should use part-level cache control for multi-part tool message":                        r78provbKeyOrder,
}

const r78provbKeyOrder = "TS JSON.stringify keeps the json tool output's insertion order; the case file decodes it into a Go map, which marshals sorted"

// TestR78provbTSCasesUsage replays convert-alibaba-usage.test.ts.
func TestR78provbTSCasesUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		if len(args) == 0 || args[0] == nil {
			return tsfixture.UsageToTS(types.Usage{}), nil, nil
		}
		if m, ok := args[0].(map[string]interface{}); ok && m["__undef"] != nil {
			return r78provbNullUsage(), nil, nil
		}
		b, _ := json.Marshal(args[0])
		var u AlibabaUsage
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(ConvertAlibabaUsage(u)), nil, nil
	}, nil)
}

// r78provbNullUsage is TS createNullLanguageModelUsage().
func r78provbNullUsage() map[string]interface{} {
	return map[string]interface{}{
		"inputTokens":  map[string]interface{}{"total": nil, "noCache": nil, "cacheRead": nil, "cacheWrite": nil},
		"outputTokens": map[string]interface{}{"total": nil, "text": nil, "reasoning": nil},
	}
}

// TestR78provbTSCasesPrepareTools replays alibaba-prepare-tools.test.ts
// through the request builder (tools, tool_choice and the tool warnings).
func TestR78provbTSCasesPrepareTools(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		tools, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, Tools: tools}
		if tc, ok := in["toolChoice"]; ok {
			opts.ToolChoice = tsfixture.BuildToolChoice(tc)
		}
		lm, err := New(Config{APIKey: "k"}).LanguageModel("qwen-plus")
		if err != nil {
			return nil, nil, err
		}
		body, warnings, err := lm.(*LanguageModel).buildRequestBodyChecked(opts, false)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{"tools": r78provbJSON(body["tools"]), "toolChoice": r78provbJSON(body["tool_choice"]), "toolWarnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}
