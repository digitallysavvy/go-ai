package main

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MockModel implements a mock language model for testing
type MockModel struct {
	response string
	err      error
}

func (m *MockModel) DoGenerate(ctx context.Context, options interface{}) (*ai.GenerateTextResult, error) {
	if m.err != nil {
		return nil, m.err
	}

	return &ai.GenerateTextResult{
		Text: m.response,
		Usage: types.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
		FinishReason: "stop",
	}, nil
}

func (m *MockModel) ModelID() string         { return "mock-model" }
func (m *MockModel) Provider() string        { return "mock" }
func (m *MockModel) MaxTokens() int          { return 4096 }
func (m *MockModel) SupportsStreaming() bool { return false }

func TestGenerateText(t *testing.T) {
	mock := &MockModel{response: "Hello, World!"}

	result, err := mock.DoGenerate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result.Text != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got '%s'", result.Text)
	}

	if result.Usage.TotalTokens != 30 {
		t.Errorf("Expected 30 tokens, got %d", result.Usage.TotalTokens)
	}
}

func TestGenerateTextError(t *testing.T) {
	mock := &MockModel{err: context.DeadlineExceeded}

	_, err := mock.DoGenerate(context.Background(), nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func BenchmarkGenerateText(b *testing.B) {
	mock := &MockModel{response: "Benchmark response"}

	for i := 0; i < b.N; i++ {
		mock.DoGenerate(context.Background(), nil)
	}
}
