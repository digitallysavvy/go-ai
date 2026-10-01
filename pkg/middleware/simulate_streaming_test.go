package middleware

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Helper function to convert int to *int64
func int64Ptr(i int64) *int64 {
	return &i
}

func TestSimulateStreamingMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		generateResult *types.GenerateResult
		expectedChunks int
	}{
		{
			name: "text only",
			generateResult: &types.GenerateResult{
				Text:         "Hello, world!",
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					TotalTokens: int64Ptr(10),
				},
			},
			expectedChunks: 5, // stream-start, text-start, text-delta, text-end, finish
		},
		{
			name: "text with tool calls",
			generateResult: &types.GenerateResult{
				Text: "Let me help",
				ToolCalls: []types.ToolCall{
					{
						ID:       "call1",
						ToolName: "get_weather",
						Arguments: map[string]interface{}{
							"city": "NYC",
						},
					},
				},
				FinishReason: types.FinishReasonToolCalls,
				Usage: types.Usage{
					TotalTokens: int64Ptr(15),
				},
			},
			expectedChunks: 6, // stream-start, text-start, text-delta, text-end, tool-call, finish
		},
		{
			name: "empty text",
			generateResult: &types.GenerateResult{
				Text:         "",
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					TotalTokens: int64Ptr(5),
				},
			},
			expectedChunks: 2, // stream-start, finish (no text chunk)
		},
		{
			name: "multiple tool calls",
			generateResult: &types.GenerateResult{
				Text: "Multiple tools",
				ToolCalls: []types.ToolCall{
					{ID: "call1", ToolName: "tool1"},
					{ID: "call2", ToolName: "tool2"},
				},
				FinishReason: types.FinishReasonToolCalls,
				Usage: types.Usage{
					TotalTokens: int64Ptr(20),
				},
			},
			expectedChunks: 7, // stream-start, text-start, text-delta, text-end, tool-call1, tool-call2, finish
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockModel := &mockLanguageModel{
				generateResult: tt.generateResult,
			}

			middleware := SimulateStreamingMiddleware()
			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			chunkCount := 0
			var hasText, hasToolCall, hasUsage, hasFinish bool

			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error during streaming: %v", err)
				}

				chunkCount++

				switch chunk.Type {
				case provider.ChunkTypeText:
					hasText = true
					if chunk.Text != tt.generateResult.Text {
						t.Errorf("text chunk: expected %q, got %q", tt.generateResult.Text, chunk.Text)
					}
				case provider.ChunkTypeToolCall:
					hasToolCall = true
					if chunk.ToolCall == nil {
						t.Error("tool call chunk has nil ToolCall")
					}
				case provider.ChunkTypeFinish:
					hasFinish = true
					hasUsage = chunk.Usage != nil
					if chunk.FinishReason != tt.generateResult.FinishReason {
						t.Errorf("finish reason: expected %v, got %v", tt.generateResult.FinishReason, chunk.FinishReason)
					}
				}
			}

			if chunkCount != tt.expectedChunks {
				t.Errorf("expected %d chunks, got %d", tt.expectedChunks, chunkCount)
			}

			if len(tt.generateResult.Text) > 0 && !hasText {
				t.Error("expected text chunk but didn't get one")
			}

			if len(tt.generateResult.ToolCalls) > 0 && !hasToolCall {
				t.Error("expected tool call chunk but didn't get one")
			}

			if !hasUsage {
				t.Error("expected usage chunk but didn't get one")
			}

			if !hasFinish {
				t.Error("expected finish chunk but didn't get one")
			}
		})
	}
}

func TestSimulateStreamingMiddleware_ChunkOrder(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "test",
			ToolCalls: []types.ToolCall{
				{ID: "call1", ToolName: "tool1"},
			},
			FinishReason: types.FinishReasonToolCalls,
			Usage:        types.Usage{TotalTokens: int64Ptr(10)},
		},
	}

	middleware := SimulateStreamingMiddleware()
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify chunk order: stream-start -> text-start -> text -> text-end -> tool-call -> finish
	expectedOrder := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeTextStart,
		provider.ChunkTypeText,
		provider.ChunkTypeTextEnd,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeFinish,
	}

	for i, expectedType := range expectedOrder {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatalf("unexpected error at chunk %d: %v", i, err)
		}

		if chunk.Type != expectedType {
			t.Errorf("chunk %d: expected type %v, got %v", i, expectedType, chunk.Type)
		}
	}

	// Verify stream ends
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

// TestSimulateStreamingMiddleware_ProviderExecutedToolResultContent verifies
// that a provider-executed tool result carried in GenerateResult.Content
// (e.g. Anthropic tool-search, xAI web search) is forwarded as a
// ChunkTypeToolResult rather than silently dropped: unlike tool calls,
// types.GenerateResult has no flat ToolResults field to fall back to.
func TestSimulateStreamingMiddleware_ProviderExecutedToolResultContent(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "",
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID:       "call1",
					ToolName:         "web_search",
					Result:           "some search result",
					ProviderExecuted: true,
				},
			},
			FinishReason: types.FinishReasonStop,
			Usage:        types.Usage{TotalTokens: int64Ptr(10)},
		},
	}

	middleware := SimulateStreamingMiddleware()
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *types.ToolResult
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolResult {
			found = chunk.ToolResult
		}
	}

	if found == nil {
		t.Fatal("provider-executed tool result was dropped, want a ChunkTypeToolResult chunk")
	}
	if found.ToolCallID != "call1" || found.ToolName != "web_search" {
		t.Errorf("unexpected tool result: %+v", found)
	}
	if found.Result != "some search result" {
		t.Errorf("Result = %v, want %q", found.Result, "some search result")
	}
	if !found.ProviderExecuted {
		t.Error("ProviderExecuted = false, want true")
	}
}

func TestSimulateStreamingMiddleware_Close(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text:         "test",
			FinishReason: types.FinishReasonStop,
			Usage:        types.Usage{TotalTokens: int64Ptr(5)},
		},
	}

	middleware := SimulateStreamingMiddleware()
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close the stream
	err = stream.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	// Verify Next returns EOF after close
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected EOF after close, got %v", err)
	}
}

// TestSimulateStreamingMiddleware_PreservesTextPartMetadata ports the TS
// "should preserve provider metadata" case (audit row 4775577 / WG12): a
// text content part's ProviderMetadata must land on its text-start chunk,
// and the top-level result ProviderMetadata must land on the finish chunk.
func TestSimulateStreamingMiddleware_PreservesTextPartMetadata(t *testing.T) {
	textMeta := json.RawMessage(`{"google":{"thoughtSignature":"sig"}}`)
	finishMeta := map[string]interface{}{"google": map[string]interface{}{"finishMessage": "done"}}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "hello",
			Content: []types.ContentPart{
				types.TextContent{Text: "hello", ProviderMetadata: textMeta},
			},
			FinishReason:     types.FinishReasonStop,
			Usage:            types.Usage{TotalTokens: int64Ptr(3)},
			ProviderMetadata: finishMeta,
		},
	}

	middleware := SimulateStreamingMiddleware()
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawTextStartMeta, sawFinishMeta bool
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeTextStart:
			if string(chunk.ProviderMetadata) == string(textMeta) {
				sawTextStartMeta = true
			}
		case provider.ChunkTypeFinish:
			if len(chunk.ProviderMetadata) > 0 {
				sawFinishMeta = true
			}
		}
	}

	if !sawTextStartMeta {
		t.Error("text-start chunk did not carry the text part's ProviderMetadata")
	}
	if !sawFinishMeta {
		t.Error("finish chunk did not carry the result's ProviderMetadata")
	}
}
