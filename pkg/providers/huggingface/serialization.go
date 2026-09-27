package huggingface

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Serialize implements provider.SerializableModel, mirroring TS
// HuggingFaceResponsesLanguageModel's static [WORKFLOW_SERIALIZE], which
// calls serializeModelOptions({ modelId: model.modelId, config: model.config
// }).
func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func init() {
	provider.RegisterModelDeserializer("huggingface", deserializeModel)
	provider.RegisterModelDeserializer("huggingface.responses", deserializeModel)
}

// deserializeModel mirrors TS's static [WORKFLOW_DESERIALIZE], which
// reconstructs `new HuggingFaceResponsesLanguageModel(options.modelId,
// options.config)`.
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	return p.LanguageModel(serialized.ModelID)
}
