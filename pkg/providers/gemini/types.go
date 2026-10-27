package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Candidate holds a single candidate from a Gemini API response.
type Candidate struct {
	Content struct {
		Parts []Part `json:"parts"`
		Role  string `json:"role"`
	} `json:"content"`
	FinishReason  string `json:"finishReason"`
	FinishMessage string `json:"finishMessage,omitempty"`
	// ContinuationToken is set on long-decode responses (Gemini 4): with
	// finishReason CONTINUATION it resumes the response, with MAX_TOKENS it
	// can be passed back to continue after raising maxOutputTokens.
	ContinuationToken  string          `json:"continuationToken,omitempty"`
	Index              int             `json:"index"`
	GroundingMetadata  json.RawMessage `json:"groundingMetadata,omitempty"`
	UrlContextMetadata json.RawMessage `json:"urlContextMetadata,omitempty"`
	SafetyRatings      json.RawMessage `json:"safetyRatings,omitempty"`
}

// Response represents the Gemini API response body.
type Response struct {
	// Error is an in-stream error frame (TS chunkSchema `error`): Google can
	// send one mid-stream after partial content was already sent.
	Error          *StreamError    `json:"error,omitempty"`
	Candidates     []Candidate     `json:"candidates"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
	PromptFeedback json.RawMessage `json:"promptFeedback,omitempty"`
	// ResponseID is the provider-assigned response ID (TS: response.responseId).
	ResponseID string `json:"responseId,omitempty"`
	// ServiceTier is kept for compatibility with older responses. Current
	// Gemini API responses report it inside usageMetadata.
	ServiceTier string `json:"serviceTier,omitempty"`
}

// StreamError is the error object of an in-stream error frame
// (TS getGoogleErrorSchema).
type StreamError struct {
	Code    *float64        `json:"code"`
	Message string          `json:"message"`
	Status  string          `json:"status"`
	Details json.RawMessage `json:"details,omitempty"`
}

// GenerateResponse decodes a doGenerate body or batch result line (TS
// responseSchema). That schema has no `error` key, so one of any shape is
// ignored rather than decoded into Response.Error.
type GenerateResponse struct {
	Response
	Error json.RawMessage `json:"error,omitempty"`
}

// tokenDetail is one entry of a *TokensDetails array (modality + token count).
type tokenDetail struct {
	Modality   string `json:"modality,omitempty"`
	TokenCount int    `json:"tokenCount,omitempty"`
}

// UsageMetadata represents token usage information returned by Gemini.
// Field set matches TS GoogleUsageMetadata (convert-google-usage.ts) so that
// json.Marshal(UsageMetadata) reproduces the same raw usage object.
type UsageMetadata struct {
	PromptTokenCount           int           `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount       int           `json:"candidatesTokenCount,omitempty"`
	ToolUsePromptTokenCount    int           `json:"toolUsePromptTokenCount,omitempty"`
	TotalTokenCount            int           `json:"totalTokenCount,omitempty"`
	CachedContentTokenCount    int           `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount         int           `json:"thoughtsTokenCount,omitempty"`
	TrafficType                string        `json:"trafficType,omitempty"`
	ServiceTier                string        `json:"serviceTier,omitempty"`
	PromptTokensDetails        []tokenDetail `json:"promptTokensDetails,omitempty"`
	CacheTokensDetails         []tokenDetail `json:"cacheTokensDetails,omitempty"`
	CandidatesTokensDetails    []tokenDetail `json:"candidatesTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []tokenDetail `json:"toolUsePromptTokensDetails,omitempty"`

	// raw is the usage object exactly as the API sent it. TS passes it
	// through as usage.raw and providerMetadata.usageMetadata, so fields the
	// SDK does not model survive.
	raw map[string]interface{}

	// summed is set when the counts combine several responses (long decode
	// continuation); the typed fields then win over the wire object.
	summed bool
}

type usageMetadataFields UsageMetadata

