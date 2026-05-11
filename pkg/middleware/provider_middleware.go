package middleware

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// wrappedProvider wraps a Provider with middleware
type wrappedProvider struct {
	provider                 provider.Provider
	languageModelMiddleware  []*LanguageModelMiddleware
	embeddingModelMiddleware []*EmbeddingModelMiddleware
}

// WrapProvider wraps a Provider instance with middleware functionality.
// This function allows you to apply middleware to all language models and
// embedding models from the provider.
func WrapProvider(p provider.Provider, languageModelMiddleware []*LanguageModelMiddleware, embeddingModelMiddleware []*EmbeddingModelMiddleware) provider.Provider {
	wrapped := &wrappedProvider{
		provider:                 p,
		languageModelMiddleware:  languageModelMiddleware,
		embeddingModelMiddleware: embeddingModelMiddleware,
	}
	_, hasFiles := p.(provider.FilesProvider)
	_, hasSkills := p.(provider.SkillsProvider)
	switch {
	case hasFiles && hasSkills:
		return &wrappedProviderWithFilesAndSkills{wrappedProvider: wrapped}
	case hasFiles:
		return &wrappedProviderWithFiles{wrappedProvider: wrapped}
	case hasSkills:
		return &wrappedProviderWithSkills{wrappedProvider: wrapped}
	default:
		return wrapped
	}
}

// Name returns the provider name
func (w *wrappedProvider) Name() string {
	return w.provider.Name()
}

// LanguageModel returns a language model by ID, with middleware applied
func (w *wrappedProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	model, err := w.provider.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}

	if len(w.languageModelMiddleware) > 0 {
		model = WrapLanguageModel(model, w.languageModelMiddleware, nil, nil)
	}

	return model, nil
}

// EmbeddingModel returns an embedding model by ID, with middleware applied
func (w *wrappedProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	model, err := w.provider.EmbeddingModel(modelID)
	if err != nil {
		return nil, err
	}

	if len(w.embeddingModelMiddleware) > 0 {
		model = WrapEmbeddingModel(model, w.embeddingModelMiddleware, nil, nil)
	}

	return model, nil
}

// ImageModel returns an image generation model by ID (no middleware applied)
func (w *wrappedProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return w.provider.ImageModel(modelID)
}

// SpeechModel returns a speech synthesis model by ID (no middleware applied)
func (w *wrappedProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return w.provider.SpeechModel(modelID)
}

// TranscriptionModel returns a speech-to-text model by ID (no middleware applied)
func (w *wrappedProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return w.provider.TranscriptionModel(modelID)
}

// RerankingModel returns a reranking model by ID (no middleware applied)
func (w *wrappedProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return w.provider.RerankingModel(modelID)
}

type wrappedProviderWithFiles struct {
	*wrappedProvider
}

func (w *wrappedProviderWithFiles) Files() provider.FilesAPI {
	if fp, ok := w.provider.(provider.FilesProvider); ok {
		return fp.Files()
	}
	return nil
}

type wrappedProviderWithSkills struct {
	*wrappedProvider
}

func (w *wrappedProviderWithSkills) Skills() provider.SkillsAPI {
	if sp, ok := w.provider.(provider.SkillsProvider); ok {
		return sp.Skills()
	}
	return nil
}

type wrappedProviderWithFilesAndSkills struct {
	*wrappedProvider
}

func (w *wrappedProviderWithFilesAndSkills) Files() provider.FilesAPI {
	if fp, ok := w.provider.(provider.FilesProvider); ok {
		return fp.Files()
	}
	return nil
}

func (w *wrappedProviderWithFilesAndSkills) Skills() provider.SkillsAPI {
	if sp, ok := w.provider.(provider.SkillsProvider); ok {
		return sp.Skills()
	}
	return nil
}
