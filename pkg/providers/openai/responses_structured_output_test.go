package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func structuredOutputResponsesBody() map[string]interface{} {
	msg := func(id, phase, text string) map[string]interface{} {
		return map[string]interface{}{
			"type": "message", "id": id, "role": "assistant", "status": "completed", "phase": phase,
			"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text, "annotations": []interface{}{}}},
		}
	}
	return map[string]interface{}{
		"id": "resp_1", "object": "response", "created_at": 1760000000, "model": "gpt-5.3-codex", "status": "completed",
		"output": []interface{}{
			msg("msg_0", "commentary", `{"items":[]}`),
			msg("msg_1", "final_answer", `{"items":["a"]}`),
		},
		"usage": map[string]interface{}{"input_tokens": 13, "output_tokens": 7},
	}
}

// TS #23895: JSON output is parsed from the last final_answer message only;
// text and content keep every message.
func TestResponsesDoGenerate_StructuredOutputTextIsLastFinalAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(structuredOutputResponsesBody())
	}))
	defer server.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gpt-5.3-codex")
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         prompt,
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredOutputText == nil || *result.StructuredOutputText != `{"items":["a"]}` {
		t.Fatalf("StructuredOutputText = %v", result.StructuredOutputText)
	}
	if result.Text != `{"items":[]}{"items":["a"]}` || len(result.Content) != 2 {
		t.Fatalf("text/content must keep every message: %q (%d parts)", result.Text, len(result.Content))
	}

	// Without a JSON response format the field is not set.
	result, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredOutputText != nil {
		t.Fatalf("StructuredOutputText = %q, want nil", *result.StructuredOutputText)
	}
}
