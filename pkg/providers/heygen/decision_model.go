package heygen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// decisionProviderName is the provider ID of HeyGen decision models.
const decisionProviderName = "heygen.decision"

// DecisionModel implements provider.DecisionModel for the HeyGen Decision API
// (POST {baseURL}/v3/models/decisions). Mirrors TS DecisionHeyGenModel
// (585caa76cd). Boolean questions are sent as the native "noul" type; image
// and video state parts become ordered assets.
type DecisionModel struct {
	prov    *Provider
	modelID string
}

// NewDecisionModel creates a HeyGen decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{prov: p, modelID: modelID}
}

// SpecificationVersion returns the decision model specification version.
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "heygen.decision", matching the TS provider ID.
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, score and boolean.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean"}
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// Typed-option ruling: read Go-typed option literals in their JSON form.
	opts.ProviderOptions = providerutils.NormalizeOptionsJSON(opts.ProviderOptions)
	// Typed Go containers in Instructions / Criteria take their JSON form.
	opts.Questions = provider.DecisionQuestionsJSON(opts.Questions)
	ids := opts.QuestionIDs()
	if len(ids) == 0 || len(ids) > decisionMaxQuestions {
		return nil, providererrors.NewArgumentError("questions", fmt.Sprintf("HeyGen Decision supports 1 to %d questions per request.", decisionMaxQuestions), nil)
	}

	nativeQuestions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		// TS: id.length === 0 || id.trim() !== id || /[:\r\n]/.test(id).
		if id == "" || providerutils.JSTrim(id) != id || strings.ContainsAny(id, ":\r\n") {
			return nil, providererrors.NewArgumentError("questions", "HeyGen Decision question names must be nonempty, contain no colon or newline, and have no leading or trailing whitespace.", nil)
		}
		q := opts.Questions[id]
		wire := map[string]interface{}{}
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
			if len(criteria) == 0 || len(criteria) > decisionMaxChoiceOptions {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id), fmt.Sprintf("HeyGen Choice questions support 1 to %d options.", decisionMaxChoiceOptions), nil)
			}
			wire["type"] = "choice"
			if order := opts.CriteriaOrder[id]; len(order) > 0 {
				wire["criteria"] = providerutils.OrderedMap{Keys: provider.OrderedKeys(criteria, order), Vals: criteria}
			} else {
				wire["criteria"] = q.Criteria
			}
		case "score":
			criteria, _ := q.Criteria.([]interface{})
			invalid := len(criteria) == 0 || len(criteria) > decisionMaxScoreLevels
			for _, level := range criteria {
				if level == nil {
					invalid = true
				}
			}
			if invalid {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id), fmt.Sprintf("HeyGen Score questions require 1 to %d non-null levels.", decisionMaxScoreLevels), nil)
			}
			wire["type"] = "score"
			wire["criteria"] = q.Criteria
		default:
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		// TS literal order: type, instructions, criteria.
		nativeQuestions.Vals[id] = providerutils.OrderedMap{Keys: []string{"type", "instructions", "criteria"}, Vals: wire}
	}

	state, assets, err := convertToHeyGenDecisionInput(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	headers, err := m.prov.resolveHeaders(ctx, opts.Headers)
	if err != nil {
		return nil, err
	}
	// TS body literal order: model, state, assets (when present), questions.
	body := providerutils.OrderedMap{Keys: []string{"model", "state"}, Vals: map[string]interface{}{"model": m.modelID, "state": state}}
	if assets != nil {
		body.Keys = append(body.Keys, "assets")
		body.Vals["assets"] = assets
	}
	body.Keys = append(body.Keys, "questions")
	body.Vals["questions"] = nativeQuestions

	var response heygenDecisionEnvelope
	httpResp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v3/models/decisions",
		Body:    body,
		Headers: headers,
		// TS createJsonResponseHandler(heyGenDecisionResponseSchema).
		ValidateResponse: validateHeyGenDecisionResponse,
	}, &response)
	if err != nil {
		var status *internalhttp.HTTPStatusError
		if errors.As(err, &status) {
			return nil, newHeyGenAPIErrorFrom(status)
		}
		return nil, err
	}
	data := response.Data

	var rawBody interface{}
	_ = jsonexact.Unmarshal(httpResp.Body, &rawBody)

	if len(data.Answers) != len(ids) {
		return nil, providererrors.NewInvalidResponseDataError(rawBody, "HeyGen Decision must return exactly one answer for every question.")
	}
	for _, id := range ids {
		if _, ok := data.Answers[id]; !ok {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "HeyGen Decision must return exactly one answer for every question.")
		}
	}

	answers := make(map[string]provider.DecisionAnswer, len(ids))
	confidence := map[string]float64{}
	legend := map[string]interface{}{}
	answerWarnings := map[string]string{}
	for id, raw := range data.Answers {
		var fields heygenDecisionAnswer
		if err := jsonexact.Unmarshal(raw, &fields); err != nil {
			return nil, providererrors.NewInvalidResponseDataError(rawBody, "HeyGen Decision returned a malformed answer.")
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
		if fields.Warning != nil {
			answerWarnings[id] = *fields.Warning
		}
	}

	var usage *provider.DecisionUsage
	if data.Usage != nil {
		usage = &provider.DecisionUsage{InputTokens: heygenNumberToInt(data.Usage.InputTokens), OutputTokens: heygenNumberToInt(data.Usage.OutputTokens)}
	}

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	if heygenOpts, ok := opts.ProviderOptions["heygen"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(heygenOpts))
		for k := range heygenOpts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.heygen." + k})
		}
	}

	metadata := map[string]interface{}{"confidence": confidence, "legend": legend, "answerWarnings": answerWarnings}
	if data.RequestID != nil {
		metadata["requestId"] = *data.RequestID
	}
	modelID := m.modelID
	if data.Model != nil { // TS `data.model ?? modelId`: an empty string is kept
		modelID = *data.Model
	}
	info := &provider.DecisionResponseInfo{
		ModelID: modelID,
		Headers: heygenLowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
		Body:    rawBody,
	}
	if data.ID != nil {
		info.ID = *data.ID
	}
	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"heygen": metadata},
		Response:         info,
	}, nil
}

