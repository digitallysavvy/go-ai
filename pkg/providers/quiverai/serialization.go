package quiverai

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func init() {
	provider.RegisterImageModelDeserializer("quiverai.image", deserializeImageModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS quiverai-image-model.ts [WORKFLOW_SERIALIZE]. (QuiverAI's
// LanguageModel already implements SerializeStrict in language_model.go.)
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.modelID,
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, providererrors.NewSerializationError(fmt.Sprintf("quiverai: failed to restore config: %v", err), err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, providererrors.NewSerializationError(fmt.Sprintf("quiverai: failed to restore config: %v", err), err)
	}
	return New(cfg).ImageModel(serialized.ModelID)
}
