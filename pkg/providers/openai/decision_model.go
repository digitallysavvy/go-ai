package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// DecisionModelID is an OpenAI Decisions API model ID. Any string is accepted.
type DecisionModelID = string

// DecisionModelOptions are the OpenAI decision provider options, passed under
// ProviderOptions["openai"].
type DecisionModelOptions struct {
	// SafetyIdentifier is an opaque end-user identifier for safety monitoring,
	// up to 128 characters.
	SafetyIdentifier *string `json:"safetyIdentifier,omitempty"`
}

// DecisionModel implements provider.DecisionModel on top of the OpenAI
// Decisions API (POST /decisions). Mirrors TypeScript's DecisionOpenAIModel
// (packages/openai/src/openai-decision-model.ts).
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates an OpenAI Decisions API model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "<provider name>.decision".
func (m *DecisionModel) Provider() string { return m.provider.Name() + ".decision" }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score, and boolean.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoEvaluate is the pre-rename form of DoDecide.
//
// Deprecated: Use DoDecide instead.
func (m *DecisionModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return m.DoDecide(ctx, opts)
}

// Decisions API wire types.

type decisionUsageWire struct {
	InputTokens        *float64 `json:"input_tokens"`
	InputTokensDetails *struct {
		CachedTokens     *float64 `json:"cached_tokens"`
		CacheWriteTokens *float64 `json:"cache_write_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens        *float64 `json:"output_tokens"`
	OutputTokensDetails *struct {
		ReasoningTokens *float64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens *float64 `json:"total_tokens"`
}

type decisionProbabilityWire struct {
	// Value is a string for choice distributions and a non-negative integer
	// for score distributions; decoded lazily.
	Value       json.RawMessage `json:"value"`
	Probability *float64        `json:"probability"`
}

type decisionAnswerWire struct {
	Type          string                    `json:"type"`
	Name          *string                   `json:"name"`
	Probability   *float64                  `json:"probability"`
	Choice        *string                   `json:"choice"`
	Score         *float64                  `json:"score"`
	Confidence    *float64                  `json:"confidence"`
	Probabilities []decisionProbabilityWire `json:"probabilities"`
}

type decisionResponseWire struct {
	Model   *string              `json:"model"`
	Usage   *decisionUsageWire   `json:"usage"`
	Answers []decisionAnswerWire `json:"answers"`
}

func validProbability(p *float64) bool {
	return p != nil && *p >= 0 && *p <= 1
}

// validate mirrors the TS zod response schema; a failure is reported as an
// invalid successful response.
func (r *decisionResponseWire) validate() error {
	if r.Answers == nil {
		return fmt.Errorf("answers must be an array")
	}
	for i, a := range r.Answers {
		switch a.Type {
		case "refusal":
			// name may be null.
		case "predicate":
			if a.Name == nil || !validProbability(a.Probability) {
				return fmt.Errorf("answers[%d]: predicate requires a name and a probability in [0, 1]", i)
			}
		case "choice":
			if a.Name == nil || a.Choice == nil || a.Probabilities == nil || (a.Confidence != nil && !validProbability(a.Confidence)) {
				return fmt.Errorf("answers[%d]: invalid choice answer", i)
			}
			for _, p := range a.Probabilities {
				var v string
				if err := json.Unmarshal(p.Value, &v); err != nil || !validProbability(p.Probability) {
					return fmt.Errorf("answers[%d]: invalid choice probability", i)
				}
			}
		case "score":
			if a.Name == nil || a.Score == nil || a.Probabilities == nil || (a.Confidence != nil && !validProbability(a.Confidence)) {
				return fmt.Errorf("answers[%d]: invalid score answer", i)
			}
			for _, p := range a.Probabilities {
				var v float64
				if err := json.Unmarshal(p.Value, &v); err != nil || v < 0 || v != float64(int64(v)) || !validProbability(p.Probability) {
					return fmt.Errorf("answers[%d]: invalid score probability", i)
				}
			}
		default:
			return fmt.Errorf("answers[%d]: unknown answer type %q", i, a.Type)
		}
	}
	return nil
}

// DoDecide sends every question in one Decisions API request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	openaiOpts, err := parseDecisionProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	content, err := decisionInputContent(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	ids := opts.QuestionIDs()
	questions := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		questions = append(questions, decisionQuestionWire(id, opts.Questions[id], opts.CriteriaOrder[id]))
	}

	body := map[string]interface{}{
		"model":     m.modelID,
		"input":     []interface{}{map[string]interface{}{"role": "user", "content": content}},
		"questions": questions,
	}
	if openaiOpts.SafetyIdentifier != nil {
		body["safety_identifier"] = *openaiOpts.SafetyIdentifier
	}

	var raw json.RawMessage
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/decisions",
		Body:    body,
		Headers: opts.Headers,
	}, &raw)
	if err != nil {
		return nil, m.handleError(err)
	}

	var response decisionResponseWire
	var rawBody interface{}
	_ = json.Unmarshal(raw, &rawBody)
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Invalid response from the OpenAI Decisions API: "+err.Error())
	}
	if err := response.validate(); err != nil {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Invalid response from the OpenAI Decisions API: "+err.Error())
	}

	answers := make(map[string]provider.DecisionAnswer, len(response.Answers))
	names := make([]string, 0, len(response.Answers))
	confidence := map[string]interface{}{}
	for _, a := range response.Answers {
		if a.Type == "refusal" {
			if a.Name == nil {
				return nil, providererrors.NewInvalidResponseDataError(rawBody, "OpenAI Decisions refused an unnamed question.")
			}
			names = append(names, *a.Name)
			answers[*a.Name] = provider.DecisionAnswer{Type: "refusal"}
			continue
		}
		name := *a.Name
		names = append(names, name)
		if a.Type == "predicate" {
			p := *a.Probability
			answers[name] = provider.DecisionAnswer{Type: "boolean", Probability: &p}
			continue
		}
		probabilities := make(map[string]float64, len(a.Probabilities))
		for _, p := range a.Probabilities {
			key := decisionProbabilityKey(p.Value)
			probabilities[key] = *p.Probability
		}
		if len(probabilities) != len(a.Probabilities) {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "Decisions returned duplicate probability values.")
		}
		if a.Type == "choice" {
			answers[name] = provider.DecisionAnswer{Type: "choice", Choice: *a.Choice, Probabilities: probabilities}
		} else {
			score := *a.Score
			answers[name] = provider.DecisionAnswer{Type: "score", Score: &score, Probabilities: probabilities}
		}
		if a.Confidence != nil {
			confidence[name] = *a.Confidence
		}
	}

	seen := make(map[string]struct{}, len(names))
	valid := len(names) == len(opts.Questions)
	for _, name := range names {
		if _, dup := seen[name]; dup {
			valid = false
		}
		seen[name] = struct{}{}
		if _, known := opts.Questions[name]; !known {
			valid = false
		}
	}
	if !valid {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Decisions must return exactly one answer for every question.")
	}

	var unsupported []types.Warning
	if opts.MaxOutputTokens != nil {
		unsupported = append(unsupported, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		unsupported = append(unsupported, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	two := 2
	result := &provider.DecisionResult{
		Answers: answers,
		// The Decisions API reports probabilities and scores to two decimal places.
		Rounding: &provider.DecisionRounding{ProbabilityDecimals: &two, ScoreDecimals: &two},
		Warnings: append(unsupported, decisionUnsupportedOptionWarnings(opts.ProviderOptions)...),
		Response: &provider.DecisionResponseInfo{
			ModelID: m.modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawBody,
		},
	}
	if response.Model != nil {
		result.Response.ModelID = *response.Model
	}

	openaiMetadata := map[string]interface{}{"confidence": confidence}
	if response.Usage != nil {
		result.Usage = &provider.DecisionUsage{
			InputTokens:  floatToIntPtr(response.Usage.InputTokens),
			OutputTokens: floatToIntPtr(response.Usage.OutputTokens),
		}
		var usageMap interface{}
		if rawMap, ok := rawBody.(map[string]interface{}); ok {
			usageMap = pickDecisionUsage(rawMap["usage"])
		}
		openaiMetadata["usage"] = usageMap
	}
	result.ProviderMetadata = map[string]interface{}{"openai": openaiMetadata}
	return result, nil
}

// handleError mirrors openaiFailedResponseHandler: an HTTP error keeps its
// status code (so 408/409/429/5xx retry) and uses the OpenAI error message.
func (m *DecisionModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	message := string(statusErr.Body)
	errorCode := ""
	var envelope struct {
		Error *struct {
			Message string      `json:"message"`
			Code    interface{} `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(statusErr.Body, &envelope) == nil && envelope.Error != nil && envelope.Error.Message != "" {
		message = envelope.Error.Message
		if code, ok := envelope.Error.Code.(string); ok {
			errorCode = code
		}
	}
	return providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, errorCode, message, err)
}

