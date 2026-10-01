package baseten

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func init() {
	provider.RegisterModelDeserializer("baseten.chat", deserializeModel)
	provider.RegisterEmbeddingModelDeserializer("baseten.embedding", deserializeEmbeddingModel)
}

// deserializeModel restores a serialized Baseten chat model.
//
// Provider.LanguageModel returns a plain *openai.LanguageModel (Baseten has
// no dedicated chat model type -- TS has no baseten-chat-language-model.ts
// either, it wraps OpenAICompatibleChatLanguageModel directly), configured
// with ChatProviderName: "baseten.chat". openai.LanguageModel.Serialize()
// already exists and already tags the result "baseten.chat" via that
// override, so the only missing piece is a deserializer registered under
// that name. serialized.ModelID already carries the concrete resolved model
// ID (e.g. "chat"/"placeholder"/a caller-provided ID), so passing it
// straight to LanguageModel bypasses the ModelURL-based default-resolution
// branch entirely.
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg openai.Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(Config{BaseURL: cfg.BaseURL, Headers: cfg.Headers}).LanguageModel(serialized.ModelID)
}

// deserializeEmbeddingModel restores a serialized Baseten embedding model.
//
// Provider.EmbeddingModel wraps a plain *openai.EmbeddingModel (embeddingModel
// embeds it), so Serialize() is promoted from openai.EmbeddingModel and
// already tags the result "baseten.embedding" (the Name set on the internal
// openai.Provider used for embeddings). The serialized BaseURL is the
// already-resolved embeddings endpoint (ModelURL run through
// basetenEmbeddingURL); passing it back as ModelURL re-applies that
// transform, which is idempotent for an already-resolved "/sync/v1" URL.
func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg openai.Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(Config{ModelURL: cfg.BaseURL, Headers: cfg.Headers}).EmbeddingModel(serialized.ModelID)
}
