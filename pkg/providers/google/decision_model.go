package google

import (
	"context"
	"encoding/base64"
	"errors"
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
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// ModelGeminiDecision1 is the Gemini Decisions model (TS GoogleDecisionModelId).
const ModelGeminiDecision1 = "gemini-decision-1"

// DecisionModel implements provider.DecisionModel for the native Gemini
// Decisions API (POST {baseURL}/decisions). Mirrors TS DecisionGoogleModel
// (google-decision-model.ts, f70336158e). Boolean questions are sent as the
// native "noul" type and their answer probability is kept as returned.
type DecisionModel struct {
	provider *Provider
	modelID  string
	name     string
}

// SpecificationVersion returns the decision model specification version.
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider ID, "google.decision" by default (the
// provider name with ".generative-ai" replaced by ".decision").
func (m *DecisionModel) Provider() string { return m.name }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score and boolean.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoEvaluate is the deprecated form of DoDecide.
//
// Deprecated: Use DoDecide instead.
func (m *DecisionModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return m.DoDecide(ctx, opts)
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// TS abortSignal?.throwIfAborted() before any work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Typed Go containers in Instructions / Criteria take their JSON form.
	opts.Questions = provider.DecisionQuestionsJSON(opts.Questions)
	ids := opts.QuestionIDs()
	if len(ids) == 0 {
		return nil, providererrors.NewArgumentError("questions", "Google Decisions requires at least one question.", nil)
	}

	questions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		wire, err := m.decisionQuestion(id, opts.Questions[id], opts.CriteriaOrder[id])
		if err != nil {
			return nil, err
		}
		questions.Vals[id] = wire
	}

	input, err := googleDecisionInput(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	var response googleDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/decisions",
		Body: providerutils.OrderedMap{
			Keys: []string{"model", "input", "questions"},
			Vals: map[string]interface{}{"model": m.modelID, "input": input, "questions": questions},
		},
		Headers:          opts.Headers,
		ValidateResponse: providerutils.NodeResponseValidator(m.Provider(), zGoogleDecisionResponse),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rawValue := providerutils.RawJSONBody(httpResp.Body)
	blocked := response.PromptFeedback != nil && response.PromptFeedback.BlockReason != nil
	if !blocked {
		complete := response.Answers != nil && len(response.Answers) == len(ids)
		for _, id := range ids {
			if _, ok := response.Answers[id]; !ok {
				complete = false
			}
		}
		if !complete {
			return nil, providererrors.NewInvalidResponseDataError(rawValue, "Google Decisions must return exactly one answer for every question.")
		}
	}

	answers := make(map[string]provider.DecisionAnswer, len(ids))
	for _, id := range ids {
		if blocked {
			answers[id] = provider.DecisionAnswer{Type: "refusal"}
			continue
		}
		answer, ok := googleDecisionAnswer(opts.Questions[id], response.Answers[id])
		if !ok {
			return nil, providererrors.NewInvalidResponseDataError(rawValue, fmt.Sprintf("Google Decisions returned an invalid answer for question %q.", id))
		}
		answers[id] = answer
	}

	confidence := map[string]float64{}
	legend := map[string]interface{}{}
	for id, answer := range response.Answers {
		if answer.Confidence != nil {
			confidence[id] = *answer.Confidence
		}
		if answer.Type == "score" && answer.Legend != nil {
			legend[id] = answer.Legend
		}
	}
	googleMeta := map[string]interface{}{"confidence": confidence, "legend": legend}
	var usage *provider.DecisionUsage
	if response.Usage != nil {
		usage = &provider.DecisionUsage{
			InputTokens:  googleDecisionTokens(response.Usage.TotalInputTokens),
			OutputTokens: googleDecisionTokens(response.Usage.TotalOutputTokens),
		}
		googleMeta["usage"] = googleDecisionDeclared(rawValue, "usage", "total_input_tokens", "total_output_tokens", "total_tokens", "input_tokens_by_modality")
	}
	if response.PromptFeedback != nil {
		googleMeta["promptFeedback"] = googleDecisionDeclared(rawValue, "prompt_feedback", "block_reason", "block_reason_message")
	}

	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         googleDecisionWarnings(opts),
		ProviderMetadata: map[string]interface{}{"google": googleMeta},
		Response: &provider.DecisionResponseInfo{
			ModelID: m.modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawValue,
		},
	}, nil
}

// decisionQuestion validates one question and builds its wire form (TS
// requestQuestions), keys in the TS literal order.
func (m *DecisionModel) decisionQuestion(id string, q provider.DecisionQuestion, criteriaOrder []string) (providerutils.OrderedMap, error) {
	switch q.Type {
	case "choice", "score", "boolean":
	default:
		return providerutils.OrderedMap{}, &providererrors.DecisionUnsupportedQuestionTypeError{
			QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
		}
	}
	if q.Instructions == nil {
		return providerutils.OrderedMap{}, providererrors.NewArgumentError("questions."+id+".instructions", "Google Decisions requires instructions for every question.", nil)
	}
	instructions := googleDecisionText(q.Instructions)
	if q.Type == "boolean" {
		wire := providerutils.OrderedMap{Keys: []string{"type", "instructions"}, Vals: map[string]interface{}{"type": "noul", "instructions": instructions}}
		if criteria, ok := q.Criteria.(map[string]interface{}); ok {
			desc := providerutils.OrderedMap{Vals: map[string]interface{}{}}
			if v := criteria["true"]; v != nil {
				desc.Keys = append(desc.Keys, "true_description")
				desc.Vals["true_description"] = googleDecisionText(v)
			}
			if v := criteria["false"]; v != nil {
				desc.Keys = append(desc.Keys, "false_description")
				desc.Vals["false_description"] = googleDecisionText(v)
			}
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = desc
		}
		return wire, nil
	}

	var count int
	choices, _ := q.Criteria.(map[string]interface{})
	levels, _ := q.Criteria.([]interface{})
	if q.Type == "choice" {
		count = len(choices)
	} else {
		count = len(levels)
	}
	if count < 2 || count > 255 {
		return providerutils.OrderedMap{}, providererrors.NewArgumentError("questions."+id+".criteria", "Google Decisions requires 2 to 255 options or score levels.", nil)
	}
	var criteria interface{}
	if q.Type == "score" {
		texts := make([]interface{}, len(levels))
		for i, level := range levels {
			if level == nil || level == "" {
				return providerutils.OrderedMap{}, providererrors.NewArgumentError("questions."+id+".criteria", "Google Decisions requires non-empty score level descriptions.", nil)
			}
			texts[i] = googleDecisionText(level)
		}
		criteria = texts
	} else {
		keys := provider.OrderedKeys(choices, criteriaOrder)
		vals := make(map[string]interface{}, len(keys))
		for _, k := range keys {
			if v := choices[k]; v != nil {
				vals[k] = googleDecisionText(v)
			} else {
				vals[k] = nil
			}
		}
		criteria = providerutils.OrderedMap{Keys: keys, Vals: vals}
	}
	return providerutils.OrderedMap{
		Keys: []string{"type", "instructions", "criteria"},
		Vals: map[string]interface{}{"type": q.Type, "instructions": instructions, "criteria": criteria},
	}, nil
}

// googleDecisionText is TS toText: a string as is, anything else
// JSON.stringify'd.
func googleDecisionText(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// googleDecisionInput converts the state parts (TS `state.map`): text and
// JSON become text inputs; files become image / audio / video / document
// inputs with inline base64 data or a URI.
func googleDecisionInput(parts []provider.DecisionStatePart) ([]interface{}, error) {
	out := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			out = append(out, providerutils.OrderedMap{Keys: []string{"type", "text"}, Vals: map[string]interface{}{"type": "text", "text": part.Text}})
		case "json":
			out = append(out, providerutils.OrderedMap{Keys: []string{"type", "text"}, Vals: map[string]interface{}{"type": "text", "text": googleDecisionJSONText(part.Value)}})
		case "file":
			file, err := googleDecisionFile(part.File)
			if err != nil {
				return nil, err
			}
			out = append(out, file)
		default:
			return nil, fmt.Errorf("unknown decision state part type %q", part.Type)
		}
	}
	return out, nil
}

