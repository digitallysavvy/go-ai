package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

type compatFactory struct {
	name, key string
	mk        func(base string) provider.LanguageModel
}

func compatFactories() []compatFactory {
	must := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	return []compatFactory{
		{"together", "togetherai", func(b string) provider.LanguageModel {
			return must(together.New(together.Config{APIKey: "k", BaseURL: b}).LanguageModel("m"))
		}},
		{"fireworks", "fireworks", func(b string) provider.LanguageModel {
			return must(fireworks.New(fireworks.Config{APIKey: "k", BaseURL: b}).LanguageModel("m"))
		}},
		{"gmicloud", "gmicloud", func(b string) provider.LanguageModel {
			return must(gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: b}).LanguageModel("m"))
		}},
		{"zai", "zai", func(b string) provider.LanguageModel {
			return must(zai.New(zai.Config{APIKey: "k", BaseURL: b}).LanguageModel("m"))
		}},
		{"ollama", "ollama", func(b string) provider.LanguageModel {
			return must(ollama.New(ollama.Config{BaseURL: b}).LanguageModel("m"))
		}},
	}
}

func compatServer(body string, stream bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write([]byte(body))
	}))
}

var compatPrompt = types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}

// TS OpenAICompatibleChatLanguageModel.doGenerate: content may be an array of
// text / thinking parts, reasoning comes from reasoning_content ?? reasoning,
// a tool call without an id gets a generated one, extra_content carries the
// thought signature, and providerMetadata/response metadata are always set.
func TestCompatChatGenerateTSShapes(t *testing.T) {
	for _, f := range compatFactories() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			gen := func(body string) (*types.GenerateResult, error) {
				srv := compatServer(body, false)
				defer srv.Close()
				return f.mk(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: compatPrompt})
			}

			res, err := gen(`{"id":"i1","created":5,"model":"mm","choices":[{"message":{"content":"hi","reasoning":"think"},"finish_reason":"stop"}],"usage":{"completion_tokens_details":{"accepted_prediction_tokens":2}}}`)
			if err != nil {
				t.Fatal(err)
			}
			if rc, ok := res.Content[0].(types.ReasoningContent); !ok || rc.Text != "think" || res.Text != "hi" {
				t.Fatalf("reasoning field: text=%q content=%v", res.Text, res.Content)
			}
			md, _ := res.ProviderMetadata[f.key].(map[string]interface{})
			if md["acceptedPredictionTokens"] != 2 {
				t.Fatalf("metadata = %v", res.ProviderMetadata)
			}
			if res.ResponseMetadata == nil || res.ResponseMetadata.ID != "i1" || res.ResponseMetadata.ModelID != "mm" || res.ResponseMetadata.Timestamp.Unix() != 5 {
				t.Fatalf("response metadata = %+v", res.ResponseMetadata)
			}

			res, err = gen(`{"choices":[{"message":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"T"}]},{"type":"future","text":{"x":1}},{"type":"text","text":"Ans"}]},"finish_reason":"stop"}]}`)
			if err != nil {
				t.Fatal(err)
			}
			if rc, ok := res.Content[0].(types.ReasoningContent); !ok || rc.Text != "T" || res.Text != "Ans" {
				t.Fatalf("content array: text=%q content=%v", res.Text, res.Content)
			}

			_, err = gen(`{"id":"x","choices":[]}`)
			var invalid *providererrors.InvalidResponseDataError
			if !errors.As(err, &invalid) || err.Error() != "Response did not contain any choices." {
				t.Fatalf("empty choices err = %v", err)
			}

			res, err = gen(`{"choices":[{"message":{"content":null,"tool_calls":[{"function":{"name":"f","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig"}}}]},"finish_reason":"tool_calls"}]}`)
			if err != nil {
				t.Fatal(err)
			}
			tc := res.ToolCalls[0]
			if tc.ID == "" || tc.ThoughtSignature != "sig" {
				t.Fatalf("tool call = %+v", tc)
			}
			if got, _ := tc.ProviderMetadata[f.key].(map[string]interface{}); got["thoughtSignature"] != "sig" {
				t.Fatalf("tool call metadata = %v", tc.ProviderMetadata)
			}
		})
	}
}

