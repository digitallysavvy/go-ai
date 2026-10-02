package cerebras

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestCerebrasErrorShapeIsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad request","type":"invalid_request_error","param":"messages","code":"bad_messages"}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("llama3.1-8b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.Provider != "cerebras" || providerErr.StatusCode != http.StatusBadRequest || providerErr.ErrorCode != "bad_messages" || providerErr.Message != "bad request" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

func TestCerebrasStructuredJSONSuppressesIncidentalToolCalls(t *testing.T) {
	base := &mockCerebrasBaseModel{
		result: &types.GenerateResult{
			Text:         `{"ok":true}`,
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "emit_json"}},
		},
	}
	model := &LanguageModel{base: base}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.FinishReason != types.FinishReasonStop || len(result.ToolCalls) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

type mockCerebrasBaseModel struct {
	result *types.GenerateResult
}

func (m *mockCerebrasBaseModel) SpecificationVersion() string { return "v3" }
func (m *mockCerebrasBaseModel) Provider() string             { return "openai" }
func (m *mockCerebrasBaseModel) ModelID() string              { return "model" }
func (m *mockCerebrasBaseModel) SupportsTools() bool          { return true }
func (m *mockCerebrasBaseModel) SupportsStructuredOutput() bool {
	return true
}
func (m *mockCerebrasBaseModel) SupportsImageInput() bool { return false }
func (m *mockCerebrasBaseModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.result, nil
}
func (m *mockCerebrasBaseModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, errors.New("not used")
}

// Cerebras exposes only the OpenAI-compatible Chat Completions API; the
// language model must not call /responses (TS createCerebras builds an
// OpenAICompatibleChatLanguageModel at `${baseURL}/chat/completions`).
func TestCerebrasLanguageModelUsesChatCompletions(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUserAgent = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"llama3.1-8b","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	for _, factory := range []string{"LanguageModel", "ChatModel"} {
		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		var model provider.LanguageModel
		var err error
		if factory == "LanguageModel" {
			model, err = p.LanguageModel("llama3.1-8b")
		} else {
			model, err = p.ChatModel("llama3.1-8b")
		}
		if err != nil {
			t.Fatalf("%s error = %v", factory, err)
		}
		result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
		if err != nil {
			t.Fatalf("%s DoGenerate error = %v", factory, err)
		}
		if gotPath != "/chat/completions" {
			t.Fatalf("%s path = %q, want /chat/completions", factory, gotPath)
		}
		if _, ok := gotBody["messages"]; !ok {
			t.Fatalf("%s body has no chat messages: %#v", factory, gotBody)
		}
		if result.Text != "hi" {
			t.Fatalf("%s text = %q", factory, result.Text)
		}
		// Cerebras is implemented by reusing pkg/providers/openai as its
		// Chat-Completions-compatible transport, but TS cerebras-provider.ts
		// has its own `ai-sdk-cerebras/VERSION` tag, distinct from
		// @ai-sdk/openai's own `ai-sdk-openai/VERSION`. openai.Config's
		// UserAgentName field is what makes that distinction reach the wire.
		if !strings.HasPrefix(gotUserAgent, "ai-sdk-cerebras/") {
			t.Fatalf("%s User-Agent = %q, want ai-sdk-cerebras/... prefix", factory, gotUserAgent)
		}
		if strings.Contains(gotUserAgent, "ai-sdk-openai/") {
			t.Fatalf("%s User-Agent = %q, must not carry the ai-sdk/openai tag", factory, gotUserAgent)
		}
	}
}
