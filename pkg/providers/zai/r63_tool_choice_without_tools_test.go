package zai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r63: TS generateText always passes a tool choice (auto when unset), and the
// openai-compatible base Z.AI delegates to drops it when there are no tools
// (prepareTools), so a call without tools sends no tool_choice.
func TestR63DoGenerateAutoToolChoiceWithoutToolsSendsNoToolChoice(t *testing.T) {
	var capturedBody map[string]interface{}
	p, _ := newZaiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiSuccessResponseJSON))
	})
	model := NewLanguageModel(p, "glm-5.3")
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "Hello"},
		ToolChoice: types.AutoToolChoice(),
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if v, ok := capturedBody["tool_choice"]; ok {
		t.Fatalf("body has tool_choice %v without tools", v)
	}
}