func floatToIntPtr(v *float64) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

// decisionProbabilityKey renders a distribution value (string or integer) as
// the map key core expects.
func decisionProbabilityKey(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatInt(int64(n), 10)
	}
	return string(raw)
}

func parseDecisionProviderOptions(providerOptions map[string]interface{}) (DecisionModelOptions, error) {
	var out DecisionModelOptions
	openaiOpts, ok := providerOptions["openai"]
	if !ok || openaiOpts == nil {
		return out, nil
	}
	options, ok := openaiOpts.(map[string]interface{})
	if !ok {
		return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai", Message: "must be an object"}
	}
	value, present := options["safetyIdentifier"]
	if !present {
		return out, nil
	}
	s, ok := value.(string)
	if !ok || len(s) > 128 {
		return out, &providererrors.InvalidArgumentError{
			Field:   "providerOptions.openai.safetyIdentifier",
			Message: "must be a string of at most 128 characters",
		}
	}
	out.SafetyIdentifier = &s
	return out, nil
}

func decisionUnsupportedOptionWarnings(providerOptions map[string]interface{}) []types.Warning {
	openaiOpts, _ := providerOptions["openai"].(map[string]interface{})
	keys := make([]string, 0, len(openaiOpts))
	for k := range openaiOpts {
		if k != "safetyIdentifier" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	warnings := make([]types.Warning, 0, len(keys))
	for _, k := range keys {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.openai." + k})
	}
	return warnings
}

func decisionToText(input interface{}) string {
	if s, ok := input.(string); ok {
		return s
	}
	return decisionJSONText(input)
}

// decisionJSONText is JSON.stringify: no HTML escaping, and a string value
// keeps its quotes.
func decisionJSONText(input interface{}) string {
	b, err := jsonstr.Marshal(input)
	if err != nil {
		return fmt.Sprint(input)
	}
	return string(b)
}

func decisionInputContent(parts []provider.DecisionStatePart) ([]interface{}, error) {
	content := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, map[string]interface{}{"type": "input_text", "text": part.Text})
		case "json":
			content = append(content, map[string]interface{}{"type": "input_text", "text": decisionJSONText(part.Value)})
		case "file":
			image, err := decisionImageContent(part)
			if err != nil {
				return nil, err
			}
			content = append(content, image)
		default:
			return nil, fmt.Errorf("unknown decision state part type %q", part.Type)
		}
	}
	return content, nil
}

