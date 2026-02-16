package provider

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockLanguageModel is a simple mock implementation for testing
type mockLanguageModel struct {
	providerName      string
	modelName         string
	toolSupport       bool
	structuredSupport bool
	imageSupport      bool

	mu            sync.Mutex
	generateCalls []*GenerateOptions
	streamCalls   []*GenerateOptions
}

func (m *mockLanguageModel) SpecificationVersion() string { return "v3" }
func (m *mockLanguageModel) Provider() string              { return m.providerName }
func (m *mockLanguageModel) ModelID() string               { return m.modelName }
func (m *mockLanguageModel) SupportsTools() bool          { return m.toolSupport }
func (m *mockLanguageModel) SupportsStructuredOutput() bool { return m.structuredSupport }
func (m *mockLanguageModel) SupportsImageInput() bool     { return m.imageSupport }

func (m *mockLanguageModel) DoGenerate(ctx context.Context, opts *GenerateOptions) (*types.GenerateResult, error) {
	m.mu.Lock()
	m.generateCalls = append(m.generateCalls, opts)
	m.mu.Unlock()
	inputTokens := int64(10)
	outputTokens := int64(5)
	totalTokens := int64(15)
	return &types.GenerateResult{
		Text:         "mock response",
		FinishReason: types.FinishReasonStop,
		Usage:        types.Usage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens},
	}, nil
}

func (m *mockLanguageModel) DoStream(ctx context.Context, opts *GenerateOptions) (TextStream, error) {
	m.mu.Lock()
	m.streamCalls = append(m.streamCalls, opts)
	m.mu.Unlock()
	return &mockTextStream{
		chunks: []StreamChunk{
			{Type: ChunkTypeText, Text: "mock "},
			{Type: ChunkTypeText, Text: "response"},
			{Type: ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		},
	}, nil
}

type mockTextStream struct {
	chunks []StreamChunk
	index  int
	closed bool
}

func (m *mockTextStream) Next() (*StreamChunk, error) {
	if m.closed || m.index >= len(m.chunks) {
		return nil, io.EOF
	}
	chunk := &m.chunks[m.index]
	m.index++
	return chunk, nil
}

func (m *mockTextStream) Read(p []byte) (n int, err error) {
	chunk, err := m.Next()
	if err != nil {
		return 0, err
	}
	if chunk.Type == ChunkTypeText {
		copy(p, chunk.Text)
		return len(chunk.Text), nil
	}
	return 0, nil
}

func (m *mockTextStream) Close() error {
	m.closed = true
	return nil
}

func (m *mockTextStream) Err() error {
	return nil
}

// TestLanguageModel_InterfaceCompliance verifies that implementations correctly
// implement all required methods of the LanguageModel interface
func TestLanguageModel_InterfaceCompliance(t *testing.T) {
	t.Parallel()

	model := &mockLanguageModel{
		providerName:      "test-provider",
		modelName:         "test-model",
		toolSupport:       true,
		structuredSupport: true,
		imageSupport:      true,
	}

	// Test metadata methods
	if model.SpecificationVersion() != "v3" {
		t.Errorf("expected SpecificationVersion 'v3', got %s", model.SpecificationVersion())
	}
	if model.Provider() != "test-provider" {
		t.Errorf("expected Provider 'test-provider', got %s", model.Provider())
	}
	if model.ModelID() != "test-model" {
		t.Errorf("expected ModelID 'test-model', got %s", model.ModelID())
	}

	// Test capability methods
	if !model.SupportsTools() {
		t.Error("expected SupportsTools to return true")
	}
	if !model.SupportsStructuredOutput() {
		t.Error("expected SupportsStructuredOutput to return true")
	}
	if !model.SupportsImageInput() {
		t.Error("expected SupportsImageInput to return true")
	}

	// Test generation methods
	opts := &GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	}
	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error from DoGenerate: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from DoGenerate")
	}

	stream, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error from DoStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected non-nil stream from DoStream")
	}
	defer stream.Close()
}