// UnmarshalJSON decodes the typed fields and keeps the wire object.
func (u *UsageMetadata) UnmarshalJSON(data []byte) error {
	var fields struct {
		usageMetadataFields
	}
	// TS counts are z.number(), so a fractional count is a valid frame; Go's
	// typed fields are ints, so decode from an integer-coerced copy.
	if err := json.Unmarshal(coerceUsageCounts(data), &fields.usageMetadataFields); err != nil {
		return err
	}
	*u = UsageMetadata(fields.usageMetadataFields)
	var raw map[string]interface{}
	if json.Unmarshal(data, &raw) == nil {
		u.raw = raw
	}
	return nil
}

// MarshalJSON reproduces the wire object when one was received, otherwise the
// typed fields.
func (u UsageMetadata) MarshalJSON() ([]byte, error) {
	if u.raw != nil && !u.summed {
		// Exactly what the API sent, including keys nested inside the
		// token-detail entries that the typed fields do not model.
		return json.Marshal(u.raw)
	}
	typed, err := json.Marshal(usageMetadataFields(u))
	if err != nil || u.raw == nil {
		return typed, err
	}
	merged := make(map[string]interface{}, len(u.raw))
	for k, v := range u.raw {
		merged[k] = v
	}
	// Typed fields win (they are the summed values after a continuation).
	var overlay map[string]interface{}
	if json.Unmarshal(typed, &overlay) == nil {
		for k, v := range overlay {
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

// initialPromptTokens is TS `usage?.promptTokenCount` for the first request of
// a continued response: nil when there is no usage object or it carries no
// numeric promptTokenCount (a missing count is not a zero count).
func initialPromptTokens(u *UsageMetadata) *int {
	if u == nil {
		return nil
	}
	if u.raw != nil {
		if _, ok := u.raw["promptTokenCount"].(float64); !ok {
			return nil
		}
	}
	v := u.PromptTokenCount
	return &v
}

// promptFeedbackBlockReason extracts the blockReason field from a raw
// promptFeedback payload, if present.
func promptFeedbackBlockReason(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var pf struct {
		BlockReason string `json:"blockReason"`
	}
	if err := json.Unmarshal(raw, &pf); err != nil {
		return ""
	}
	return pf.BlockReason
}

// isConfirmedPromptBlockReason reports whether blockReason represents an
// actual prompt block (TS isConfirmedPromptBlockReason): present, non-empty,
// and not one of the "unspecified" sentinel values.
func isConfirmedPromptBlockReason(blockReason string) bool {
	return blockReason != "" &&
		blockReason != "BLOCK_REASON_UNSPECIFIED" &&
		blockReason != "BLOCKED_REASON_UNSPECIFIED"
}

// FunctionCall represents a Gemini `functionCall` part. It carries both the
// traditional complete form (name + args) and the streaming partial-args
// form (TS `google-json-accumulator.ts` / google-language-model.ts): a
// function call may arrive as one complete chunk, or be spread across
// multiple chunks whose `partialArgs` leaves accumulate into the final
// arguments object, or as a terminal `{}` chunk that only signals "the
// active streaming call is done".
//
// Presence, not just nil-ness, matters for classifying a chunk (TS checks
// `!= null` throughout), so Args/Name/WillContinue use explicit *Set flags
// or pointers rather than relying on Go's zero values.
type FunctionCall struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`

	// Args holds the parsed arguments object (order not significant — use
	// ArgsRaw to reproduce the original wire key order). ArgsSet is true iff
	// the wire JSON had a non-null "args" field.
	Args    map[string]interface{}
	ArgsRaw json.RawMessage
	ArgsSet bool

	// ArgsIsString/ArgsString handle the rare wire shape where "args" is a
	// JSON string rather than an object (TS: `typeof part.functionCall.args
	// === 'string'`), which google-language-model.ts uses verbatim as the
	// call's `input` text instead of re-stringifying it.
	ArgsIsString bool
	ArgsString   string

	// PartialArgs holds the streamed leaf values for this chunk.
	// PartialArgsSet is true iff the wire JSON had a non-null "partialArgs"
	// field (as opposed to PartialArgs being nil because the field was
	// simply absent).
	PartialArgs    []PartialArg
	PartialArgsSet bool

	// WillContinue is nil when the field is absent/null, matching TS
	// `willContinue == null` / `willContinue === true` checks.
	WillContinue *bool
}

// UnmarshalJSON decodes a functionCall part, tracking field presence
// (Args/PartialArgs "!= null") separately from Go's zero values.
func (f *FunctionCall) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		Args         json.RawMessage `json:"args"`
		PartialArgs  json.RawMessage `json:"partialArgs"`
		WillContinue *bool           `json:"willContinue"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.ID = raw.ID
	f.Name = raw.Name
	f.WillContinue = raw.WillContinue

	if len(raw.Args) > 0 && string(raw.Args) != "null" {
		f.ArgsSet = true
		f.ArgsRaw = raw.Args
		if err := json.Unmarshal(raw.Args, &f.Args); err != nil {
			// "args" is `z.unknown()` in the TS schema: a string value is a
			// valid (if rare) wire shape, used as-is rather than re-encoded
			// (google-language-model.ts:1180). Any other non-object shape
			// (number/bool/array) falls back to an empty object, mirroring
			// the effect of `JSON.stringify(part.functionCall.args ?? {})`
			// only for the object/null cases — such payloads aren't valid
			// tool arguments either way.
			var s string
			if jsonErr := json.Unmarshal(raw.Args, &s); jsonErr == nil {
				f.ArgsIsString = true
				f.ArgsString = s
				f.Args = nil
			} else {
				f.Args = map[string]interface{}{}
			}
		}
	}
	if len(raw.PartialArgs) > 0 && string(raw.PartialArgs) != "null" {
		f.PartialArgsSet = true
		if err := json.Unmarshal(raw.PartialArgs, &f.PartialArgs); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalJSON decodes the part and keeps the wire form of toolCall.args.
func (p *Part) UnmarshalJSON(b []byte) error {
	type plain Part
	var v plain
	if err := json.Unmarshal(lenientPart(b), &v); err != nil {
		return err
	}
	*p = Part(v)
	var aux struct {
		Text     json.RawMessage `json:"text"`
		ToolCall *struct {
			Args json.RawMessage `json:"args"`
		} `json:"toolCall"`
	}
	auxErr := json.Unmarshal(b, &aux)
	// TS checks `'text' in part && part.text != null`: a part decoded without
	// a text value takes no part in the empty-text/thoughtSignature handling.
	p.textAbsent = auxErr == nil && (len(aux.Text) == 0 || string(aux.Text) == "null")
	if auxErr == nil && aux.ToolCall != nil && len(aux.ToolCall.Args) > 0 && string(aux.ToolCall.Args) != "null" {
		p.toolCallArgsRaw = string(aux.ToolCall.Args)
	}
	return nil
}

// Part represents a single part in a Gemini content block.
// ExecutableCode and CodeExecutionResult are populated only by Google Generative AI,
// not by Vertex AI. Both fields are safe to include in the shared type — JSON
// unmarshaling simply leaves them nil when absent.
type Part struct {
	Text             string        `json:"text,omitempty"`
	Thought          bool          `json:"thought,omitempty"`
	ThoughtSignature string        `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall `json:"functionCall,omitempty"`
	InlineData       *struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64-encoded
	} `json:"inlineData,omitempty"`
	// ExecutableCode is set when the model requests code execution (Google only).
	ExecutableCode *struct {
		Language string `json:"language"`
		Code     string `json:"code"`
	} `json:"executableCode,omitempty"`
	// CodeExecutionResult is set when the model returns a code execution result (Google only).
	CodeExecutionResult *struct {
		Outcome string `json:"outcome"`
		Output  string `json:"output"`
	} `json:"codeExecutionResult,omitempty"`
	// ToolCall/ToolResponse carry a server-executed built-in tool invocation
	// and its result (distinct from FunctionCall, which is user-invoked).
	// TS google-language-model.ts: `'toolCall' in part` / `'toolResponse' in
	// part`, surfaced as a `server:${toolType}` tool call/result with
	// providerExecuted+dynamic true and serverToolCallId/serverToolType in
	// provider metadata.
	ToolCall *struct {
		ToolType string                 `json:"toolType"`
		Args     map[string]interface{} `json:"args,omitempty"`
		ID       string                 `json:"id"`
	} `json:"toolCall,omitempty"`
	// toolCallArgsRaw is the wire text of ToolCall.Args, which keeps the key
	// order JSON.stringify(part.toolCall.args) has in TS. A string (not
	// json.RawMessage) keeps Part comparable with ==, as in v0.5.2.
	toolCallArgsRaw string
	// textAbsent is set when the part was decoded from JSON that had no
	// (or a null) text value. A hand-built Part keeps the zero value.
	textAbsent   bool
	ToolResponse *struct {
		ToolType string                 `json:"toolType"`
		Response map[string]interface{} `json:"response,omitempty"`
		ID       string                 `json:"id"`
	} `json:"toolResponse,omitempty"`
}

