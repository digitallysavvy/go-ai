package middleware

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestExtractReasoningMiddleware_Generate(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		tagName           string
		startWithReasoning bool
		expectedText      string
	}{
		{
			name:         "single reasoning block",
			input:        "Some text <think>reasoning here</think> more text",
			tagName:      "think",
			expectedText: "Some text \n more text",
		},
		{
			name:         "multiple reasoning blocks",
			input:        "<think>reason1</think>text1<think>reason2</think>text2",
			tagName:      "think",
			expectedText: "text1\ntext2",
		},
		{
			name:         "no reasoning blocks",
			input:        "just plain text",
			tagName:      "think",
			expectedText: "just plain text",
		},
		{
			name:         "empty reasoning block",
			input:        "text<think></think>more",
			tagName:      "think",
			expectedText: "text\nmore",
		},
		{
			name:               "start with reasoning",
			input:              "reasoning here</think> text after",
			tagName:            "think",
			startWithReasoning: true,
			expectedText:       " text after",
		},
		{
			name:         "different tag name",
			input:        "<reasoning>thinking</reasoning> result",
			tagName:      "reasoning",
			expectedText: " result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockModel := &mockLanguageModel{
				generateResult: &types.GenerateResult{
					Text: tt.input,
				},
			}

			middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{
				TagName:            tt.tagName,
				Separator:          "\n",
				StartWithReasoning: tt.startWithReasoning,
			})

			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Text != tt.expectedText {
				t.Errorf("expected %q, got %q", tt.expectedText, result.Text)
			}
		})
	}
}

func TestExtractReasoningMiddleware_Stream(t *testing.T) {
	tests := []struct {
		name              string
		chunks            []*provider.StreamChunk
		tagName           string
		expectedReasoning []string
		expectedText      []string
	}{
		{
			name: "simple reasoning block",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reasoning"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
				{Type: provider.ChunkTypeText, Text: "text"},
			},
			tagName:           "think",
			expectedReasoning: []string{"reasoning"},
			expectedText:      []string{"text"},
		},
		{
			name: "text then reasoning",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "some text "},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "thinking"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
			},
			tagName:           "think",
			expectedReasoning: []string{"thinking"},
			expectedText:      []string{"some text "},
		},
		{
			name: "multiple switches",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "text1"},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reason1"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
				{Type: provider.ChunkTypeText, Text: "text2"},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reason2"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
			},
			tagName:           "think",
			expectedReasoning: []string{"reason1", "reason2"},
			expectedText:      []string{"text1", "text2"},
		},
		{
			name: "partial tag buffering",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "<th"},
				{Type: provider.ChunkTypeText, Text: "ink>"},
				{Type: provider.ChunkTypeText, Text: "reasoning"},
				{Type: provider.ChunkTypeText, Text: "</th"},
				{Type: provider.ChunkTypeText, Text: "ink>"},
				{Type: provider.ChunkTypeText, Text: "text"},
			},
			tagName:           "think",
			expectedReasoning: []string{"reasoning"},
			expectedText:      []string{"text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStream := &mockTextStream{chunks: tt.chunks}
			mockModel := &mockLanguageModel{stream: mockStream}

			middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{
				TagName:   tt.tagName,
				Separator: "\n",
			})

			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var reasoningChunks []string
			var textChunks []string

			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error during streaming: %v", err)
				}

				if chunk.Type == provider.ChunkTypeReasoning {
					reasoningChunks = append(reasoningChunks, chunk.Reasoning)
				} else if chunk.Type == provider.ChunkTypeText {
					textChunks = append(textChunks, chunk.Text)
				}
			}

			// Compare reasoning chunks
			if len(reasoningChunks) != len(tt.expectedReasoning) {
				t.Errorf("reasoning: expected %d chunks, got %d", len(tt.expectedReasoning), len(reasoningChunks))
			} else {
				for i, expected := range tt.expectedReasoning {
					if reasoningChunks[i] != expected {
						t.Errorf("reasoning chunk %d: expected %q, got %q", i, expected, reasoningChunks[i])
					}
				}
			}

			// Compare text chunks
			if len(textChunks) != len(tt.expectedText) {
				t.Errorf("text: expected %d chunks, got %d", len(tt.expectedText), len(textChunks))
			} else {
				for i, expected := range tt.expectedText {
					if textChunks[i] != expected {
						t.Errorf("text chunk %d: expected %q, got %q", i, expected, textChunks[i])
					}
				}
			}
		})
	}
}

func TestGetPotentialStartIndex(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		searchedText string
		expected     int
	}{
		{
			name:         "complete match",
			text:         "hello world",
			searchedText: "world",
			expected:     6,
		},
		{
			name:         "partial match at end",
			text:         "hello wo",
			searchedText: "world",
			expected:     6,
		},
		{
			name:         "no match",
			text:         "hello",
			searchedText: "world",
			expected:     -1,
		},
		{
			name:         "empty search",
			text:         "hello",
			searchedText: "",
			expected:     -1,
		},
		{
			name:         "match at beginning",
			text:         "world",
			searchedText: "world",
			expected:     0,
		},
		{
			name:         "single char partial",
			text:         "hello w",
			searchedText: "world",
			expected:     6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPotentialStartIndex(tt.text, tt.searchedText)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestExtractReasoningMiddleware_NilOptions(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "<think>reasoning</think>text",
		},
	}

	// Test with nil options - should use defaults
	middleware := ExtractReasoningMiddleware(nil)
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "text"
	if result.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Text)
	}
}
