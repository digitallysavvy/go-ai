package middleware

import (
	"context"
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
			expectedChunks: 3, // text, usage, finish
		},
		{
			name: "text with tool calls",
			generateResult: &types.GenerateResult{
				Text: "Let me help",
				ToolCalls: []types.ToolCall{
					{
						ID:   "call1",
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
			expectedChunks: 4, // text, tool-call, usage, finish
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
			expectedChunks: 2, // usage, finish (no text chunk)
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
			expectedChunks: 5, // text, tool-call1, tool-call2, usage, finish
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
				case provider.ChunkTypeUsage:
					hasUsage = true
					if chunk.Usage == nil {
						t.Error("usage chunk has nil Usage")
					}
				case provider.ChunkTypeFinish:
					hasFinish = true
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

	// Verify chunk order: text -> tool-call -> usage -> finish
	expectedOrder := []provider.ChunkType{
		provider.ChunkTypeText,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeUsage,
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
