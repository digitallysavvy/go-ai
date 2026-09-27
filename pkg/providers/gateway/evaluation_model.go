package gateway

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EvaluationModel implements provider.EvaluationModel for the AI Gateway's
// evaluation route (`POST {baseURL}/evaluation-model`). Mirrors TypeScript's
// GatewayEvaluationModel.
type EvaluationModel struct {
	provider *Provider
	modelID  string
}

// NewEvaluationModel creates a new Gateway evaluation model.
func NewEvaluationModel(p *Provider, modelID string) *EvaluationModel {
	return &EvaluationModel{provider: p, modelID: modelID}
}

// EvaluationModel implements provider.EvaluationModelProvider for Provider,
// resolving evaluation models by ID through the Gateway.
func (p *Provider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	return NewEvaluationModel(p, modelID), nil
}

// SpecificationVersion returns the evaluation model specification version.
func (m *EvaluationModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *EvaluationModel) Provider() string { return "gateway" }

// ModelID returns the model ID.
func (m *EvaluationModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports that the Gateway evaluation route accepts
// all three question types; the route itself validates per-question support
// for the resolved underlying model.
func (m *EvaluationModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoEvaluate evaluates every question against the same state through the
// Gateway.
func (m *EvaluationModel) DoEvaluate(ctx context.Context, opts provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
	body := map[string]interface{}{
		"state":     opts.State,
		"questions": encodeGatewayEvaluationQuestions(opts.Questions),
	}
	if len(opts.ProviderOptions) > 0 {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := m.getModelConfigHeaders()
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	for k, v := range opts.Headers {
		headers[k] = v
	}

	var response gatewayEvaluationResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/evaluation-model",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(ctx, err)
	}

	return &provider.EvaluationResult{
		Answers:          response.toAnswers(),
		Rounding:         response.toRounding(),
		Usage:            response.toUsage(),
		Warnings:         warningsOrEmpty(response.Warnings),
		ProviderMetadata: copyGatewayProviderMetadata(response.ProviderMetadata),
		Response: &provider.EvaluationResponseInfo{
			ModelID: m.modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawJSONBody(httpResp.Body),
		},
	}, nil
}

func (m *EvaluationModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-evaluation-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

func (m *EvaluationModel) handleError(ctx context.Context, err error) error {
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleErrorWithContext(ctx, err)
}

// encodeGatewayEvaluationQuestions converts a question map into the
// Gateway's wire shape. Criteria is included only when non-nil: choice/score
// questions always carry it, boolean questions may omit it.
func encodeGatewayEvaluationQuestions(questions map[string]provider.EvaluationQuestion) map[string]interface{} {
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

type gatewayEvaluationAnswerWire struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probability   *float64           `json:"probability,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

type gatewayEvaluationRoundingWire struct {
	ProbabilityDecimals *int `json:"probabilityDecimals,omitempty"`
	ScoreDecimals       *int `json:"scoreDecimals,omitempty"`
}

type gatewayEvaluationUsageWire struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

type gatewayEvaluationResponse struct {
	Answers          map[string]gatewayEvaluationAnswerWire `json:"answers"`
	Rounding         *gatewayEvaluationRoundingWire         `json:"rounding,omitempty"`
	Usage            *gatewayEvaluationUsageWire            `json:"usage,omitempty"`
	Warnings         []types.Warning                        `json:"warnings,omitempty"`
	ProviderMetadata map[string]map[string]interface{}      `json:"providerMetadata,omitempty"`
}

func (r gatewayEvaluationResponse) toAnswers() map[string]provider.EvaluationAnswer {
	out := make(map[string]provider.EvaluationAnswer, len(r.Answers))
	for id, a := range r.Answers {
		out[id] = provider.EvaluationAnswer{
			Type:          a.Type,
			Choice:        a.Choice,
			Score:         a.Score,
			Probability:   a.Probability,
			Probabilities: a.Probabilities,
		}
	}
	return out
}

func (r gatewayEvaluationResponse) toRounding() *provider.EvaluationRounding {
	if r.Rounding == nil {
		return nil
	}
	return &provider.EvaluationRounding{
		ProbabilityDecimals: r.Rounding.ProbabilityDecimals,
		ScoreDecimals:       r.Rounding.ScoreDecimals,
	}
}

func (r gatewayEvaluationResponse) toUsage() *provider.EvaluationUsage {
	if r.Usage == nil {
		return nil
	}
	return &provider.EvaluationUsage{
		InputTokens:  r.Usage.InputTokens,
		OutputTokens: r.Usage.OutputTokens,
	}
}
