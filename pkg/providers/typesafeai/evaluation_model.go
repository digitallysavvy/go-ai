package typesafeai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EvaluationModel implements provider.EvaluationModel for TypeSafe AI's
// evaluation endpoint (`POST {baseURL}/systemone`). Mirrors TypeScript's
// EvaluationTypeSafeAiModel (packages/typesafe-ai/src/typesafe-ai-evaluation-model.ts).
type EvaluationModel struct {
	provider *Provider
	modelID  string
}

// NewEvaluationModel creates a new TypeSafe AI evaluation model.
func NewEvaluationModel(p *Provider, modelID string) *EvaluationModel {
	return &EvaluationModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the evaluation model specification version.
func (m *EvaluationModel) SpecificationVersion() string { return "v4" }

// Provider returns "typesafe.evaluation", matching TypeScript's provider ID.
func (m *EvaluationModel) Provider() string { return "typesafe.evaluation" }

// ModelID returns the model ID.
func (m *EvaluationModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports that TypeSafe AI accepts all three question
// types.
func (m *EvaluationModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoEvaluate evaluates every question against the same state via TypeSafe
// AI's /systemone endpoint. Boolean questions are sent as TypeSafe's native
// "noul" type and mapped back to "boolean" answers on the way out.
func (m *EvaluationModel) DoEvaluate(ctx context.Context, opts provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
	// TypeSafe-specific provider limits (mirrors the TS doEvaluate checks,
	// enforced before any HTTP request).
	ids := make([]string, 0, len(opts.Questions))
	for id := range opts.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		q := opts.Questions[id]
		switch q.Type {
		case "choice":
			criteria, _ := q.Criteria.(map[string]interface{})
			if len(criteria) > 255 {
				return nil, &providererrors.InvalidArgumentError{
					Field:   fmt.Sprintf("questions.%s.criteria", id),
					Message: "TypeSafe Choice questions support at most 255 options.",
				}
			}
		case "score":
			criteria, _ := q.Criteria.([]interface{})
			if len(criteria) > 10 {
				return nil, &providererrors.InvalidArgumentError{
					Field:   fmt.Sprintf("questions.%s.criteria", id),
					Message: "TypeSafe Score questions support at most 10 levels.",
				}
			}
		}
	}

	var warnings []types.Warning
	if typesafeOpts, ok := opts.ProviderOptions["typesafe"].(map[string]interface{}); ok {
		optionKeys := make([]string, 0, len(typesafeOpts))
		for k := range typesafeOpts {
			optionKeys = append(optionKeys, k)
		}
		sort.Strings(optionKeys)
		for _, k := range optionKeys {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("providerOptions.typesafe.%s", k),
			})
		}
	}

	requestQuestions := make(map[string]interface{}, len(opts.Questions))
	for id, q := range opts.Questions {
		wireType := q.Type
		if wireType == "boolean" {
			wireType = "noul"
		}
		entry := map[string]interface{}{
			"type":         wireType,
			"instructions": q.Instructions,
		}
		if q.Criteria != nil {
			entry["criteria"] = q.Criteria
		}
		requestQuestions[id] = entry
	}

	body := map[string]interface{}{
		"model":     m.modelID,
		"state":     opts.State,
		"questions": requestQuestions,
	}

	headers, err := m.provider.resolveHeaders()
	if err != nil {
		return nil, err
	}
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	var response typesafeResponseWire
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/systemone",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	answers, confidence, err := convertTypesafeAnswers(response.Answers)
	if err != nil {
		return nil, err
	}

	var usage *provider.EvaluationUsage
	if response.Usage != nil {
		usage = &provider.EvaluationUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		}
	} else {
		usage = &provider.EvaluationUsage{}
	}

	modelID := m.modelID
	if response.Model != nil && *response.Model != "" {
		modelID = *response.Model
	}

	var rawBody interface{}
	_ = json.Unmarshal(httpResp.Body, &rawBody)

	probabilityDecimals := 2
	scoreDecimals := 2

	return &provider.EvaluationResult{
		Answers: answers,
		Rounding: &provider.EvaluationRounding{
			ProbabilityDecimals: &probabilityDecimals,
			ScoreDecimals:       &scoreDecimals,
		},
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"typesafe": map[string]interface{}{"confidence": confidence}},
		Response: &provider.EvaluationResponseInfo{
			ModelID: modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawBody,
		},
	}, nil
}

