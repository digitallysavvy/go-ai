package tencent

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func init() {
	provider.RegisterModelDeserializer("tencent.chat", deserializeModel)
}

// deserializeModel restores a serialized TokenHub chat model (TS
// TencentChatLanguageModel [WORKFLOW_DESERIALIZE]). The chat model is an
// *openai.LanguageModel whose Serialize tags it "tencent.chat"; the
// TokenHub endpoint is rebuilt from the serialized base URL, and the key is
// loaded again (Config.APIKey or TENCENT_TOKENHUB_API_KEY).
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