// decodeInlineData base64-decodes an inlineData payload.
// Errors are intentionally ignored: if the data is malformed the caller receives
// nil bytes, which propagates as an empty content block rather than a hard failure.
// The Gemini API guarantees valid base64 for inlineData fields.
func decodeInlineData(encoded string) []byte {
	data, _ := base64.StdEncoding.DecodeString(encoded)
	return data
}

// convertUsage converts a Gemini UsageMetadata payload to the SDK Usage struct.
// Mirrors TS convertGoogleUsage (convert-google-usage.ts): input tokens are
// promptTokenCount + toolUsePromptTokenCount, noCache/cacheRead and the
// output text/reasoning split are always populated (not gated on being
// non-zero), and Raw carries the full usage object.
func convertUsage(usage *UsageMetadata) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	promptTokens := int64(usage.PromptTokenCount)
	candidatesTokens := int64(usage.CandidatesTokenCount)
	toolUsePromptTokens := int64(usage.ToolUsePromptTokenCount)
	cachedContentTokens := int64(usage.CachedContentTokenCount)
	thoughtsTokens := int64(usage.ThoughtsTokenCount)

	inputTokens := promptTokens + toolUsePromptTokens
	noCacheTokens := inputTokens - cachedContentTokens
	outputTokens := candidatesTokens + thoughtsTokens
	totalTokens := inputTokens + outputTokens

	// Input token details: cache breakdown (always) plus text/image split
	// (Go-only addition; TS convertGoogleUsage has no modality split).
	var textTokens *int64
	var imageTokens *int64
	for _, detail := range usage.PromptTokensDetails {
		switch detail.Modality {
		case "TEXT":
			v := int64(detail.TokenCount)
			textTokens = &v
		case "IMAGE":
			v := int64(detail.TokenCount)
			imageTokens = &v
		}
	}

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedContentTokens,
			TextTokens:      textTokens,
			ImageTokens:     imageTokens,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &candidatesTokens,
			ReasoningTokens: &thoughtsTokens,
		},
	}

	if raw, err := json.Marshal(usage); err == nil {
		var rawMap map[string]interface{}
		if json.Unmarshal(raw, &rawMap) == nil {
			result.Raw = rawMap
		}
	}

	return result
}

