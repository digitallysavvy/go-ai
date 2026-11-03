package parallel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// decisionProviderName is the provider ID of Parallel decision models.
const decisionProviderName = "parallel.decision"

// DecisionModelOptions are the Parallel decision provider options
// (providerOptions["parallel"]). Mirrors TS ParallelDecisionModelOptions.
type DecisionModelOptions struct {
	// IncludeExcerpts includes evidence excerpts in citations. Defaults to
	// true on the server.
	IncludeExcerpts *bool `json:"includeExcerpts,omitempty"`
}

// DecisionModel implements provider.DecisionModel for the Parallel
// Decisions API (POST {baseURL}/decisions). Mirrors TS
// DecisionParallelModel: boolean questions are sent as Parallel's "noul"
// type, probabilities keep full precision, and the request's shared
// citations, per-question confidence and score legends are returned in
// ProviderMetadata["parallel"].
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a Parallel decision model for a mode.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "parallel.decision".
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the decision mode.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports boolean, choice and score.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"boolean", "choice", "score"}
}

var parallelQuestionName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var parallelOptionsSchema = providerutils.OptionSchema{"includeExcerpts": providerutils.OptBool}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	ids := opts.QuestionIDs()
	if len(ids) == 0 || len(ids) > 100 {
		return nil, &providererrors.InvalidArgumentError{Field: "questions", Message: "Parallel Decisions supports 1 to 100 questions."}
	}
	questions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		if !parallelQuestionName.MatchString(id) {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "questions",
				Message: "Parallel question names may contain only ASCII letters, numbers, underscores, and hyphens.",
			}
		}
		q := opts.Questions[id]
		wireType := q.Type
		criteria := q.Criteria
		switch q.Type {
		case "boolean":
			wireType = "noul"
		case "choice":
			options, _ := q.Criteria.(map[string]interface{})
			if len(options) < 2 || len(options) > 255 {
				return nil, &providererrors.InvalidArgumentError{Field: fmt.Sprintf("questions.%s.criteria", id), Message: "Parallel Choice questions require 2 to 255 options."}
			}
			if order := opts.CriteriaOrder[id]; len(order) > 0 {
				criteria = providerutils.OrderedMap{Keys: provider.OrderedKeys(options, order), Vals: options}
			}
		case "score":
			levels, _ := q.Criteria.([]interface{})
			if len(levels) < 2 || len(levels) > 10 {
				return nil, &providererrors.InvalidArgumentError{Field: fmt.Sprintf("questions.%s.criteria", id), Message: "Parallel Score questions require 2 to 10 levels."}
			}
		default:
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		// TS sends the question object as is ({...question, type: 'noul'}
		// for a boolean): type, then instructions and criteria when set.
		wire := providerutils.OrderedMap{Keys: []string{"type"}, Vals: map[string]interface{}{"type": wireType}}
		if q.Instructions != nil {
			wire.Keys = append(wire.Keys, "instructions")
			wire.Vals["instructions"] = q.Instructions
		}
		if q.Criteria != nil {
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = criteria
		}
		questions.Vals[id] = wire
	}

	parallelOptions, err := providerutils.ParseProviderOptions("parallel", opts.ProviderOptions, parallelOptionsSchema)
	if err != nil {
		return nil, err
	}

	// TS evaluates the postJsonToApi headers (loadApiKey) before the body
	// (toParallelState), so a missing key wins over a file part.
	apiKey, err := providerutils.ResolveAPIKey(ctx, providerutils.ResolveAPIKeyOptions{
		APIKey: m.provider.config.APIKey, EnvVar: apiKeyEnvVar, Description: "Parallel",
	})
	if err != nil {
		return nil, err
	}

	state, err := toParallelState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}
	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{"x-api-key": apiKey}, m.provider.config.Headers), version.ProviderUserAgent("parallel"))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	body := providerutils.OrderedMap{Keys: []string{"mode"}, Vals: map[string]interface{}{"mode": m.modelID}}
	if v, ok := parallelOptions["includeExcerpts"]; ok {
		body.Keys = append(body.Keys, "include_excerpts")
		body.Vals["include_excerpts"] = v
	}
	body.Keys = append(body.Keys, "state", "questions")
	body.Vals["state"] = state
	body.Vals["questions"] = questions

	var response parallelDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/decisions",
		Body:             body,
		Headers:          headers,
		ValidateResponse: validateParallelDecisionResponse,
	}, &response)
	if err != nil {
		return nil, parallelHTTPError(err)
	}

	var rawBody interface{}
	_ = json.Unmarshal(httpResp.Body, &rawBody)

	if len(response.Answers) != len(ids) {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Parallel Decisions must return exactly one answer for every question.")
	}
	for _, id := range ids {
		if _, ok := response.Answers[id]; !ok {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "Parallel Decisions must return exactly one answer for every question.")
		}
	}

	answers := make(map[string]provider.DecisionAnswer, len(ids))
	confidence := map[string]interface{}{}
	legend := map[string]interface{}{}
	for _, id := range ids {
		a := response.Answers[id]
		switch a.Type {
		case "noul":
			p := *a.Noul
			answers[id] = provider.DecisionAnswer{Type: "boolean", Probability: &p}
		case "choice":
			answers[id] = provider.DecisionAnswer{Type: "choice", Choice: *a.Choice, Probabilities: a.Probabilities}
		case "score":
			s := *a.Score
			answers[id] = provider.DecisionAnswer{Type: "score", Score: &s, Probabilities: a.Probabilities}
			legend[id] = a.Legend
		default:
			answers[id] = provider.DecisionAnswer{Type: "refusal"}
		}
		if (a.Type == "choice" || a.Type == "score") && a.Confidence != nil {
			confidence[id] = *a.Confidence
		}
	}

	inputTokens, outputTokens := int(response.Usage.InputTokens), 0

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	if raw, ok := opts.ProviderOptions["parallel"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(raw))
		for k := range raw {
			if k != "includeExcerpts" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.parallel." + k})
		}
	}

	return &provider.DecisionResult{
		Answers: answers,
		Usage:   &provider.DecisionUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
		ProviderMetadata: map[string]interface{}{"parallel": map[string]interface{}{
			"citations":  response.citations(),
			"confidence": confidence,
			"decisionId": response.DecisionID,
			"legend":     legend,
			"mode":       response.Mode,
		}},
		Warnings: warnings,
		Response: &provider.DecisionResponseInfo{
			ID:      response.DecisionID,
			ModelID: m.modelID,
			Headers: lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
			Body:    rawBody,
		},
	}, nil
}

