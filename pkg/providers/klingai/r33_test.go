package klingai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `prompt != null` sends an explicit empty prompt, and
// `task_status_msg ?? 'Unknown error'` keeps an empty message.
func TestR33_EmptyStringFallbacks(t *testing.T) {
	var posted map[string]interface{}
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &posted)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"submitted"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"failed","task_status_msg":""}}`))
	})
	defer server.Close()
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "", PromptSet: true}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := posted["prompt"]; !ok || v != "" {
		t.Errorf("prompt = %v (present %v)", v, ok)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil {
		t.Fatal(err)
	}
	if status.Error != "Video generation failed: " {
		t.Errorf("error = %q", status.Error)
	}
}

// apiKey?.trim() is JS trim: a BOM-only key is empty, a NEL key is a key.
func TestR33_APIKeyTrimIsJSTrim(t *testing.T) {
	t.Setenv("KLINGAI_API_KEY", "")
	t.Setenv("KLINGAI_ACCESS_KEY", "")
	t.Setenv("KLINGAI_SECRET_KEY", "")
	if tok, err := resolveKlingAIAuthToken("\u0085", "", ""); err != nil || tok != "\u0085" {
		t.Errorf("NEL key: %q, %v", tok, err)
	}
	if _, err := resolveKlingAIAuthToken("\ufeff ", "", ""); err == nil {
		t.Error("BOM-only key accepted")
	}
}
