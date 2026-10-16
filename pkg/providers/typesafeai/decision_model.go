package typesafeai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DecisionModel implements provider.DecisionModel for TypeSafe AI's
// decision endpoint (`POST {baseURL}/systemone`). Mirrors TypeScript's
// DecisionTypeSafeAiModel (packages/typesafe-ai/src/typesafe-ai-decision-model.ts).
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a new TypeSafe AI decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the decision model specification version.
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "typesafe.decision", matching TypeScript's provider ID.
func (m *DecisionModel) Provider() string { return "typesafe.decision" }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports that TypeSafe AI accepts all three question
// types.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoDecide decides every question against the same state via TypeSafe
// AI's /systemone endpoint. Boolean questions are sent as TypeSafe's native
// "noul" type and mapped back to "boolean" answers on the way out.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// TypeSafe-specific provider limits (mirrors the TS doDecide checks,
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

	requestState, err := typeSafeRequestState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"model":     m.modelID,
		"state":     requestState,
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
		// TS createJsonResponseHandler(typesafeDecisionResponseSchema).
		ValidateResponse: validateDecisionResponse,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	answers, confidence, err := convertTypesafeAnswers(response.Answers)
	if err != nil {
		return nil, err
	}

	var usage *provider.DecisionUsage
	if response.Usage != nil {
		usage = &provider.DecisionUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		}
	} else {
		usage = &provider.DecisionUsage{}
	}

	modelID := m.modelID
	if response.Model != nil && *response.Model != "" {
		modelID = *response.Model
	}

	var rawBody interface{}
	_ = json.Unmarshal(httpResp.Body, &rawBody)

	probabilityDecimals := 2
	scoreDecimals := 2

	return &provider.DecisionResult{
		Answers: answers,
		Rounding: &provider.DecisionRounding{
			ProbabilityDecimals: &probabilityDecimals,
			ScoreDecimals:       &scoreDecimals,
		},
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"typesafe": map[string]interface{}{"confidence": confidence}},
		Response: &provider.DecisionResponseInfo{
			ModelID: modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawBody,
		},
	}, nil
}

