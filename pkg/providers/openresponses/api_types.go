package openresponses

import (
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// OpenResponsesRequestBody represents the request body for the Open Responses API
type OpenResponsesRequestBody struct {
	// Model is the model ID to use (e.g., "llama-2-7b", "mistral-7b")
	Model string `json:"model"`

	// Input is the context for the model - either a string or array of message items
	Input interface{} `json:"input"`

	// Instructions are additional instructions to guide the model
	Instructions string `json:"instructions,omitempty"`

	// MaxOutputTokens is the maximum number of tokens to generate
	MaxOutputTokens *int `json:"max_output_tokens,omitempty"`

	// Temperature controls randomness (0.0 to 2.0)
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP controls nucleus sampling
	TopP *float64 `json:"top_p,omitempty"`

	// PresencePenalty penalizes tokens based on presence
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`

	// FrequencyPenalty penalizes tokens based on frequency
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`

	// Tools are the available function tools
	Tools []FunctionTool `json:"tools,omitempty"`

	// ToolChoice controls which tool to use
	ToolChoice interface{} `json:"tool_choice,omitempty"`

	// Text contains text output configuration
	Text *TextConfig `json:"text,omitempty"`

	// Stream enables streaming responses
	Stream bool `json:"stream,omitempty"`
}

// TextConfig contains text output configuration
type TextConfig struct {
	Format interface{} `json:"format,omitempty"`
}

// FunctionTool represents a function tool definition
type FunctionTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Strict      *bool                  `json:"strict,omitempty"`

	// rawParameters is the schema as raw JSON when the caller supplied one;
	// it is written in its own key order (a JS object keeps insertion order).
	rawParameters json.RawMessage
}

// MarshalJSON writes the tool with raw JSON parameters in author key order.
func (t FunctionTool) MarshalJSON() ([]byte, error) {
	w := struct {
		Type        string      `json:"type"`
		Name        string      `json:"name"`
		Description string      `json:"description,omitempty"`
		Parameters  interface{} `json:"parameters,omitempty"`
		Strict      *bool       `json:"strict,omitempty"`
	}{Type: t.Type, Name: t.Name, Description: t.Description, Strict: t.Strict}
	switch {
	case len(t.rawParameters) > 0:
		w.Parameters = t.rawParameters
	case len(t.Parameters) > 0:
		w.Parameters = t.Parameters
	}
	return json.Marshal(w)
}

// CustomToolItem represents a caller-executed "custom" tool definition sent
// in the request `tools` array, matched via Config.CustomToolID. Mirrors the
// TS Open Responses `CustomTool` wire shape (type/name/description/format).
type CustomToolItem struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Format      map[string]interface{} `json:"format,omitempty"`
}

// Message item types for input

// ReasoningInputItem is a top-level reasoning item sent in the request input.
// Used to forward reasoning blocks from previous response turns back to the API
// so multi-turn reasoning works when store=false or itemId is unavailable (#12869).
type ReasoningInputItem struct {
	Type             string              `json:"type"`
	ID               string              `json:"id,omitempty"`
	EncryptedContent string              `json:"encrypted_content,omitempty"`
	Summary          []SummaryPart       `json:"summary,omitempty"`
	Content          []ReasoningTextPart `json:"content,omitempty"`
}

// MarshalJSON mirrors the TS reasoning item literal: summary is always
// written (an empty array when there is none), content is written whenever it
// is set, even as an empty array.
func (r ReasoningInputItem) MarshalJSON() ([]byte, error) {
	summary := r.Summary
	if summary == nil {
		summary = []SummaryPart{}
	}
	out := struct {
		Type             string               `json:"type"`
		Summary          []SummaryPart        `json:"summary"`
		ID               string               `json:"id,omitempty"`
		Content          *[]ReasoningTextPart `json:"content,omitempty"`
		EncryptedContent string               `json:"encrypted_content,omitempty"`
	}{Type: r.Type, Summary: summary, ID: r.ID, EncryptedContent: r.EncryptedContent}
	if r.Content != nil {
		out.Content = &r.Content
	}
	return json.Marshal(out)
}

// SummaryPart is a single entry in a reasoning item's summary array.
type SummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReasoningTextPart is a single entry in a reasoning item's content array.
type ReasoningTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MessageItem represents a message in the conversation
type MessageItem struct {
	Type    string      `json:"type"`
	Role    string      `json:"role,omitempty"`
	Content interface{} `json:"content"`
	ID      string      `json:"id,omitempty"`
	Status  string      `json:"status,omitempty"`
}

