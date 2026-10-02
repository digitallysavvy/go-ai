package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports of ai/packages/ai/src/middleware/wrap-image-model.test.ts.

func TestWrapImageModel_NoMiddleware(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ProviderName: "test", ModelName: "test-model"}

	wrapped := WrapImageModel(model, []*ImageModelMiddleware{}, nil, nil)

	if wrapped != model {
		t.Error("expected same model when no middleware")
	}
}

// "model property: should pass through by default"
func TestWrapImageModel_ModelID_PassThrough(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ModelName: "original-model"}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{{}}, nil, nil)

	if wrapped.ModelID() != "original-model" {
		t.Errorf("expected 'original-model', got %s", wrapped.ModelID())
	}
}

// "model property: should use middleware overrideModelId if provided"
func TestWrapImageModel_OverrideModelID(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ModelName: "original-model"}
	middleware := &ImageModelMiddleware{
		OverrideModelID: func(model provider.ImageModel) string {
			return "override-model"
		},
	}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	if wrapped.ModelID() != "override-model" {
		t.Errorf("expected 'override-model', got %s", wrapped.ModelID())
	}
}

// "model property: should use modelId parameter if provided"
func TestWrapImageModel_ModelIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ModelName: "original-model"}
	middleware := &ImageModelMiddleware{
		OverrideModelID: func(model provider.ImageModel) string {
			return "override-model"
		},
	}
	paramModelID := "param-model"
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, &paramModelID, nil)

	if wrapped.ModelID() != "param-model" {
		t.Errorf("expected 'param-model' to take precedence, got %s", wrapped.ModelID())
	}
}

// "provider property: should pass through by default"
func TestWrapImageModel_Provider_PassThrough(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ProviderName: "original-provider"}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{{}}, nil, nil)

	if wrapped.Provider() != "original-provider" {
		t.Errorf("expected 'original-provider', got %s", wrapped.Provider())
	}
}

// "provider property: should use middleware overrideProvider if provided"
func TestWrapImageModel_OverrideProvider(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ProviderName: "original-provider"}
	middleware := &ImageModelMiddleware{
		OverrideProvider: func(model provider.ImageModel) string {
			return "override-provider"
		},
	}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	if wrapped.Provider() != "override-provider" {
		t.Errorf("expected 'override-provider', got %s", wrapped.Provider())
	}
}

// "provider property: should use providerId parameter if provided"
func TestWrapImageModel_ProviderIDParam(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{ProviderName: "original-provider"}
	middleware := &ImageModelMiddleware{
		OverrideProvider: func(model provider.ImageModel) string {
			return "override-provider"
		},
	}
	paramProviderID := "param-provider"
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, &paramProviderID)

	if wrapped.Provider() != "param-provider" {
		t.Errorf("expected 'param-provider' to take precedence, got %s", wrapped.Provider())
	}
}

// "maxImagesPerCall property: should pass through by default"
func TestWrapImageModel_MaxImagesPerCall_PassThrough(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{MaxImages: 5}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{{}}, nil, nil)

	wrappedWithMax, ok := wrapped.(interface{ MaxImagesPerCall() int })
	if !ok {
		t.Fatal("expected wrapped model to expose MaxImagesPerCall")
	}
	if wrappedWithMax.MaxImagesPerCall() != 5 {
		t.Errorf("expected 5, got %d", wrappedWithMax.MaxImagesPerCall())
	}
}

// "maxImagesPerCall property: should use middleware overrideMaxImagesPerCall if provided"
func TestWrapImageModel_OverrideMaxImagesPerCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{MaxImages: 5}
	middleware := &ImageModelMiddleware{
		OverrideMaxImagesPerCall: func(model provider.ImageModel) int {
			return 10
		},
	}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	wrappedWithMax := wrapped.(interface{ MaxImagesPerCall() int })
	if wrappedWithMax.MaxImagesPerCall() != 10 {
		t.Errorf("expected 10, got %d", wrappedWithMax.MaxImagesPerCall())
	}
}

func boolPtr(b bool) *bool { return &b }

// Ports wrap-image-model.test.ts's file/mask capability-propagation cases
// (TS #19230): the wrapped model passes through the underlying model's
// advertised capability when no override is configured.
func TestWrapImageModel_SupportsFileAndMaskInputs_PassThrough(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{SupportsFiles: boolPtr(true), SupportsMasks: boolPtr(false)}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{{}}, nil, nil)

	wrappedModel, ok := wrapped.(interface {
		SupportsFileInputs() *bool
		SupportsMaskInputs() *bool
	})
	if !ok {
		t.Fatal("expected wrapped model to expose SupportsFileInputs/SupportsMaskInputs")
	}
	if got := wrappedModel.SupportsFileInputs(); got == nil || *got != true {
		t.Errorf("SupportsFileInputs() = %v, want true", got)
	}
	if got := wrappedModel.SupportsMaskInputs(); got == nil || *got != false {
		t.Errorf("SupportsMaskInputs() = %v, want false", got)
	}
}

// A model that doesn't implement the optional capability methods at all
// (e.g. a legacy adapter) reports unknown (nil) through the wrapper too.
func TestWrapImageModel_SupportsFileAndMaskInputs_UnknownWhenUnset(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{{}}, nil, nil)

	wrappedModel := wrapped.(interface {
		SupportsFileInputs() *bool
		SupportsMaskInputs() *bool
	})
	if got := wrappedModel.SupportsFileInputs(); got != nil {
		t.Errorf("SupportsFileInputs() = %v, want nil (unknown)", *got)
	}
	if got := wrappedModel.SupportsMaskInputs(); got != nil {
		t.Errorf("SupportsMaskInputs() = %v, want nil (unknown)", *got)
	}
}

