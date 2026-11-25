package providerutils

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// SerializeStrict implements provider.SerializableModelStrict. Mirrors TS
// ExtractionLanguageModel [WORKFLOW_SERIALIZE] (provider-utils
// extraction-language-model.ts): the wrapped language model, the provider
// ID and maxFieldsPerCall travel. The wrapped model is serialized through
// its own provider's Serialize, so it fails when that model is not
// serializable. An unlimited maxFieldsPerCall (TS Infinity) is left out.
func (e *ExtractionLanguageModel) SerializeStrict() (provider.SerializedModel, error) {
	inner, err := provider.SerializeModel(e.model)
	if err != nil {
		return provider.SerializedModel{}, err
	}
	config := map[string]interface{}{
		"model": map[string]interface{}{
			"provider": inner.Provider,
			"modelId":  inner.ModelID,
			"config":   inner.Config,
		},
	}
	if e.maxFieldsPerCall > 0 {
		config["maxFieldsPerCall"] = e.maxFieldsPerCall
	}
	return provider.SerializedModel{Provider: e.providerName, ModelID: e.model.ModelID(), Config: config}, nil
}

// extractionLanguageModelConfig is the serialized config: caller-persisted
// workflow state, not a provider response.
type extractionLanguageModelConfig struct {
	Model            *provider.SerializedModel `json:"model"`
	MaxFieldsPerCall *int                      `json:"maxFieldsPerCall"`
}

// DeserializeExtractionLanguageModel restores a model serialized by
// (*ExtractionLanguageModel).SerializeStrict. Mirrors TS
// ExtractionLanguageModel [WORKFLOW_DESERIALIZE]. The wrapped language
// model is restored with provider.DeserializeModel, so its provider package
// must be imported. Provider packages register this function for their
// "<name>.extraction" key.
func DeserializeExtractionLanguageModel(serialized provider.SerializedModel) (provider.ExtractionModel, error) {
	var cfg extractionLanguageModelConfig
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, providererrors.NewSerializationError("providerutils: invalid extraction model config", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, providererrors.NewSerializationError("providerutils: invalid extraction model config", err)
	}
	if cfg.Model == nil || cfg.Model.Provider == "" {
		return nil, providererrors.NewSerializationError(
			fmt.Sprintf("providerutils: serialized extraction model %q has no wrapped language model", serialized.Provider), nil)
	}
	inner, err := provider.DeserializeModel(*cfg.Model)
	if err != nil {
		return nil, err
	}
	return NewExtractionLanguageModel(ExtractionLanguageModelOptions{
		Model:            inner,
		Provider:         serialized.Provider,
		MaxFieldsPerCall: cfg.MaxFieldsPerCall,
	})
}
