package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ExtractionModel implements provider.ExtractionModel for the AI Gateway's
// extraction route (`POST {baseURL}/extraction-model`). Mirrors TypeScript's
// GatewayExtractionModel (f886264eb6).
type ExtractionModel struct {
	provider *Provider
	modelID  string
}

// NewExtractionModel creates a new Gateway extraction model.
func NewExtractionModel(p *Provider, modelID string) *ExtractionModel {
	return &ExtractionModel{provider: p, modelID: modelID}
}

// ExtractionModel implements provider.ExtractionModelProvider for Provider,
// resolving extraction models by ID through the Gateway (TS
// gateway.extractionModel).
func (p *Provider) ExtractionModel(modelID string) (provider.ExtractionModel, error) {
	return NewExtractionModel(p, modelID), nil
}

// Extraction is the short form of ExtractionModel (TS gateway.extraction).
func (p *Provider) Extraction(modelID string) (provider.ExtractionModel, error) {
	return p.ExtractionModel(modelID)
}

// SpecificationVersion returns the extraction model specification version.
func (m *ExtractionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *ExtractionModel) Provider() string { return "gateway" }

// ModelID returns the model ID.
func (m *ExtractionModel) ModelID() string { return m.modelID }

// SupportedFieldTypes lists every field type: support depends on the model
// behind the Gateway, and the Gateway rejects a field the model cannot
// extract.
func (m *ExtractionModel) SupportedFieldTypes() []string {
	return provider.ExtractionFieldTypes()
}

// DoExtract extracts every field from the same state through the Gateway.
func (m *ExtractionModel) DoExtract(ctx context.Context, opts provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
	stateParts, err := encodeGatewayState(provider.DecisionStateParts(opts.State), "extraction", true)
	if err != nil {
		return nil, err
	}

	body := providerutils.OrderedMap{
		Keys: []string{"stateParts", "fields", "referenceDate", "maxOutputTokens", "reasoning", "providerOptions"},
		Vals: map[string]interface{}{
			"stateParts": stateParts,
			"fields":     encodeGatewayExtractionFields(opts),
		},
	}
	if opts.ReferenceDate != "" {
		body.Vals["referenceDate"] = opts.ReferenceDate
	}
	if opts.MaxOutputTokens != nil {
		body.Vals["maxOutputTokens"] = *opts.MaxOutputTokens
	}
	if opts.Reasoning != nil {
		body.Vals["reasoning"] = string(*opts.Reasoning)
	}
	// TS `...(providerOptions ? {providerOptions} : {})`: a caller's empty
	// map is truthy and is sent as {}.
	if opts.ProviderOptions != nil {
		body.Vals["providerOptions"] = opts.ProviderOptions
	}

	headers := gatewayRequestHeaders(ctx, opts.Headers, m.getModelConfigHeaders())

	var response gatewayExtractionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.NodeResponseValidator("gateway", gatewayExtractionResponseNode),
		Method:           http.MethodPost,
		Path:             "/extraction-model",
		Body:             body,
		Headers:          headers,
	}, &response)
	if err != nil {
		if f := gatewayUnsupportedField(err); f != nil {
			return nil, &providererrors.ExtractionUnsupportedFieldTypeError{
				FieldID: f.fieldID, FieldType: f.fieldType, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		return nil, m.handleError(ctx, err)
	}

	// TS `responseBody.model ?? this.modelId`: a present "" is kept.
	modelID := m.modelID
	if response.Model != nil {
		modelID = *response.Model
	}

	result := &provider.ExtractionResult{
		Answers:          response.toAnswers(),
		Warnings:         response.toWarnings(),
		ProviderMetadata: copyGatewayProviderMetadata(response.ProviderMetadata),
		Response: &provider.ExtractionResponseInfo{
			ModelID: modelID,
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    rawJSONBody(httpResp.Body),
		},
	}
	if response.Usage != nil {
		result.Usage = &provider.ExtractionUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		}
	}
	return result, nil
}