// googleDecisionJSONText is JSON.stringify(part.value).
func googleDecisionJSONText(v interface{}) string {
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func googleDecisionFile(f *types.FileContent) (providerutils.OrderedMap, error) {
	if f == nil {
		return providerutils.OrderedMap{}, &providererrors.UnsupportedFunctionalityError{Functionality: "Google decision file input"}
	}
	mediaType := f.MediaType
	if mediaType == "" {
		mediaType = f.MimeType
	}
	if mediaType == "" {
		mediaType = f.FileData.MediaType
	}
	kind := "document"
	if mediaType != "application/pdf" {
		kind, _, _ = strings.Cut(mediaType, "/")
	}
	switch kind {
	case "image", "audio", "video", "document":
	default:
		return providerutils.OrderedMap{}, &providererrors.UnsupportedFunctionalityError{Functionality: "Google decision file media type: " + mediaType}
	}
	wire := providerutils.OrderedMap{Keys: []string{"type", "mime_type"}, Vals: map[string]interface{}{"type": kind, "mime_type": mediaType}}
	dataType := string(f.FileData.Type)
	if dataType == "" {
		switch {
		case len(f.Data) > 0:
			dataType = string(types.FileDataTypeData)
		case f.URL != "":
			dataType = string(types.FileDataTypeURL)
		case f.Text != "":
			dataType = string(types.FileDataTypeText)
		}
	}
	switch dataType {
	case string(types.FileDataTypeData):
		// TS: a base64 string is sent as is; bytes are base64-encoded.
		data := f.FileData.Data
		if len(data) == 0 {
			data = f.Data
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		if len(data) == 0 && f.FileData.DataString != "" {
			encoded = f.FileData.DataString
		}
		wire.Keys = append(wire.Keys, "data")
		wire.Vals["data"] = encoded
	case string(types.FileDataTypeURL):
		u := f.FileData.URL
		if u == "" {
			u = f.URL
		}
		wire.Keys = append(wire.Keys, "uri")
		wire.Vals["uri"] = u
	default:
		return providerutils.OrderedMap{}, &providererrors.UnsupportedFunctionalityError{Functionality: "Google decision file input: " + dataType}
	}
	return wire, nil
}

// googleDecisionAnswer maps a native answer to the core answer for its
// question (TS answers mapping); false means an invalid answer.
func googleDecisionAnswer(q provider.DecisionQuestion, a googleDecisionAnswerWire) (provider.DecisionAnswer, bool) {
	switch {
	case q.Type == "boolean" && a.Type == "noul" && a.Noul != nil:
		p := *a.Noul
		return provider.DecisionAnswer{Type: "boolean", Probability: &p}, true
	case q.Type == "choice" && a.Type == "choice" && a.Choice != nil:
		criteria, _ := q.Criteria.(map[string]interface{})
		if _, ok := criteria[*a.Choice]; ok {
			return provider.DecisionAnswer{Type: "choice", Choice: *a.Choice, Probabilities: a.Probabilities}, true
		}
	case q.Type == "score" && a.Type == "score" && a.Score != nil:
		levels, _ := q.Criteria.([]interface{})
		if s := *a.Score; s >= 0 && s <= float64(len(levels)-1) {
			return provider.DecisionAnswer{Type: "score", Score: &s, Probabilities: a.Probabilities}, true
		}
	}
	return provider.DecisionAnswer{}, false
}

// googleDecisionWarnings: the endpoint takes no generation settings or
// provider options, so they are reported instead of sent.
func googleDecisionWarnings(opts provider.DecisionCallOptions) []types.Warning {
	warnings := []types.Warning{}
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	if googleOpts, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["google"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(googleOpts))
		for k := range googleOpts {
			keys = append(keys, k)
		}
		sort.Strings(keys) // Go maps carry no insertion order
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.google." + k})
		}
	}
	return warnings
}