// "should use middleware overrideSupportsFileInputs/overrideSupportsMaskInputs
// if provided", including overriding to explicit unknown (nil).
func TestWrapImageModel_OverrideSupportsFileAndMaskInputs(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{SupportsFiles: boolPtr(false), SupportsMasks: boolPtr(false)}
	middleware := &ImageModelMiddleware{
		OverrideSupportsFileInputs: func(model provider.ImageModel) *bool { return boolPtr(true) },
		OverrideSupportsMaskInputs: func(model provider.ImageModel) *bool { return nil },
	}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	wrappedModel := wrapped.(interface {
		SupportsFileInputs() *bool
		SupportsMaskInputs() *bool
	})
	if got := wrappedModel.SupportsFileInputs(); got == nil || *got != true {
		t.Errorf("SupportsFileInputs() = %v, want true (override)", got)
	}
	if got := wrappedModel.SupportsMaskInputs(); got != nil {
		t.Errorf("SupportsMaskInputs() = %v, want nil (overridden to unknown)", *got)
	}
}

// "should call transformParams middleware for doGenerate"
func TestWrapImageModel_TransformParams(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}
	middleware := &ImageModelMiddleware{
		TransformParams: func(ctx context.Context, params *provider.ImageGenerateOptions, model provider.ImageModel) (*provider.ImageGenerateOptions, error) {
			cloned := *params
			cloned.Prompt = "transformed: " + params.Prompt
			return &cloned, nil
		},
	}

	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	var receivedPrompt string
	model.DoGenerateFunc = func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
		receivedPrompt = opts.Prompt
		return &types.ImageResult{}, nil
	}

	_, err := wrapped.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedPrompt != "transformed: a cat" {
		t.Errorf("expected transformed prompt, got %q", receivedPrompt)
	}
}

// "should call wrapGenerate middleware"
func TestWrapImageModel_WrapGenerate(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}
	wrapGenerateCalled := false
	middleware := &ImageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.ImageResult, error), params *provider.ImageGenerateOptions, model provider.ImageModel) (*types.ImageResult, error) {
			wrapGenerateCalled = true
			return doGenerate()
		},
	}

	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	_, err := wrapped.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wrapGenerateCalled {
		t.Error("expected WrapGenerate to be called")
	}
}

func TestWrapImageModel_NoWrapGenerate(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}
	middleware := &ImageModelMiddleware{}
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoGenerate(context.Background(), &provider.ImageGenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

// "multiple middlewares: should call multiple transformParams middlewares in sequence for doGenerate"
func TestWrapImageModel_MultipleMiddleware_TransformParams(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}

	var callOrder []string
	mw1 := &ImageModelMiddleware{
		TransformParams: func(ctx context.Context, params *provider.ImageGenerateOptions, model provider.ImageModel) (*provider.ImageGenerateOptions, error) {
			callOrder = append(callOrder, "mw1")
			return params, nil
		},
	}
	mw2 := &ImageModelMiddleware{
		TransformParams: func(ctx context.Context, params *provider.ImageGenerateOptions, model provider.ImageModel) (*provider.ImageGenerateOptions, error) {
			callOrder = append(callOrder, "mw2")
			return params, nil
		},
	}

	wrapped := WrapImageModel(model, []*ImageModelMiddleware{mw1, mw2}, nil, nil)

	_, err := wrapped.DoGenerate(context.Background(), &provider.ImageGenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(callOrder) != 2 || callOrder[0] != "mw1" || callOrder[1] != "mw2" {
		t.Errorf("expected [mw1 mw2] order, got %v", callOrder)
	}
}

// "multiple middlewares: should chain multiple wrapGenerate middlewares in the correct order"
func TestWrapImageModel_MultipleMiddleware_WrapGenerate(t *testing.T) {
	t.Parallel()

	model := &testutil.MockImageModel{}

	var callOrder []string
	mw1 := &ImageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.ImageResult, error), params *provider.ImageGenerateOptions, model provider.ImageModel) (*types.ImageResult, error) {
			callOrder = append(callOrder, "mw1-before")
			result, err := doGenerate()
			callOrder = append(callOrder, "mw1-after")
			return result, err
		},
	}
	mw2 := &ImageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.ImageResult, error), params *provider.ImageGenerateOptions, model provider.ImageModel) (*types.ImageResult, error) {
			callOrder = append(callOrder, "mw2-before")
			result, err := doGenerate()
			callOrder = append(callOrder, "mw2-after")
			return result, err
		},
	}

	// First middleware transforms first / is outermost; last middleware wraps
	// directly around the model.
	wrapped := WrapImageModel(model, []*ImageModelMiddleware{mw1, mw2}, nil, nil)

	_, err := wrapped.DoGenerate(context.Background(), &provider.ImageGenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"mw1-before", "mw2-before", "mw2-after", "mw1-after"}
	if len(callOrder) != len(want) {
		t.Fatalf("expected %v, got %v", want, callOrder)
	}
	for i := range want {
		if callOrder[i] != want[i] {
			t.Errorf("expected %v, got %v", want, callOrder)
			break
		}
	}
}
