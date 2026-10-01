package anthropicaws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAnthropicAWSSerializeAndDeserializeLanguageModel(t *testing.T) {
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "ws-1", APIKey: "k"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.LanguageModel("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel error = %v", err)
	}
	if serialized.Provider != "anthropic-aws.messages" || serialized.ModelID != "claude-sonnet-4-20250514" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel error = %v", err)
	}
	if restored.Provider() != "anthropic-aws.messages" || restored.ModelID() != "claude-sonnet-4-20250514" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

// TestAnthropicAWSDeserializeRecoversAPIKeyFromEnv is the regression test for
// the R1-2 follow-up: API-key auth's credential lives ONLY in the inner
// anthropic.Config.Headers["x-api-key"] map entry (never in a struct field —
// see New() above), so once SerializableConfig redacts that header entry
// (R1-2), a naive deserializeModel would silently restore an unauthenticated
// model that only fails once a real request hits the Anthropic API. This
// confirms deserializeModel instead recovers the credential from
// ANTHROPIC_AWS_API_KEY, the same environment variable anthropicaws.New()
// itself already falls back to, by checking the actual "x-api-key" header a
// restored model sends on a real request.
func TestAnthropicAWSDeserializeRecoversAPIKeyFromEnv(t *testing.T) {
	var gotAPIKeyHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKeyHeader = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	p, err := New(Config{Region: "us-east-1", WorkspaceID: "ws-1", APIKey: "original-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.LanguageModel("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel error = %v", err)
	}
	if headers, ok := serialized.Config["headers"].(map[string]interface{}); ok {
		if _, leaked := headers["x-api-key"]; leaked {
			t.Fatalf("x-api-key leaked into serialized config: %#v", headers)
		}
	}

	t.Setenv("ANTHROPIC_AWS_API_KEY", "env-recovered-key")

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel error = %v", err)
	}
	if _, err := restored.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotAPIKeyHeader != "env-recovered-key" {
		t.Fatalf("restored model sent x-api-key = %q, want recovery from ANTHROPIC_AWS_API_KEY", gotAPIKeyHeader)
	}
}
