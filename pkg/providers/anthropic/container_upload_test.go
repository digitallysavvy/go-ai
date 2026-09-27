package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDoGenerate_ContainerUploadContentBlock verifies that a "container_upload"
// response content block (a file the model uploaded to the code execution
// container) is surfaced as a CustomContent{Kind: "anthropic.container_upload"}
// part with the file id in ProviderMetadata, mirroring TS
// anthropic-language-model.ts's doGenerate case 'container_upload' (custom-
// content emission, item 8).
func TestDoGenerate_ContainerUploadContentBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_container_upload",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-5",
			"content": [
				{"type": "text", "text": "Here is the file."},
				{"type": "container_upload", "file_id": "file_123"}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		MaxTokens: intPtr(100),
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	var custom *types.CustomContent
	for _, c := range result.Content {
		if cc, ok := c.(types.CustomContent); ok {
			custom = &cc
			break
		}
	}
	if custom == nil {
		t.Fatalf("expected a CustomContent part in result.Content, got %+v", result.Content)
	}
	if custom.Kind != "anthropic.container_upload" {
		t.Errorf("Kind = %q, want %q", custom.Kind, "anthropic.container_upload")
	}
	var metadata map[string]map[string]interface{}
	if err := json.Unmarshal(custom.ProviderMetadata, &metadata); err != nil {
		t.Fatalf("failed to unmarshal ProviderMetadata: %v", err)
	}
	anthropicMeta, ok := metadata["anthropic"]
	if !ok {
		t.Fatalf("expected ProviderMetadata namespaced under \"anthropic\", got %+v", metadata)
	}
	if anthropicMeta["fileId"] != "file_123" {
		t.Errorf("ProviderMetadata[\"anthropic\"][\"fileId\"] = %v, want %q", anthropicMeta["fileId"], "file_123")
	}
	if result.Text != "Here is the file." {
		t.Errorf("Text = %q, want %q", result.Text, "Here is the file.")
	}
}
