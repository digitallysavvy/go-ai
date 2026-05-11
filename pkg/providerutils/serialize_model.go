package providerutils

import "github.com/digitallysavvy/go-ai/pkg/provider"

// SerializeModel extracts a JSON-safe model configuration for workflow
// reconstruction.
func SerializeModel(model provider.LanguageModel) (map[string]interface{}, error) {
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"provider": serialized.Provider,
		"modelId":  serialized.ModelID,
		"config":   serialized.Config,
	}, nil
}

// DeserializeModel reconstructs a language model from a provider ID, model ID,
// and serialized config.
func DeserializeModel(providerID, modelID string, config map[string]interface{}) (provider.LanguageModel, error) {
	return provider.DeserializeModel(provider.SerializedModel{
		Provider: providerID,
		ModelID:  modelID,
		Config:   config,
	})
}
