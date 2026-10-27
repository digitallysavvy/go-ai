package typesafeai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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

// SupportedQuestionTypes reports that TypeSafe AI accepts choice, score,
// boolean and box questions.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "score", "boolean", "box"}
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

	longContext, err := parseLongContext(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	var warnings []types.Warning
	if typesafeOpts, ok := opts.ProviderOptions["typesafe"].(map[string]interface{}); ok {
		optionKeys := make([]string, 0, len(typesafeOpts))
		for k := range typesafeOpts {
			optionKeys = append(optionKeys, k)
		}
		sort.Strings(optionKeys)
		for _, k := range optionKeys {
			if k == "longContext" {
				continue
			}
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("providerOptions.typesafe.%s", k),
			})
		}
	}

	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}

	requestQuestions := make(map[string]interface{}, len(opts.Questions))
	for id, q := range opts.Questions {
		wireType := q.Type
		if wireType == "boolean" {
			wireType = "noul"
		}
		entry := map[string]interface{}{"type": wireType}
		// Instructions are optional for criteria-only questions.
		if q.Instructions != nil {
			entry["instructions"] = q.Instructions
		}
		if q.Type == "box" {
			// A box question is sent as {type, instructions} only.
			requestQuestions[id] = entry
			continue
		}
		if q.Criteria != nil {
			entry["criteria"] = q.Criteria
		}
		requestQuestions[id] = entry
	}

	requestState, assets, images, err := typeSafeRequestState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}
	hasImages := len(assets) > 0

	body := map[string]interface{}{
		"model":     m.modelID,
		"state":     requestState,
		"questions": requestQuestions,
	}
	if hasImages {
		body["assets"] = assets
	}

	headers, err := m.provider.resolveHeaders()
	if err != nil {
		return nil, err
	}
	// Images and box answers (image regions) turn on TypeSafe's images mode
	// (TS TypeSafe-Danger-Zone-Images).
	imagesMode := hasImages
	for _, q := range opts.Questions {
		if q.Type == "box" {
			imagesMode = true
			break
		}
	}
	if imagesMode {
		headers = internalhttp.MergeHeaders(headers, map[string]string{"TypeSafe-Danger-Zone-Images": "true"})
	}
	if longContext {
		headers = internalhttp.MergeHeaders(headers, map[string]string{"TypeSafe-Danger-Zone-Long-Context": "true"})
	}
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	// Serialize like JSON.stringify (no HTML escaping) so the bytes match TS.
	requestJSON, err := jsonstr.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	var requestBody interface{} = requestJSON
	if hasImages {
		// Images are sent as a multipart upload (more efficient than base64):
		// a JSON "request" part followed by one part per image.
		multipartBody, contentType, mpErr := buildImageMultipart(body, images)
		if mpErr != nil {
			return nil, mpErr
		}
		requestBody = multipartBody
		headers = internalhttp.MergeHeaders(headers, map[string]string{"Content-Type": contentType})
	}

	var response typesafeResponseWire
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/systemone",
		Body:    requestBody,
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
	// TS `response.model ?? this.modelId`: only null/absent falls back.
	if response.Model != nil {
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
		case "box":
			box, ok := toBox(fields["box"])
			if !ok {
				return nil, nil, providererrors.NewInvalidResponseDataError(fields, fmt.Sprintf("TypeSafe AI returned a malformed box answer for %q.", id))
			}
			answers[id] = provider.DecisionAnswer{Type: "box", Box: box}
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

func toBox(v interface{}) (*provider.DecisionBox, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	top, ok1 := m["top"].(float64)
	bottom, ok2 := m["bottom"].(float64)
	left, ok3 := m["left"].(float64)
	right, ok4 := m["right"].(float64)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, false
	}
	return &provider.DecisionBox{Top: top, Bottom: bottom, Left: left, Right: right}, true
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

// typesafeAsset is one entry of the request's "assets" map.
type typesafeAsset struct {
	Type string `json:"type"`
	Part string `json:"part"`
}

// typesafeImagePart is one image uploaded alongside the JSON request.
type typesafeImagePart struct {
	name      string
	filename  string
	mediaType string
	data      []byte
}

// parseLongContext validates providerOptions.typesafe (TS
// typesafeAiDecisionModelOptions) and reports whether longContext is true.
func parseLongContext(providerOptions map[string]interface{}) (bool, error) {
	parsed, err := providerutils.ParseProviderOptions("typesafe", providerOptions, providerutils.OptionSchema{
		"longContext": providerutils.OptBool,
	})
	if err != nil {
		return false, err
	}
	enabled, _ := parsed["longContext"].(bool)
	return enabled, nil
}

// typesafeFilePartSchema is typesafeAiFilePartProviderOptions: a non-blank
// reference and an insertReference flag.
var typesafeFilePartSchema = providerutils.OptionSchema{
	"reference": func(path string, v interface{}) error {
		if err := providerutils.OptString(path, v); err != nil {
			return err
		}
		if s, _ := v.(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("invalid input (at %s)", path)
		}
		return nil
	},
	"insertReference": providerutils.OptBool,
}

const typesafeIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// typesafeGenerateID returns a random 16 character ID (TS generateId), used
// as the default image reference so it cannot match request text by accident.
func typesafeGenerateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate image reference: %w", err)
	}
	for i := range b {
		b[i] = typesafeIDAlphabet[int(b[i])%len(typesafeIDAlphabet)]
	}
	return string(b[:]), nil
}

