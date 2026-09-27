package middleware

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockLanguageModel is a test implementation of provider.LanguageModel
type mockLanguageModel struct {
	generateResult *types.GenerateResult
	generateError  error
	stream         provider.TextStream
	streamError    error
}

func (m *mockLanguageModel) SpecificationVersion() string { return "v3" }
func (m *mockLanguageModel) Provider() string              { return "test" }
func (m *mockLanguageModel) ModelID() string               { return "test-model" }
func (m *mockLanguageModel) SupportsTools() bool           { return true }
func (m *mockLanguageModel) SupportsStructuredOutput() bool { return true }
func (m *mockLanguageModel) SupportsImageInput() bool     { return false }

func (m *mockLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.generateError != nil {
		return nil, m.generateError
	}
	return m.generateResult, nil
}

func (m *mockLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if m.streamError != nil {
		return nil, m.streamError
	}
	return m.stream, nil
}

// mockTextStream is a test implementation of provider.TextStream
type mockTextStream struct {
	chunks  []*provider.StreamChunk
	current int
}

func (m *mockTextStream) Next() (*provider.StreamChunk, error) {
	if m.current >= len(m.chunks) {
		return nil, io.EOF
	}
	chunk := m.chunks[m.current]
	m.current++
	return chunk, nil
}

func (m *mockTextStream) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (m *mockTextStream) Close() error {
	return nil
}

func (m *mockTextStream) Err() error {
	return nil
}

func TestExtractJSONMiddleware_DefaultTransform_Generate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json with markdown fence",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: "{\"key\": \"value\"}",
		},
		{
			name:     "json with plain fence",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: "{\"key\": \"value\"}",
		},
		{
			name:     "json without fence",
			input:    "{\"key\": \"value\"}",
			expected: "{\"key\": \"value\"}",
		},
		{
			name:     "json with extra whitespace",
			input:    "```json\n  {\"key\": \"value\"}  \n```",
			expected: "{\"key\": \"value\"}",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockModel := &mockLanguageModel{
				generateResult: &types.GenerateResult{
					Text: tt.input,
				},
			}

			middleware := ExtractJSONMiddleware(nil)
			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Text != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.Text)
			}
		})
	}
}

func TestExtractJSONMiddleware_CustomTransform_Generate(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "PREFIX: {\"key\": \"value\"}",
		},
	}

	middleware := ExtractJSONMiddleware(&ExtractJSONOptions{
		Transform: func(text string) string {
			// Custom transform: remove "PREFIX: "
			if len(text) > 8 {
				return text[8:]
			}
			return text
		},
	})

	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "{\"key\": \"value\"}"
	if result.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Text)
	}
}

func TestExtractJSONMiddleware_Stream(t *testing.T) {
	tests := []struct {
		name         string
		chunks       []*provider.StreamChunk
		expectedText string // expected combined text
	}{
		{
			name: "simple json with fence",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "```json\n"},
				{Type: provider.ChunkTypeText, Text: "{\"key\":"},
				{Type: provider.ChunkTypeText, Text: " \"value\"}"},
				{Type: provider.ChunkTypeText, Text: "\n```"},
			},
			expectedText: "{\"key\": \"value\"}",
		},
		{
			name: "json without fence",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "{\"key\":"},
				{Type: provider.ChunkTypeText, Text: " \"value\"}"},
			},
			expectedText: "{\"key\": \"value\"}",
		},
		{
			name: "non-text chunks pass through",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "{\"key\": \"value\"}"},
				{Type: provider.ChunkTypeUsage, Usage: &types.Usage{}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			},
			expectedText: "{\"key\": \"value\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStream := &mockTextStream{chunks: tt.chunks}
			mockModel := &mockLanguageModel{stream: mockStream}

			middleware := ExtractJSONMiddleware(nil)
			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var combinedText string
			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error during streaming: %v", err)
				}

				if chunk.Type == provider.ChunkTypeText {
					combinedText += chunk.Text
				}
			}

			if combinedText != tt.expectedText {
				t.Errorf("expected %q, got %q", tt.expectedText, combinedText)
			}
		})
	}
}

func TestExtractJSONMiddleware_Stream_WithFinalBuffer(t *testing.T) {
	// Test that buffered content at the end is properly flushed
	mockStream := &mockTextStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "```json\n"},
			{Type: provider.ChunkTypeText, Text: "{\"key\": \"value\"}\n```"},
		},
	}

	mockModel := &mockLanguageModel{stream: mockStream}
	middleware := ExtractJSONMiddleware(nil)
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var allText string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if chunk.Type == provider.ChunkTypeText {
			allText += chunk.Text
		}
	}

	expected := "{\"key\": \"value\"}"
	if allText != expected {
		t.Errorf("expected %q, got %q", expected, allText)
	}
}

// TestExtractJSONMiddleware_Stream_LongTrailingWhitespaceAfterFence ports the
// TS "should strip a closing fence followed by long trailing whitespace"
// case (audit row 1058ed5 / WG12): with the old fixed 12-byte suffix buffer,
// a closing fence followed by 20+ trailing whitespace characters pushed the
// fence itself out of the buffering window and leaked "```" into the output.
// getPotentialSuffixStart must hold back arbitrarily long trailing
// whitespace plus the fence.
func TestExtractJSONMiddleware_Stream_LongTrailingWhitespaceAfterFence(t *testing.T) {
	const jsonText = `{"value": "test"}`
	fencedJSON := "```json\n" + jsonText + "\n```"

	for _, trailing := range []string{strings.Repeat(" ", 20), strings.Repeat("\n", 20)} {
		text := fencedJSON + trailing

		layouts := [][]string{
			{text},
			splitToChars(text),
			{"```json\n" + jsonText + "\n", "```", trailing},
		}

		for li, chunkTexts := range layouts {
			chunks := make([]*provider.StreamChunk, len(chunkTexts))
			for i, c := range chunkTexts {
				chunks[i] = &provider.StreamChunk{Type: provider.ChunkTypeText, Text: c}
			}

			mockStream := &mockTextStream{chunks: chunks}
			mockModel := &mockLanguageModel{stream: mockStream}
			middleware := ExtractJSONMiddleware(nil)
			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("layout %d: unexpected error: %v", li, err)
			}

			var allText string
			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("layout %d: unexpected error: %v", li, err)
				}
				if chunk.Type == provider.ChunkTypeText {
					allText += chunk.Text
				}
			}

			if allText != jsonText {
				t.Errorf("layout %d (trailing %q): got %q, want %q", li, trailing, allText, jsonText)
			}
		}
	}
}

func splitToChars(s string) []string {
	runes := []rune(s)
	out := make([]string, len(runes))
	for i, r := range runes {
		out[i] = string(r)
	}
	return out
}
