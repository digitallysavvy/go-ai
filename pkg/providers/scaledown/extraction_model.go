package scaledown

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// extractionProviderName is the provider ID of ScaleDown extraction models.
const extractionProviderName = "scaledown.extraction"

// ExtractionModel implements provider.ExtractionModel for ScaleDown's
// experimental extraction API (the chat-completions endpoint with a JSON
// schema response format). Mirrors TS ScaleDownExtractionModel: only "text"
// and "list" of text fields are supported, scalar fields keep the best match
// and list fields keep every match with per-item confidence and sources.
type ExtractionModel struct {
	provider *Provider
	modelID  string
}

var _ provider.ExtractionModel = (*ExtractionModel)(nil)

// NewExtractionModel creates a ScaleDown extraction model.
func NewExtractionModel(p *Provider, modelID string) *ExtractionModel {
	return &ExtractionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *ExtractionModel) SpecificationVersion() string { return "v4" }

// Provider returns "scaledown.extraction".
func (m *ExtractionModel) Provider() string { return extractionProviderName }

// ModelID returns the model ID.
func (m *ExtractionModel) ModelID() string { return m.modelID }

// SupportedFieldTypes reports text and list.
func (m *ExtractionModel) SupportedFieldTypes() []string {
	return []string{provider.ExtractionFieldText, provider.ExtractionFieldList}
}

// jsTrimmedEmpty is `s.trim() === ”` with JavaScript's whitespace set.
func jsTrimmedEmpty(s string) bool {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
			return true
		}
		return r >= 0x2000 && r <= 0x200A
	}) == ""
}

