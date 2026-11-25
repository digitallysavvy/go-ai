package quiverai

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModel implements the Arrow 2 / Arrow 2 Telos QuiverAI language
// models. It applies QuiverAI's request policy (reasoning effort/summary
// validation, opaque reasoning replay) while reusing the Open Responses
// transport, mirroring the TS SDK's QuiverAILanguageModel
// (quiverai-language-model.ts).
type LanguageModel struct {
	inner   provider.LanguageModel
	modelID string
	config  Config
}

// NewLanguageModel wraps an Open Responses language model with QuiverAI's
// request policy.
func NewLanguageModel(inner provider.LanguageModel, modelID string, config Config) *LanguageModel {
	return &LanguageModel{inner: inner, modelID: modelID, config: config}
}

func (m *LanguageModel) SpecificationVersion() string   { return m.inner.SpecificationVersion() }
func (m *LanguageModel) Provider() string               { return m.inner.Provider() }
func (m *LanguageModel) ModelID() string                { return m.modelID }
func (m *LanguageModel) SupportsTools() bool            { return m.inner.SupportsTools() }
func (m *LanguageModel) SupportsStructuredOutput() bool { return m.inner.SupportsStructuredOutput() }
func (m *LanguageModel) SupportsImageInput() bool       { return m.inner.SupportsImageInput() }

// SupportedURLs forwards the underlying Open Responses model's supported URL
// patterns, when it exposes them.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	type supportedURLser interface{ SupportedURLs() map[string][]string }
	if s, ok := m.inner.(supportedURLser); ok {
		return s.SupportedURLs()
	}
	return nil
}

func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	prepared, warnings, err := prepareQuiverAICall(opts)
	if err != nil {
		return nil, err
	}
	result, err := m.inner.DoGenerate(ctx, prepared)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(append([]types.Warning{}, warnings...), result.Warnings...)
	return result, nil
}

func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	prepared, warnings, err := prepareQuiverAICall(opts)
	if err != nil {
		return nil, err
	}
	stream, err := m.inner.DoStream(ctx, prepared)
	if err != nil {
		return nil, err
	}
	if len(warnings) == 0 {
		return stream, nil
	}
	return &quiverAIStream{inner: stream, warnings: warnings}, nil
}

// SerializeStrict implements provider.SerializableModelStrict for workflow
// boundaries. QuiverAI's HTTP error handlers (FailedResponseHandler /
// GetResponseErrorMetadata) are Go functions and cannot cross a workflow
// boundary; deserializeQuiverAIModel restores them fresh, mirroring the TS
// SDK's `static [WORKFLOW_DESERIALIZE]` comment ("Restore QuiverAI's
// handlers, which cannot cross workflow boundaries"). Like the shared Open
// Responses foundation this model is built on, APIKey is intentionally
// excluded from the serialized config (provider.SerializableConfig treats it
// as a sensitive field) -- a workflow step that resumes a serialized
// QuiverAI model must supply QUIVERAI_API_KEY (or another credential source)
// in its own environment, it is never carried across the boundary as plain
// JSON.
func (m *LanguageModel) SerializeStrict() (provider.SerializedModel, error) {
	if strict, ok := m.inner.(provider.SerializableModelStrict); ok {
		if _, err := strict.SerializeStrict(); err != nil {
			return provider.SerializedModel{}, err
		}
	}
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.modelID,
		Config:   provider.SerializableConfig(m.config),
	}, nil
}

func init() {
	provider.RegisterModelDeserializer("quiverai.responses", deserializeQuiverAIModel)
}

func deserializeQuiverAIModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, providererrors.NewSerializationError(fmt.Sprintf("quiverai: failed to restore config: %v", err), err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, providererrors.NewSerializationError(fmt.Sprintf("quiverai: failed to restore config: %v", err), err)
	}
	p := New(cfg)
	return p.LanguageModel(serialized.ModelID)
}

// quiverAIReasoningEffortMap is the TS prepareQuiverAICall effortMap: the
// levels QuiverAI accepts.
var quiverAIReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningLow:    "low",
	types.ReasoningMedium: "medium",
	types.ReasoningHigh:   "high",
	types.ReasoningXHigh:  "xhigh",
}

