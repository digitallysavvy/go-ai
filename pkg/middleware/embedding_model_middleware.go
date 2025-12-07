package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModelMiddleware defines middleware that can be applied to embedding models
// to transform parameters and wrap embedding operations.
type EmbeddingModelMiddleware struct {
	// SpecificationVersion should be "v3" for the current version
	SpecificationVersion string

	// OverrideProvider allows overriding the provider name
	OverrideProvider func(model provider.EmbeddingModel) string

	// OverrideModelID allows overriding the model ID
	OverrideModelID func(model provider.EmbeddingModel) string

	// TransformInput transforms the input before it is passed to the embedding model
	TransformInput func(ctx context.Context, input string, model provider.EmbeddingModel) (string, error)

	// WrapEmbed wraps the embed operation of the embedding model
	WrapEmbed func(ctx context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, model provider.EmbeddingModel) (*types.EmbeddingResult, error)

	// WrapEmbedMany wraps the batch embed operation of the embedding model
	WrapEmbedMany func(ctx context.Context, doEmbedMany func() (*types.EmbeddingsResult, error), inputs []string, model provider.EmbeddingModel) (*types.EmbeddingsResult, error)
}

// wrappedEmbeddingModel wraps an EmbeddingModel with middleware
type wrappedEmbeddingModel struct {
	model      provider.EmbeddingModel
	middleware *EmbeddingModelMiddleware
	modelID    *string
	providerID *string
}

// WrapEmbeddingModel wraps an EmbeddingModel instance with middleware functionality.
// This function allows you to apply middleware to transform parameters and wrap
// embed operations of an embedding model.
//
// When multiple middlewares are provided, the first middleware will transform
// the input first, and the last middleware will be wrapped directly around the model.
func WrapEmbeddingModel(model provider.EmbeddingModel, middleware []*EmbeddingModelMiddleware, modelID, providerID *string) provider.EmbeddingModel {
	if len(middleware) == 0 {
		return model
	}

	// Apply middleware in reverse order (last middleware wraps directly around model)
	wrappedModel := model
	for i := len(middleware) - 1; i >= 0; i-- {
		wrappedModel = doWrapEmbeddingModel(wrappedModel, middleware[i], modelID, providerID)
	}
	return wrappedModel
}

func doWrapEmbeddingModel(model provider.EmbeddingModel, middleware *EmbeddingModelMiddleware, modelID, providerID *string) provider.EmbeddingModel {
	return &wrappedEmbeddingModel{
		model:      model,
		middleware: middleware,
		modelID:    modelID,
		providerID: providerID,
	}
}

// SpecificationVersion returns the specification version
func (w *wrappedEmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (w *wrappedEmbeddingModel) Provider() string {
	if w.providerID != nil {
		return *w.providerID
	}
	if w.middleware.OverrideProvider != nil {
		return w.middleware.OverrideProvider(w.model)
	}
	return w.model.Provider()
}

// ModelID returns the model ID
func (w *wrappedEmbeddingModel) ModelID() string {
	if w.modelID != nil {
		return *w.modelID
	}
	if w.middleware.OverrideModelID != nil {
		return w.middleware.OverrideModelID(w.model)
	}
	return w.model.ModelID()
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
func (w *wrappedEmbeddingModel) MaxEmbeddingsPerCall() int {
	return w.model.MaxEmbeddingsPerCall()
}

// SupportsParallelCalls returns whether parallel calls are supported
func (w *wrappedEmbeddingModel) SupportsParallelCalls() bool {
	return w.model.SupportsParallelCalls()
}

// DoEmbed performs embedding for a single input
func (w *wrappedEmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	// Transform input if middleware provides transformInput
	transformedInput := input
	if w.middleware.TransformInput != nil {
		var err error
		transformedInput, err = w.middleware.TransformInput(ctx, input, w.model)
		if err != nil {
			return nil, err
		}
	}

	// Create the doEmbed function
	doEmbed := func() (*types.EmbeddingResult, error) {
		return w.model.DoEmbed(ctx, transformedInput)
	}

	// Wrap embed if middleware provides wrapEmbed
	if w.middleware.WrapEmbed != nil {
		return w.middleware.WrapEmbed(ctx, doEmbed, transformedInput, w.model)
	}

	return doEmbed()
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (w *wrappedEmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	// Create the doEmbedMany function
	doEmbedMany := func() (*types.EmbeddingsResult, error) {
		return w.model.DoEmbedMany(ctx, inputs)
	}

	// Wrap embed many if middleware provides wrapEmbedMany
	if w.middleware.WrapEmbedMany != nil {
		return w.middleware.WrapEmbedMany(ctx, doEmbedMany, inputs, w.model)
	}

	return doEmbedMany()
}
