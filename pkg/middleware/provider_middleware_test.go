package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestWrapProvider_NoMiddleware(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{
		ProviderName: "test-provider",
	}

	wrapped := WrapProvider(mockProvider, nil, nil)

	if wrapped.Name() != "test-provider" {
		t.Errorf("expected Name() to return 'test-provider', got %s", wrapped.Name())
	}
}

func TestWrapProvider_NamePassthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider provider.Provider
		want     string
	}{
		{
			name:     "mock provider",
			provider: &testutil.MockProvider{ProviderName: "test"},
			want:     "test",
		},
		{
			name:     "empty name",
			provider: &testutil.MockProvider{ProviderName: ""},
			want:     "mock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := WrapProvider(tt.provider, nil, nil)
			if wrapped.Name() != tt.want {
				t.Errorf("Name() = %s, want %s", wrapped.Name(), tt.want)
			}
		})
	}
}

func TestWrapProvider_LanguageModel_NoMiddleware(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	// Verify it's the same model (no wrapping when no middleware)
	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-model" {
		t.Errorf("expected model ID 'test-model', got %s", model.ModelID())
	}
}

func TestWrapProvider_LanguageModel_WithMiddleware(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	middlewareCalled := false

	middleware := &LanguageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
			middlewareCalled = true
			return doGenerate()
		},
	}

	wrapped := WrapProvider(mockProvider, []*LanguageModelMiddleware{middleware}, nil)

	model, err := wrapped.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	// Call generate to verify middleware is applied
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !middlewareCalled {
		t.Error("expected middleware to be called")
	}
}

func TestWrapProvider_LanguageModel_ErrorPassthrough(t *testing.T) {
	t.Parallel()

	testErr := errors.New("model not found")
	mockProvider := &testutil.MockProvider{
		LanguageModelFunc: func(modelID string) (provider.LanguageModel, error) {
			return nil, testErr
		},
	}

	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.LanguageModel("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err != testErr {
		t.Errorf("expected error %v, got %v", testErr, err)
	}
	if model != nil {
		t.Error("expected nil model on error")
	}
}

func TestWrapProvider_EmbeddingModel_NoMiddleware(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.EmbeddingModel("test-embedding")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-embedding" {
		t.Errorf("expected model ID 'test-embedding', got %s", model.ModelID())
	}
}

func TestWrapProvider_EmbeddingModel_WithMiddleware(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	middlewareCalled := false

	middleware := &EmbeddingModelMiddleware{
		WrapEmbed: func(ctx context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, model provider.EmbeddingModel) (*types.EmbeddingResult, error) {
			middlewareCalled = true
			return doEmbed()
		},
	}

	wrapped := WrapProvider(mockProvider, nil, []*EmbeddingModelMiddleware{middleware})

	model, err := wrapped.EmbeddingModel("test-embedding")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	// Call embed to verify middleware is applied
	_, err = model.DoEmbed(context.Background(), "test input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !middlewareCalled {
		t.Error("expected middleware to be called")
	}
}

func TestWrapProvider_EmbeddingModel_ErrorPassthrough(t *testing.T) {
	t.Parallel()

	testErr := errors.New("embedding model not found")
	mockProvider := &testutil.MockProvider{
		EmbeddingModelFunc: func(modelID string) (provider.EmbeddingModel, error) {
			return nil, testErr
		},
	}

	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.EmbeddingModel("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err != testErr {
		t.Errorf("expected error %v, got %v", testErr, err)
	}
	if model != nil {
		t.Error("expected nil model on error")
	}
}

func TestWrapProvider_ImageModel_Passthrough(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.ImageModel("test-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	// Image models should not have middleware applied
	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-image" {
		t.Errorf("expected model ID 'test-image', got %s", model.ModelID())
	}
}

func TestWrapProvider_SpeechModel_Passthrough(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.SpeechModel("test-speech")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-speech" {
		t.Errorf("expected model ID 'test-speech', got %s", model.ModelID())
	}
}

func TestWrapProvider_TranscriptionModel_Passthrough(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.TranscriptionModel("test-transcription")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-transcription" {
		t.Errorf("expected model ID 'test-transcription', got %s", model.ModelID())
	}
}

func TestWrapProvider_RerankingModel_Passthrough(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, nil, nil)

	model, err := wrapped.RerankingModel("test-reranking")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}

	if model.Provider() != "mock" {
		t.Errorf("expected provider 'mock', got %s", model.Provider())
	}
	if model.ModelID() != "test-reranking" {
		t.Errorf("expected model ID 'test-reranking', got %s", model.ModelID())
	}
}

func TestWrapProvider_BothMiddlewares(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	langMiddlewareCalled := false
	embedMiddlewareCalled := false

	langMiddleware := &LanguageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
			langMiddlewareCalled = true
			return doGenerate()
		},
	}

	embedMiddleware := &EmbeddingModelMiddleware{
		WrapEmbed: func(ctx context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, model provider.EmbeddingModel) (*types.EmbeddingResult, error) {
			embedMiddlewareCalled = true
			return doEmbed()
		},
	}

	wrapped := WrapProvider(mockProvider, []*LanguageModelMiddleware{langMiddleware}, []*EmbeddingModelMiddleware{embedMiddleware})

	// Test language model middleware
	langModel, err := wrapped.LanguageModel("test-lang")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = langModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !langMiddlewareCalled {
		t.Error("expected language model middleware to be called")
	}

	// Test embedding model middleware
	embedModel, err := wrapped.EmbeddingModel("test-embed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = embedModel.DoEmbed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !embedMiddlewareCalled {
		t.Error("expected embedding model middleware to be called")
	}
}

func TestWrapProvider_EmptyMiddlewareSlices(t *testing.T) {
	t.Parallel()

	mockProvider := &testutil.MockProvider{}
	wrapped := WrapProvider(mockProvider, []*LanguageModelMiddleware{}, []*EmbeddingModelMiddleware{})

	// Should work the same as nil slices
	langModel, err := wrapped.LanguageModel("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if langModel == nil {
		t.Fatal("expected non-nil model")
	}

	embedModel, err := wrapped.EmbeddingModel("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if embedModel == nil {
		t.Fatal("expected non-nil model")
	}
}

