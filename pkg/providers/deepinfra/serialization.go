package deepinfra

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("deepinfra", deserializeLanguageModel)
	provider.RegisterImageModelDeserializer("deepinfra", deserializeImageModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS deepinfra-chat-language-model.ts [WORKFLOW_SERIALIZE]. The
// embedded *openai.LanguageModel already implements Serialize(), but its
// Provider() dispatch (ChatProviderName/Name-based) doesn't know about
// DeepInfra's own hardcoded "deepinfra" tag, so this defines the method
// directly on the outer type (taking precedence over the promoted one) and
// only borrows the inner model's already-sanitized Config.
func (m *LanguageModel) Serialize() provider.SerializedModel {
	cfg := map[string]interface{}{}
	if inner, err := provider.SerializeModel(m.LanguageModel); err == nil {
		cfg = inner.Config
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: cfg}
}

func deserializeLanguageModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).LanguageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS deepinfra-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config: provider.SerializableConfig(Config{
			APIKey:  m.provider.apiKey,
			BaseURL: m.editBaseURL,
			Headers: m.provider.headers,
		}),
	}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).ImageModel(serialized.ModelID)
}
