package baseten

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r88provcDeepSeek = "deepseek-ai/DeepSeek-V4-Pro-0813"

type r88provcCapture struct {
	mu   sync.Mutex
	path string
	body map[string]interface{}
}

func r88provcServer(t *testing.T) (*httptest.Server, *r88provcCapture) {
	t.Helper()
	c := &r88provcCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.path = r.URL.Path
		c.body = nil
		_ = json.Unmarshal(raw, &c.body)
		c.mu.Unlock()
		if strings.Contains(string(raw), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{\\\"answer\\\":\\\"PONG\\\"}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"answer\":\"PONG\"}"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// TS baseten-chat-language-model.test.ts (c4a8b80e02), doGenerate and
// doStream.
func TestR88provcBasetenDeepSeekJSONWithReasoning(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)
	high := types.ReasoningHigh
	none := types.ReasoningNone

	type call struct {
		id        string
		dedicated bool
		format    *provider.ResponseFormat
		reasoning *types.ReasoningLevel
		provOpts  map[string]interface{}
	}
	run := func(t *testing.T, method string, c call) (map[string]interface{}, []types.Warning, string) {
		t.Helper()
		srv, cap := r88provcServer(t)
		cfg := Config{APIKey: "test-api-key", BaseURL: srv.URL + "/v1"}
		if c.dedicated {
			cfg.ModelURL = srv.URL + "/sync/v1"
		}
		id := c.id
		if id == "" {
			id = r88provcDeepSeek
		}
		model, err := New(cfg).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		format := c.format
		if format == nil {
			format = &provider.ResponseFormat{Type: "json", Schema: schema}
		}
		opts := &provider.GenerateOptions{
			Prompt:          types.Prompt{System: "You are a helpful assistant.", Text: "Reply with PONG."},
			ResponseFormat:  format,
			Reasoning:       c.reasoning,
			ProviderOptions: c.provOpts,
		}
		var warnings []types.Warning
		if method == "generate" {
			res, err := model.DoGenerate(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			warnings = res.Warnings
		} else {
			stream, err := model.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			for {
				chunk, err := stream.Next()
				if err != nil {
					break
				}
				if chunk.Type == provider.ChunkTypeStreamStart {
					warnings = chunk.Warnings
				}
			}
			_ = stream.Close()
		}
		cap.mu.Lock()
		defer cap.mu.Unlock()
		return cap.body, warnings, cap.path
	}
	hasCompat := func(ws []types.Warning) bool {
		for _, w := range ws {
			if w.Type == "compatibility" && w.Feature == "responseFormat" && strings.Contains(w.Details, "injected into the system message") {
				return true
			}
		}
		return false
	}

	for _, method := range []string{"generate", "stream"} {
		t.Run(method, func(t *testing.T) {
			t.Run("schema goes into the prompt without JSON mode when reasoning is enabled", func(t *testing.T) {
				body, warnings, path := run(t, method, call{reasoning: &high})
				if path != "/v1/chat/completions" {
					t.Fatalf("path = %q", path)
				}
				if _, ok := body["response_format"]; ok {
					t.Fatalf("response_format sent: %v", body["response_format"])
				}
				if body["reasoning_effort"] != "high" {
					t.Fatalf("reasoning_effort = %v", body["reasoning_effort"])
				}
				msgs := body["messages"].([]interface{})
				first := msgs[0].(map[string]interface{})
				want := "JSON schema:\n" + string(schema) + "\nYou MUST answer with a JSON object that matches the JSON schema above."
				if len(first) != 2 || first["role"] != "system" || first["content"] != want {
					t.Fatalf("first message = %#v", first)
				}
				if len(msgs) != 3 || msgs[1].(map[string]interface{})["content"] != "You are a helpful assistant." {
					t.Fatalf("messages = %#v", msgs)
				}
				if !hasCompat(warnings) {
					t.Fatalf("warnings = %#v", warnings)
				}
			})
			t.Run("JSON mode without a schema and default reasoning", func(t *testing.T) {
				body, _, _ := run(t, method, call{format: &provider.ResponseFormat{Type: "json"}})
				if _, ok := body["response_format"]; ok {
					t.Fatal("response_format sent")
				}
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatal("reasoning_effort sent")
				}
				first := body["messages"].([]interface{})[0].(map[string]interface{})
				if first["content"] != "You MUST answer with JSON." {
					t.Fatalf("first = %#v", first)
				}
			})
			for name, c := range map[string]call{
				"reasoning none":                     {reasoning: &none},
				"provider reasoningEffort none":      {reasoning: &high, provOpts: map[string]interface{}{"baseten": map[string]interface{}{"reasoningEffort": "none"}}},
				"raw provider reasoning_effort none": {provOpts: map[string]interface{}{"baseten": map[string]interface{}{"reasoning_effort": "none"}}},
				"other model":                        {id: "zai-org/GLM-4.7", reasoning: &high},
				"dedicated deployment":               {dedicated: true, reasoning: &high},
			} {
				t.Run("keeps JSON Schema: "+name, func(t *testing.T) {
					body, warnings, _ := run(t, method, c)
					rf, _ := body["response_format"].(map[string]interface{})
					if rf["type"] != "json_schema" || len(body["messages"].([]interface{})) != 2 || hasCompat(warnings) {
						t.Fatalf("body = %#v, warnings = %#v", body, warnings)
					}
				})
			}
			t.Run("text requests are unchanged", func(t *testing.T) {
				body, warnings, _ := run(t, method, call{format: &provider.ResponseFormat{Type: "text"}, reasoning: &high})
				if _, ok := body["response_format"]; ok || len(body["messages"].([]interface{})) != 2 || hasCompat(warnings) {
					t.Fatalf("body = %#v", body)
				}
			})
		})
	}
}

// TS createChatModel uses modelURL for chat only when it contains /sync/v1;
// a /sync embeddings deployment URL leaves chat on the Model APIs.
func TestR88provcBasetenChatModelURLRouting(t *testing.T) {
	srv, cap := r88provcServer(t)
	for _, tc := range []struct {
		modelURL, id, wantPath, wantModel string
	}{
		{srv.URL + "/sync", "", "/v1/chat/completions", "chat"},
		{srv.URL + "/sync/v1", "", "/sync/v1/chat/completions", "placeholder"},
		{"", "", "/v1/chat/completions", "chat"},
	} {
		model, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", ModelURL: tc.modelURL}).LanguageModel(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatal(err)
		}
		cap.mu.Lock()
		path, gotModel := cap.path, cap.body["model"]
		cap.mu.Unlock()
		if path != tc.wantPath || gotModel != tc.wantModel {
			t.Fatalf("modelURL %q: path %q model %v, want %q %q", tc.modelURL, path, gotModel, tc.wantPath, tc.wantModel)
		}
	}
}

// The model-ID constants match the TS catalog IDs exactly.
func TestR88provcBasetenModelIDs(t *testing.T) {
	want := []string{"deepseek-ai/DeepSeek-R1-0528", "deepseek-ai/DeepSeek-V3-0324", "deepseek-ai/DeepSeek-V3.1", "moonshotai/Kimi-K2-Instruct-0905", "moonshotai/Kimi-K2-Thinking", "Qwen/Qwen3-235B-A22B-Instruct-2507", "Qwen/Qwen3-Coder-480B-A35B-Instruct", "openai/gpt-oss-120b", "zai-org/GLM-4.6", "zai-org/GLM-4.7"}
	got := []string{ModelDeepSeekR10528, ModelDeepSeekV30324, ModelDeepSeekV31, ModelKimiK2Instruct0905, ModelKimiK2Thinking, ModelQwen3235BA22BInstruct2507, ModelQwen3Coder480BA35BInstruct, ModelGPTOSS120B, ModelGLM46, ModelGLM47}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d: %q != %q", i, got[i], want[i])
		}
	}
}
