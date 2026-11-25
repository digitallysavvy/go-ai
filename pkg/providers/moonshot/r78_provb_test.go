package moonshot

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
func TestR78provbMoonshotReadsPastDone(t *testing.T) {
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
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
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

// Reviewer item 3 (r78): TS createLanguageModelResponseMetadata uses
// `created != null ? new Date(created * 1000)`, so created 0 is the epoch in
// generate and in the stream's response-metadata; an absent created is none.
func TestR78provbMoonshotCreatedZeroIsEpoch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		created string
		want    time.Time
	}{
		{"zero", `"created":0,`, time.Unix(0, 0)},
		{"absent", ``, time.Time{}},
		// r79 provb: TS z.number() accepts a fractional created; new Date(created * 1000).
		{"fractional", `"created":1700000000.25,`, time.UnixMilli(1700000000250)},
	} {
		check := func(t *testing.T, ts time.Time) {
			t.Helper()
			if ts.IsZero() != tc.want.IsZero() || !ts.Equal(tc.want) {
				t.Fatalf("timestamp = %v, want %v", ts, tc.want)
			}
		}
		t.Run("generate/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion",` + tc.created + `"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
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
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
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
// convert-to-moonshotai-chat-messages.test.ts (TS
// convertToMoonshotAIChatMessages, extracted at e64ed82f27 with --fn).
func TestR78provbTSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		if name, ok := in["providerOptionsName"].(string); ok && name != "moonshotai" {
			// the Go provider always reads providerOptions.moonshotai (TS
			// createMoonshotAI's provider name)
			return nil, nil, fmt.Errorf("%w: providerOptionsName %s", tsfixture.ErrUnsupported, name)
		}
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		modelID, _ := in["modelId"].(string)
		rfType := ""
		if rf, ok := in["responseFormat"].(map[string]interface{}); ok {
			rfType, _ = rf["type"].(string)
		}
		messages, warnings, err := convertToMoonshotChatMessages(modelID, msgs, rfType)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{"messages": r78provbJSON(messages), "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

// TestR78provbTSCasesUsage replays convert-moonshotai-chat-usage.test.ts.
func TestR78provbTSCasesUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		var raw []byte
		if m, ok := args[0].(map[string]interface{}); !ok || m["__undef"] == nil {
			raw, _ = json.Marshal(args[0])
		}
		u, err := ConvertMoonshotUsageRaw(raw)
		if err != nil {
			return nil, nil, err
		}
		if len(raw) == 0 || string(raw) == "null" {
			// TS createNullLanguageModelUsage()
			return map[string]interface{}{
				"inputTokens":  map[string]interface{}{"total": nil, "noCache": nil, "cacheRead": nil, "cacheWrite": nil},
				"outputTokens": map[string]interface{}{"total": nil, "text": nil, "reasoning": nil},
			}, nil, nil
		}
		return tsfixture.UsageToTS(u), nil, nil
	}, nil)
}

// TestR78provbTSCasesNormalizeJSONSchema replays
// normalize-json-schema-for-mfjs.test.ts.
func TestR78provbTSCasesNormalizeJSONSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/normalize_json_schema.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		out, err := NormalizeJSONSchemaForMFJS(args[0])
		if err != nil {
			return nil, nil, err
		}
		return r78provbJSON(out), nil, nil
	}, nil)
}