// prepareQuiverAICall applies QuiverAI's request policy to a call's options,
// mirroring TS's prepareQuiverAICall: validates providerOptions.quiverai
// reasoning fields, maps the top-level reasoning to a supported level when
// providerOptions.quiverai.reasoningEffort is unset, and strips
// private/encrypted reasoning text from replayed assistant history down to
// its opaque item id + safe summary.
func prepareQuiverAICall(opts *provider.GenerateOptions) (*provider.GenerateOptions, []types.Warning, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}

	// Go-typed literals (the exported options struct, map[string]string)
	// read like their JSON form (OVC-01).
	raw, _ := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["quiverai"].(map[string]interface{})
	var reasoningEffort, reasoningSummary string
	if raw != nil {
		if v, ok := raw["reasoningEffort"]; ok && v != nil {
			s, ok := v.(string)
			if !ok || !quiverLanguageReasoningEfforts[s] {
				return nil, nil, providererrors.NewArgumentError("providerOptions", "Unsupported reasoning effort: "+quiverTemplateString(v), nil)
			}
			reasoningEffort = s
		}
		if v, ok := raw["reasoningSummary"]; ok && v != nil {
			s, ok := v.(string)
			if !ok || s != "auto" {
				return nil, nil, providererrors.NewArgumentError("providerOptions", "Unsupported reasoning summary: "+quiverTemplateString(v), nil)
			}
			reasoningSummary = s
		}
	}
	_ = reasoningSummary

	var warnings []types.Warning
	prepared := *opts
	if reasoningEffort == "" && providerutils.IsCustomReasoning(opts.Reasoning) {
		// TS 61a315e333: round the top-level reasoning to a level QuiverAI
		// supports ("none" and "minimal" to "low", "max" to "xhigh").
		prepared.Reasoning = nil
		if effort, ok := providerutils.MapReasoningToProviderEffort(opts.Reasoning, quiverAIReasoningEffortMap, nil, &warnings); ok {
			level := types.ReasoningLevel(effort)
			prepared.Reasoning = &level
		}
	}
	prepared.Prompt.Messages = quiverAIStripReasoningText(opts.Prompt.Messages)

	return &prepared, warnings, nil
}

// quiverAIStripReasoningText replays assistant reasoning content down to its
// opaque item id and safe summary, dropping private reasoning text and
// encrypted content before it reaches the Responses transport, mirroring
// TS's prepareQuiverAICall message mapping. Non-assistant messages and
// non-reasoning content parts pass through unchanged.
func quiverAIStripReasoningText(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	for i, msg := range messages {
		if msg.Role != types.RoleAssistant {
			out[i] = msg
			continue
		}
		changed := false
		newContent := make([]types.ContentPart, len(msg.Content))
		for j, part := range msg.Content {
			rc, ok := part.(types.ReasoningContent)
			if !ok {
				newContent[j] = part
				continue
			}
			changed = true
			newOptions := map[string]interface{}{}
			for k, v := range rc.ProviderOptions {
				newOptions[k] = v
			}
			// TS {itemId: metadata?.itemId, reasoningSummary:
			// metadata?.reasoningSummary}: each value as given (undefined
			// when absent), so the Open Responses converter sends an item id
			// only for a string itemId.
			replay := map[string]interface{}{}
			metadata := quiverAIReasoningMetadata(rc)
			for _, key := range []string{"itemId", "reasoningSummary"} {
				if v, ok := metadata[key]; ok && v != nil {
					replay[key] = v
				}
			}
			newOptions["quiverai"] = replay
			rc.Text = ""
			rc.EncryptedContent = ""
			rc.ProviderOptions = newOptions
			newContent[j] = rc
		}
		if !changed {
			out[i] = msg
			continue
		}
		newMsg := msg
		newMsg.Content = newContent
		out[i] = newMsg
	}
	return out
}

// quiverAIReasoningMetadata reads itemId/reasoningSummary from a reasoning
// part's providerOptions.quiverai, falling back to providerMetadata.quiverai
// when providerOptions carries no quiverai key at all -- mirrors TS's
// `part.providerOptions?.quiverai ?? part.providerMetadata?.quiverai`
// (checked as a whole, not per-field).
func quiverAIReasoningMetadata(rc types.ReasoningContent) map[string]interface{} {
	// Typed part options read like their JSON form (OVC-01); TS `??`
	// falls back only for a missing or null quiverai key.
	if value, present := providerutils.NormalizeOptionsJSON(rc.ProviderOptions)["quiverai"]; present && value != nil {
		metadata, _ := value.(map[string]interface{})
		return metadata
	}
	return decodeQuiverAIProviderMetadata(rc.ProviderMetadata)
}

// decodeQuiverAIProviderMetadata decodes the "quiverai" key out of a
// content part's raw providerMetadata JSON, when present.
func decodeQuiverAIProviderMetadata(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	quiverRaw, ok := values["quiverai"]
	if !ok {
		return nil
	}
	var metadata map[string]interface{}
	// TS reads the in-memory prompt object (no JSON parse), so a __proto__
	// key is an ordinary property.
	if err := jsonexact.UnmarshalPlain(quiverRaw, &metadata); err != nil {
		return nil
	}
	return metadata
}

// quiverTemplateString is the JS template literal `${value}` of a provider
// option: JS String() of a JSON-decoded value (an array joins its elements,
// an object is "[object Object]"), with Go integer and float32 values as
// numbers.
func quiverTemplateString(v interface{}) string {
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float32:
		return providerutils.JSNumberString(float64(x))
	}
	return providerutils.JSString(v)
}