// heygenDecisionAsset is one entry of the request's assets.images / videos.
type heygenDecisionAsset = providerutils.OrderedMap

// convertToHeyGenDecisionInput is TS convertToHeyGenDecisionInput: text and
// JSON parts make up the state, image and video files become ordered assets
// named image-1, image-2, video-1.
func convertToHeyGenDecisionInput(parts []provider.DecisionStatePart) (interface{}, interface{}, error) {
	var values []interface{}
	images := providerutils.OrderedMap{Vals: map[string]interface{}{}}
	videos := providerutils.OrderedMap{Vals: map[string]interface{}{}}
	for _, part := range parts {
		switch part.Type {
		case "text":
			values = append(values, part.Text)
		case "json":
			values = append(values, part.Value)
		case "file":
			file := part.File
			if file == nil {
				return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "HeyGen decision file media type: "}
			}
			mediaType := heygenFileMediaType(file)
			kind := fileutil.TopLevelMediaType(mediaType)
			if kind != "image" && kind != "video" {
				return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "HeyGen decision file media type: " + mediaType}
			}
			// Asset names are not shown to the model; it reads images, then
			// videos, then state, each in request order.
			group := &images
			if kind == "video" {
				group = &videos
			}
			asset, err := toHeyGenDecisionAsset(file, mediaType, kind)
			if err != nil {
				return nil, nil, err
			}
			name := fmt.Sprintf("%s-%d", kind, len(group.Keys)+1)
			group.Keys = append(group.Keys, name)
			group.Vals[name] = asset
		}
	}
	if len(images.Keys) > decisionMaxImages || len(videos.Keys) > decisionMaxVideos {
		return nil, nil, providererrors.NewArgumentError("state", fmt.Sprintf("HeyGen Decision supports at most %d images and %d video per request.", decisionMaxImages, decisionMaxVideos), nil)
	}

	// A single string, object, or array is sent as native state. Other values
	// stay in an array because the API rejects a root number, boolean, or null.
	var state interface{} = values
	if values == nil {
		state = []interface{}{}
	}
	if len(values) == 1 && isNativeDecisionState(values[0]) {
		state = values[0]
	}

	var assets interface{}
	if len(images.Keys) > 0 || len(videos.Keys) > 0 {
		out := providerutils.OrderedMap{Vals: map[string]interface{}{}}
		if len(images.Keys) > 0 {
			out.Keys = append(out.Keys, "images")
			out.Vals["images"] = images
		}
		if len(videos.Keys) > 0 {
			out.Keys = append(out.Keys, "videos")
			out.Vals["videos"] = videos
		}
		assets = out
	}
	return state, assets, nil
}

// isNativeDecisionState is `value !== null && (typeof value === 'string' ||
// typeof value === 'object')` for a JSON value.
func isNativeDecisionState(v interface{}) bool {
	switch v.(type) {
	case nil:
		return false
	case string:
		return true
	case bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return false
	}
	return true
}

func heygenFileMediaType(f *types.FileContent) string {
	mediaType := f.MediaType
	if mediaType == "" {
		mediaType = f.MimeType
	}
	if mediaType == "" {
		mediaType = f.FileData.MediaType
	}
	return mediaType
}

