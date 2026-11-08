package azure

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// azureTranscriptionDispatchModel resolves the API to use per request
// (providerOptions.azure.api, falling back to the model ID), mirroring TS's
// AzureTranscriptionModel wrapper class in azure-openai-provider.ts.
// providerOptions also reach this model via Gateway.
type azureTranscriptionDispatchModel struct {
	modelID string
	config  Config
	openai  provider.TranscriptionModel
	speech  *AzureSpeechTranscriptionModel
	mai     *AzureMaiTranscriptionModel
}

func newAzureTranscriptionDispatchModel(modelID string, config Config, openai provider.TranscriptionModel, speech *AzureSpeechTranscriptionModel, mai *AzureMaiTranscriptionModel) *azureTranscriptionDispatchModel {
	return &azureTranscriptionDispatchModel{modelID: modelID, config: config, openai: openai, speech: speech, mai: mai}
}

// Serialize implements provider.SerializableModel for workflow boundaries,
// mirroring TS AzureTranscriptionModel's WORKFLOW_SERIALIZE/DESERIALIZE
// (serializes {modelId, config}; deserializing recreates the full dispatcher
// via createAzure(config).transcription(modelId)).
func (m *azureTranscriptionDispatchModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.modelID, Config: provider.SerializableConfig(m.config)}
}

func (m *azureTranscriptionDispatchModel) SpecificationVersion() string { return "v4" }
func (m *azureTranscriptionDispatchModel) Provider() string             { return "azure.transcription" }
func (m *azureTranscriptionDispatchModel) ModelID() string              { return m.modelID }

func (m *azureTranscriptionDispatchModel) resolvedAPI(providerOptions map[string]interface{}) (AzureTranscriptionModelOptions, error) {
	opts, err := parseAzureTranscriptionProviderOptions(providerOptions)
	if err != nil {
		return opts, err
	}
	if opts.API == "" {
		switch {
		case isMAITranscribeStreaming(m.modelID):
			opts.API = AzureTranscriptionAPIMai
		default:
			if _, ok := getMAITranscribeModel(m.modelID); ok {
				opts.API = AzureTranscriptionAPISpeech
			} else {
				opts.API = AzureTranscriptionAPIOpenAI
			}
		}
	}
	return opts, nil
}

// DoTranscribe dispatches to the OpenAI, Azure Speech, or MAI backend,
// mirroring TS AzureTranscriptionModel#doGenerate.
func (m *azureTranscriptionDispatchModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	resolved, err := m.resolvedAPI(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	switch resolved.API {
	case AzureTranscriptionAPIMai:
		return m.mai.DoGenerate(ctx, opts)

	case AzureTranscriptionAPISpeech:
		result, err := m.speech.DoGenerate(ctx, opts, resolved.AzureSpeechTranscriptionOptions)
		if err != nil {
			return nil, err
		}
		result.Warnings = append(result.Warnings, azureTranscriptionMaiOnlyWarnings(resolved.Language)...)
		result.Warnings = append(result.Warnings, azureOpenAITranscriptionOnlyWarnings(resolved.openaiOptions)...)
		return result, nil

	default:
		// TS 6400cf8b88: OpenAI options under azure (language included)
		// reach the OpenAI model, azure winning; no MAI language warning.
		openaiOpts := *opts
		openaiOpts.ProviderOptions = withAzureOptionsForOpenAI(opts.ProviderOptions, resolved.openaiOptions)
		result, err := m.openai.DoTranscribe(ctx, &openaiOpts)
		if err != nil {
			return nil, err
		}
		result.Warnings = append(result.Warnings, azureTranscriptionSpeechOnlyWarnings(resolved.AzureSpeechTranscriptionOptions)...)
		return result, nil
	}
}

// DoStream dispatches streaming transcription to the OpenAI or MAI backend,
// mirroring TS AzureTranscriptionModel#doStream. The Azure Speech API has no
// streaming endpoint.
func (m *azureTranscriptionDispatchModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	resolved, err := m.resolvedAPI(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	switch resolved.API {
	case AzureTranscriptionAPISpeech:
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming transcription with the Azure Speech API",
		}

	case AzureTranscriptionAPIMai:
		warnings := azureTranscriptionSpeechOnlyWarnings(resolved.AzureSpeechTranscriptionOptions)
		warnings = append(warnings, azureOpenAITranscriptionOnlyWarnings(resolved.openaiOptions)...)
		return m.mai.DoStream(ctx, opts, resolved, warnings)

	default:
		streamer, ok := m.openai.(provider.TranscriptionStreamer)
		if !ok {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "streaming transcription with " + m.modelID,
			}
		}
		openaiOpts := *opts
		openaiOpts.ProviderOptions = withAzureOptionsForOpenAI(opts.ProviderOptions, resolved.openaiOptions)
		return streamer.DoStream(ctx, &openaiOpts)
	}
}

var (
	_ provider.TranscriptionModel    = (*azureTranscriptionDispatchModel)(nil)
	_ provider.TranscriptionStreamer = (*azureTranscriptionDispatchModel)(nil)
)
