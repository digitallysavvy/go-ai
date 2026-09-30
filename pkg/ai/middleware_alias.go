package ai

import (
	mw "github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Middleware type aliases exposed from the ai package for TS-style discoverability.
type LanguageModelMiddleware = mw.LanguageModelMiddleware
type EmbeddingModelMiddleware = mw.EmbeddingModelMiddleware
type ImageModelMiddleware = mw.ImageModelMiddleware
type ExtractReasoningOptions = mw.ExtractReasoningOptions
type ExtractJSONOptions = mw.ExtractJSONOptions
type AddToolInputExamplesOptions = mw.AddToolInputExamplesOptions

func WrapLanguageModel(model provider.LanguageModel, middleware []*LanguageModelMiddleware, modelID, providerID *string) provider.LanguageModel {
	return mw.WrapLanguageModel(model, middleware, modelID, providerID)
}

func WrapEmbeddingModel(model provider.EmbeddingModel, middleware []*EmbeddingModelMiddleware, modelID, providerID *string) provider.EmbeddingModel {
	return mw.WrapEmbeddingModel(model, middleware, modelID, providerID)
}

func WrapImageModel(model provider.ImageModel, middleware []*ImageModelMiddleware, modelID, providerID *string) provider.ImageModel {
	return mw.WrapImageModel(model, middleware, modelID, providerID)
}

func WrapProvider(p provider.Provider, languageModelMiddleware []*LanguageModelMiddleware, embeddingModelMiddleware []*EmbeddingModelMiddleware, opts ...mw.ProviderMiddlewareOption) provider.Provider {
	return mw.WrapProvider(p, languageModelMiddleware, embeddingModelMiddleware, opts...)
}

// WithImageModelMiddleware sets the middleware applied to every image model
// resolved through a provider wrapped via WrapProvider, matching
// TypeScript's wrapProvider options.imageModelMiddleware.
func WithImageModelMiddleware(imageModelMiddleware []*ImageModelMiddleware) mw.ProviderMiddlewareOption {
	return mw.WithImageModelMiddleware(imageModelMiddleware)
}

func SimulateStreamingMiddleware() *LanguageModelMiddleware {
	return mw.SimulateStreamingMiddleware()
}

func DefaultSettingsMiddleware(settings *provider.GenerateOptions) *LanguageModelMiddleware {
	return mw.DefaultSettingsMiddleware(settings)
}

func ExtractReasoningMiddleware(options *ExtractReasoningOptions) *LanguageModelMiddleware {
	return mw.ExtractReasoningMiddleware(options)
}

func ExtractJSONMiddleware(options *ExtractJSONOptions) *LanguageModelMiddleware {
	return mw.ExtractJSONMiddleware(options)
}

func AddToolInputExamplesMiddleware(options *AddToolInputExamplesOptions) *LanguageModelMiddleware {
	return mw.AddToolInputExamplesMiddleware(options)
}
