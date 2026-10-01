package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderAnthropicModelFactory(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-east5",
		AccessToken: "parent-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.AnthropicModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("AnthropicModel error = %v", err)
	}
	if model.Provider() != "googleVertex.anthropic.messages" {
		t.Fatalf("Provider() = %q, want googleVertex.anthropic.messages", model.Provider())
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotPath != "/claude-sonnet-4-6:rawPredict" {
		t.Fatalf("path = %q, want Vertex Anthropic rawPredict path", gotPath)
	}
	if gotAuth != "Bearer parent-token" {
		t.Fatalf("Authorization = %q, want Bearer parent-token", gotAuth)
	}
	if _, ok := gotBody["model"]; ok {
		t.Fatal("request body should not include model")
	}
}

func TestProviderAnthropicModelFactoryHeaderOverride(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-east5",
		AccessToken: "generated-token",
		BaseURL:     server.URL,
		Headers:     map[string]string{"Authorization": "Bearer user-token"},
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.AnthropicModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("AnthropicModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotAuth != "Bearer user-token" {
		t.Fatalf("Authorization = %q, want user override", gotAuth)
	}
}
