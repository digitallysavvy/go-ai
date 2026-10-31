package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DecisionModel implements provider.DecisionModel for the AI Gateway's
// decision route (`POST {baseURL}/decision-model`). Mirrors TypeScript's
// GatewayDecisionModel.
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a new Gateway decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// DecisionModel implements provider.DecisionModelProvider for Provider,
// resolving decision models by ID through the Gateway.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	return NewDecisionModel(p, modelID), nil
}

// SpecificationVersion returns the decision model specification version.
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *DecisionModel) Provider() string { return "gateway" }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports that the Gateway decision route accepts
// all three question types; the route itself validates per-question support
// for the resolved underlying model.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoDecide decides every question against the same state through the
// Gateway.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	if gatewayOptions, ok := opts.ProviderOptions["gateway"]; ok {
		if err := validateGatewayDecisionProviderOptions(gatewayOptions); err != nil {
			return nil, err
		}
	}

	stateParts, err := encodeGatewayDecisionState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"stateParts": stateParts,
		"questions":  encodeGatewayDecisionQuestions(opts),
	}
	if opts.MaxOutputTokens != nil {
		body["maxOutputTokens"] = *opts.MaxOutputTokens
	}
	if opts.Reasoning != nil {
		body["reasoning"] = string(*opts.Reasoning)
	}
	if len(opts.ProviderOptions) > 0 {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := gatewayRequestHeaders(ctx, opts.Headers, m.getModelConfigHeaders())

	var response gatewayDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator("gateway", providerutils.SchemaGatewayDecision),
		Method:           http.MethodPost,
		Path:             "/decision-model",
		Body:             body,
		Headers:          headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(ctx, err)
	}

	modelID := m.modelID
	if response.Model != nil && *response.Model != "" {
		modelID = *response.Model
	}

	return &provider.DecisionResult{
		Answers:          response.toAnswers(),
		Rounding:         response.toRounding(),
		Usage:            response.toUsage(),
		Warnings:         warningsOrEmpty(response.Warnings),
		ProviderMetadata: copyGatewayProviderMetadata(response.ProviderMetadata),
		Response: &provider.DecisionResponseInfo{
			ModelID: modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawJSONBody(httpResp.Body),
		},
	}, nil
}