// toHeyGenDecisionAsset is TS toHeyGenAsset.
func toHeyGenDecisionAsset(f *types.FileContent, mediaType, kind string) (heygenDecisionAsset, error) {
	dataType := f.FileData.Type
	if dataType == "" {
		switch {
		case len(f.Data) > 0:
			dataType = types.FileDataTypeData
		case f.URL != "":
			dataType = types.FileDataTypeURL
		case f.Reference != "":
			dataType = types.FileDataTypeReference
		case f.Text != "":
			dataType = types.FileDataTypeText
		}
	}
	switch dataType {
	case types.FileDataTypeURL:
		raw := f.FileData.URL
		if raw == "" {
			raw = f.URL
		}
		href, err := urlnorm.Href(raw)
		if err != nil {
			href = raw
		}
		if !strings.HasPrefix(strings.ToLower(href), "https:") {
			return heygenDecisionAsset{}, providererrors.NewArgumentError("state", "HeyGen decision media URLs must use HTTPS.", nil)
		}
		return providerutils.OrderedMap{Keys: []string{"type", "url"}, Vals: map[string]interface{}{"type": "url", "url": href}}, nil
	case types.FileDataTypeReference:
		reference := f.FileData.Reference
		if reference == nil && f.Reference != "" {
			reference = types.ProviderReference{"": f.Reference}
		}
		assetID, err := providerutils.ResolveProviderReference(reference, "heygen")
		if err != nil {
			return heygenDecisionAsset{}, err
		}
		return providerutils.OrderedMap{Keys: []string{"type", "asset_id"}, Vals: map[string]interface{}{"type": "asset_id", "asset_id": assetID}}, nil
	case types.FileDataTypeData:
		data := f.FileData.Data
		if len(data) == 0 {
			data = f.Data
		}
		encoded := ""
		fromString := len(data) == 0 && f.FileData.DataString != ""
		if fromString {
			encoded = f.FileData.DataString
		} else {
			encoded = base64.StdEncoding.EncodeToString(data)
		}
		if !fileutil.IsFullMediaType(mediaType) {
			var detected string
			var ok bool
			if fromString {
				var err error
				detected, ok, err = fileutil.DetectMediaTypeSignatureBase64(encoded, kind)
				if err != nil {
					return heygenDecisionAsset{}, err
				}
			} else {
				detected, ok = fileutil.DetectMediaTypeSignature(data, kind)
			}
			if !ok {
				example := "video/mp4"
				if kind == "image" {
					example = "image/png"
				}
				return heygenDecisionAsset{}, providererrors.NewArgumentError("state", fmt.Sprintf("Could not detect the %s media type. Specify a full mediaType such as %s.", kind, example), nil)
			}
			mediaType = detected
		}
		return providerutils.OrderedMap{Keys: []string{"type", "media_type", "data"}, Vals: map[string]interface{}{"type": "base64", "media_type": mediaType, "data": encoded}}, nil
	default: // inline text (and anything that is not data, a URL or a reference)
		return heygenDecisionAsset{}, &providererrors.UnsupportedFunctionalityError{Functionality: "HeyGen decision inline text files"}
	}
}

// heygenDecisionEnvelope is the wire shape of a successful response.
type heygenDecisionEnvelope struct {
	Data struct {
		ID        *string                    `json:"id"`
		RequestID *string                    `json:"request_id"`
		Model     *string                    `json:"model"`
		Answers   map[string]json.RawMessage `json:"answers"`
		Usage     *struct {
			InputTokens  *heygenNumber `json:"input_tokens"`
			OutputTokens *heygenNumber `json:"output_tokens"`
		} `json:"usage"`
	} `json:"data"`
}

// heygenNumber is a JSON number as TS zod number() accepts it.
type heygenNumber float64

func (n *heygenNumber) UnmarshalJSON(b []byte) error {
	var f float64
	if err := jsonexact.Unmarshal(b, &f); err != nil {
		return err
	}
	*n = heygenNumber(f)
	return nil
}

func heygenNumberToInt(n *heygenNumber) *int {
	if n == nil {
		return nil
	}
	v := int(*n)
	return &v
}

type heygenDecisionAnswer struct {
	Type          string                 `json:"type"`
	Noul          *float64               `json:"noul"`
	Choice        *string                `json:"choice"`
	Score         *float64               `json:"score"`
	Probabilities map[string]float64     `json:"probabilities"`
	Confidence    *float64               `json:"confidence"`
	Legend        map[string]interface{} `json:"legend"`
	Warning       *string                `json:"warning"`
}

// heygenDecisionResponseNode is heyGenDecisionResponseSchema.
var heygenDecisionResponseNode = func() zodlite.Node {
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
	warning := zodlite.F("warning", zodlite.Nullish(zodlite.String))
	nullishNumber := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(zodlite.F("data", zodlite.Object(
		zodlite.F("id", zodlite.Nullish(zodlite.String)),
		zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", probability), warning),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability), warning),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishProbability),
				zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.JSON))), warning),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", nullishNumber), zodlite.F("output_tokens", nullishNumber)))),
	)))
}()

// validateHeyGenDecisionResponse is createJsonResponseHandler(
// heyGenDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateHeyGenDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(heygenDecisionResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}

// heygenLowerHeaders lowercases header names, as the TS SDK's response
// headers (fetch) are.
func heygenLowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