// googleDecisionDeclared returns body[field] with only the declared keys
// (zod z.object strips undeclared ones), or nil.
func googleDecisionDeclared(body interface{}, field string, keys ...string) interface{} {
	root, _ := body.(map[string]interface{})
	obj, ok := root[field].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, present := obj[k]; present {
			out[k] = v
		}
	}
	return out
}

func googleDecisionTokens(v *float64) *int {
	if v == nil {
		return nil
	}
	n := int(math.Trunc(*v))
	return &n
}

func (m *DecisionModel) handleError(err error) error {
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure is an APICallError as is
	}
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		// TS googleFailedResponseHandler.
		return gemini.GoogleHTTPError(m.Provider(), statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
	// LoadAPIKeyError, aborts and the "Cannot connect to API" error pass
	// through unchanged (TS postJsonToApi).
	return err
}

type googleDecisionAnswerWire struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
	Legend        map[string]string  `json:"legend"`
}

type googleDecisionResponse struct {
	Answers map[string]googleDecisionAnswerWire `json:"answers"`
	Usage   *struct {
		TotalInputTokens  *float64 `json:"total_input_tokens"`
		TotalOutputTokens *float64 `json:"total_output_tokens"`
	} `json:"usage"`
	PromptFeedback *struct {
		BlockReason *string `json:"block_reason"`
	} `json:"prompt_feedback"`
}

// zGoogleDecisionResponse is googleDecisionResponseSchema
// (google-decision-api.ts).
var zGoogleDecisionResponse = func() zodlite.Node {
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
		zodlite.F("answers", zodlite.Nullish(zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability), zodlite.F("confidence", nullishProbability)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability)),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability),
				zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.String)))),
		})))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("total_input_tokens", nullishNumber),
			zodlite.F("total_output_tokens", nullishNumber),
			zodlite.F("total_tokens", nullishNumber),
			zodlite.F("input_tokens_by_modality", zodlite.Nullish(zodlite.Array(zodlite.Object(
				zodlite.F("modality", zodlite.String), zodlite.F("tokens", zodlite.Number))))),
		))),
		zodlite.F("prompt_feedback", zodlite.Nullish(zodlite.Object(
			zodlite.F("block_reason", zodlite.Nullish(zodlite.String)),
			zodlite.F("block_reason_message", zodlite.Nullish(zodlite.String)),
		))),
	)
}()
