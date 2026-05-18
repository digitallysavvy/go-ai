package together

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestLanguageModelBuildRequestBodyAndUsageConversion(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "meta-llama/test")

	maxTokens := 256
	temperature := 0.7
	topP := 0.9
	body, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hello"},
		MaxTokens:     &maxTokens,
		Temperature:   &temperature,
		TopP:          &topP,
		StopSequences: []string{"\n\n"},
		Tools: []types.Tool{
			{Name: "sum", Description: "sum numbers", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "sum"},
		ResponseFormat: &provider.ResponseFormat{
			Type: "json_object",
		},
	}, false)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if body["model"] != "meta-llama/test" || body["stream"] != false {
		t.Fatalf("unexpected request body: %+v", body)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatal("expected tools in request body")
	}
	if _, ok := body["response_format"]; !ok {
		t.Fatal("expected response_format in request body")
	}

	usage := convertTogetherUsage(togetherUsage{
		PromptTokens:     10,
		CompletionTokens: 6,
		TotalTokens:      16,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{
			CachedTokens: intPtr(3),
			TextTokens:   intPtr(7),
			ImageTokens:  intPtr(3),
		},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{
			ReasoningTokens: intPtr(2),
		},
	})
	if usage.InputDetails == nil || usage.OutputDetails == nil {
		t.Fatalf("expected detailed usage, got %+v", usage)
	}
}

func TestTogetherStream_IncludeRawChunksMatchesTypeScript(t *testing.T) {
	sseData := `data: {"id":"raw-1","created":1702657020,"model":"meta-llama/test","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"raw-2","created":1702657020,"model":"meta-llama/test","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newTogetherStream(io.NopCloser(strings.NewReader(sseData)))
	stream.IncludeRawChunks = true
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["id"] != "raw-1" {
		t.Fatalf("raw chunk = %#v, want id raw-1", chunk.Raw)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("second chunk type = %v, want response-metadata", chunk.Type)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "raw-1" || chunk.ResponseMetadata.ModelID != "meta-llama/test" {
		t.Fatalf("response metadata = %#v, want first provider event metadata", chunk.ResponseMetadata)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("third chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
		t.Fatalf("third chunk = %#v, want text Hello", chunk)
	}
}

func TestLanguageModelDoGenerateAndDoStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !json.Valid(body) {
			t.Fatalf("invalid json request: %s", string(body))
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Stream", "ok")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("X-Request-Id", "req-123")
		_, _ = w.Write([]byte(`{
			"choices":[{"finish_reason":"stop","message":{"content":"done"}}],
			"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewLanguageModel(p, "meta-llama/test")

	gen, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gen.Text != "done" || gen.FinishReason != types.FinishReasonStop {
		t.Fatalf("unexpected generate result: %+v", gen)
	}
	if gen.ResponseHeaders["X-Request-Id"] != "req-123" {
		t.Fatalf("missing response header propagation: %+v", gen.ResponseHeaders)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "stream"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first stream chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk type = %v, want stream-start", chunk.Type)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second stream chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("second chunk type = %v, want response metadata", chunk.Type)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Text != "hello " {
		t.Fatalf("third stream chunk = %+v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Text != "world" {
		t.Fatalf("fourth stream chunk = %+v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeFinish || chunk.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish chunk = %+v err=%v", chunk, err)
	}
}

func TestLanguageModelConvertResponseNoChoices(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "m")
	res := m.convertResponse(togetherResponse{})
	if res.Text != "" || res.FinishReason != types.FinishReasonOther {
		t.Fatalf("unexpected empty conversion: %+v", res)
	}
}

func intPtr(v int) *int { return &v }
