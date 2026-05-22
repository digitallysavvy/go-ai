package cerebras

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