func (m *ExtractionModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-extraction-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

func (m *ExtractionModel) handleError(ctx context.Context, err error) error {
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleErrorWithContext(ctx, err)
}

// encodeGatewayExtractionFields is the TS `fields` object as the caller wrote
// it: undefined members are omitted.
func encodeGatewayExtractionFields(opts provider.ExtractionCallOptions) providerutils.OrderedMap {
	out := providerutils.OrderedMap{
		Keys: opts.FieldIDs(),
		Vals: make(map[string]interface{}, len(opts.Fields)),
	}
	for id, f := range opts.Fields {
		wire := providerutils.OrderedMap{Keys: []string{"type", "instructions", "items", "unit", "integer", "decimals"}, Vals: map[string]interface{}{"type": f.Type}}
		if f.Instructions != nil {
			wire.Vals["instructions"] = f.Instructions
		}
		if f.Items != "" {
			wire.Vals["items"] = f.Items
		}
		if f.Unit != "" {
			wire.Vals["unit"] = f.Unit
		}
		if f.Integer != nil {
			wire.Vals["integer"] = *f.Integer
		}
		if f.Decimals != nil {
			wire.Vals["decimals"] = *f.Decimals
		}
		out.Vals[id] = wire
	}
	return out
}

// Response schema, mirroring gatewayExtractionResponseSchema.
var (
	gatewayExtractionSourceNode = zodlite.Disc(map[string]zodlite.Node{
		"text": zodlite.Typed("text", zodlite.F("part", zodlite.Number), zodlite.F("start", zodlite.Number), zodlite.F("end", zodlite.Number)),
		"file": zodlite.Typed("file", zodlite.F("part", zodlite.Number)),
	})
	gatewayExtractionNullishNum    = zodlite.Nullish(zodlite.Number)
	gatewayExtractionNullishSource = zodlite.Nullish(gatewayExtractionSourceNode)
)

func gatewayExtractionScalarNode(typ string, value zodlite.Node, withGuess bool) zodlite.Node {
	fields := []zodlite.Field{zodlite.F("value", zodlite.Nullable(value))}
	if withGuess {
		fields = append(fields, zodlite.F("guess", zodlite.Nullish(value)))
	}
	fields = append(fields, zodlite.F("pNull", gatewayExtractionNullishNum), zodlite.F("confidence", gatewayExtractionNullishNum))
	fields = append(fields, zodlite.F("source", gatewayExtractionNullishSource))
	return zodlite.Typed(typ, fields...)
}

var gatewayExtractionAnswerNode = zodlite.Disc(map[string]zodlite.Node{
	"text":     gatewayExtractionScalarNode("text", zodlite.String, true),
	"number":   gatewayExtractionScalarNode("number", zodlite.Number, true),
	"date":     gatewayExtractionScalarNode("date", zodlite.String, true),
	"time":     gatewayExtractionScalarNode("time", zodlite.String, true),
	"datetime": gatewayExtractionScalarNode("datetime", zodlite.String, true),
	"duration": gatewayExtractionScalarNode("duration", zodlite.Number, true),
	"list": zodlite.Typed("list",
		zodlite.F("value", zodlite.Nullable(zodlite.Union(zodlite.Array(zodlite.String), zodlite.Array(zodlite.Number)))),
		zodlite.F("pNull", gatewayExtractionNullishNum),
		zodlite.F("confidence", gatewayExtractionNullishNum),
		zodlite.F("items", zodlite.Nullish(zodlite.Array(zodlite.Object(
			zodlite.F("confidence", gatewayExtractionNullishNum),
			zodlite.F("source", gatewayExtractionNullishSource),
		))))),
	"refusal": zodlite.Typed("refusal"),
})

// gatewayExtractionWarningNode is gatewayWarningSchema; a warning it rejects
// is not a failure but is kept as an `other` warning.
var gatewayExtractionWarningNode = zodlite.Disc(map[string]zodlite.Node{
	"unsupported":   zodlite.Typed("unsupported", zodlite.F("feature", zodlite.String), zodlite.F("details", zodlite.Optional(zodlite.String))),
	"compatibility": zodlite.Typed("compatibility", zodlite.F("feature", zodlite.String), zodlite.F("details", zodlite.Optional(zodlite.String))),
	"deprecated":    zodlite.Typed("deprecated", zodlite.F("setting", zodlite.String), zodlite.F("message", zodlite.String)),
	"other":         zodlite.Typed("other", zodlite.F("message", zodlite.String)),
})

var gatewayExtractionResponseNode = zodlite.Object(
	zodlite.F("answers", zodlite.RecordOf(gatewayExtractionAnswerNode)),
	zodlite.F("model", zodlite.Nullish(zodlite.String)),
	zodlite.F("usage", zodlite.Nullish(zodlite.Object(
		zodlite.F("inputTokens", gatewayExtractionNullishNum),
		zodlite.F("outputTokens", gatewayExtractionNullishNum),
	))),
	zodlite.F("warnings", zodlite.Nullish(zodlite.Array(zodlite.Unknown))),
	zodlite.F("providerMetadata", zodlite.Nullish(zodlite.RecordOf(zodlite.RecordOf(zodlite.Unknown)))),
)

type gatewayExtractionResponse struct {
	// Answers are validated by gatewayExtractionAnswerNode before decoding.
	Answers map[string]map[string]interface{} `json:"answers"`
	Model   *string                           `json:"model"`
	Usage   *struct {
		InputTokens  *int `json:"inputTokens"`
		OutputTokens *int `json:"outputTokens"`
	} `json:"usage"`
	Warnings         []json.RawMessage                 `json:"warnings"`
	ProviderMetadata map[string]map[string]interface{} `json:"providerMetadata"`
}

func extractionFloat(v interface{}) *float64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	return &f
}

