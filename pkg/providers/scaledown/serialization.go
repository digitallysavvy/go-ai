package scaledown

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterDecisionModelDeserializer(decisionProviderName, deserializeDecisionModel)
	provider.RegisterExtractionModelDeserializer(extractionProviderName, DeserializeExtractionModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS scaledown-decision-model.ts [WORKFLOW_SERIALIZE]: the endpoint and
// static headers travel, the fetch implementation does not. As for every Go
// provider, the API key is not serialized: the restored model reads
// SCALEDOWN_API_KEY again.
func (m *DecisionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeDecisionModel(serialized provider.SerializedModel) (provider.DecisionModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).DecisionModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS scaledown-extraction-model.ts [WORKFLOW_SERIALIZE]; like the
// decision model, the API key is not serialized.
func (m *ExtractionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

// DeserializeExtractionModel restores a model serialized by
// (*ExtractionModel).Serialize. The restored model reads SCALEDOWN_API_KEY
// again.
func DeserializeExtractionModel(serialized provider.SerializedModel) (provider.ExtractionModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).ExtractionModel(serialized.ModelID)
}
