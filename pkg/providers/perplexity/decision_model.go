package perplexity

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// decisionProviderName is the provider ID of Perplexity decision models.
const decisionProviderName = "perplexity.decision"

// DecisionModel implements provider.DecisionModel for the Perplexity
// Decisions API (POST {baseURL}/v1/decisions). Mirrors TS
// DecisionPerplexityModel. Boolean questions are sent as Perplexity's native
// "noul" type; probabilities keep full precision (no rounding allowance).
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a Perplexity decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the decision model specification version.
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "perplexity.decision", matching the TS provider ID.
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score and boolean.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	ids := opts.QuestionIDs()
	if len(ids) == 0 || len(ids) > 128 {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "questions",
			Message: "Perplexity Decisions supports 1 to 128 questions per request.",
		}
	}

	nativeQuestions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		if id == "" {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "questions",
				Message: "Perplexity Decisions requires non-empty question names.",
			}
		}
		q := opts.Questions[id]
		wire := map[string]interface{}{}
		// Instructions are optional; a criteria-only question is sent as is.
		if q.Instructions != nil {
			wire["instructions"] = q.Instructions
		}
		switch q.Type {
		case "boolean":
			wire["type"] = "noul"
			if q.Criteria != nil {
				wire["criteria"] = q.Criteria
			}
		case "choice":
			criteria, _ := q.Criteria.(map[string]interface{})
			if len(criteria) == 0 || len(criteria) > 255 {
				return nil, &providererrors.InvalidArgumentError{
					Field:   fmt.Sprintf("questions.%s.criteria", id),
					Message: "Perplexity Choice questions support 1 to 255 options.",
				}
			}
			wire["type"] = "choice"
			if order := opts.CriteriaOrder[id]; len(order) > 0 {
				wire["criteria"] = providerutils.OrderedMap{Keys: provider.OrderedKeys(criteria, order), Vals: criteria}
			} else {
				wire["criteria"] = q.Criteria
			}
		case "score":
			criteria, _ := q.Criteria.([]interface{})
			invalid := len(criteria) == 0 || len(criteria) > 10
			for _, level := range criteria {
				if level == nil {
					invalid = true
				}
			}
			if invalid {
				return nil, &providererrors.InvalidArgumentError{
					Field:   fmt.Sprintf("questions.%s.criteria", id),
					Message: "Perplexity Score questions require 1 to 10 non-null levels.",
				}
			}
			wire["type"] = "score"
			wire["criteria"] = q.Criteria
		default:
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		nativeQuestions.Vals[id] = wire
	}

	state, err := convertToPerplexityDecisionState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	var response perplexityDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/decisions",
		Body: map[string]interface{}{
			"model":     m.modelID,
			"state":     state,
			"questions": nativeQuestions,
		},
		Headers: opts.Headers,
		// TS createJsonResponseHandler(perplexityDecisionResponseSchema).
		ValidateResponse: validatePerplexityDecisionResponse,
	}, &response)
	if err != nil {
		return nil, handlePerplexityDecisionError(err)
	}

	var rawBody interface{}
	_ = json.Unmarshal(httpResp.Body, &rawBody)

	if len(response.Answers) != len(ids) {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Perplexity Decisions must return exactly one answer for every question.")
	}
	for _, id := range ids {
		if _, ok := response.Answers[id]; !ok {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "Perplexity Decisions must return exactly one answer for every question.")
		}
	}

	answers := make(map[string]provider.DecisionAnswer, len(ids))
	confidence := map[string]float64{}
	legend := map[string]interface{}{}
	for _, id := range ids {
		var fields perplexityDecisionAnswer
		if err := json.Unmarshal(response.Answers[id], &fields); err != nil {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "Perplexity Decisions returned a malformed answer.")
		}
		switch fields.Type {
		case "noul":
			p := *fields.Noul
			answers[id] = provider.DecisionAnswer{Type: "boolean", Probability: &p}
		case "choice":
			answers[id] = provider.DecisionAnswer{Type: "choice", Choice: *fields.Choice, Probabilities: fields.Probabilities}
			if fields.Confidence != nil {
				confidence[id] = *fields.Confidence
			}
		default: // score
			s := *fields.Score
			answers[id] = provider.DecisionAnswer{Type: "score", Score: &s, Probabilities: fields.Probabilities}
			if fields.Confidence != nil {
				confidence[id] = *fields.Confidence
			}
			if fields.Legend != nil {
				legend[id] = fields.Legend
			}
		}
	}

	var usage *provider.DecisionUsage
	if response.Usage != nil {
		usage = &provider.DecisionUsage{InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens}
	}

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	if perplexityOpts, ok := opts.ProviderOptions["perplexity"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(perplexityOpts))
		for k := range perplexityOpts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.perplexity." + k})
		}
	}

	modelID := m.modelID
	if response.Model != nil { // TS `response.model ?? modelId`: an empty string is kept
		modelID = *response.Model
	}
	headers := lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers))

	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"perplexity": map[string]interface{}{"confidence": confidence, "legend": legend}},
		Response: &provider.DecisionResponseInfo{
			ID:      headers["x-request-id"],
			ModelID: modelID,
			Headers: headers,
			Body:    rawBody,
		},
	}, nil
}

// perplexityDecisionResponse is the wire shape of a successful response.
type perplexityDecisionResponse struct {
	Model   *string                    `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *struct {
		InputTokens  *int `json:"input_tokens"`
		OutputTokens *int `json:"output_tokens"`
	} `json:"usage"`
}

type perplexityDecisionAnswer struct {
	Type          string                 `json:"type"`
	Noul          *float64               `json:"noul"`
	Choice        *string                `json:"choice"`
	Score         *float64               `json:"score"`
	Probabilities map[string]float64     `json:"probabilities"`
	Confidence    *float64               `json:"confidence"`
	Legend        map[string]interface{} `json:"legend"`
}

// perplexityDecisionResponseNode is perplexityDecisionResponseSchema.
var perplexityDecisionResponseNode = func() zodlite.Node {
	probability := func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if issue := zodlite.Number(v, present, path); issue != nil {
			return issue
		}
		f := v.(float64)
		switch {
		case f < 0 || math.IsNaN(f):
			return &zodlite.Issue{Path: path, Code: "too_small", Message: "Too small: expected number to be >=0"}
		case f > 1:
			return &zodlite.Issue{Path: path, Code: "too_big", Message: "Too big: expected number to be <=1"}
		}
		return nil
	}
	nullishProbability := zodlite.Nullish(probability)
	probabilities := zodlite.RecordOf(probability)
	nullishNumber := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability)),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability),
				zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.Unknown)))),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", nullishNumber), zodlite.F("output_tokens", nullishNumber)))),
	)
}()

// validatePerplexityDecisionResponse is createJsonResponseHandler(
// perplexityDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validatePerplexityDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := json.Unmarshal(body, &value)
	if err == nil {
		if bad := perplexityDecisionResponseNode(value, true, nil); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}

// handlePerplexityDecisionError turns a failed HTTP response into a
// ProviderError (see perplexityHTTPError).
func handlePerplexityDecisionError(err error) error {
	return perplexityHTTPError(err, decisionProviderName)
}

// lowerHeaders lowercases header names, as the TS SDK's response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
