package liquid

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
)

// decisionProviderName is the provider ID of Liquid decision models.
const decisionProviderName = "liquid.decision"

// supportedMediaTypes are the image and audio media types the Liquid API
// accepts.
var supportedMediaTypes = map[string][]string{
	"image": {"image/jpeg", "image/png", "image/webp"},
	"audio": {"audio/wav", "audio/mpeg", "audio/flac", "audio/ogg"},
}

// DecisionModel implements provider.DecisionModel for the Liquid Decisions
// API. Mirrors TS DecisionLiquidModel: boolean questions are sent as Liquid's
// "noul" type, images and audio travel as ordered data URLs beside the state,
// and per-question confidence, score legends and Box found probabilities are
// returned in ProviderMetadata["liquid"].
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a Liquid decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "liquid.decision".
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score, boolean and box.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean", "box"}
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// Typed Go containers in Instructions / Criteria take their JSON form
	// (TS providers always receive JSON).
	opts.Questions = provider.DecisionQuestionsJSON(opts.Questions)
	ids := opts.QuestionIDs()
	if len(ids) == 0 || len(ids) > maxQuestions {
		return nil, providererrors.NewArgumentError("questions", fmt.Sprintf("Liquid decisions support 1 to %d questions per request.", maxQuestions), nil)
	}

	questions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	hasBox := false
	for _, id := range ids {
		q := opts.Questions[id]
		if q.Instructions == nil {
			return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.instructions", id), "Liquid decision questions require instructions.", nil)
		}
		wire := providerutils.OrderedMap{Keys: []string{"type", "instructions"}, Vals: map[string]interface{}{"instructions": q.Instructions}}
		switch q.Type {
		case "boolean":
			wire.Vals["type"] = "noul"
			if q.Criteria != nil {
				wire.Keys = append(wire.Keys, "criteria")
				wire.Vals["criteria"] = q.Criteria
			}
		case "choice":
			options, _ := q.Criteria.(map[string]interface{})
			if len(options) < minChoiceOptions || len(options) > maxChoiceOptions {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id), fmt.Sprintf("Liquid Choice questions support %d to %d options.", minChoiceOptions, maxChoiceOptions), nil)
			}
			wire.Vals["type"] = "choice"
			criteria := q.Criteria
			if order := opts.CriteriaOrder[id]; len(order) > 0 {
				criteria = providerutils.OrderedMap{Keys: provider.OrderedKeys(options, order), Vals: options}
			}
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = criteria
		case "score":
			levels, _ := q.Criteria.([]interface{})
			nullLevel := false
			for _, level := range levels {
				if level == nil {
					nullLevel = true
				}
			}
			if len(levels) < minScoreLevels || len(levels) > maxScoreLevels || nullLevel {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id), fmt.Sprintf("Liquid Score questions require %d to %d non-null levels.", minScoreLevels, maxScoreLevels), nil)
			}
			wire.Vals["type"] = "score"
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = q.Criteria
		case "box":
			wire.Vals["type"] = "box"
			hasBox = true
		default:
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		questions.Vals[id] = wire
	}

	state, images, audio, err := convertToLiquidDecisionInput(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}
	if hasBox && len(images) != 1 {
		return nil, providererrors.NewArgumentError("state", "Liquid Box questions require exactly one image in the state.", nil)
	}

	// TS builds the headers (loadApiKey) inside the postJsonToApi call, after
	// the request has been validated.
	headers, err := m.provider.resolveHeaders()
	if err != nil {
		return nil, err
	}
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	body := providerutils.OrderedMap{Keys: []string{"model", "state"}, Vals: map[string]interface{}{"model": m.modelID, "state": state}}
	if len(images) > 0 {
		body.Keys = append(body.Keys, "images")
		body.Vals["images"] = images
	}
	if len(audio) > 0 {
		body.Keys = append(body.Keys, "audio")
		body.Vals["audio"] = audio
	}
	body.Keys = append(body.Keys, "questions")
	body.Vals["questions"] = questions

	var response liquidDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/decisions/v1/systemone",
		Body:             body,
		Headers:          headers,
		ValidateResponse: validateLiquidDecisionResponse,
	}, &response)
	if err != nil {
		return nil, liquidHTTPError(err)
	}

	var rawBody interface{}
	_ = jsonexact.Unmarshal(httpResp.Body, &rawBody)

	wrongAnswers := len(response.Answers) != len(ids)
	for _, id := range ids {
		if _, ok := response.Answers[id]; !ok {
			wrongAnswers = true
		}
	}
	if wrongAnswers {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "Liquid must return exactly one answer for every question.")
	}

	// Answers in the order the response sent them, which is the order TS
	// builds the metadata records in; Go maps carry no order, so sort.
	answerIDs := make([]string, 0, len(response.Answers))
	for id := range response.Answers {
		answerIDs = append(answerIDs, id)
	}
	sort.Strings(answerIDs)

	answers := make(map[string]provider.DecisionAnswer, len(answerIDs))
	confidence := map[string]interface{}{}
	legend := map[string]interface{}{}
	found := map[string]interface{}{}
	for _, id := range answerIDs {
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
			if a.Legend != nil {
				legend[id] = a.Legend
			}
		case "box":
			// Liquid omits the box when the object is not found (found < 0.5).
			if a.Box == nil {
				answers[id] = provider.DecisionAnswer{Type: "refusal"}
			} else {
				answers[id] = provider.DecisionAnswer{Type: "box", Box: &provider.DecisionBox{
					Top: a.Box.Top, Bottom: a.Box.Bottom, Left: a.Box.Left, Right: a.Box.Right,
				}}
			}
			if a.Found != nil {
				found[id] = *a.Found
			}
		}
		if a.Type != "noul" && a.Confidence != nil {
			confidence[id] = *a.Confidence
		}
	}

	metadata := map[string]interface{}{"confidence": confidence, "legend": legend, "found": found}
	var usage *provider.DecisionUsage
	if response.Usage != nil {
		usage = &provider.DecisionUsage{
			InputTokens:  liquidTokenCount(response.Usage.InputTokens),
			OutputTokens: liquidTokenCount(response.Usage.OutputTokens),
		}
		if response.Usage.Cost != nil {
			metadata["cost"] = *response.Usage.Cost
		}
	}

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	// Go-typed literals read like their JSON form (OVC-01).
	if raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["liquid"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(raw))
		for k := range raw {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.liquid." + k})
		}
	}

	modelID := m.modelID
	// TS `response.model ?? this.modelId`: only null or absent falls back.
	if response.Model != nil {
		modelID = *response.Model
	}
	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"liquid": metadata},
		Response: &provider.DecisionResponseInfo{
			ModelID: modelID,
			Headers: lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
			Body:    rawBody,
		},
	}, nil
}