// jsonStrings collects every string value (not object keys) in a JSON value.
func jsonStrings(value interface{}) []string {
	switch v := value.(type) {
	case nil, bool, float64, float32, int, int64, json.Number:
		return nil
	case string:
		return []string{v}
	case []interface{}:
		var out []string
		for _, item := range v {
			out = append(out, jsonStrings(item)...)
		}
		return out
	case map[string]interface{}:
		var out []string
		for _, item := range v {
			out = append(out, jsonStrings(item)...)
		}
		return out
	default:
		// Other Go types (structs, typed slices/maps): look at their JSON form.
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var generic interface{}
		if json.Unmarshal(encoded, &generic) != nil {
			return nil
		}
		switch generic.(type) {
		case string, []interface{}, map[string]interface{}:
			return jsonStrings(generic)
		}
		return nil
	}
}

// typeSafeRequestState maps ordered state parts to TypeSafe's native state, the
// request's image assets and the image parts to upload. Mirrors TS
// DecisionTypeSafeAiModel's requestState:
//   - Image file parts (PNG/JPEG inline data) get a reference (the
//     providerOptions.typesafe.reference of the part, else a random ID) that is
//     inserted into the state at the file's position unless the state already
//     contains it (or insertReference is false).
//   - A single remaining json part is sent as native JSON; otherwise text and
//     json parts are joined with newlines.
func typeSafeRequestState(parts []provider.DecisionStatePart) (interface{}, map[string]typesafeAsset, []typesafeImagePart, error) {
	var stateStrings []string
	for _, part := range parts {
		switch part.Type {
		case "text":
			stateStrings = append(stateStrings, part.Text)
		case "json":
			stateStrings = append(stateStrings, jsonStrings(part.Value)...)
		}
	}

	assets := map[string]typesafeAsset{}
	var images []typesafeImagePart
	stateParts := make([]provider.DecisionStatePart, 0, len(parts))
	for index, part := range parts {
		if part.Type != "file" {
			stateParts = append(stateParts, part)
			continue
		}
		file := part.File
		if file == nil {
			return nil, nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "TypeSafe AI decision file input"}
		}
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = file.MimeType
		}
		if mediaType == "" {
			mediaType = file.FileData.MediaType
		}
		if kind := file.FileData.Type; kind != "" && kind != types.FileDataTypeData {
			return nil, nil, nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("TypeSafe AI decision file input: %s (%s)", mediaType, kind),
			}
		}
		if mediaType != "image/png" && mediaType != "image/jpeg" {
			return nil, nil, nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("TypeSafe AI decision image media type: %s", mediaType),
			}
		}
		_, data, err := part.InlineData()
		if err != nil {
			return nil, nil, nil, &providererrors.InvalidArgumentError{Field: "state", Message: err.Error(), Cause: err}
		}
		fileOptions, err := providerutils.ParseProviderOptions("typesafe", file.ProviderOptions, typesafeFilePartSchema)
		if err != nil {
			return nil, nil, nil, err
		}
		explicitRef, hasRef := fileOptions["reference"].(string)
		insertOpt, hasInsert := fileOptions["insertReference"].(bool)
		if hasInsert && !insertOpt && !hasRef {
			return nil, nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.typesafe.reference",
				Message: "TypeSafe images with insertReference: false need an explicit reference.",
			}
		}
		// Default to random IDs to avoid accidental text matches, as TypeSafe
		// recommends as the safest option.
		reference := explicitRef
		if !hasRef {
			if reference, err = typesafeGenerateID(); err != nil {
				return nil, nil, nil, err
			}
		}
		insertReference := !containsString(stateStrings, reference)
		if hasInsert {
			insertReference = insertOpt
		}
		if _, dup := assets[reference]; dup {
			return nil, nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.typesafe.reference",
				Message: "Duplicate TypeSafe image reference: " + reference,
			}
		}
		partName := fmt.Sprintf("image-%d", index)
		assets[reference] = typesafeAsset{Type: "image", Part: partName}
		filename := file.Filename
		if filename == "" {
			ext := "jpeg"
			if mediaType == "image/png" {
				ext = "png"
			}
			filename = partName + "." + ext
		}
		images = append(images, typesafeImagePart{name: partName, filename: filename, mediaType: mediaType, data: data})
		if insertReference {
			stateParts = append(stateParts, provider.DecisionTextPart(reference))
		}
	}

	if len(stateParts) == 1 && stateParts[0].Type == "json" {
		return stateParts[0].Value, assets, images, nil
	}
	texts := make([]string, 0, len(stateParts))
	for _, part := range stateParts {
		if part.Type == "text" {
			texts = append(texts, part.Text)
			continue
		}
		// JSON.stringify keeps <, > and & literal; json.Marshal would escape them.
		encoded, err := jsonstr.Marshal(part.Value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to encode decision state: %w", err)
		}
		texts = append(texts, string(encoded))
	}
	return strings.Join(texts, "\n"), assets, images, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// buildImageMultipart builds the multipart/form-data body: the JSON request as
// the "request" part, then each image part in state order.
func buildImageMultipart(body map[string]interface{}, images []typesafeImagePart) (*bytes.Buffer, string, error) {
	requestJSON, err := jsonstr.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request body: %w", err)
	}
	// The body holds the request JSON plus one copy of each image; size the
	// buffer once so building it never reallocates past that bound.
	size := len(requestJSON) + 512*(len(images)+1)
	for _, image := range images {
		size += len(image.data)
	}
	var buf bytes.Buffer
	buf.Grow(size)
	w := multipart.NewWriter(&buf)
	write := func(name, filename, contentType string, data []byte) error {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": name, "filename": filename}))
		header.Set("Content-Type", contentType)
		part, err := w.CreatePart(header)
		if err != nil {
			return err
		}
		_, err = part.Write(data)
		return err
	}
	if err := write("request", "request.json", "application/json", requestJSON); err != nil {
		return nil, "", fmt.Errorf("failed to build multipart body: %w", err)
	}
	for _, image := range images {
		if err := write(image.name, image.filename, image.mediaType, image.data); err != nil {
			return nil, "", fmt.Errorf("failed to build multipart body: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to build multipart body: %w", err)
	}
	return &buf, w.FormDataContentType(), nil
}
