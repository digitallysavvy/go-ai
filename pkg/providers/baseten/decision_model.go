package baseten

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// decisionProviderName is the provider ID of Baseten decision models (TS
// provider: 'baseten.decision').
const decisionProviderName = "baseten.decision"

// Baseten decision request limits (TS basetenDecisionModelLimits). They apply
// to custom model IDs as well as the catalog models.
const (
	minChoiceOptions = 1
	maxChoiceOptions = 255
	minScoreLevels   = 1
	maxScoreLevels   = 10
	// maxDecisionOptions counts each boolean question as two options.
	maxDecisionOptions = 512
	maxDecisionImages  = 16
)

var supportedImageMediaTypes = []string{"image/jpeg", "image/png", "image/webp"}

// DecisionModel implements provider.DecisionModel for the Baseten Model APIs
// System One endpoint (POST {baseURL}/systemone). Mirrors TS
// DecisionBasetenModel: boolean questions are sent as "noul", images travel as
// JSON data URLs in one user message, and probabilities keep full precision.
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a Baseten decision model. Decision models always
// use the Model APIs base URL and ignore Config.ModelURL.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

var _ provider.DecisionModel = (*DecisionModel)(nil)

// SpecificationVersion returns "v4".
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "baseten.decision".
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score and boolean; the API rejects
// box questions.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// Typed Go containers in Instructions / Criteria take their JSON form
	// (TS providers always receive JSON).
	opts.Questions = provider.DecisionQuestionsJSON(opts.Questions)
	ids := opts.QuestionIDs()
	if len(ids) == 0 {
		return nil, providererrors.NewArgumentError("questions", "Baseten decisions require at least one question.", nil)
	}

	optionCount := 0
	questions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		q := opts.Questions[id]
		wire := providerutils.OrderedMap{Keys: []string{"type"}, Vals: map[string]interface{}{}}
		addInstructions := func() {
			if q.Instructions != nil {
				wire.Keys = append(wire.Keys, "instructions")
				wire.Vals["instructions"] = q.Instructions
			}
		}
		switch q.Type {
		case "boolean":
			optionCount += 2
			wire.Vals["type"] = "noul"
			addInstructions()
			if criteria, ok := q.Criteria.(map[string]interface{}); ok && criteria != nil {
				// Baseten requires both outcomes when criteria are present; a
				// null description uses its default for that outcome.
				wire.Keys = append(wire.Keys, "criteria")
				wire.Vals["criteria"] = providerutils.OrderedMap{
					Keys: []string{"true", "false"},
					Vals: map[string]interface{}{"true": criteria["true"], "false": criteria["false"]},
				}
			}
		case "choice":
			criteria, _ := q.Criteria.(map[string]interface{})
			if len(criteria) < minChoiceOptions || len(criteria) > maxChoiceOptions {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id),
					fmt.Sprintf("Baseten Choice questions support %d to %d options.", minChoiceOptions, maxChoiceOptions), nil)
			}
			optionCount += len(criteria)
			wire.Vals["type"] = "choice"
			addInstructions()
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = q.Criteria
			if order := opts.CriteriaOrder[id]; len(order) > 0 {
				wire.Vals["criteria"] = providerutils.OrderedMap{Keys: provider.OrderedKeys(criteria, order), Vals: criteria}
			}
		case "score":
			levels, _ := q.Criteria.([]interface{})
			if len(levels) < minScoreLevels || len(levels) > maxScoreLevels {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id),
					fmt.Sprintf("Baseten Score questions support %d to %d levels.", minScoreLevels, maxScoreLevels), nil)
			}
			optionCount += len(levels)
			wire.Vals["type"] = "score"
			addInstructions()
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = q.Criteria
		default:
			// TS: the Box case (the only other question type) throws.
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Baseten decision Box questions"}
		}
		questions.Vals[id] = wire
	}
	if optionCount > maxDecisionOptions {
		return nil, providererrors.NewArgumentError("questions",
			fmt.Sprintf("Baseten decisions support at most %d options across all questions. Each boolean question counts as two.", maxDecisionOptions), nil)
	}

	// TS evaluates the postJsonToApi headers (loadApiKey) before the body
	// (convertToSystemOneState), so a missing key wins over a file part.
	apiKey, err := providerutils.LoadAPIKey(m.provider.apiKey, "BASETEN_API_KEY", "Baseten API key")
	if err != nil {
		return nil, err
	}
	state, err := convertToSystemOneState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}
	headers := version.WithUserAgentSuffix(
		internalhttp.MergeHeaders(map[string]string{"Authorization": "Bearer " + apiKey}, m.provider.headers),
		version.ProviderUserAgent("baseten"))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	body := providerutils.OrderedMap{Keys: []string{"model", "state", "questions"}, Vals: map[string]interface{}{
		"model":     m.modelID,
		"state":     state,
		"questions": questions,
	}}

	var response map[string]interface{}
	httpResp, err := m.provider.decisionClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/systemone",
		Body:    body,
		Headers: headers,
		// TS createJsonResponseHandler(basetenDecisionResponseSchema).
		ValidateResponse: validateBasetenDecisionResponse,
	}, &response)
	if err != nil {
		return nil, basetenDecisionError(err)
	}

	var rawBody interface{}
	_ = jsonexact.Unmarshal(httpResp.Body, &rawBody)
	rawAnswers, _ := response["answers"].(map[string]interface{})
	invalid := func() error {
		return providererrors.NewInvalidResponseDataError(rawBody, "Baseten must return exactly one answer for every question.")
	}
	if len(rawAnswers) != len(ids) {
		return nil, invalid()
	}
	for _, id := range ids {
		if _, ok := rawAnswers[id]; !ok {
			return nil, invalid()
		}
	}

	answers := make(map[string]provider.DecisionAnswer, len(ids))
	confidence := map[string]interface{}{}
	legend := map[string]interface{}{}
	for _, id := range ids {
		a, _ := rawAnswers[id].(map[string]interface{})
		switch a["type"] {
		case "noul":
			p := a["noul"].(float64)
			answers[id] = provider.DecisionAnswer{Type: "boolean", Probability: &p}
		case "choice":
			answers[id] = provider.DecisionAnswer{Type: "choice", Choice: a["choice"].(string), Probabilities: floatMap(a["probabilities"])}
		case "score":
			s := a["score"].(float64)
			answers[id] = provider.DecisionAnswer{Type: "score", Score: &s, Probabilities: floatMap(a["probabilities"])}
			if l, ok := a["legend"]; ok && l != nil {
				legend[id] = l
			}
		}
		if a["type"] != "noul" {
			if c, ok := a["confidence"].(float64); ok {
				confidence[id] = c
			}
		}
	}

	var usage *provider.DecisionUsage
	if u, ok := response["usage"].(map[string]interface{}); ok {
		usage = &provider.DecisionUsage{InputTokens: tokenCount(u["input_tokens"]), OutputTokens: tokenCount(u["output_tokens"])}
	}

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	if raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["baseten"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(raw))
		for k := range raw {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.baseten." + k})
		}
	}

	// TS `response.model ?? this.modelId`: only null/absent falls back.
	modelID := m.modelID
	if model, ok := response["model"].(string); ok {
		modelID = model
	}
	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"baseten": map[string]interface{}{"confidence": confidence, "legend": legend}},
		Response: &provider.DecisionResponseInfo{
			ModelID: modelID,
			Headers: lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
			Body:    rawBody,
		},
	}, nil
}

