package azure

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// azureSpeechDispatchModel resolves the API to use per request
// (providerOptions.azure.api, falling back to the model ID), mirroring TS's
// AzureSpeechModel wrapper class in azure-openai-provider.ts.
// providerOptions also reach this model via Gateway.
type azureSpeechDispatchModel struct {
	modelID string
	config  Config
	openai  provider.SpeechModel
	speech  *AzureSpeechSpeechModel
}

func newAzureSpeechDispatchModel(modelID string, config Config, openai provider.SpeechModel, speech *AzureSpeechSpeechModel) *azureSpeechDispatchModel {
	return &azureSpeechDispatchModel{modelID: modelID, config: config, openai: openai, speech: speech}
}

// Serialize implements provider.SerializableModel for workflow boundaries,
// mirroring TS AzureSpeechModel's WORKFLOW_SERIALIZE/DESERIALIZE.
func (m *azureSpeechDispatchModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.modelID, Config: provider.SerializableConfig(m.config)}
}

func (m *azureSpeechDispatchModel) SpecificationVersion() string { return "v4" }
func (m *azureSpeechDispatchModel) Provider() string             { return "azure.speech" }
func (m *azureSpeechDispatchModel) ModelID() string              { return m.modelID }

// DoGenerate dispatches to the OpenAI or Azure Speech backend, mirroring TS
// AzureSpeechModel#doGenerate.
func (m *azureSpeechDispatchModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	resolved, err := parseAzureSpeechProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	api := resolved.API
	if api == "" {
		if _, ok := getMAIVoiceModel(m.modelID); ok {
			api = AzureTranscriptionAPISpeech
		} else {
			api = AzureTranscriptionAPIOpenAI
		}
	}

	if api == AzureTranscriptionAPISpeech {
		return m.speech.DoGenerate(ctx, opts, resolved.AzureSpeechModelSpeechOptions)
	}

	result, err := m.openai.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, azureSpeechOnlyWarnings(resolved.AzureSpeechModelSpeechOptions)...)
	return result, nil
}

var _ provider.SpeechModel = (*azureSpeechDispatchModel)(nil)
