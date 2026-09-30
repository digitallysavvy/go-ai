package cerebras

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("cerebras", deserializeModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS cerebras-chat-language-model.ts [WORKFLOW_SERIALIZE]. m.base is
// the underlying OpenAI-compatible chat model (already SerializableModel);
// its Config (BaseURL/Headers, minus APIKey/HTTPClient) is reused as-is, but
// the Provider tag is overridden to "cerebras" to match m.Provider() --
// m.base's own Provider() would otherwise report "cerebras.chat" (via
// OpenAI-compatible's Name+".chat" suffixing), which is not the identifier
// this wrapper actually exposes to callers.
func (m *LanguageModel) Serialize() provider.SerializedModel {
	cfg := map[string]interface{}{}
	if inner, err := provider.SerializeModel(m.base); err == nil {
		cfg = inner.Config
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: cfg}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
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