func floatMap(v interface{}) map[string]float64 {
	m, _ := v.(map[string]interface{})
	out := make(map[string]float64, len(m))
	for k, raw := range m {
		out[k], _ = raw.(float64)
	}
	return out
}

// tokenCount maps a usage count (z.number().nullish(), already validated as
// finite) to the integral Go field: truncated toward zero and clamped to the
// int range.
func tokenCount(v interface{}) *int {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	f = math.Trunc(f)
	var n int
	switch {
	case f >= math.MaxInt:
		n = math.MaxInt
	case f <= math.MinInt:
		n = math.MinInt
	default:
		n = int(f)
	}
	return &n
}

// lowerHeaders lowercases header names, as TS response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}

// convertToSystemOneState is TS convertToSystemOneState: a state with a file
// part becomes a conversation; otherwise a single text, object, array or
// string value is sent as is and other combinations are joined as text.
func convertToSystemOneState(parts []provider.DecisionStatePart) (interface{}, error) {
	values := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "file":
			return convertToSystemOneConversation(parts)
		case "text":
			values = append(values, part.Text)
		default:
			values = append(values, part.Value)
		}
	}
	if len(values) == 1 && nativeState(values[0]) {
		return values[0], nil
	}
	texts := make([]string, len(values))
	for i, value := range values {
		text, err := stateText(value)
		if err != nil {
			return nil, err
		}
		texts[i] = text
	}
	return strings.Join(texts, "\n"), nil
}

// nativeState is TS `value !== null && (typeof value === 'string' || typeof
// value === 'object')`, judged on the value's JSON form.
func nativeState(value interface{}) bool {
	encoded, err := jsonstr.Marshal(value)
	if err != nil || len(encoded) == 0 {
		return false
	}
	return encoded[0] == '{' || encoded[0] == '[' || encoded[0] == '"'
}