// convertToLiquidDecisionInput is TS convertToLiquidDecisionInput: text and
// JSON parts become the state, image and audio files become ordered data URLs
// (Liquid reads them from top-level arrays, before the state). A single
// string, object or array state value is sent as is, anything else joined as
// text, and a state with no text or JSON is null.
func convertToLiquidDecisionInput(parts []provider.DecisionStatePart) (interface{}, []string, []string, error) {
	var values []interface{}
	var images, audio []string
	for _, part := range parts {
		switch part.Type {
		case "text":
			values = append(values, part.Text)
		case "json":
			values = append(values, part.Value)
		case "file":
			kind, dataURL, err := liquidFileDataURL(part)
			if err != nil {
				return nil, nil, nil, err
			}
			if kind == "image" {
				images = append(images, dataURL)
			} else {
				audio = append(audio, dataURL)
			}
		}
	}
	if len(images) > maxImages || len(audio) > maxAudioClips {
		return nil, nil, nil, providererrors.NewArgumentError("state", fmt.Sprintf("Liquid decisions support at most %d images and %d audio clips per request.", maxImages, maxAudioClips), nil)
	}

	if len(values) == 0 {
		return nil, images, audio, nil
	}
	texts := make([]string, len(values))
	native := false
	for i, value := range values {
		if s, ok := value.(string); ok {
			texts[i] = s
			native = len(values) == 1
			continue
		}
		encoded, err := jsonstr.Marshal(value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to encode decision state: %w", err)
		}
		texts[i] = string(encoded)
		// typeof object and not null: the JSON form starts with { or [.
		native = len(values) == 1 && (texts[i][0] == '{' || texts[i][0] == '[')
	}
	if native {
		return values[0], images, audio, nil
	}
	return strings.Join(texts, "\n"), images, audio, nil
}

