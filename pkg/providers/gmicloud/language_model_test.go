package gmicloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from gmicloud-chat-language-model.test.ts "surfaces the engine
// diagnostic from error.details on a 400". Verbatim capture from
// api.gmi-serving.com/v1/chat/completions (2026-08).
const gmicloudMaxTokensErrorBody = `{"error":{"message":"Backend request failed with status 400","type":"backend_error","code":400,"details":"{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"400001\",\"message\":\"The request is invalid: Invalid max_tokens value, the valid range of max_tokens is [1, 393216]. Please check the request body, required fields, and request format.\",\"message_zh\":\"请求不合法\",\"source\":\"client\",\"request_id\":\"6d6429ae-0ee8-49c1-9308-cbd2e2889b45\"}}"}}`

func TestDoGenerateSurfacesEngineDiagnosticFromDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(gmicloudMaxTokensErrorBody))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model := NewLanguageModel(p, "deepseek-ai/DeepSeek-V4-Flash-0731")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err == nil {
		t.Fatal("DoGenerate() expected error, got nil")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", provErr.StatusCode)
	}
	want := `The request is invalid: Invalid max_tokens value, the valid range of max_tokens is [1, 393216]. Please check the request body, required fields, and request format.`
	if provErr.Message != want {
		t.Fatalf("Message = %q, want %q", provErr.Message, want)
	}
}

func TestDoGenerateAndDoStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		if stream, _ := payload["stream"].(bool); stream {
			so, ok := payload["stream_options"].(map[string]interface{})
			if !ok || so["include_usage"] != true {
				t.Fatalf("stream_options.include_usage not injected: %#v", payload["stream_options"])
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"resp-1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"resp-1","model":"m",
			"choices":[{"finish_reason":"stop","message":{"content":"done"}}],
			"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewLanguageModel(p, "deepseek-ai/DeepSeek-V4-Flash-0731")

	gen, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gen.Text != "done" || gen.FinishReason != types.FinishReasonStop {
		t.Fatalf("unexpected result: %+v", gen)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	var sawText, sawFinish bool
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeText && chunk.Text == "hi" {
			sawText = true
		}
		if chunk.Type == provider.ChunkTypeFinish {
			sawFinish = true
			if chunk.FinishReason != types.FinishReasonStop {
				t.Fatalf("FinishReason = %v, want stop", chunk.FinishReason)
			}
		}
	}
	if !sawText || !sawFinish {
		t.Fatalf("sawText=%v sawFinish=%v", sawText, sawFinish)
	}
}
