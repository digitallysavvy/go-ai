package baseten

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func init() {
	provider.RegisterModelDeserializer("baseten.chat", deserializeModel)
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
