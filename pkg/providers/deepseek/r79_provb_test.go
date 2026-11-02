package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS deepseek prepareTools default branch: an unknown tool choice type sends
// no tool_choice and warns `tool choice type: <type>` (Go's shared
// ConvertToolChoiceToOpenAI would send "auto").
func TestR79provbDeepseekUnknownToolChoiceWarns(t *testing.T) {
	lm, err := New(Config{APIKey: "k"}).LanguageModel("deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	tools := []types.Tool{{Name: "f", Parameters: map[string]interface{}{"type": "object"}}}
	dsTools, choice, warnings, err := lm.(*LanguageModel).prepareDeepSeekTools(tools, types.ToolChoice{Type: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dsTools) != 1 || choice != nil {
		t.Fatalf("tools %v, choice %#v", dsTools, choice)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "tool choice type: bogus" {
		t.Fatalf("warnings = %+v", warnings)
	}
	_, choice, warnings, _ = lm.(*LanguageModel).prepareDeepSeekTools(tools, types.ToolChoice{Type: types.ToolChoiceRequired})
	if choice != "required" || len(warnings) != 0 {
		t.Fatalf("required: %#v %v", choice, warnings)
	}
}

// TS records finish_reason when it is != null, so an empty one maps to
// "other" with raw "" (Go read "" as no finish_reason: interrupted).
func TestR79provbDeepseekEmptyFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":""}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonOther || finish.RawFinishReason != "" {
		t.Fatalf("finish = %+v", finish)
	}
}