// convertTypesafeAnswers maps the wire answer shapes ("choice"/"score"/"noul")
// to provider.EvaluationAnswer ("choice"/"score"/"boolean"), and collects the
// per-question confidence values reported for choice/score answers.
func convertTypesafeAnswers(raw map[string]json.RawMessage) (map[string]provider.EvaluationAnswer, map[string]float64, error) {
	answers := make(map[string]provider.EvaluationAnswer, len(raw))
	confidence := make(map[string]float64, len(raw))

	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		var fields map[string]interface{}
		if err := json.Unmarshal(raw[id], &fields); err != nil {
			return nil, nil, providererrors.NewInvalidResponseDataError(string(raw[id]), "TypeSafe AI returned a malformed evaluation answer.")
		}
		answerType, _ := fields["type"].(string)
		switch answerType {
		case "choice":
			choice, ok := fields["choice"].(string)
			probs, probsOK := toFloatMap(fields["probabilities"])
			if !ok || !probsOK {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed choice answer for %q.", id))
			}
			answers[id] = provider.EvaluationAnswer{Type: "choice", Choice: choice, Probabilities: probs}
			if c, ok := fields["confidence"].(float64); ok {
				confidence[id] = c
			}
		case "score":
			score, ok := fields["score"].(float64)
			probs, probsOK := toFloatMap(fields["probabilities"])
			if !ok || !probsOK {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed score answer for %q.", id))
			}
			answers[id] = provider.EvaluationAnswer{Type: "score", Score: &score, Probabilities: probs}
			if c, ok := fields["confidence"].(float64); ok {
				confidence[id] = c
			}
		case "noul":
			noul, ok := fields["noul"].(float64)
			if !ok {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed boolean answer for %q.", id))
			}
			answers[id] = provider.EvaluationAnswer{Type: "boolean", Probability: &noul}
		default:
			return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned an unknown answer type for %q.", id))
		}
	}

	return answers, confidence, nil
}

func toFloatMap(v interface{}) (map[string]float64, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	out := make(map[string]float64, len(m))
	for k, raw := range m {
		f, ok := raw.(float64)
		if !ok {
			return nil, false
		}
		out[k] = f
	}
	return out, true
}

// handleError converts a failed TypeSafe AI request into a
// providererrors.ProviderError, extracting the error message per TypeScript's
// priority order: message ?? (error string or error.message) ??
// (detail string or JSON.stringify(detail)) ?? error_type ?? fallback.
func (m *EvaluationModel) handleError(err error) error {
	var httpErr *internalhttp.HTTPStatusError
	if !errors.As(err, &httpErr) {
		return err
	}

	var envelope typesafeErrorEnvelope
	_ = json.Unmarshal(httpErr.Body, &envelope)

	message := envelope.Message
	if message == "" {
		if envelope.Error != nil {
			if envelope.Error.str != "" {
				message = envelope.Error.str
			} else if envelope.Error.obj != nil {
				message = envelope.Error.obj.Message
			}
		}
	}
	if message == "" && envelope.Detail != nil {
		if s, ok := envelope.Detail.(string); ok {
			message = s
		} else if b, marshalErr := json.Marshal(envelope.Detail); marshalErr == nil {
			message = string(b)
		}
	}
	if message == "" {
		message = envelope.ErrorType
	}
	if message == "" {
		message = "TypeSafe request failed"
	}

	return &providererrors.ProviderError{
		Provider:        m.Provider(),
		StatusCode:      httpErr.StatusCode,
		Message:         message,
		ResponseHeaders: providerutils.ExtractHeaders(httpErr.Headers),
		ResponseBody:    string(httpErr.Body),
	}
}

// typesafeErrorEnvelope decodes TypeSafe AI's several observed error body
// shapes: {message}, {error: string}, {error: {message}}, {detail}, {error_type}.
type typesafeErrorEnvelope struct {
	Message   string             `json:"message"`
	Detail    interface{}        `json:"detail"`
	Error     *typesafeErrorUnion `json:"error"`
	ErrorType string             `json:"error_type"`
}

// typesafeErrorUnion decodes the "error" field, which may be either a bare
// string or an object with a "message" field.
type typesafeErrorUnion struct {
	str string
	obj *struct {
		Message string `json:"message"`
	}
}

func (u *typesafeErrorUnion) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		u.str = s
		return nil
	}
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	u.obj = &obj
	return nil
}

// typesafeResponseWire is the wire shape of a successful /systemone response.
type typesafeResponseWire struct {
	Model   *string                    `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *typesafeUsageWire         `json:"usage"`
}

type typesafeUsageWire struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
}
