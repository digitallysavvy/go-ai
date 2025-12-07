package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestWrapEmbeddingModel_NoMiddleware(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test",
		ModelName:    "test-model",
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{}, nil, nil)

	// Should return same model when no middleware
	if wrapped != model {
		t.Error("expected same model when no middleware")
	}
}

func TestWrapEmbeddingModel_TransformInput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	middleware := &EmbeddingModelMiddleware{
		TransformInput: func(ctx context.Context, input string, model provider.EmbeddingModel) (string, error) {
			// Prefix the input
			return "transformed: " + input, nil
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoEmbed(context.Background(), "test input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the model received transformed input
	if len(model.EmbedCalls) != 1 {
		t.Fatal("expected 1 embed call")
	}
	if model.EmbedCalls[0] != "transformed: test input" {
		t.Errorf("expected transformed input, got %s", model.EmbedCalls[0])
	}
}

func TestWrapEmbeddingModel_WrapEmbed(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	wrapEmbedCalled := false
	middleware := &EmbeddingModelMiddleware{
		WrapEmbed: func(ctx context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, model provider.EmbeddingModel) (*types.EmbeddingResult, error) {
			wrapEmbedCalled = true
			// Call the original embed
			return doEmbed()
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoEmbed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wrapEmbedCalled {
		t.Error("expected WrapEmbed to be called")
	}
}

func TestWrapEmbeddingModel_WrapEmbedMany(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	wrapEmbedManyCalled := false
	middleware := &EmbeddingModelMiddleware{
		WrapEmbedMany: func(ctx context.Context, doEmbedMany func() (*types.EmbeddingsResult, error), inputs []string, model provider.EmbeddingModel) (*types.EmbeddingsResult, error) {
			wrapEmbedManyCalled = true
			return doEmbedMany()
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoEmbedMany(context.Background(), []string{"test1", "test2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wrapEmbedManyCalled {
		t.Error("expected WrapEmbedMany to be called")
	}
}

func TestWrapEmbeddingModel_OverrideProvider(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "original-provider",
	}

	middleware := &EmbeddingModelMiddleware{
		OverrideProvider: func(model provider.EmbeddingModel) string {
			return "custom-provider"
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	if wrapped.Provider() != "custom-provider" {
		t.Errorf("expected 'custom-provider', got %s", wrapped.Provider())
	}
}

func TestWrapEmbeddingModel_OverrideModelID(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ModelName: "original-model",
	}

	middleware := &EmbeddingModelMiddleware{
		OverrideModelID: func(model provider.EmbeddingModel) string {
			return "custom-model"
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	if wrapped.ModelID() != "custom-model" {
		t.Errorf("expected 'custom-model', got %s", wrapped.ModelID())
	}
}

func TestWrapEmbeddingModel_ProviderIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "original",
	}

	providerID := "param-provider"
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, &providerID)

	if wrapped.Provider() != "param-provider" {
		t.Errorf("expected 'param-provider', got %s", wrapped.Provider())
	}
}

func TestWrapEmbeddingModel_ModelIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ModelName: "original",
	}

	modelID := "param-model"
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, &modelID, nil)

	if wrapped.ModelID() != "param-model" {
		t.Errorf("expected 'param-model', got %s", wrapped.ModelID())
	}
}

func TestWrappedEmbeddingModel_SpecificationVersion(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	if wrapped.SpecificationVersion() != "v3" {
		t.Errorf("expected 'v3', got %s", wrapped.SpecificationVersion())
	}
}

func TestWrappedEmbeddingModel_MaxEmbeddingsPerCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings: 100,
	}

	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	if wrapped.MaxEmbeddingsPerCall() != 100 {
		t.Errorf("expected 100, got %d", wrapped.MaxEmbeddingsPerCall())
	}
}

func TestWrappedEmbeddingModel_SupportsParallelCalls(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ParallelSupport: true,
	}

	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	if !wrapped.SupportsParallelCalls() {
		t.Error("expected SupportsParallelCalls to return true")
	}
}

func TestWrapEmbeddingModel_MultipleMiddleware(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	callOrder := []string{}

	mw1 := &EmbeddingModelMiddleware{
		TransformInput: func(ctx context.Context, input string, model provider.EmbeddingModel) (string, error) {
			callOrder = append(callOrder, "mw1")
			return input + "-mw1", nil
		},
	}

	mw2 := &EmbeddingModelMiddleware{
		TransformInput: func(ctx context.Context, input string, model provider.EmbeddingModel) (string, error) {
			callOrder = append(callOrder, "mw2")
			return input + "-mw2", nil
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{mw1, mw2}, nil, nil)

	_, err := wrapped.DoEmbed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Middleware should be applied in order (first middleware transforms first)
	if len(callOrder) != 2 {
		t.Errorf("expected 2 middleware calls, got %d", len(callOrder))
	}
	if callOrder[0] != "mw1" {
		t.Errorf("expected first call to be 'mw1', got %s", callOrder[0])
	}
}

func TestWrapEmbeddingModel_NoTransformInput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	// Middleware without TransformInput
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoEmbed(context.Background(), "original input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Input should be passed through unchanged
	if model.EmbedCalls[0] != "original input" {
		t.Error("expected input to be unchanged")
	}
}

func TestWrapEmbeddingModel_NoWrapEmbed(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	// Middleware without WrapEmbed
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoEmbed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestWrapEmbeddingModel_NoWrapEmbedMany(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	// Middleware without WrapEmbedMany
	middleware := &EmbeddingModelMiddleware{}
	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoEmbedMany(context.Background(), []string{"test1", "test2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestWrapEmbeddingModel_TransformInputError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	middleware := &EmbeddingModelMiddleware{
		TransformInput: func(ctx context.Context, input string, model provider.EmbeddingModel) (string, error) {
			return "", context.Canceled
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoEmbed(context.Background(), "test")
	if err == nil {
		t.Error("expected error from TransformInput")
	}
}