func TestLanguageModel_CapabilityFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		toolSupport       bool
		structuredSupport bool
		imageSupport      bool
	}{
		{
			name:              "all capabilities",
			toolSupport:       true,
			structuredSupport: true,
			imageSupport:      true,
		},
		{
			name:              "no capabilities",
			toolSupport:       false,
			structuredSupport: false,
			imageSupport:      false,
		},
		{
			name:              "tools only",
			toolSupport:       true,
			structuredSupport: false,
			imageSupport:      false,
		},
		{
			name:              "structured output only",
			toolSupport:       false,
			structuredSupport: true,
			imageSupport:      false,
		},
		{
			name:              "image input only",
			toolSupport:       false,
			structuredSupport: false,
			imageSupport:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := &mockLanguageModel{
				toolSupport:       tt.toolSupport,
				structuredSupport: tt.structuredSupport,
				imageSupport:      tt.imageSupport,
			}

			if model.SupportsTools() != tt.toolSupport {
				t.Errorf("SupportsTools() = %v, want %v", model.SupportsTools(), tt.toolSupport)
			}
			if model.SupportsStructuredOutput() != tt.structuredSupport {
				t.Errorf("SupportsStructuredOutput() = %v, want %v", model.SupportsStructuredOutput(), tt.structuredSupport)
			}
			if model.SupportsImageInput() != tt.imageSupport {
				t.Errorf("SupportsImageInput() = %v, want %v", model.SupportsImageInput(), tt.imageSupport)
			}
		})
	}
}

func TestLanguageModel_GenerateOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options *GenerateOptions
		wantErr bool
	}{
		{
			name: "simple text prompt",
			options: &GenerateOptions{
				Prompt: types.Prompt{Text: "Hello"},
			},
			wantErr: false,
		},
		{
			name: "messages prompt",
			options: &GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "with temperature",
			options: &GenerateOptions{
				Prompt:     types.Prompt{Text: "Test"},
				Temperature: floatPtr(0.7),
			},
			wantErr: false,
		},
		{
			name: "with max tokens",
			options: &GenerateOptions{
				Prompt:   types.Prompt{Text: "Test"},
				MaxTokens: intPtr(100),
			},
			wantErr: false,
		},
		{
			name: "with tools",
			options: &GenerateOptions{
				Prompt: types.Prompt{Text: "Test"},
				Tools: []types.Tool{
					{
						Name:        "test_tool",
						Description: "A test tool",
						Parameters:  map[string]interface{}{"type": "object"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := &mockLanguageModel{}
			_, err := model.DoGenerate(context.Background(), tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("DoGenerate() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify options were passed correctly
			if len(model.generateCalls) != 1 {
				t.Fatalf("expected 1 generate call, got %d", len(model.generateCalls))
			}
		})
	}
}

func TestLanguageModel_StreamOptions(t *testing.T) {
	t.Parallel()

	model := &mockLanguageModel{}

	opts := &GenerateOptions{
		Prompt: types.Prompt{Text: "Stream test"},
	}

	stream, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	// Verify options were passed correctly
	if len(model.streamCalls) != 1 {
		t.Fatalf("expected 1 stream call, got %d", len(model.streamCalls))
	}

	// Verify stream can be read
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error reading stream: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}
}

func TestGenerateOptions_ResponseFormat(t *testing.T) {
	t.Parallel()

	opts := &GenerateOptions{
		Prompt: types.Prompt{Text: "Test"},
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
	}

	if opts.ResponseFormat.Type != "json_object" {
		t.Errorf("expected ResponseFormat.Type 'json_object', got %s", opts.ResponseFormat.Type)
	}
}

func TestChunkType_Constants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		chunkType ChunkType
		want     string
	}{
		{"text", ChunkTypeText, "text"},
		{"tool-call", ChunkTypeToolCall, "tool-call"},
		{"usage", ChunkTypeUsage, "usage"},
		{"finish", ChunkTypeFinish, "finish"},
		{"error", ChunkTypeError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.chunkType) != tt.want {
				t.Errorf("ChunkType = %q, want %q", tt.chunkType, tt.want)
			}
		})
	}
}

// Helper functions
func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}