// Streaming: delta.content may be an array of text / thinking parts, and a
// finish_reason in the same event as content or tool calls still counts.
func TestCompatChatStreamContentArray(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"T"}]},{"type":"text","text":"Ans"}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	for _, f := range compatFactories() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			srv := compatServer(sse, true)
			defer srv.Close()
			s, err := f.mk(srv.URL).DoStream(context.Background(), &provider.GenerateOptions{Prompt: compatPrompt})
			if err != nil {
				t.Fatal(err)
			}
			var reasoning, text string
			var finish *provider.StreamChunk
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				switch c.Type {
				case provider.ChunkTypeReasoning:
					reasoning += c.Reasoning
				case provider.ChunkTypeText:
					text += c.Text
				case provider.ChunkTypeError:
					t.Fatalf("error chunk: %s", c.Text)
				case provider.ChunkTypeFinish:
					finish = c
				}
			}
			if reasoning != "T" || text != "Ans!" || finish == nil || finish.FinishReason != types.FinishReasonStop {
				t.Fatalf("reasoning=%q text=%q finish=%+v", reasoning, text, finish)
			}
		})
	}
}

// TS convertToOpenAICompatibleChatMessages: a replayed tool call's
// thoughtSignature (providerOptions[metadataKey] or .google) becomes
// extra_content.google.thought_signature.
func TestCompatChatReplaysThoughtSignature(t *testing.T) {
	for _, f := range compatFactories() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			prompt := types.Prompt{Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
					ToolCallID: "c1", ToolName: "f", Input: "{}",
					ProviderOptions: map[string]interface{}{f.key: map[string]interface{}{"thoughtSignature": "SIG"}},
				}}},
				{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "f", Result: "r"}}},
			}}
			if _, err := f.mk(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, `"extra_content":{"google":{"thought_signature":"SIG"}}`) {
				t.Fatalf("request body lacks the thought signature: %s", body)
			}
		})
	}
}

