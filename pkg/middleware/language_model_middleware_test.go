package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestWrapLanguageModel_NoMiddleware(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "test-model",
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{}, nil, nil)

	// Should return same model when no middleware
	if wrapped != model {
		t.Error("expected same model when no middleware")
	}
}

func TestWrapLanguageModel_TransformParams(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	temp := 0.5
	middleware := &LanguageModelMiddleware{
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			// Override temperature
			params.Temperature = &temp
			return params, nil
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	originalTemp := 0.9
	opts := &provider.GenerateOptions{
		Temperature: &originalTemp,
	}

	_, err := wrapped.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the model received transformed params
	if len(model.GenerateCalls) != 1 {
		t.Fatal("expected 1 generate call")
	}
	if model.GenerateCalls[0].Temperature == nil || *model.GenerateCalls[0].Temperature != temp {
		t.Errorf("expected temperature %f, got %v", temp, model.GenerateCalls[0].Temperature)
	}
}

func TestWrapLanguageModel_WrapGenerate(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	wrapGenerateCalled := false
	middleware := &LanguageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
			wrapGenerateCalled = true
			// Call the original generate
			return doGenerate()
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wrapGenerateCalled {
		t.Error("expected WrapGenerate to be called")
	}
}

func TestWrapLanguageModel_WrapStream(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	wrapStreamCalled := false
	middleware := &LanguageModelMiddleware{
		WrapStream: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (provider.TextStream, error) {
			wrapStreamCalled = true
			// Call the original stream
			return doStream()
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wrapStreamCalled {
		t.Error("expected WrapStream to be called")
	}
}

func TestWrapLanguageModel_OverrideProvider(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ProviderName: "original-provider",
	}

	middleware := &LanguageModelMiddleware{
		OverrideProvider: func(model provider.LanguageModel) string {
			return "custom-provider"
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if wrapped.Provider() != "custom-provider" {
		t.Errorf("expected 'custom-provider', got %s", wrapped.Provider())
	}
}

func TestWrapLanguageModel_OverrideModelID(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ModelName: "original-model",
	}

	middleware := &LanguageModelMiddleware{
		OverrideModelID: func(model provider.LanguageModel) string {
			return "custom-model"
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if wrapped.ModelID() != "custom-model" {
		t.Errorf("expected 'custom-model', got %s", wrapped.ModelID())
	}
}

func TestWrapLanguageModel_MultipleMiddleware(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	callOrder := []string{}

	mw1 := &LanguageModelMiddleware{
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			callOrder = append(callOrder, "mw1")
			return params, nil
		},
	}

	mw2 := &LanguageModelMiddleware{
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			callOrder = append(callOrder, "mw2")
			return params, nil
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{mw1, mw2}, nil, nil)

	_, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
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

func TestWrappedModel_SupportsTools(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{ToolSupport: true}

	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if !wrapped.SupportsTools() {
		t.Error("expected SupportsTools to return true")
	}
}

func TestWrappedModel_SupportsStructuredOutput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}

	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if !wrapped.SupportsStructuredOutput() {
		t.Error("expected SupportsStructuredOutput to return true")
	}
}

func TestWrappedModel_SupportsImageInput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{ImageSupport: true}

	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if !wrapped.SupportsImageInput() {
		t.Error("expected SupportsImageInput to return true")
	}
}

func TestWrappedModel_SpecificationVersion(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	if wrapped.SpecificationVersion() != "v3" {
		t.Errorf("expected 'v3', got %s", wrapped.SpecificationVersion())
	}
}

func TestWrapLanguageModel_ProviderIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ProviderName: "original",
	}

	providerID := "param-provider"
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, &providerID)

	if wrapped.Provider() != "param-provider" {
		t.Errorf("expected 'param-provider', got %s", wrapped.Provider())
	}
}

func TestWrapLanguageModel_ModelIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ModelName: "original",
	}

	modelID := "param-model"
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, &modelID, nil)

	if wrapped.ModelID() != "param-model" {
		t.Errorf("expected 'param-model', got %s", wrapped.ModelID())
	}
}

func TestWrapLanguageModel_NoTransformParams(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	// Middleware without TransformParams
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	temp := 0.7
	opts := &provider.GenerateOptions{
		Temperature: &temp,
	}

	_, err := wrapped.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Params should be passed through unchanged
	if model.GenerateCalls[0].Temperature == nil || *model.GenerateCalls[0].Temperature != temp {
		t.Error("expected params to be unchanged")
	}
}

func TestWrapLanguageModel_NoWrapGenerate(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	// Middleware without WrapGenerate
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestWrapLanguageModel_NoWrapStream(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	// Middleware without WrapStream
	middleware := &LanguageModelMiddleware{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stream == nil {
		t.Error("expected non-nil stream")
	}
}