// InputTextContent represents text input
type InputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// InputImageContent represents image input
type InputImageContent struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// InputFileContent represents non-image file input.
type InputFileContent struct {
	Type     string `json:"type"`
	FileURL  string `json:"file_url,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	FileData string `json:"file_data,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// OutputTextContent represents text output
type OutputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`

	// Annotations and Logprobs are only populated when replaying assistant
	// history under strictResponseInput, mirroring the TS SDK's complete
	// output-text item shape.
	Annotations []Annotation  `json:"annotations,omitempty"`
	Logprobs    []interface{} `json:"logprobs,omitempty"`
}

// MarshalJSON writes annotations and logprobs whenever they are set, even as
// empty arrays (TS: `annotations: part.annotations ?? []` under
// strictResponseInput, and a parsed empty annotations array otherwise).
func (o OutputTextContent) MarshalJSON() ([]byte, error) {
	out := struct {
		Type        string         `json:"type"`
		Text        string         `json:"text"`
		Annotations *[]Annotation  `json:"annotations,omitempty"`
		Logprobs    *[]interface{} `json:"logprobs,omitempty"`
	}{Type: o.Type, Text: o.Text}
	if o.Annotations != nil {
		out.Annotations = &o.Annotations
	}
	if o.Logprobs != nil {
		out.Logprobs = &o.Logprobs
	}
	return json.Marshal(out)
}

// FunctionCallItem represents a function call
type FunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Namespace string `json:"namespace,omitempty"`
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
}

// FunctionCallOutputItem represents function call output
type FunctionCallOutputItem struct {
	Type   string      `json:"type"`
	CallID string      `json:"call_id"`
	Output interface{} `json:"output"`
	ID     string      `json:"id,omitempty"`
	Status string      `json:"status,omitempty"`
}

// CustomToolCallItem replays a caller-executed "custom" tool call back into
// request input, matched via Config.CustomToolID. Mirrors the TS Open
// Responses `custom_tool_call` request input item
// (CustomToolCallItemParam): id?/call_id/name/input, where Input is the raw
// (unescaped) text the model originally produced.
type CustomToolCallItem struct {
	Type   string `json:"type"`
	ID     string `json:"id,omitempty"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

// CustomToolCallOutputItem replays a caller-executed "custom" tool result
// back into request input, matched via Config.CustomToolID. Mirrors the TS
// Open Responses `custom_tool_call_output` request input item
// (CustomToolCallOutputItemParam): id?/call_id/output, using the same
// Output shape as FunctionCallOutputItem.
type CustomToolCallOutputItem struct {
	Type   string      `json:"type"`
	CallID string      `json:"call_id"`
	Output interface{} `json:"output"`
	ID     string      `json:"id,omitempty"`
}

// OpenResponsesResponse represents the non-streaming response
type OpenResponsesResponse struct {
	ID          string       `json:"id"`
	Object      string       `json:"object"`
	CreatedAt   int64        `json:"created_at"`
	CompletedAt *int64       `json:"completed_at,omitempty"`
	Status      string       `json:"status"`
	Model       string       `json:"model"`
	Output      []OutputItem `json:"output"`
	Usage       *Usage       `json:"usage,omitempty"`

	// Incomplete details if response wasn't completed
	IncompleteDetails *IncompleteDetails `json:"incomplete_details,omitempty"`

	// Error if response failed
	Error *ResponseError `json:"error,omitempty"`

	// Instructions that were used
	Instructions string `json:"instructions,omitempty"`

	// Tools that were available
	Tools []FunctionTool `json:"tools,omitempty"`

	// Other optional fields
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}

// openResponsesResponseWire decodes a response body as TS does: the body is
// not validated (jsonSchema<OpenResponsesResponseBody> in doGenerate, z.any()
// in doStream only apply types), so a fractional `created_at` or
// `completed_at` (unix seconds) is truncated to whole seconds instead of
// failing the whole decode, and a non-numeric one is left at its zero value.
// It is a wrapper rather than an UnmarshalJSON method on the exported
// OpenResponsesResponse so that a caller's struct embedding that type keeps
// decoding its own fields.
type openResponsesResponseWire struct {
	OpenResponsesResponse
	CreatedAt   json.RawMessage `json:"created_at"`
	CompletedAt json.RawMessage `json:"completed_at,omitempty"`
}

// UnmarshalJSON decodes the response leniently (openResponsesLenientUnmarshal):
// TS applies types to the body without validating it, so a field of an
// unexpected JSON type does not fail the response or the stream event that
// carries it.
func (w *openResponsesResponseWire) UnmarshalJSON(data []byte) error {
	type plain openResponsesResponseWire
	var p plain
	if err := openResponsesLenientUnmarshal(data, &p); err != nil {
		return err
	}
	*w = openResponsesResponseWire(p)
	return nil
}

// response returns the decoded OpenResponsesResponse.
func (w *openResponsesResponseWire) response() OpenResponsesResponse {
	r := w.OpenResponsesResponse
	if v, ok := openResponsesUnixSeconds(w.CreatedAt); ok {
		r.CreatedAt = v
	}
	if v, ok := openResponsesUnixSeconds(w.CompletedAt); ok {
		r.CompletedAt = &v
	}
	return r
}

// openResponsesUnixSeconds reads a JSON number as whole unix seconds
// (truncated toward zero); null, absent, non-numeric or out-of-range values
// report false.
func openResponsesUnixSeconds(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var f *float64
	if json.Unmarshal(raw, &f) != nil || f == nil {
		return 0, false
	}
	if math.IsNaN(*f) || math.IsInf(*f, 0) || math.Abs(*f) > 8.64e12 {
		return 0, false
	}
	return int64(*f), true
}

// OutputItem represents an item in the response output
type OutputItem struct {
	Type    string        `json:"type"`
	ID      string        `json:"id,omitempty"`
	Role    string        `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`
	Status  string        `json:"status,omitempty"`

	// For function_call type
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	// For custom_tool_call type
	Input string `json:"input,omitempty"`

	// For reasoning type
	Summary          []ContentPart `json:"summary,omitempty"`
	EncryptedContent string        `json:"encrypted_content,omitempty"`

	// Raw preserves this item's original JSON bytes, including any fields
	// not modeled above (e.g. a namespaced Open Responses extension item's
	// custom fields). Used for extension item decode/replay (row 9a68261,
	// OR-EXT). Populated by UnmarshalJSON.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known OutputItem fields via a type alias (so the
// default struct-tag-driven decoding still applies), then separately
// preserves the item's original bytes in Raw.
//
// The decode is lenient like TS, which reads the item without validating it:
// a string field holding another JSON value keeps JS String(value) (`type`
// only matches as a string, as TS compares it with ===), and any other field
// of an unexpected type keeps its zero value instead of failing the item.
func (o *OutputItem) UnmarshalJSON(data []byte) error {
	type outputItemAlias OutputItem
	var alias outputItemAlias
	if err := openResponsesLenientUnmarshal(data, &alias); err != nil {
		return err
	}
	var w struct {
		Type             json.RawMessage `json:"type"`
		ID               json.RawMessage `json:"id"`
		Role             json.RawMessage `json:"role"`
		Status           json.RawMessage `json:"status"`
		CallID           json.RawMessage `json:"call_id"`
		Name             json.RawMessage `json:"name"`
		Arguments        json.RawMessage `json:"arguments"`
		Namespace        json.RawMessage `json:"namespace"`
		Input            json.RawMessage `json:"input"`
		EncryptedContent json.RawMessage `json:"encrypted_content"`
	}
	if json.Unmarshal(data, &w) == nil {
		alias.Type = openResponsesWireType(w.Type)
		alias.ID = openResponsesWireString(w.ID)
		alias.Role = openResponsesWireString(w.Role)
		alias.Status = openResponsesWireString(w.Status)
		alias.CallID = openResponsesWireString(w.CallID)
		alias.Name = openResponsesWireString(w.Name)
		alias.Arguments = openResponsesWireString(w.Arguments)
		alias.Namespace = openResponsesWireString(w.Namespace)
		alias.Input = openResponsesWireString(w.Input)
		alias.EncryptedContent = openResponsesWireString(w.EncryptedContent)
	}
	*o = OutputItem(alias)
	o.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ContentPart represents a part of message content
type ContentPart struct {
	Type        string       `json:"type"`
	Text        string       `json:"text,omitempty"`
	Refusal     string       `json:"refusal,omitempty"`
	Annotations []Annotation `json:"annotations,omitempty"`
}

// Annotation represents an annotation on text
type Annotation struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
	Title      string `json:"title,omitempty"`
}

// MarshalJSON always writes start_index and end_index for url_citation
// annotations: an index of 0 is a valid offset and must not be omitted.
func (a Annotation) MarshalJSON() ([]byte, error) {
	if a.Type != "url_citation" {
		type plain Annotation
		return json.Marshal(plain(a))
	}
	return json.Marshal(struct {
		Type       string `json:"type"`
		URL        string `json:"url"`
		StartIndex int    `json:"start_index"`
		EndIndex   int    `json:"end_index"`
		Title      string `json:"title"`
	}{a.Type, a.URL, a.StartIndex, a.EndIndex, a.Title})
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	// Detailed token breakdown
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`

	// rawWire is the usage object exactly as the API sent it (TS usage.raw),
	// as JSON text so Usage stays comparable with == as in v0.5.2.
	rawWire string
}

// wire decodes rawWire; nil when the usage was not decoded from the wire.
func (u Usage) wire() map[string]interface{} {
	if u.rawWire == "" {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(u.rawWire), &m) != nil {
		return nil
	}
	return m
}

// UnmarshalJSON decodes the typed fields and keeps the wire object.
func (u *Usage) UnmarshalJSON(b []byte) error {
	type plain Usage
	var p plain
	if err := openResponsesLenientUnmarshal(b, &p); err != nil {
		return err
	}
	*u = Usage(p)
	var raw map[string]interface{}
	if json.Unmarshal(b, &raw) == nil {
		u.rawWire = string(b)
	}
	return nil
}

// InputTokensDetails provides detailed input token information
type InputTokensDetails struct {
	CachedTokens     int  `json:"cached_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

// OutputTokensDetails provides detailed output token information
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// IncompleteDetails explains why a response was incomplete
type IncompleteDetails struct {
	Reason string `json:"reason"`
}

// ResponseError represents an error in the response
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`

	// StatusCode is an endpoint-specific HTTP status carried inside the
	// error payload itself (e.g. QuiverAI's `error.status_code`), distinct
	// from the transport-level HTTP status of the response envelope. Used
	// by Config.GetResponseErrorMetadata to classify retryability.
	StatusCode *int `json:"status_code,omitempty"`
}

// UnmarshalJSON decodes a response error like TS reads it: neither doGenerate
// (no schema validation) nor doStream (z.any()) checks the error's field
// types, so a numeric code or message is kept (as JS String() of it) instead
// of failing the whole response or event; null or absent is "". StatusCode is
// set only for an integer number (TS getResponseErrorMetadata checks
// Number.isInteger).
func (e *ResponseError) UnmarshalJSON(data []byte) error {
	var w struct {
		Code       json.RawMessage `json:"code"`
		Message    json.RawMessage `json:"message"`
		StatusCode json.RawMessage `json:"status_code"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*e = ResponseError{Code: responseErrorText(w.Code), Message: responseErrorText(w.Message)}
	var n json.Number
	if len(w.StatusCode) > 0 && json.Unmarshal(w.StatusCode, &n) == nil {
		if f, err := n.Float64(); err == nil && f == math.Trunc(f) && math.Abs(f) <= math.MaxInt32 {
			status := int(f)
			e.StatusCode = &status
		}
	}
	return nil
}

// responseErrorText is JS String(value) for a JSON scalar ("" for null or
// absent); an object or array keeps its compact JSON text.
func responseErrorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return providerutils.JSNumberString(f)
	}
	return providerutils.CompactJSON(raw)
}

