package ollama

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderFactoriesAndUnsupported(t *testing.T) {
	p := New(Config{})
	if p.Name() != "ollama" {
		t.Fatalf("Name = %q", p.Name())
	}
	if lm, err := p.LanguageModel(""); err != nil || lm.ModelID() != "llama2" {
		t.Fatalf("default language model mismatch: model=%v err=%v", lm, err)
	}
	if em, err := p.EmbeddingModel(""); err != nil || em.ModelID() != "llama2" {
		t.Fatalf("default embedding model mismatch: model=%v err=%v", em, err)
	}
	if im, err := p.ImageModel("x"); im != nil || err == nil {
		t.Fatalf("ImageModel expected unsupported error, got model=%v err=%v", im, err)
	}
	if sm, err := p.SpeechModel("x"); sm != nil || err == nil {
		t.Fatalf("SpeechModel expected unsupported error, got model=%v err=%v", sm, err)
	}
	if tm, err := p.TranscriptionModel("x"); tm != nil || err == nil {
		t.Fatalf("TranscriptionModel expected unsupported error, got model=%v err=%v", tm, err)
	}
}

func TestLanguageAndEmbeddingHelpers(t *testing.T) {
	p := New(Config{})
	lm := NewLanguageModel(p, "llama3")
	max := 50
	temp := 0.1

	body := lm.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Text:   "hi",
			System: "sys",
		},
		MaxTokens:   &max,
		Temperature: &temp,
	}, true)
	if body["model"] != "llama3" || body["stream"] != true {
		t.Fatalf("language body mismatch: %#v", body)
	}
	msgs, ok := body["messages"].([]map[string]interface{})
	if !ok || len(msgs) == 0 || msgs[0]["role"] != "system" {
		t.Fatalf("messages should include leading system message: %#v", body["messages"])
	}
	if body["max_tokens"] != max || body["temperature"] != temp {
		t.Fatalf("optional params missing: %#v", body)
	}

	usage := convertOllamaUsage(ollamaUsage{
		PromptTokens:     20,
		CompletionTokens: 8,
		TotalTokens:      28,
	})
	if usage.TotalTokens == nil || *usage.TotalTokens != 28 {
		t.Fatalf("usage total tokens = %#v", usage.TotalTokens)
	}

	gr := lm.convertResponse(ollamaResponse{
		Choices: []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				FinishReason: "stop",
				Message: struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				}{
					Content: "hello",
					ToolCalls: []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}{
						{
							ID: "tc1",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	})
	if gr.Text != "hello" || len(gr.ToolCalls) != 1 || gr.ToolCalls[0].ToolName != "weather" {
		t.Fatalf("convertResponse mismatch: %#v", gr)
	}

	em := NewEmbeddingModel(p, "nomic")
	if em.MaxEmbeddingsPerCall() != 1 || !em.SupportsParallelCalls() {
		t.Fatalf("embedding capabilities mismatch")
	}
	if optsHeaders(nil) != nil {
		t.Fatal("optsHeaders(nil) should return nil")
	}
	if h := optsHeaders(&provider.EmbedModelOptions{Headers: map[string]string{"X-A": "B"}}); h["X-A"] != "B" {
		t.Fatalf("optsHeaders mismatch: %#v", h)
	}

	stream := &ollamaStream{OpenAICompatStream: nil}
	_ = stream
}

func TestConvertResponseWithNoChoices(t *testing.T) {
	p := New(Config{})
	lm := NewLanguageModel(p, "llama3")
	r := lm.convertResponse(ollamaResponse{})
	if r.FinishReason != types.FinishReasonOther || r.Text != "" {
		t.Fatalf("unexpected no-choice result: %#v", r)
	}
}