// stateText is `typeof value === 'string' ? value : JSON.stringify(value)`.
func stateText(value interface{}) (string, error) {
	if s, ok := value.(string); ok {
		return s, nil
	}
	encoded, err := jsonstr.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("failed to encode decision state: %w", err)
	}
	return string(encoded), nil
}

// convertToSystemOneConversation is TS convertToSystemOneConversation:
// System One reads images only from user message content, so the state is one
// user message whose content holds the parts in request order.
func convertToSystemOneConversation(parts []provider.DecisionStatePart) (interface{}, error) {
	imageCount := 0
	content := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, textPart(part.Text))
		case "json":
			text, err := stateText(part.Value)
			if err != nil {
				return nil, err
			}
			content = append(content, textPart(text))
		case "file":
			imageCount++
			url, err := toImageURL(part)
			if err != nil {
				return nil, err
			}
			content = append(content, providerutils.OrderedMap{
				Keys: []string{"type", "image_url"},
				Vals: map[string]interface{}{
					"type":      "image_url",
					"image_url": providerutils.OrderedMap{Keys: []string{"url"}, Vals: map[string]interface{}{"url": url}},
				},
			})
		}
	}
	if imageCount > maxDecisionImages {
		return nil, providererrors.NewArgumentError("state", fmt.Sprintf("Baseten decisions support at most %d images per request.", maxDecisionImages), nil)
	}
	return providerutils.OrderedMap{Keys: []string{"messages"}, Vals: map[string]interface{}{
		"messages": []interface{}{providerutils.OrderedMap{
			Keys: []string{"role", "content"},
			Vals: map[string]interface{}{"role": "user", "content": content},
		}},
	}}, nil
}

func textPart(text string) providerutils.OrderedMap {
	return providerutils.OrderedMap{Keys: []string{"type", "text"}, Vals: map[string]interface{}{"type": "text", "text": text}}
}

// toImageURL is TS toImageUrl: a URL is forwarded as is, inline JPEG, PNG and
// WebP data becomes a base64 data URL, and anything else is unsupported.
func toImageURL(part provider.DecisionStatePart) (string, error) {
	file := part.File
	if file == nil {
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: "Baseten decision file input"}
	}
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = file.MimeType
	}
	if mediaType == "" {
		mediaType = file.FileData.MediaType
	}
	if fileutil.TopLevelMediaType(mediaType) != "image" {
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: "Baseten decision file media type: " + mediaType}
	}
	kind, fd := file.FileData.Type, file.FileData
	if kind == "" { // legacy flat fields
		switch {
		case len(file.Data) > 0:
			kind, fd.Data = types.FileDataTypeData, file.Data
		case file.URL != "":
			kind, fd.URL = types.FileDataTypeURL, file.URL
		default:
			kind = types.FileDataTypeData
		}
	}
	switch kind {
	case types.FileDataTypeURL:
		return fd.URL, nil
	case types.FileDataTypeData:
		resolved := mediaType
		if !fileutil.IsFullMediaType(mediaType) {
			resolved = ""
			if fd.DataString != "" {
				detected, ok, err := fileutil.DetectMediaTypeSignatureBase64(fd.DataString, "image")
				if err != nil {
					return "", err
				}
				if ok {
					resolved = detected
				}
			} else if detected, ok := fileutil.DetectMediaTypeSignature(fd.Data, "image"); ok {
				resolved = detected
			}
		}
		if resolved == "" || !containsString(supportedImageMediaTypes, resolved) {
			shown := resolved
			if shown == "" {
				shown = mediaType
			}
			return "", &providererrors.UnsupportedFunctionalityError{Functionality: "Baseten decision image media type: " + shown}
		}
		payload := fd.DataString
		if payload == "" {
			payload = base64.StdEncoding.EncodeToString(fd.Data)
		}
		return "data:" + resolved + ";base64," + payload, nil
	default:
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("Baseten decision file input: %s", kind)}
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// basetenDecisionResponseNode is TS basetenDecisionResponseSchema.
var basetenDecisionResponseNode = func() zodlite.Node {
	probability := zodlite.Max(zodlite.Min(zodlite.Number, 0), 1)
	probabilities := zodlite.RecordOf(probability)
	nullishNum := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities),
				zodlite.F("confidence", zodlite.Nullish(probability))),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities),
				zodlite.F("confidence", zodlite.Nullish(probability)),
				zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.JSON)))),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", nullishNum), zodlite.F("output_tokens", nullishNum)))),
	)
}()

// validateBasetenDecisionResponse is createJsonResponseHandler(
// basetenDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateBasetenDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(basetenDecisionResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}
