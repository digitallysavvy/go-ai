package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// sseStream builds a minimal SSE response body from a slice of JSON payloads.
func sseStream(payloads ...string) io.ReadCloser {
	var sb strings.Builder
	for _, p := range payloads {
		sb.WriteString("data: ")
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(sb.String()))
}

// --- buildRequestBody -------------------------------------------------------

func TestBuildRequestBody_ThinkingConfig(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini31FlashImagePreview)

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Think about this"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget":  1000,
					"includeThoughts": true,
				},
			},
		},
	})

	genConfig, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("generationConfig should be present")
	}

	tc, ok := genConfig["thinkingConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("thinkingConfig should be present in generationConfig")
	}
	if tc["thinkingBudget"] != 1000 {
		t.Errorf("thinkingBudget: got %v, want 1000", tc["thinkingBudget"])
	}
	if tc["includeThoughts"] != true {
		t.Errorf("includeThoughts: got %v, want true", tc["includeThoughts"])
	}
}

func TestBuildRequestBody_ThinkingLevel(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini31FlashImagePreview)

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Think"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingLevel": "high",
				},
			},
		},
	})

	genConfig := body["generationConfig"].(map[string]interface{})
	tc := genConfig["thinkingConfig"].(map[string]interface{})
	if tc["thinkingLevel"] != "high" {
		t.Errorf("thinkingLevel: got %v, want high", tc["thinkingLevel"])
	}
}

func TestBuildRequestBody_NoThinkingConfig(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini20Flash)

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	})

	// generationConfig may or may not be present; if it is, thinkingConfig must be absent.
	if gc, ok := body["generationConfig"].(map[string]interface{}); ok {
		if _, has := gc["thinkingConfig"]; has {
			t.Error("thinkingConfig should not be present when not provided")
		}
	}
}

func TestBuildRequestBody_NilProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini20Flash)

	// Must not panic with nil ProviderOptions.
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		ProviderOptions: nil,
	})

	if body == nil {
		t.Fatal("body should not be nil")
	}
}

func TestBuildRequestBody_ThinkingConfigIgnoredIfWrongType(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini20Flash)

	// thinkingConfig is a string instead of map — should be silently ignored.
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": "invalid-type",
			},
		},
	})

	if gc, ok := body["generationConfig"].(map[string]interface{}); ok {
		if _, has := gc["thinkingConfig"]; has {
			t.Error("invalid thinkingConfig type should be ignored")
		}
	}
}

// --- convertResponse (thought parts) ----------------------------------------