// convertTypesafeAnswers maps the wire answer shapes ("choice"/"score"/"noul")
// to provider.DecisionAnswer ("choice"/"score"/"boolean"), and collects the
// per-question confidence values reported for choice/score answers.
func convertTypesafeAnswers(raw map[string]json.RawMessage) (map[string]provider.DecisionAnswer, map[string]float64, error) {
	answers := make(map[string]provider.DecisionAnswer, len(raw))
	confidence := make(map[string]float64, len(raw))

	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		var fields map[string]interface{}
		if err := json.Unmarshal(raw[id], &fields); err != nil {
			return nil, nil, providererrors.NewInvalidResponseDataError(string(raw[id]), "TypeSafe AI returned a malformed decision answer.")
		}
		answerType, _ := fields["type"].(string)
		switch answerType {
		case "choice":
			choice, ok := fields["choice"].(string)
			probs, probsOK := toFloatMap(fields["probabilities"])
			if !ok || !probsOK {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed choice answer for %q.", id))
			}
			answers[id] = provider.DecisionAnswer{Type: "choice", Choice: choice, Probabilities: probs}
			if c, ok := fields["confidence"].(float64); ok {
				confidence[id] = c
			}
		case "score":
			score, ok := fields["score"].(float64)
			probs, probsOK := toFloatMap(fields["probabilities"])
			if !ok || !probsOK {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed score answer for %q.", id))
			}
			answers[id] = provider.DecisionAnswer{Type: "score", Score: &score, Probabilities: probs}
			if c, ok := fields["confidence"].(float64); ok {
				confidence[id] = c
			}
		case "noul":
			noul, ok := fields["noul"].(float64)
			if !ok {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed boolean answer for %q.", id))
			}
			answers[id] = provider.DecisionAnswer{Type: "boolean", Probability: &noul}
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
func (m *DecisionModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var httpErr *internalhttp.HTTPStatusError
	if !errors.As(err, &httpErr) {
		return err // includes the schema failure (Invalid JSON response)
	}

	message := typesafeErrorMessage(httpErr.StatusCode, httpErr.Body)

	return &providererrors.ProviderError{
		Provider:        m.Provider(),
		StatusCode:      httpErr.StatusCode,
		Message:         message,
		ResponseHeaders: providerutils.ExtractHeaders(httpErr.Headers),
		ResponseBody:    string(httpErr.Body),
	}
}

// typesafeErrorMessage mirrors TS typesafeFailedResponseHandler. A body that
// is not an object matching the error schema (empty, not JSON, a wrongly
// typed field) gives the HTTP status text. Otherwise the message is, with JS
// `??` semantics (only null/absent falls through): message, then error (a
// string, or error.message), then detail (a string, else JSON.stringify of
// it, which is the text "null" for an explicit null), then error_type, then
// "TypeSafe request failed".
func typesafeErrorMessage(statusCode int, body []byte) string {
	statusText := http.StatusText(statusCode)
	var fields map[string]json.RawMessage
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(body, &fields) != nil {
		return statusText
	}
	// A map keeps an explicit null distinguishable from an absent key, which
	// a RawMessage struct field does not.
	raw := struct{ Message, Detail, Error, ErrorType json.RawMessage }{
		Message: fields["message"], Detail: fields["detail"], Error: fields["error"], ErrorType: fields["error_type"],
	}
	isNull := func(r json.RawMessage) bool { return len(r) == 0 || string(bytes.TrimSpace(r)) == "null" }
	// nullableString reports (value, present, schemaOK) for a string|null field.
	nullableString := func(r json.RawMessage) (string, bool, bool) {
		if isNull(r) {
			return "", false, true
		}
		var s string
		if json.Unmarshal(r, &s) != nil {
			return "", false, false
		}
		return s, true, true
	}

	message, haveMessage, ok := nullableString(raw.Message)
	if !ok {
		return statusText
	}
	errorType, haveErrorType, ok := nullableString(raw.ErrorType)
	if !ok {
		return statusText
	}
	var errorText string
	haveError := false
	if !isNull(raw.Error) {
		var s string
		if json.Unmarshal(raw.Error, &s) == nil {
			errorText, haveError = s, true
		} else {
			var obj struct {
				Message json.RawMessage `json:"message"`
			}
			trimmed := bytes.TrimSpace(raw.Error)
			if trimmed[0] != '{' || json.Unmarshal(raw.Error, &obj) != nil {
				return statusText
			}
			var present bool
			errorText, present, ok = nullableString(obj.Message)
			if !ok {
				return statusText
			}
			haveError = present
		}
	}

	switch {
	case haveMessage:
		return message
	case haveError:
		return errorText
	}
	// `detail` is z.unknown().nullish(): absent skips this step, anything
	// else (including an explicit null) is stringified.
	if _, present := fields["detail"]; present {
		if isNull(raw.Detail) {
			return "null"
		}
		var s string
		if json.Unmarshal(raw.Detail, &s) == nil {
			return s
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw.Detail) == nil {
			return compact.String()
		}
	}
	if haveErrorType {
		return errorType
	}
	return "TypeSafe request failed"
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

// DoEvaluate is the deprecated form of DoDecide.
//
// Deprecated: Use DoDecide instead.
func (m *DecisionModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return m.DoDecide(ctx, opts)
}

// typeSafeRequestState maps ordered state parts to TypeSafe's native state: a
// single json part is sent as native JSON; otherwise text and json parts are
// joined with newlines. File parts are rejected because the endpoint is
// text-only. Mirrors TS DecisionTypeSafeAiModel's requestState.
func typeSafeRequestState(parts []provider.DecisionStatePart) (interface{}, error) {
	if len(parts) == 1 && parts[0].Type == "json" {
		return parts[0].Value, nil
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "file":
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "TypeSafe AI decision file input"}
		case "text":
			texts = append(texts, part.Text)
		default:
			encoded, err := json.Marshal(part.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to encode decision state: %w", err)
			}
			texts = append(texts, string(encoded))
		}
	}
	return strings.Join(texts, "\n"), nil
}
