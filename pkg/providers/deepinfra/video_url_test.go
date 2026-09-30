package deepinfra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestLanguageModelVideoContentPartBecomesVideoURL ports TS's
// convert-to-openai-compatible-chat-messages.ts video_url branch
// (7dd9ec320c): DeepInfraChatLanguageModel extends
// OpenAICompatibleChatLanguageModel, so a video/* file part must become a
// "video_url" content part, not the generic "file" fallback.
func TestLanguageModelVideoContentPartBecomesVideoURL(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.LanguageModel("m")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{URL: "https://example.com/video.mp4", MediaType: "video/mp4"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}

	messages := got["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	part := content[0].(map[string]interface{})
	if part["type"] != "video_url" {
		t.Fatalf("content part type = %v, want video_url", part["type"])
	}
	videoURL := part["video_url"].(map[string]interface{})
	if videoURL["url"] != "https://example.com/video.mp4" {
		t.Fatalf("video_url.url = %v", videoURL["url"])
	}
}
