package deepseek

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
func TestR78provbDeepseekReadsPastDone(t *testing.T) {
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
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat")
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
func TestR78provbDeepseekCreatedZeroIsEpoch(t *testing.T) {
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
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat")
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
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat")
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

// TestR78provbTSCasesConvertMessages replays
// convert-to-deepseek-chat-messages.test.ts (TS convertToDeepSeekChatMessages,
// extracted at e64ed82f27 with --expand-each --fn) through convertMessages.
func TestR78provbTSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		if rf, ok := in["responseFormat"].(map[string]interface{}); ok && rf["__undef"] == nil {
			return nil, nil, fmt.Errorf("%w: responseFormat", tsfixture.ErrUnsupported)
		}
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		cfg := Config{APIKey: "k"}
		if name, ok := in["providerOptionsName"].(string); ok {
			cfg.Name, cfg.ProviderOptionsName = name, name
		}
		if v, _ := in["supportsAssistantPrefixCompletion"].(bool); v {
			cfg.BaseURL = "https://api.deepseek.com/beta"
		}
		if v, ok := in["supportsStructuredOutputs"].(bool); ok {
			cfg.SupportsStructuredOutputs = &v
		}
		modelID, _ := in["modelId"].(string)
		lm, err := New(cfg).LanguageModel(modelID)
		if err != nil {
			return nil, nil, err
		}
		messages, warnings, err := lm.(*LanguageModel).convertMessages(msgs)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(messages)
		var out interface{}
		_ = json.Unmarshal(b, &out)
		return map[string]interface{}{"messages": out, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

// TestR78provbTSCasesUsage replays convert-to-deepseek-usage.test.ts.
func TestR78provbTSCasesUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		return tsfixture.UsageToTS(convertDeepseekUsage(b)), nil, nil
	}, nil)
}

// TestR78provbTSCasesPrepareTools replays deepseek-prepare-tools.test.ts:
// supportsStrictToolCalls true is a /beta base URL, false the default one,
// and unset (Azure) UncheckedStrictToolCalls.
func TestR78provbTSCasesPrepareTools(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		tools, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		cfg := Config{APIKey: "k"}
		switch v, ok := in["supportsStrictToolCalls"].(bool); {
		case !ok:
			cfg.UncheckedStrictToolCalls = true
		case v:
			cfg.BaseURL = "https://api.deepseek.com/beta"
		}
		var choice types.ToolChoice
		if tc, ok := in["toolChoice"]; ok {
			choice = tsfixture.BuildToolChoice(tc)
		}
		lm, err := New(cfg).LanguageModel("deepseek-chat")
		if err != nil {
			return nil, nil, err
		}
		dsTools, toolChoice, warnings, err := lm.(*LanguageModel).prepareDeepSeekTools(tools, choice)
		if err != nil {
			return nil, nil, err
		}
		var toolsOut interface{}
		if dsTools != nil {
			b, _ := json.Marshal(dsTools)
			_ = json.Unmarshal(b, &toolsOut)
		}
		return map[string]interface{}{"tools": toolsOut, "toolChoice": toolChoice, "toolWarnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}
