package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModelMiddleware defines middleware that can be applied to language models
// to transform parameters, wrap generation operations, and wrap streaming operations.
type LanguageModelMiddleware struct {
	// SpecificationVersion should be "v3" for the current version
	SpecificationVersion string

	// OverrideProvider allows overriding the provider name
	OverrideProvider func(model provider.LanguageModel) string

	// OverrideModelID allows overriding the model ID
	OverrideModelID func(model provider.LanguageModel) string

	// TransformParams transforms the parameters before they are passed to the language model
	TransformParams func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error)

	// WrapGenerate wraps the generate operation of the language model
	WrapGenerate func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error)

	// WrapStream wraps the stream operation of the language model
	WrapStream func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (provider.TextStream, error)
}

// wrappedLanguageModel wraps a LanguageModel with middleware
type wrappedLanguageModel struct {
	model      provider.LanguageModel
	middleware *LanguageModelMiddleware
	modelID    *string
	providerID *string
}

// WrapLanguageModel wraps a LanguageModel instance with middleware functionality.
// This function allows you to apply middleware to transform parameters,
// wrap generate operations, and wrap stream operations of a language model.
//
// When multiple middlewares are provided, the first middleware will transform
// the input first, and the last middleware will be wrapped directly around the model.
func WrapLanguageModel(model provider.LanguageModel, middleware []*LanguageModelMiddleware, modelID, providerID *string) provider.LanguageModel {
	if len(middleware) == 0 {
		return model
	}

	// Apply middleware in reverse order (last middleware wraps directly around model)
	wrappedModel := model
	for i := len(middleware) - 1; i >= 0; i-- {
		wrappedModel = doWrapLanguageModel(wrappedModel, middleware[i], modelID, providerID)
	}
	return wrappedModel
}

func doWrapLanguageModel(model provider.LanguageModel, middleware *LanguageModelMiddleware, modelID, providerID *string) provider.LanguageModel {
	return &wrappedLanguageModel{
		model:      model,
		middleware: middleware,
		modelID:    modelID,
		providerID: providerID,
	}
}

// SpecificationVersion returns the specification version
func (w *wrappedLanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (w *wrappedLanguageModel) Provider() string {
	if w.providerID != nil {
		return *w.providerID
	}
	if w.middleware.OverrideProvider != nil {
		return w.middleware.OverrideProvider(w.model)
	}
	return w.model.Provider()
}

// ModelID returns the model ID
func (w *wrappedLanguageModel) ModelID() string {
	if w.modelID != nil {
		return *w.modelID
	}
	if w.middleware.OverrideModelID != nil {
		return w.middleware.OverrideModelID(w.model)
	}
	return w.model.ModelID()
}

// SupportsTools returns whether the model supports tool calling
func (w *wrappedLanguageModel) SupportsTools() bool {
	return w.model.SupportsTools()
}

// SupportsStructuredOutput returns whether the model supports structured output
func (w *wrappedLanguageModel) SupportsStructuredOutput() bool {
	return w.model.SupportsStructuredOutput()
}

// SupportsImageInput returns whether the model accepts image inputs
func (w *wrappedLanguageModel) SupportsImageInput() bool {
	return w.model.SupportsImageInput()
}

// DoGenerate performs non-streaming text generation
func (w *wrappedLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Transform parameters if middleware provides transformParams
	transformedOpts := opts
	if w.middleware.TransformParams != nil {
		var err error
		transformedOpts, err = w.middleware.TransformParams(ctx, "generate", opts, w.model)
		if err != nil {
			return nil, err
		}
	}

	// Create the doGenerate and doStream functions
	doGenerate := func() (*types.GenerateResult, error) {
		return w.model.DoGenerate(ctx, transformedOpts)
	}
	doStream := func() (provider.TextStream, error) {
		return w.model.DoStream(ctx, transformedOpts)
	}

	// Wrap generate if middleware provides wrapGenerate
	if w.middleware.WrapGenerate != nil {
		return w.middleware.WrapGenerate(ctx, doGenerate, doStream, transformedOpts, w.model)
	}

	return doGenerate()
}

// DoStream performs streaming text generation
func (w *wrappedLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Transform parameters if middleware provides transformParams
	transformedOpts := opts
	if w.middleware.TransformParams != nil {
		var err error
		transformedOpts, err = w.middleware.TransformParams(ctx, "stream", opts, w.model)
		if err != nil {
			return nil, err
		}
	}

	// Create the doGenerate and doStream functions
	doGenerate := func() (*types.GenerateResult, error) {
		return w.model.DoGenerate(ctx, transformedOpts)
	}
	doStream := func() (provider.TextStream, error) {
		return w.model.DoStream(ctx, transformedOpts)
	}

	// Wrap stream if middleware provides wrapStream
	if w.middleware.WrapStream != nil {
		return w.middleware.WrapStream(ctx, doGenerate, doStream, transformedOpts, w.model)
	}

	return doStream()
}
