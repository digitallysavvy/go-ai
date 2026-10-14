package gateway

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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

	body := map[string]interface{}{
		"state":     opts.State,
		"questions": encodeGatewayDecisionQuestions(opts.Questions),
	}
	if len(opts.ProviderOptions) > 0 {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := m.getModelConfigHeaders()
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	for k, v := range opts.Headers {
		headers[k] = v
	}

	var response gatewayDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/decision-model",
		Body:    body,
		Headers: headers,
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
func encodeGatewayDecisionQuestions(questions map[string]provider.DecisionQuestion) map[string]interface{} {
	out := make(map[string]interface{}, len(questions))
	for id, q := range questions {
		wire := map[string]interface{}{
			"type":         q.Type,
			"instructions": q.Instructions,
		}
		if q.Criteria != nil {
			wire["criteria"] = q.Criteria
		}
		out[id] = wire
	}
	return out
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