// liquidFileDataURL converts one file part to its top-level kind ("image" or
// "audio") and base64 data URL, rejecting anything Liquid does not accept.
func liquidFileDataURL(part provider.DecisionStatePart) (string, string, error) {
	file := part.File
	if file == nil {
		return "", "", &providererrors.UnsupportedFunctionalityError{Functionality: "Liquid decision file input: undefined"}
	}
	declared := file.MediaType
	if declared == "" {
		declared = file.MimeType
	}
	if declared == "" {
		declared = file.FileData.MediaType
	}
	kind := declared
	if slash := strings.IndexByte(declared, '/'); slash >= 0 {
		kind = declared[:slash]
	}
	if kind != "image" && kind != "audio" {
		return "", "", &providererrors.UnsupportedFunctionalityError{Functionality: "Liquid decision file media type: " + declared}
	}
	if dataType := file.FileData.Type; dataType != "" && dataType != types.FileDataTypeData {
		return "", "", &providererrors.UnsupportedFunctionalityError{Functionality: "Liquid decision file input: " + string(dataType)}
	}

	// A base64 string is sent unchanged; bytes are encoded.
	raw := file.FileData.Data
	if len(raw) == 0 {
		raw = file.Data
	}
	payload := file.FileData.DataString
	if payload == "" {
		payload = base64.StdEncoding.EncodeToString(raw)
	}

	mediaType := declared
	if slash := strings.IndexByte(declared, '/'); slash < 0 || slash == len(declared)-1 || declared[slash+1:] == "*" {
		var detected string
		var ok bool
		if file.FileData.DataString != "" {
			var err error
			if detected, ok, err = fileutil.DetectMediaTypeSignatureBase64(file.FileData.DataString, kind); err != nil {
				return "", "", err
			}
		} else {
			detected, ok = fileutil.DetectMediaTypeSignature(raw, kind)
		}
		if !ok {
			detected = ""
		}
		mediaType = detected
	}
	supported := false
	for _, candidate := range supportedMediaTypes[kind] {
		if candidate == mediaType {
			supported = true
		}
	}
	if mediaType == "" || !supported {
		shown := mediaType
		if shown == "" {
			shown = declared
		}
		return "", "", &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("Liquid decision %s media type: %s", kind, shown)}
	}
	return kind, "data:" + mediaType + ";base64," + payload, nil
}

// liquidTokenCount maps a usage count to the integral Go usage field:
// truncated toward zero and clamped to the int range.
func liquidTokenCount(v *float64) *int {
	if v == nil {
		return nil
	}
	f := math.Trunc(*v)
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

// liquidDecisionResponse is the decoded successful response (validated by
// liquidResponseNode first).
type liquidDecisionResponse struct {
	Model   *string                         `json:"model"`
	Answers map[string]liquidDecisionAnswer `json:"answers"`
	Usage   *struct {
		InputTokens  *float64 `json:"input_tokens"`
		OutputTokens *float64 `json:"output_tokens"`
		Cost         *float64 `json:"cost"`
	} `json:"usage"`
}

type liquidDecisionAnswer struct {
	Type          string                 `json:"type"`
	Noul          *float64               `json:"noul"`
	Choice        *string                `json:"choice"`
	Score         *float64               `json:"score"`
	Probabilities map[string]float64     `json:"probabilities"`
	Confidence    *float64               `json:"confidence"`
	Legend        map[string]interface{} `json:"legend"`
	Box           *struct {
		Top    float64 `json:"top"`
		Bottom float64 `json:"bottom"`
		Left   float64 `json:"left"`
		Right  float64 `json:"right"`
	} `json:"box"`
	Found *float64 `json:"found"`
}

// liquidResponseNode is TS liquidDecisionResponseSchema.
var liquidResponseNode = func() zodlite.Node {
	probability := zodlite.Max(zodlite.Min(zodlite.Number, 0), 1)
	nullishProbability := zodlite.Nullish(probability)
	probabilities := zodlite.RecordOf(probability)
	return zodlite.Object(
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability)),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities),
				zodlite.F("confidence", nullishProbability), zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.JSON)))),
			"box": zodlite.Typed("box",
				zodlite.F("box", zodlite.Nullish(zodlite.Object(
					zodlite.F("top", probability), zodlite.F("bottom", probability),
					zodlite.F("left", probability), zodlite.F("right", probability)))),
				zodlite.F("found", nullishProbability), zodlite.F("confidence", nullishProbability)),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", zodlite.Nullish(zodlite.Number)),
			zodlite.F("output_tokens", zodlite.Nullish(zodlite.Number)),
			zodlite.F("cost", zodlite.Nullish(zodlite.Number))))),
	)
}()

// validateLiquidDecisionResponse is createJsonResponseHandler(
// liquidDecisionResponseSchema): a body that does not match is an APICallError
// "Invalid JSON response".
func validateLiquidDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(liquidResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}

// lowerHeaders lowercases header names, as TS response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