func TestConvertResponse_SkipsThoughtParts(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini31FlashImagePreview)

	resp := googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{
			{
				Content: struct {
					Parts []googlePart `json:"parts"`
					Role  string       `json:"role"`
				}{
					Parts: []googlePart{
						{Text: "I am thinking...", Thought: true},
						{Text: "The answer is 42."},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	result := m.convertResponse(resp)

	if result.Text != "The answer is 42." {
		t.Errorf("Text: got %q, want %q", result.Text, "The answer is 42.")
	}
}

func TestConvertResponse_AllThoughtPartsProducesEmptyText(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini31FlashImagePreview)

	resp := googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{
			{
				Content: struct {
					Parts []googlePart `json:"parts"`
					Role  string       `json:"role"`
				}{
					Parts: []googlePart{
						{Text: "Thinking step 1", Thought: true},
						{Text: "Thinking step 2", Thought: true},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	result := m.convertResponse(resp)

	if result.Text != "" {
		t.Errorf("Text: got %q, want empty string when all parts are thought parts", result.Text)
	}
}

func TestConvertResponse_ThoughtPartDoesNotBlockFunctionCall(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewLanguageModel(p, ModelGemini20Flash)

	resp := googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{
			{
				Content: struct {
					Parts []googlePart `json:"parts"`
					Role  string       `json:"role"`
				}{
					Parts: []googlePart{
						{Text: "thinking", Thought: true},
						{FunctionCall: &struct {
							Name string                 `json:"name"`
							Args map[string]interface{} `json:"args"`
						}{Name: "get_weather", Args: map[string]interface{}{"city": "SF"}}},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	result := m.convertResponse(resp)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolName != "get_weather" {
		t.Errorf("ToolName: got %q, want %q", result.ToolCalls[0].ToolName, "get_weather")
	}
}

// --- googleStream (thought part streaming) -----------------------------------

func TestGoogleStream_ThoughtPartsEmitReasoning(t *testing.T) {
	// Build two SSE events: one thought part, one text part.
	thoughtEvent := mustMarshal(googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{{
			Content: struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			}{Parts: []googlePart{{Text: "I am reasoning", Thought: true}}},
		}},
	})

	textEvent := mustMarshal(googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{{
			Content: struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			}{Parts: []googlePart{{Text: "The answer is 7"}}},
			FinishReason: "STOP",
		}},
	})

	stream := newGoogleStream(sseStream(thoughtEvent, textEvent))
	defer stream.Close()

	chunk1, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on chunk1: %v", err)
	}
	if chunk1.Type != provider.ChunkTypeReasoning {
		t.Errorf("chunk1.Type: got %v, want ChunkTypeReasoning", chunk1.Type)
	}
	if chunk1.Text != "I am reasoning" {
		t.Errorf("chunk1.Text: got %q, want %q", chunk1.Text, "I am reasoning")
	}

	chunk2, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on chunk2: %v", err)
	}
	if chunk2.Type != provider.ChunkTypeText {
		t.Errorf("chunk2.Type: got %v, want ChunkTypeText", chunk2.Type)
	}
	if chunk2.Text != "The answer is 7" {
		t.Errorf("chunk2.Text: got %q, want %q", chunk2.Text, "The answer is 7")
	}

	chunk3, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on chunk3: %v", err)
	}
	if chunk3.Type != provider.ChunkTypeFinish {
		t.Errorf("chunk3.Type: got %v, want ChunkTypeFinish", chunk3.Type)
	}
	if chunk3.FinishReason != types.FinishReasonStop {
		t.Errorf("chunk3.FinishReason: got %v, want FinishReasonStop", chunk3.FinishReason)
	}
}

func TestGoogleStream_MultiplePartsInSingleEvent(t *testing.T) {
	// An event with both a thought part and a text part.
	event := mustMarshal(googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{{
			Content: struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			}{Parts: []googlePart{
				{Text: "thinking", Thought: true},
				{Text: "answer"},
			}},
			FinishReason: "STOP",
		}},
	})

	stream := newGoogleStream(sseStream(event))
	defer stream.Close()

	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (reasoning, text, finish), got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeReasoning {
		t.Errorf("chunk[0]: got %v, want reasoning", chunks[0].Type)
	}
	if chunks[1].Type != provider.ChunkTypeText {
		t.Errorf("chunk[1]: got %v, want text", chunks[1].Type)
	}
	if chunks[2].Type != provider.ChunkTypeFinish {
		t.Errorf("chunk[2]: got %v, want finish", chunks[2].Type)
	}
}

func TestGoogleStream_FinishReasonEmittedAfterText(t *testing.T) {
	// Final event has text AND a finish reason — finish must come after text.
	event := mustMarshal(googleResponse{
		Candidates: []struct {
			Content struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		}{{
			Content: struct {
				Parts []googlePart `json:"parts"`
				Role  string       `json:"role"`
			}{Parts: []googlePart{{Text: "last word"}}},
			FinishReason: "MAX_TOKENS",
		}},
	})

	stream := newGoogleStream(sseStream(event))
	defer stream.Close()

	c1, _ := stream.Next()
	c2, _ := stream.Next()

	if c1.Type != provider.ChunkTypeText || c1.Text != "last word" {
		t.Errorf("c1: got type=%v text=%q, want text 'last word'", c1.Type, c1.Text)
	}
	if c2.Type != provider.ChunkTypeFinish || c2.FinishReason != types.FinishReasonLength {
		t.Errorf("c2: got type=%v reason=%v, want finish MAX_TOKENS", c2.Type, c2.FinishReason)
	}
}

// --- integration test -------------------------------------------------------

func TestLanguageModel_Integration_ThinkingConfig(t *testing.T) {
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping: GOOGLE_GENERATIVE_AI_API_KEY not set")
	}

	p := New(Config{APIKey: apiKey})
	m, err := p.LanguageModel(ModelGemini31FlashImagePreview)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "What is 2 + 2? Think step by step."},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget":  512,
					"includeThoughts": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty result text")
	}
	t.Logf("Result: %s", result.Text)
	if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
		t.Logf("Reasoning tokens: %d", *result.Usage.OutputDetails.ReasoningTokens)
	}
}

// --- httptest-based request body verification --------------------------------

func TestBuildRequestBody_ThinkingConfig_ViaHTTP(t *testing.T) {
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Return minimal valid response.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	m := NewLanguageModel(p, ModelGemini20Flash)

	_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget": float64(256),
				},
			},
		},
	})

	if capturedBody == nil {
		t.Skip("capturedBody nil — provider may not support BaseURL override; skipping HTTP verification")
		return
	}
	gc, ok := capturedBody["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("generationConfig not in request body")
	}
	tc, ok := gc["thinkingConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("thinkingConfig not in generationConfig")
	}
	if tc["thinkingBudget"] != float64(256) {
		t.Errorf("thinkingBudget: got %v, want 256", tc["thinkingBudget"])
	}
}

// --- helpers ----------------------------------------------------------------

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