// toParallelState is TS toParallelState: text parts become strings, JSON
// parts their values, and a single value is sent unwrapped; files are not
// supported.
func toParallelState(parts []provider.DecisionStatePart) (interface{}, error) {
	values := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "file":
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Parallel Decisions file input"}
		case "text":
			values = append(values, part.Text)
		default:
			values = append(values, part.Value)
		}
	}
	if len(values) == 1 {
		return values[0], nil
	}
	return values, nil
}

// parallelDecisionResponse is the decoded successful response (validated
// by parallelDecisionResponseNode first).
type parallelDecisionResponse struct {
	Answers    map[string]parallelDecisionAnswer `json:"answers"`
	Citations  []map[string]json.RawMessage      `json:"citations"`
	DecisionID string                            `json:"decision_id"`
	Mode       string                            `json:"mode"`
	Usage      struct {
		InputTokens float64 `json:"input_tokens"`
	} `json:"usage"`
}

type parallelDecisionAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
	Legend        map[string]string  `json:"legend"`
}

// citations returns the citations as zod's parsed output: only url, title
// (when present, null kept) and excerpts.
func (r parallelDecisionResponse) citations() []interface{} {
	out := make([]interface{}, 0, len(r.Citations))
	for _, c := range r.Citations {
		entry := map[string]interface{}{}
		for _, key := range []string{"url", "title", "excerpts"} {
			if raw, ok := c[key]; ok {
				var v interface{}
				_ = json.Unmarshal(raw, &v)
				entry[key] = v
			}
		}
		out = append(out, entry)
	}
	return out
}

// parallelDecisionResponseNode is TS parallelDecisionResponseSchema.
var parallelDecisionResponseNode = func() zodlite.Node {
	probability := zodlite.Max(zodlite.Min(zodlite.Number, 0), 1)
	probabilities := zodlite.RecordOf(probability)
	outputTokensZero := func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if f, ok := v.(float64); ok && f == 0 {
			return nil
		}
		return &zodlite.Issue{Path: path, Code: "invalid_value", Values: []string{"0"}, Message: "Invalid input: expected 0"}
	}
	return zodlite.Object(
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", zodlite.Nullish(probability))),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities),
				zodlite.F("legend", zodlite.RecordOf(zodlite.String)), zodlite.F("confidence", zodlite.Nullish(probability))),
			"refusal": zodlite.Typed("refusal"),
		}))),
		zodlite.F("citations", zodlite.Array(zodlite.Object(
			zodlite.F("url", zodlite.String),
			zodlite.F("title", zodlite.Nullish(zodlite.String)),
			zodlite.F("excerpts", zodlite.Array(zodlite.String)),
		))),
		zodlite.F("decision_id", zodlite.String),
		zodlite.F("mode", zodlite.String),
		zodlite.F("usage", zodlite.Object(
			zodlite.F("input_tokens", zodlite.Min(zodlite.Int, 0)),
			zodlite.F("output_tokens", outputTokensZero),
		)),
	)
}()

// validateParallelDecisionResponse is createJsonResponseHandler(
// parallelDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateParallelDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := json.Unmarshal(body, &value)
	if err == nil {
		if bad := parallelDecisionResponseNode(value, true, nil); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}

// parallelHTTPError is createJsonErrorResponseHandler({errorSchema:
// {error: {message}}, errorToMessage: error.error.message}): a matching
// body gives the message, anything else the status text.
func parallelHTTPError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return err
	}
	message := http.StatusText(statusErr.StatusCode)
	var data interface{}
	var envelope struct {
		Error *struct {
			Message *string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(statusErr.Body, &envelope) == nil && envelope.Error != nil && envelope.Error.Message != nil {
		message = *envelope.Error.Message
		// zod's parsed value keeps only the schema's keys.
		data = map[string]interface{}{"error": map[string]interface{}{"message": message}}
	}
	return &providererrors.ProviderError{
		Provider:        decisionProviderName,
		StatusCode:      statusErr.StatusCode,
		Message:         message,
		ResponseHeaders: lowerHeaders(providerutils.ExtractHeaders(statusErr.Headers)),
		ResponseBody:    string(statusErr.Body),
		Data:            data,
	}
}

// lowerHeaders lowercases header names, as TS response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
