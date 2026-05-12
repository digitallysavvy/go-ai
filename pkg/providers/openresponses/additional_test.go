package openresponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type nopReadCloser struct {
	io.Reader
}

func (nopReadCloser) Close() error { return nil }

func TestProviderBasicsAndOptionsExtractors(t *testing.T) {
	p := New(Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "k",
		Headers: map[string]string{"X-Test": "1"},
	})
	if p.Name() != "open-responses" || p.Client() == nil {
		t.Fatalf("unexpected provider init: %+v", p)
	}
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected empty model id error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("expected unsupported embedding error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("expected unsupported image error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking error")
	}

	opts := extractOpenResponsesProviderOptions(map[string]interface{}{
		"open-responses": map[string]interface{}{"reasoningSummary": "detailed"},
	}, "open-responses")
	if opts.ReasoningSummary != "detailed" {
		t.Fatalf("provider options parse failed: %+v", opts)
	}
}

func TestConvertToolsChoicesAndUsage(t *testing.T) {
	tools := convertToolsToOpenResponses([]types.Tool{
		{Name: "weather", Description: "lookup", Parameters: map[string]interface{}{"type": "object"}, Strict: true},
	})
	if len(tools) != 1 || tools[0].Name != "weather" || !tools[0].Strict {
		t.Fatalf("tools conversion failed: %+v", tools)
	}

	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "auto"}); got != "auto" {
		t.Fatalf("auto choice = %#v", got)
	}
	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "required"}); got != "required" {
		t.Fatalf("required choice = %#v", got)
	}
	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "none"}); got != "none" {
		t.Fatalf("none choice = %#v", got)
	}
	toolChoice := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "tool", ToolName: "weather"})
	choiceMap, ok := toolChoice.(map[string]interface{})
	if !ok || choiceMap["name"] != "weather" {
		t.Fatalf("tool choice conversion failed: %#v", toolChoice)
	}

	usage := convertOpenResponsesUsage(&Usage{
		InputTokens:         10,
		OutputTokens:        6,
		TotalTokens:         16,
		InputTokensDetails:  &InputTokensDetails{CachedTokens: 4},
		OutputTokensDetails: &OutputTokensDetails{ReasoningTokens: 2},
	})
	if usage.InputDetails == nil || usage.OutputDetails == nil {
		t.Fatalf("expected detailed usage conversion, got %+v", usage)
	}
}

func TestBuildRequestBodyWarningsAndReasoningMapping(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "local-model")

	high := types.ReasoningHigh
	topK := 10
	seed := 42
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       &high,
		TopK:            &topK,
		Seed:            &seed,
		StopSequences:   []string{"\n\n"},
		ProviderOptions: map[string]interface{}{"openResponses": map[string]interface{}{"reasoningSummary": "concise"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if len(warnings) < 3 {
		t.Fatalf("expected warnings for unsupported settings, got %+v", warnings)
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" || reasoning["summary"] != "concise" {
		t.Fatalf("reasoning mapping failed: %+v", reasoning)
	}
}

func TestOpenResponsesStreamHandleEvents(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)
	s.toolCallsByItemID["item-1"] = &toolCallState{ID: "call-1", ToolName: "weather"}

	_, _ = s.handleStreamEvent(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "item-1", Delta: `{"city":"`})
	_, _ = s.handleStreamEvent(&StreamEvent{Type: "response.function_call_arguments.done", ItemID: "item-1", Arguments: `{"city":"nyc"}`})

	chunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "function_call", ID: "item-1"},
	})
	if err != nil {
		t.Fatalf("function_call done error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall || chunk.ToolCall == nil || chunk.ToolCall.ToolName != "weather" {
		t.Fatalf("unexpected tool call chunk: %+v", chunk)
	}

	customChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "custom_tool_call", CallID: "call-2", Name: "custom", Input: "raw"},
	})
	if err != nil || customChunk.ToolCall == nil || customChunk.ToolCall.Arguments["input"] != "raw" {
		t.Fatalf("custom_tool_call conversion failed: chunk=%+v err=%v", customChunk, err)
	}

	reasoningChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "reasoning", ID: "r1", EncryptedContent: "enc"},
	})
	if err != nil || reasoningChunk.Type != provider.ChunkTypeReasoningEnd {
		t.Fatalf("reasoning chunk failed: chunk=%+v err=%v", reasoningChunk, err)
	}
	if !json.Valid(reasoningChunk.ProviderMetadata) {
		t.Fatalf("expected json provider metadata, got: %s", string(reasoningChunk.ProviderMetadata))
	}

	finishChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.completed",
		Response: &OpenResponsesResponse{
			IncompleteDetails: &IncompleteDetails{Reason: "max_output_tokens"},
			Usage:             &Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
		},
	})
	if err != nil || finishChunk.Type != provider.ChunkTypeFinish || finishChunk.Usage == nil {
		t.Fatalf("finish chunk failed: chunk=%+v err=%v", finishChunk, err)
	}

	_, err = s.handleStreamEvent(&StreamEvent{
		Type:  "error",
		Error: &ResponseError{Code: "bad_request", Message: "boom"},
	})
	if err == nil {
		t.Fatal("expected stream error event to return error")
	}
}