// addUsageMetadata sums the usage metadata of two requests, e.g. the hops of
// a long decode response that was continued automatically. Token counts and
// per-modality details are added; non-numeric fields are taken from b when
// set. Mirrors TS addGoogleUsageMetadata.
func addUsageMetadata(a, b *UsageMetadata) *UsageMetadata {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := *b
	out.summed = true
	out.PromptTokenCount = a.PromptTokenCount + b.PromptTokenCount
	out.CandidatesTokenCount = a.CandidatesTokenCount + b.CandidatesTokenCount
	out.ToolUsePromptTokenCount = a.ToolUsePromptTokenCount + b.ToolUsePromptTokenCount
	out.TotalTokenCount = a.TotalTokenCount + b.TotalTokenCount
	out.CachedContentTokenCount = a.CachedContentTokenCount + b.CachedContentTokenCount
	out.ThoughtsTokenCount = a.ThoughtsTokenCount + b.ThoughtsTokenCount
	if out.TrafficType == "" {
		out.TrafficType = a.TrafficType
	}
	if out.ServiceTier == "" {
		out.ServiceTier = a.ServiceTier
	}
	if a.raw != nil || b.raw != nil {
		merged := map[string]interface{}{}
		for _, src := range []map[string]interface{}{a.raw, b.raw} {
			for k, v := range src {
				merged[k] = v
			}
		}
		out.raw = merged
	}
	out.PromptTokensDetails = addTokenDetails(a.PromptTokensDetails, b.PromptTokensDetails)
	out.CacheTokensDetails = addTokenDetails(a.CacheTokensDetails, b.CacheTokensDetails)
	out.CandidatesTokensDetails = addTokenDetails(a.CandidatesTokensDetails, b.CandidatesTokensDetails)
	out.ToolUsePromptTokensDetails = addTokenDetails(a.ToolUsePromptTokensDetails, b.ToolUsePromptTokensDetails)
	return &out
}