// TS OpenAICompatibleChatLanguageModel.getArgs: frequency/presence penalty and
// seed are sent, topK warns, unknown provider options are spread into the body
// and the unified reasoning level becomes reasoning_effort.
func TestCompatChatRequestSettings(t *testing.T) {
	temp, fp, seed, topK := 0.5, 0.1, 7, 3
	level := types.ReasoningLow
	for _, f := range compatFactories() {
		if f.name == "zai" || f.name == "ollama" {
			continue // not TS openai-compatible wrappers
		}
		f := f
		t.Run(f.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			res, err := f.mk(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: compatPrompt, Temperature: &temp, TopK: &topK, FrequencyPenalty: &fp, PresencePenalty: &fp, Seed: &seed, Reasoning: &level,
				ProviderOptions: map[string]interface{}{f.key: map[string]interface{}{"foo": "bar", "user": "u", "textVerbosity": "low"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if body["frequency_penalty"] != 0.1 || body["presence_penalty"] != 0.1 || body["seed"] != float64(7) {
				t.Errorf("settings missing: %v", body)
			}
			if body["foo"] != "bar" || body["user"] != "u" || body["verbosity"] != "low" || body["reasoning_effort"] != "low" {
				t.Errorf("options missing: %v", body)
			}
			if _, leaked := body["textVerbosity"]; leaked {
				t.Errorf("chat option leaked into the body: %v", body)
			}
			found := false
			for _, w := range res.Warnings {
				if w.Type == "unsupported" && w.Feature == "topK" {
					found = true
				}
			}
			if !found {
				t.Errorf("no topK warning: %v", res.Warnings)
			}
		})
	}
}

// TS config includeUsage: true (togetherai, fireworks, gmicloud) sends
// stream_options.include_usage on streaming requests.
func TestCompatChatStreamIncludesUsage(t *testing.T) {
	for _, f := range compatFactories() {
		if f.name == "zai" || f.name == "ollama" {
			continue
		}
		f := f
		t.Run(f.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer srv.Close()
			s, err := f.mk(srv.URL).DoStream(context.Background(), &provider.GenerateOptions{Prompt: compatPrompt})
			if err != nil {
				t.Fatal(err)
			}
			for {
				if _, err := s.Next(); err != nil {
					break
				}
			}
			so, _ := body["stream_options"].(map[string]interface{})
			if so["include_usage"] != true {
				t.Fatalf("stream_options = %v", body["stream_options"])
			}
		})
	}
}

// TS prepareTools: a provider-defined tool is dropped with an `unsupported`
// warning instead of being sent as a function tool.
func TestCompatChatDropsProviderDefinedTools(t *testing.T) {
	for _, f := range compatFactories() {
		if f.name == "zai" || f.name == "ollama" {
			continue
		}
		f := f
		t.Run(f.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			res, err := f.mk(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: compatPrompt,
				Tools: []types.Tool{
					{Name: "search", Type: "provider", ProviderID: "acme.search"},
					{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			tools, _ := body["tools"].([]interface{})
			if len(tools) != 1 {
				t.Fatalf("tools = %v", body["tools"])
			}
			found := false
			for _, w := range res.Warnings {
				if w.Type == "unsupported" && w.Feature == "provider-defined tool acme.search" {
					found = true
				}
			}
			if !found {
				t.Fatalf("warnings = %v", res.Warnings)
			}
		})
	}
}

// TS deepinfra / baseten: provider ids "deepinfra.chat" / "baseten.chat" key
// providerOptions (unknown options are spread into the body) and
// providerMetadata.
func TestCompatChatProviderNamesKeyOptionsAndMetadata(t *testing.T) {
	type mk func(b string) provider.LanguageModel
	must := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	cases := map[string]mk{
		"deepinfra": func(b string) provider.LanguageModel {
			return must(deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: b}).LanguageModel("m"))
		},
		"baseten": func(b string) provider.LanguageModel {
			return must(baseten.New(baseten.Config{APIKey: "k", ModelURL: b}).LanguageModel("m"))
		},
	}
	for name, f := range cases {
		name, f := name, f
		t.Run(name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"completion_tokens_details":{"accepted_prediction_tokens":1}}}`))
			}))
			defer srv.Close()
			res, err := f(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: compatPrompt,
				ProviderOptions: map[string]interface{}{name: map[string]interface{}{"foo": "bar", "user": "u"}}})
			if err != nil {
				t.Fatal(err)
			}
			if body["foo"] != "bar" || body["user"] != "u" {
				t.Errorf("options not applied: %v", body)
			}
			if _, ok := res.ProviderMetadata[name]; !ok {
				t.Errorf("metadata = %v", res.ProviderMetadata)
			}
		})
	}
}

// TS togetherai / fireworks provider.completionModel: an
// OpenAICompatibleCompletionLanguageModel against /v1/completions.
func TestCompatCompletionModels(t *testing.T) {
	for _, f := range compatFactories()[:2] { // together, fireworks
		f := f
		t.Run(f.name, func(t *testing.T) {
			var path string
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"c1","choices":[{"text":"done","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
			}))
			defer srv.Close()
			var m provider.LanguageModel
			var err error
			switch f.name {
			case "together":
				m, err = together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("m")
			default:
				m, err = fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("m")
			}
			if err != nil {
				t.Fatal(err)
			}
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: compatPrompt})
			if err != nil {
				t.Fatal(err)
			}
			if path != "/v1/completions" {
				t.Errorf("path = %s", path)
			}
			if res.Text != "done" || body["model"] != "m" {
				t.Fatalf("text=%q body=%v path=%s", res.Text, body, path)
			}
			if len(res.ProviderMetadata) != 0 {
				t.Errorf("TS compat completion reports no providerMetadata: %v", res.ProviderMetadata)
			}
		})
	}
}