// Streaming event types

// StreamEvent represents a server-sent event
type StreamEvent struct {
	Type           string                 `json:"type"`
	SequenceNumber int                    `json:"sequence_number,omitempty"`
	Response       *OpenResponsesResponse `json:"response,omitempty"`
	OutputIndex    int                    `json:"output_index,omitempty"`
	Item           *OutputItem            `json:"item,omitempty"`
	ItemID         string                 `json:"item_id,omitempty"`
	ContentIndex   int                    `json:"content_index,omitempty"`
	Delta          string                 `json:"delta,omitempty"`
	Text           string                 `json:"text,omitempty"`
	CallID         string                 `json:"call_id,omitempty"`
	Arguments      string                 `json:"arguments,omitempty"`
	Input          string                 `json:"input,omitempty"`
	Error          *ResponseError         `json:"error,omitempty"`

	// Raw preserves this event's original JSON bytes, including any fields
	// not modeled above (e.g. a namespaced Open Responses extension event's
	// custom fields). Used for extension event decode (row 9a68261,
	// OR-EXT). Populated by UnmarshalJSON; left nil for StreamEvent values
	// constructed directly (e.g. in tests) rather than decoded from JSON.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes a stream event like TS reads it: doStream parses
// events with z.any() and then reads fields as JS does, so a field of an
// unexpected JSON type never fails the event decode.
//   - type matches only as a string (TS compares it with ===); any other
//     value is "" and matches no event.
//   - item_id, delta, text, call_id, arguments and input hold JS
//     String(value) for a non-string value; null or absent is "".
//   - sequence_number, output_index and content_index (never read by TS; the
//     extension check reads sequence_number from Raw) keep an integral
//     number and are 0 otherwise.
//   - response, item and error decode leniently; a non-object value (TS
//     reads undefined from each of its fields) is the zero struct, and null
//     or absent is nil.
//
// The embedded response decodes as openResponsesResponseWire, so a
// fractional created_at does not fail the event either. Raw keeps the
// event's original bytes.
func (e *StreamEvent) UnmarshalJSON(data []byte) error {
	var w struct {
		Type           json.RawMessage `json:"type"`
		SequenceNumber json.RawMessage `json:"sequence_number"`
		Response       json.RawMessage `json:"response"`
		OutputIndex    json.RawMessage `json:"output_index"`
		Item           json.RawMessage `json:"item"`
		ItemID         json.RawMessage `json:"item_id"`
		ContentIndex   json.RawMessage `json:"content_index"`
		Delta          json.RawMessage `json:"delta"`
		Text           json.RawMessage `json:"text"`
		CallID         json.RawMessage `json:"call_id"`
		Arguments      json.RawMessage `json:"arguments"`
		Input          json.RawMessage `json:"input"`
		Error          json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	ev := StreamEvent{
		Type:           openResponsesWireType(w.Type),
		SequenceNumber: openResponsesWireInt(w.SequenceNumber),
		OutputIndex:    openResponsesWireInt(w.OutputIndex),
		ItemID:         openResponsesWireString(w.ItemID),
		ContentIndex:   openResponsesWireInt(w.ContentIndex),
		Delta:          openResponsesWireString(w.Delta),
		Text:           openResponsesWireString(w.Text),
		CallID:         openResponsesWireString(w.CallID),
		Arguments:      openResponsesWireString(w.Arguments),
		Input:          openResponsesWireString(w.Input),
	}
	if openResponsesWirePresent(w.Response) {
		var r openResponsesResponseWire
		if openResponsesWireIsObject(w.Response) {
			if err := json.Unmarshal(w.Response, &r); err != nil {
				return err
			}
		}
		resp := r.response()
		ev.Response = &resp
	}
	if openResponsesWirePresent(w.Item) {
		item := OutputItem{Raw: append(json.RawMessage(nil), w.Item...)}
		if openResponsesWireIsObject(w.Item) {
			if err := json.Unmarshal(w.Item, &item); err != nil {
				return err
			}
		}
		ev.Item = &item
	}
	if openResponsesWirePresent(w.Error) {
		var re ResponseError
		if openResponsesWireIsObject(w.Error) {
			if err := json.Unmarshal(w.Error, &re); err != nil {
				return err
			}
		}
		ev.Error = &re
	}
	ev.Raw = append(json.RawMessage(nil), data...)
	*e = ev
	return nil
}

// openResponsesLenientUnmarshal is json.Unmarshal that does not fail on a
// field whose JSON type does not match its Go type: encoding/json skips such
// a field (it keeps its zero value) and decodes the rest. Syntax errors
// still fail.
func openResponsesLenientUnmarshal(data []byte, v interface{}) error {
	err := json.Unmarshal(data, v)
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return nil
	}
	return err
}

// openResponsesWirePresent reports whether a raw field is present and not
// null.
func openResponsesWirePresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// openResponsesWireIsObject reports whether a raw field is a JSON object.
func openResponsesWireIsObject(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '{'
}

// openResponsesWireType is a `type` field: only a JSON string can equal a
// TS string literal.
func openResponsesWireType(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// openResponsesWireString is JS String(value) for a present, non-null field
// ("" for null or absent).
func openResponsesWireString(raw json.RawMessage) string {
	if !openResponsesWirePresent(raw) {
		return ""
	}
	var v interface{}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return openResponsesJSString(v)
}

// openResponsesJSString is JS String(value) for a decoded JSON value: an
// object is "[object Object]" and an array joins its elements with ","
// (null elements are empty).
func openResponsesJSString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return providerutils.JSNumberString(x)
	case []interface{}:
		parts := make([]string, len(x))
		for i, el := range x {
			if el != nil {
				parts[i] = openResponsesJSString(el)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

// openResponsesWireInt keeps an integral JSON number that fits in an int32;
// any other value is 0.
func openResponsesWireInt(raw json.RawMessage) int {
	var f float64
	if len(raw) == 0 || json.Unmarshal(raw, &f) != nil {
		return 0
	}
	if f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0
	}
	return int(f)
}