func addTokenDetails(a, b []tokenDetail) []tokenDetail {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	var order []string
	counts := map[string]int{}
	for _, d := range append(append([]tokenDetail{}, a...), b...) {
		if _, ok := counts[d.Modality]; !ok {
			order = append(order, d.Modality)
		}
		counts[d.Modality] += d.TokenCount
	}
	out := make([]tokenDetail, 0, len(order))
	for _, m := range order {
		out = append(out, tokenDetail{Modality: m, TokenCount: counts[m]})
	}
	return out
}

// lenientPart drops the values whose TS schema is permissive (z.unknown() or
// a fall-through union member) but whose Go field type is narrower, so that a
// frame TS accepts is not rejected by the typed decode: toolCall.args and
// toolResponse.response that are not objects (toolCall args keep their wire
// text in toolCallArgsRaw), and a functionCall that is not an object.
func lenientPart(b []byte) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	changed := false
	if raw, ok := m["functionCall"]; ok && !isJSONObject(raw) && string(raw) != "null" {
		delete(m, "functionCall")
		changed = true
	}
	for key, inner := range map[string]string{"toolCall": "args", "toolResponse": "response"} {
		raw, ok := m[key]
		if !ok || !isJSONObject(raw) {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if v, ok := obj[inner]; ok && !isJSONObject(v) {
			delete(obj, inner)
			if nb, err := json.Marshal(obj); err == nil {
				m[key] = nb
				changed = true
			}
		}
	}
	if !changed {
		return b
	}
	out, err := json.Marshal(m)
	if err != nil {
		return b
	}
	return out
}

func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

// coerceUsageCounts rewrites fractional token counts in a usageMetadata
// object to integers (see UsageMetadata.UnmarshalJSON).
func coerceUsageCounts(data []byte) []byte {
	var m map[string]interface{}
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	toInt := func(v interface{}) interface{} {
		if f, ok := v.(float64); ok {
			return int64(f)
		}
		return v
	}
	for k, v := range m {
		switch k {
		case "promptTokensDetails", "cacheTokensDetails", "candidatesTokensDetails", "toolUsePromptTokensDetails":
			if arr, ok := v.([]interface{}); ok {
				for _, e := range arr {
					if em, ok := e.(map[string]interface{}); ok {
						em["tokenCount"] = toInt(em["tokenCount"])
					}
				}
			}
		default:
			if strings.HasSuffix(k, "TokenCount") {
				m[k] = toInt(v)
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}