func extractionSource(v interface{}) *provider.ExtractionSource {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	typ, _ := m["type"].(string)
	part, _ := m["part"].(float64)
	src := &provider.ExtractionSource{Type: typ, Part: int(part)}
	if typ == provider.ExtractionSourceText {
		start, _ := m["start"].(float64)
		end, _ := m["end"].(float64)
		src.Start, src.End = int(start), int(end)
	}
	return src
}

// toAnswers builds the answers the way TS does: each zod object keeps only
// its own keys, and null optional members are dropped (omitNulls; `value`
// keeps its null).
func (r gatewayExtractionResponse) toAnswers() map[string]provider.ExtractionAnswer {
	out := make(map[string]provider.ExtractionAnswer, len(r.Answers))
	for id, a := range r.Answers {
		typ, _ := a["type"].(string)
		answer := provider.ExtractionAnswer{Type: typ}
		if typ != "refusal" {
			answer.Value = a["value"]
			answer.PNull = extractionFloat(a["pNull"])
			answer.Confidence = extractionFloat(a["confidence"])
		}
		switch typ {
		case "refusal":
		case "list":
			if list, ok := a["value"].([]interface{}); ok {
				answer.Value = list
			}
			if items, ok := a["items"].([]interface{}); ok {
				answer.Items = make([]provider.ExtractionListItem, 0, len(items))
				for _, raw := range items {
					item, _ := raw.(map[string]interface{})
					answer.Items = append(answer.Items, provider.ExtractionListItem{
						Confidence: extractionFloat(item["confidence"]),
						Source:     extractionSource(item["source"]),
					})
				}
			}
		default:
			answer.Guess = a["guess"]
			answer.Source = extractionSource(a["source"])
		}
		out[id] = answer
	}
	return out
}

// toWarnings keeps a warning this client does not know as an `other` warning
// (`Unknown warning: <JSON>`) instead of failing a successful response.
func (r gatewayExtractionResponse) toWarnings() []types.Warning {
	out := make([]types.Warning, 0, len(r.Warnings))
	for _, raw := range r.Warnings {
		var value interface{}
		if err := jsonexact.Unmarshal(raw, &value); err == nil && zodlite.Validate(gatewayExtractionWarningNode, raw, value) == nil {
			var w types.Warning
			if err := jsonexact.Unmarshal(raw, &w); err == nil {
				out = append(out, w)
				continue
			}
		}
		out = append(out, types.Warning{Type: "other", Message: "Unknown warning: " + zodlite.CompactJSON(string(raw))})
	}
	return out
}

type gatewayUnsupportedFieldInfo struct{ fieldID, fieldType string }

var gatewayUnsupportedFieldNode = zodlite.Object(zodlite.F("error", zodlite.Object(
	zodlite.F("type", zodlite.Literal("invalid_request_error")),
	zodlite.F("param", zodlite.Object(
		zodlite.F("name", zodlite.Literal("AI_ExtractionUnsupportedFieldTypeError")),
		zodlite.F("fieldId", zodlite.String),
		zodlite.F("fieldType", zodlite.String),
	)),
)))

// gatewayUnsupportedField reads the field from the Gateway's unsupported
// field type response (TS getUnsupportedField).
func gatewayUnsupportedField(err error) *gatewayUnsupportedFieldInfo {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}
	data := gatewayErrorData(statusErr.Body)
	if data == nil || zodlite.Validate(gatewayUnsupportedFieldNode, statusErr.Body, data) != nil {
		return nil
	}
	param := data.(map[string]interface{})["error"].(map[string]interface{})["param"].(map[string]interface{})
	return &gatewayUnsupportedFieldInfo{fieldID: param["fieldId"].(string), fieldType: param["fieldType"].(string)}
}
