package groq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

func r78provagroqServer(t *testing.T, body string, sse bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TS createLanguageModelResponseMetadata: created != null ? new Date(created * 1000),
// so created: 0 is the epoch and a fractional created keeps its milliseconds.
func TestR78prova_groqCreatedTimestamp(t *testing.T) {
	for _, tc := range []struct {
		created string
		want    time.Time
	}{
		{"0", time.Unix(0, 0)},
		{"1711115037.5", time.UnixMilli(1711115037500)},
		{"null", time.Time{}},
	} {
		body := `{"id":"x","object":"chat.completion","created":` + tc.created + `,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		m, err := New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, body, false)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("created %s: %v", tc.created, err)
		}
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Fatalf("generate created %s: metadata %+v, want timestamp %v", tc.created, res.ResponseMetadata, tc.want)
		}

		sse := "data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}` + "\n\n" +
			"data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
		m, err = New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, sse, true)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		var got *provider.ResponseMetadata
		for {
			c, err := stream.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				t.Fatalf("stream created %s: error chunk %v", tc.created, c.Err)
			}
			if c.Type == provider.ChunkTypeResponseMetadata && got == nil {
				got = c.ResponseMetadata
			}
		}
		_ = stream.Close()
		if got == nil || !got.Timestamp.Equal(tc.want) {
			t.Fatalf("stream created %s: metadata %+v, want timestamp %v", tc.created, got, tc.want)
		}
	}
}

// TS parseJsonEventStream drops `data: [DONE]` and keeps reading: frames
// after it are processed and the finish comes at the end of the body.
func TestR78prova_groqReadsPastDone(t *testing.T) {
	sse := "data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"a"},"finish_reason":null}]}` + "\n\ndata: [DONE]\n\n" +
		"data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":"stop"}]}` + "\n\n"
	m, err := New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, sse, true)}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text, finishes, last := "", 0, provider.ChunkType("")
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
		last = c.Type
	}
	if text != "ab" || finishes != 1 || last != provider.ChunkTypeFinish {
		t.Fatalf("text %q, finishes %d, last %s; want \"ab\", 1 finish last", text, finishes, last)
	}
}

const r78provaTSHeaders = "needs the provider the TS test builds (API key and provider headers); replayed by TestR79prova_TSCasesPassHeaders"

// TestR78prova_TSCasesChatAsserts replays groq-chat-language-model.test.ts
// extracted at e64ed82f27 with --asserts (plain assertions as well as
// snapshots).
func TestR78prova_TSCasesChatAsserts(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_asserts.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemma2-9b-it"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should send request body":                            "TS groq returns request.body as a JSON string; Go keeps the object",
		"should handle structured outputs with Kimi K2 model": "model id comes from a custom provider call in the TS test",
		"should pass headers":                                 r78provaTSHeaders,
	})
}

// TestR78prova_TSCasesConvertMessages replays convert-to-groq-chat-messages.test.ts.
func TestR78prova_TSCasesConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		msgs, why := tsfixture.BuildPromptMessages(args[0])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		// DoGenerate/DoStream run these checks before building the body.
		if err := prompt.RejectProviderReferenceFiles(msgs); err != nil {
			return nil, nil, err
		}
		if err := prompt.ValidateOpenAIFileParts(msgs, prompt.FilePartsGroq, ""); err != nil {
			return nil, nil, err
		}
		m := NewLanguageModel(makeTestGroqProvider(), "gemma2-9b-it")
		body, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}}, false)
		return body["messages"], nil, nil
	}, nil)
}

// TestR78prova_TSCasesPrepareTools replays groq-prepare-tools.test.ts.
func TestR78prova_TSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		if id == "groq.browser_search" {
			return BrowserSearchTool(), true
		}
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice = tsfixture.BuildToolChoice(tc)
		}
		modelID, _ := in["modelId"].(string)
		tools, toolChoice, warnings := prepareTools(modelID, ts, choice)
		out := map[string]interface{}{"toolWarnings": tsfixture.WarningsToTS(warnings)}
		if tools != nil {
			out["tools"] = tools
		}
		if toolChoice != nil {
			out["toolChoice"] = toolChoice
		}
		return out, nil, nil
	}, nil)
}

// TestR78prova_TSCasesConvertUsage replays convert-groq-usage.test.ts.
func TestR78prova_TSCasesConvertUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		var u *groqUsage
		if m, ok := args[0].(map[string]interface{}); ok && m["__undef"] == nil {
			raw, _ := json.Marshal(args[0])
			if err := json.Unmarshal(raw, &u); err != nil {
				return nil, nil, err
			}
		}
		return tsfixture.UsageToTS(convertGroqUsagePtr(u)), nil, nil
	}, nil)
}