func (m *DecisionModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-decision-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

func (m *DecisionModel) handleError(ctx context.Context, err error) error {
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleErrorWithContext(ctx, err)
}

// encodeGatewayDecisionQuestions converts a question map into the
// Gateway's wire shape. Criteria is included only when non-nil: choice/score
// questions always carry it, boolean questions may omit it.
func encodeGatewayDecisionQuestions(opts provider.DecisionCallOptions) providerutils.OrderedMap {
	out := providerutils.OrderedMap{
		Keys: opts.QuestionIDs(),
		Vals: make(map[string]interface{}, len(opts.Questions)),
	}
	for id, q := range opts.Questions {
		wire := providerutils.OrderedMap{Keys: []string{"type", "instructions", "criteria"}, Vals: map[string]interface{}{"type": q.Type}}
		// Instructions are optional for criteria-only questions.
		if q.Instructions != nil {
			wire.Vals["instructions"] = q.Instructions
		}
		if q.Criteria != nil {
			wire.Vals["criteria"] = orderedCriteria(q.Criteria, opts.CriteriaOrder[id])
		}
		out.Vals[id] = wire
	}
	return out
}

// orderedCriteria keeps the TS key order of a choice criteria map when the
// caller supplied it.
func orderedCriteria(criteria interface{}, order []string) interface{} {
	if m, ok := criteria.(map[string]interface{}); ok && len(order) > 0 {
		return providerutils.OrderedMap{Keys: provider.OrderedKeys(m, order), Vals: m}
	}
	return criteria
}

type gatewayDecisionAnswerWire struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probability   *float64           `json:"probability,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

type gatewayDecisionRoundingWire struct {
	ProbabilityDecimals *int `json:"probabilityDecimals,omitempty"`
	ScoreDecimals       *int `json:"scoreDecimals,omitempty"`
}

type gatewayDecisionUsageWire struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

type gatewayDecisionResponse struct {
	Answers map[string]gatewayDecisionAnswerWire `json:"answers"`
	// Model is the model that actually produced the answers. It differs
	// from the requested model ID when a conditional decision fallback
	// (GatewayConditionalModelFallback) fired. When absent, the requested
	// model ID is used. Mirrors TS gatewayDecisionResponseSchema.model.
	Model            *string                           `json:"model,omitempty"`
	Rounding         *gatewayDecisionRoundingWire      `json:"rounding,omitempty"`
	Usage            *gatewayDecisionUsageWire         `json:"usage,omitempty"`
	Warnings         []types.Warning                   `json:"warnings,omitempty"`
	ProviderMetadata map[string]map[string]interface{} `json:"providerMetadata,omitempty"`
}

func (r gatewayDecisionResponse) toAnswers() map[string]provider.DecisionAnswer {
	out := make(map[string]provider.DecisionAnswer, len(r.Answers))
	for id, a := range r.Answers {
		out[id] = provider.DecisionAnswer{
			Type:          a.Type,
			Choice:        a.Choice,
			Score:         a.Score,
			Probability:   a.Probability,
			Probabilities: a.Probabilities,
		}
	}
	return out
}

func (r gatewayDecisionResponse) toRounding() *provider.DecisionRounding {
	if r.Rounding == nil {
		return nil
	}
	return &provider.DecisionRounding{
		ProbabilityDecimals: r.Rounding.ProbabilityDecimals,
		ScoreDecimals:       r.Rounding.ScoreDecimals,
	}
}

func (r gatewayDecisionResponse) toUsage() *provider.DecisionUsage {
	if r.Usage == nil {
		return nil
	}
	return &provider.DecisionUsage{
		InputTokens:  r.Usage.InputTokens,
		OutputTokens: r.Usage.OutputTokens,
	}
}

// EvaluationModel is the deprecated form of DecisionModel.
//
// Deprecated: Use DecisionModel instead.
func (p *Provider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	return NewDecisionModel(p, modelID), nil
}

// DoEvaluate is the deprecated form of DoDecide.
//
// Deprecated: Use DoDecide instead.
func (m *DecisionModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return m.DoDecide(ctx, opts)
}

// encodeGatewayDecisionState converts ordered state parts to the Gateway's
// stateParts wire shape. File bytes are sent as base64 in the tagged data
// shape; URL, reference, and text file data are rejected before any request.
// Mirrors TS toGatewayDecisionState.
func encodeGatewayDecisionState(parts []provider.DecisionStatePart) ([]interface{}, error) {
	out := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			out = append(out, map[string]interface{}{"type": "text", "text": part.Text})
		case "json":
			out = append(out, map[string]interface{}{"type": "json", "value": part.Value})
		case "file":
			if part.File == nil {
				return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Gateway decision file input"}
			}
			kind := string(part.File.FileData.Type)
			if kind == "" && part.File.URL != "" {
				kind = "url"
			}
			if kind != "" && kind != string(types.FileDataTypeData) {
				return nil, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("Gateway decision file input: %s data", kind)}
			}
			mediaType, data, err := part.InlineData()
			if err != nil {
				return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Gateway decision file input"}
			}
			wire := map[string]interface{}{
				"type":      "file",
				"mediaType": mediaType,
				"data":      map[string]interface{}{"type": "data", "data": base64.StdEncoding.EncodeToString(data)},
			}
			if part.File.Filename != "" {
				wire["filename"] = part.File.Filename
			}
			if len(part.File.ProviderOptions) > 0 {
				wire["providerOptions"] = part.File.ProviderOptions
			}
			out = append(out, wire)
		default:
			return nil, fmt.Errorf("unknown decision state part type %q", part.Type)
		}
	}
	return out, nil
}