// DoExtract extracts every field from the same state in one request.
func (m *ExtractionModel) DoExtract(ctx context.Context, opts provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
	ids := opts.FieldIDs()
	properties := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		field := opts.Fields[id]
		if field.Type != provider.ExtractionFieldText && !(field.Type == provider.ExtractionFieldList && field.Items == provider.ExtractionFieldText) {
			fieldType := field.Type
			if field.Type == provider.ExtractionFieldList {
				fieldType = "list<" + field.Items + ">"
			}
			return nil, &providererrors.ExtractionUnsupportedFieldTypeError{
				FieldID: id, FieldType: fieldType, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		prop := providerutils.OrderedMap{Keys: []string{"type"}, Vals: map[string]interface{}{"type": "string"}}
		if field.Instructions != nil {
			text, err := toText(field.Instructions)
			if err != nil {
				return nil, err
			}
			prop.Keys = append(prop.Keys, "description")
			prop.Vals["description"] = text
		}
		// Scalar schemas retain entity confidence and offsets for every match.
		topN := 0
		if field.Type == provider.ExtractionFieldText {
			topN = 1
		}
		prop.Keys = append(prop.Keys, "top_n")
		prop.Vals["top_n"] = topN
		properties.Vals[id] = prop
	}

	parsed, err := providerutils.ParseProviderOptions("scaledown", providerutils.NormalizeOptionsJSON(opts.ProviderOptions), extractionOptionsSchema)
	if err != nil {
		return nil, err
	}
	var threshold interface{}
	if v, ok := parsed["threshold"]; ok {
		threshold = v
	}

	// TS converts the state before it evaluates the request headers
	// (loadApiKey), so an unsupported file wins over a missing key.
	stateParts := provider.DecisionStateParts(opts.State)
	state, err := convertToScaleDownState(stateParts)
	if err != nil {
		return nil, err
	}
	inputText, _ := state.Vals["text"].(string)
	// ScaleDown requires a non-empty user message even with a document.
	if _, hasDocument := state.Vals["document"]; hasDocument && jsTrimmedEmpty(inputText) {
		inputText = "Extract the requested fields from the document."
	}
	headers, err := m.provider.resolveHeaders()
	if err != nil {
		return nil, err
	}
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	body := providerutils.OrderedMap{Keys: []string{"model", "messages", "response_format"}, Vals: map[string]interface{}{
		"model":    m.modelID,
		"messages": []interface{}{providerutils.OrderedMap{Keys: []string{"role", "content"}, Vals: map[string]interface{}{"role": "user", "content": inputText}}},
		"response_format": providerutils.OrderedMap{Keys: []string{"type", "json_schema"}, Vals: map[string]interface{}{
			"type": "json_schema",
			"json_schema": providerutils.OrderedMap{Keys: []string{"name", "schema"}, Vals: map[string]interface{}{
				"name": "extraction",
				"schema": providerutils.OrderedMap{Keys: []string{"type", "properties"}, Vals: map[string]interface{}{
					"type": "object", "properties": properties,
				}},
			}},
		}},
	}}
	if threshold != nil {
		body.Keys = append(body.Keys, "threshold")
		body.Vals["threshold"] = threshold
	}
	body.Keys = append(body.Keys, "context_chars")
	body.Vals["context_chars"] = 0
	for _, key := range state.Keys {
		if key == "text" {
			continue
		}
		body.Keys = append(body.Keys, key)
		body.Vals[key] = state.Vals[key]
	}

	var response scaledownExtractionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/v1/chat/completions",
		Body:             body,
		Headers:          headers,
		ValidateResponse: validateScaleDownExtractionResponse,
	}, &response)
	if err != nil {
		return nil, scaledownHTTPError(err, extractionProviderName)
	}
	var rawBody interface{}
	_ = jsonexact.Unmarshal(httpResp.Body, &rawBody)

	rawContent := response.Choices[0].Message.Content
	content, ok := parseExtractionContent(rawContent)
	if !ok {
		return nil, providererrors.NewInvalidResponseDataError(rawContent, "ScaleDown did not return valid extraction content.")
	}

	answers := make(map[string]provider.ExtractionAnswer, len(ids))
	for _, id := range ids {
		field := opts.Fields[id]
		var matches []scaledownExtractionEntity
		for _, entity := range content.Entities {
			if entity.Type == id && entity.Text != "" {
				matches = append(matches, entity)
			}
		}
		if field.Type == provider.ExtractionFieldList {
			values := make([]interface{}, len(matches))
			items := make([]provider.ExtractionListItem, len(matches))
			for i, entity := range matches {
				confidence := entity.Confidence
				values[i] = entity.Text
				items[i] = provider.ExtractionListItem{
					Confidence: &confidence,
					Source:     convertExtractionSource(entity, stateParts, content.OcrText),
				}
			}
			answers[id] = provider.ExtractionAnswer{Type: provider.ExtractionFieldList, Value: values, Items: items}
			continue
		}
		var best *scaledownExtractionEntity
		for i := range matches {
			if best == nil || matches[i].Confidence > best.Confidence {
				best = &matches[i]
			}
		}
		answer := provider.ExtractionAnswer{Type: provider.ExtractionFieldText}
		if best != nil {
			confidence := best.Confidence
			answer.Value = best.Text
			answer.Confidence = &confidence
			answer.Source = convertExtractionSource(*best, stateParts, content.OcrText)
		}
		answers[id] = answer
	}

	usage := &provider.ExtractionUsage{}
	switch {
	case response.Usage != nil && response.Usage.PromptTokens != nil:
		usage.InputTokens = scaledownTokenCount(response.Usage.PromptTokens)
	default:
		usage.InputTokens = scaledownTokenCount(content.InputTokens)
	}
	if response.Usage != nil {
		usage.OutputTokens = scaledownTokenCount(response.Usage.CompletionTokens)
	}

	var providerMetadata map[string]interface{}
	if content.OcrText != nil {
		providerMetadata = map[string]interface{}{"scaledown": map[string]interface{}{"ocrText": *content.OcrText}}
	}
	var warnings []types.Warning
	if opts.ReferenceDate != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "referenceDate"})
	}
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}

	info := &provider.ExtractionResponseInfo{
		ModelID: m.modelID,
		Headers: lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
		Body:    rawBody,
	}
	if response.ID != nil {
		info.ID = *response.ID
	}
	if response.Created != nil {
		// new Date(created * 1000): milliseconds truncated; out of range is
		// an invalid date, which Go reports as the zero time.
		if ms := math.Trunc(*response.Created * 1000); math.Abs(ms) <= 8.64e15 {
			info.Timestamp = time.UnixMilli(int64(ms))
		}
	}
	// TS `value.model ?? this.modelId`: only null or absent falls back.
	if response.Model != nil {
		info.ModelID = *response.Model
	}
	return &provider.ExtractionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: providerMetadata,
		Response:         info,
	}, nil
}