func decisionImageContent(part provider.DecisionStatePart) (map[string]interface{}, error) {
	if part.File == nil {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "OpenAI decision file input"}
	}
	declared := part.File.MediaType
	if declared == "" {
		declared = part.File.MimeType
	}
	kind := string(part.File.FileData.Type)
	if kind != "" && kind != string(types.FileDataTypeData) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("OpenAI decision file input: %s (%s)", declared, kind),
		}
	}
	mediaType, data, err := part.InlineData()
	if err != nil {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("OpenAI decision file input: %s (%s)", declared, err),
		}
	}
	// Direct DoDecide calls can bypass core's media type detection.
	if !strings.Contains(mediaType, "/") || strings.HasSuffix(mediaType, "/*") {
		if detected, ok := fileutil.DetectMediaTypeSignature(data, "image"); ok {
			mediaType = detected
		}
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("OpenAI decision image media type: %s", declared),
		}
	}
	image := map[string]interface{}{
		"type":      "input_image",
		"image_url": "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}
	if openaiOpts, ok := part.File.ProviderOptions["openai"].(map[string]interface{}); ok {
		if detail, ok := openaiOpts["imageDetail"]; ok && detail != nil {
			image["detail"] = detail
		}
	}
	return image, nil
}

func decisionQuestionWire(name string, q provider.DecisionQuestion, criteriaOrder []string) map[string]interface{} {
	// A criteria-only question (nil instructions) is sent without them.
	hasInstructions := q.Instructions != nil
	instructions := ""
	if hasInstructions {
		instructions = decisionToText(q.Instructions)
	}
	withInstructions := func(question map[string]interface{}) map[string]interface{} {
		if hasInstructions {
			question["instructions"] = instructions
		}
		return question
	}
	switch q.Type {
	case "boolean":
		sections := []string{}
		if hasInstructions {
			sections = append(sections, instructions)
		}
		if criteria, ok := q.Criteria.(map[string]interface{}); ok {
			if v := criteria["true"]; v != nil {
				sections = append(sections, "Criteria for true:\n"+decisionToText(v))
			}
			if v := criteria["false"]; v != nil {
				sections = append(sections, "Criteria for false:\n"+decisionToText(v))
			}
		}
		return map[string]interface{}{"type": "predicate", "name": name, "instructions": strings.Join(sections, "\n\n")}
	case "choice":
		criteria, _ := q.Criteria.(map[string]interface{})
		values := provider.OrderedKeys(criteria, criteriaOrder)
		choices := make([]interface{}, 0, len(values))
		for _, v := range values {
			choice := map[string]interface{}{"value": v}
			if criteria[v] != nil {
				choice["description"] = decisionToText(criteria[v])
			}
			choices = append(choices, choice)
		}
		return withInstructions(map[string]interface{}{"type": "choice", "name": name, "choices": choices})
	default: // score
		criteria, _ := q.Criteria.([]interface{})
		levels := make([]interface{}, 0, len(criteria))
		for i, description := range criteria {
			// Score criteria have no separate labels; indices identify each level.
			level := map[string]interface{}{"label": strconv.Itoa(i)}
			if description != nil {
				level["description"] = decisionToText(description)
			}
			levels = append(levels, level)
		}
		return withInstructions(map[string]interface{}{"type": "score", "name": name, "levels": levels})
	}
}

// pickDecisionUsage keeps only the keys of the TS zod usage schema (zod
// objects strip unknown keys, so providerMetadata.openai.usage never carries
// them). Absent keys stay absent and explicit nulls stay null.
func pickDecisionUsage(raw interface{}) interface{} {
	usage, ok := raw.(map[string]interface{})
	if !ok {
		return raw
	}
	pick := func(src map[string]interface{}, keys ...string) map[string]interface{} {
		out := map[string]interface{}{}
		for _, k := range keys {
			if v, present := src[k]; present {
				out[k] = v
			}
		}
		return out
	}
	out := pick(usage, "input_tokens", "output_tokens", "total_tokens")
	for key, nested := range map[string][]string{
		"input_tokens_details":  {"cached_tokens", "cache_write_tokens"},
		"output_tokens_details": {"reasoning_tokens"},
	} {
		v, present := usage[key]
		if !present {
			continue
		}
		if details, ok := v.(map[string]interface{}); ok {
			out[key] = pick(details, nested...)
		} else {
			out[key] = v
		}
	}
	return out
}
