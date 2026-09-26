package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// UIMessageRole is the role of a UI message. Mirrors the TypeScript
// UIMessage['role'] union.
type UIMessageRole string

const (
	UIMessageRoleSystem    UIMessageRole = "system"
	UIMessageRoleUser      UIMessageRole = "user"
	UIMessageRoleAssistant UIMessageRole = "assistant"
)

// UIMessage is the AI SDK UI message shape used between a chat frontend and
// server routes. Mirrors the TypeScript UIMessage interface; its JSON form
// uses the same camelCase keys and `type` discriminators.
type UIMessage struct {
	// ID is a unique identifier for the message.
	ID string
	// Role is system, user or assistant.
	Role UIMessageRole
	// Metadata is optional application metadata (omitted when nil).
	Metadata interface{}
	// Parts are the message parts. Parts decoded from JSON are pointers to
	// the concrete part types (*TextUIPart, *ToolUIPart, ...).
	Parts []UIMessagePart
}

// UIMessagePart is one part of a UI message. Implemented by TextUIPart,
// CustomContentUIPart, ReasoningUIPart, ToolUIPart (static and dynamic tools),
// SourceURLUIPart, SourceDocumentUIPart, FileUIPart, ReasoningFileUIPart,
// DataUIPart and StepStartUIPart (value or pointer).
type UIMessagePart interface {
	// UIPartType returns the part's `type` discriminator.
	UIPartType() string
}

// UIPartState is the streaming state of text and reasoning parts.
type UIPartState string

const (
	UIPartStateStreaming UIPartState = "streaming"
	UIPartStateDone      UIPartState = "done"
)

// ToolUIPartState is the state of a tool invocation part.
type ToolUIPartState string

const (
	ToolStateInputStreaming    ToolUIPartState = "input-streaming"
	ToolStateInputAvailable    ToolUIPartState = "input-available"
	ToolStateApprovalRequested ToolUIPartState = "approval-requested"
	ToolStateApprovalResponded ToolUIPartState = "approval-responded"
	ToolStateOutputAvailable   ToolUIPartState = "output-available"
	ToolStateOutputError       ToolUIPartState = "output-error"
	ToolStateOutputDenied      ToolUIPartState = "output-denied"
)

// TextUIPart is a text part of a message.
type TextUIPart struct {
	Text             string
	State            UIPartState
	ProviderMetadata map[string]interface{}
}

// CustomContentUIPart is a provider-specific part of a message. Kind has the
// format `{provider}.{provider-type}`.
type CustomContentUIPart struct {
	Kind             string
	ProviderMetadata map[string]interface{}
}

// ReasoningUIPart is a reasoning part of a message.
type ReasoningUIPart struct {
	ID               string
	Text             string
	State            UIPartState
	ProviderMetadata map[string]interface{}
}

// SourceURLUIPart is a URL source part of a message.
type SourceURLUIPart struct {
	SourceID         string
	URL              string
	Title            string
	ProviderMetadata map[string]interface{}
}

// SourceDocumentUIPart is a document source part of a message.
type SourceDocumentUIPart struct {
	SourceID         string
	MediaType        string
	Title            string
	Filename         string
	ProviderMetadata map[string]interface{}
}

// FileUIPart is a file part of a message. URL is a hosted URL or a data URL.
// ProviderReference (files uploaded via UploadFile) takes precedence over URL
// when converting to model messages.
type FileUIPart struct {
	MediaType         string
	Filename          string
	URL               string
	ProviderReference map[string]string
	ProviderMetadata  map[string]interface{}
}

// ReasoningFileUIPart is a file generated during reasoning.
type ReasoningFileUIPart struct {
	MediaType        string
	URL              string
	ProviderMetadata map[string]interface{}
}

// StepStartUIPart marks a step boundary in an assistant message.
type StepStartUIPart struct{}

// DataUIPart is a custom data part. Type is the full discriminator
// (`data-<name>`).
type DataUIPart struct {
	Type string
	ID   string
	Data interface{}
}

// DataName returns the data part name (Type without the `data-` prefix).
func (p DataUIPart) DataName() string { return strings.TrimPrefix(p.Type, "data-") }

// ToolUIPartApproval is the approval object of a tool part. Mirrors the
// TypeScript `approval` field across approval-requested / approval-responded /
// output-* states.
type ToolUIPartApproval struct {
	ID string
	// Approved is nil while approval is requested.
	Approved *bool
	// Descriptor is an application-defined approval descriptor.
	Descriptor interface{}
	// RequestReason is the reason the approval was requested (user-approval
	// status with a reason).
	RequestReason string
	// Reason is the approval response reason.
	Reason      string
	IsAutomatic *bool
	Signature   string
	// InputSchemaInput is the tool input before schema parsing and input
	// refinement (present only when it differs from Input).
	InputSchemaInput interface{}
}

// ToolUIPart is a tool invocation part. It represents both static tool parts
// (Type `tool-<name>`, TS ToolUIPart) and dynamic tool parts (Type
// `dynamic-tool` with ToolName set, TS DynamicToolUIPart).
type ToolUIPart struct {
	// Type is `tool-<name>` for static tools or `dynamic-tool`.
	Type string
	// ToolName is set (and serialized) for dynamic tools only.
	ToolName         string
	ToolCallID       string
	State            ToolUIPartState
	Title            string
	ToolMetadata     map[string]interface{}
	ProviderExecuted *bool
	// Input is the tool input. For input-streaming and output-error nil means
	// absent; for other states nil is serialized as null.
	Input interface{}
	// Output is the tool output (output-available).
	Output interface{}
	// RawInput is the deprecated raw input of output-error parts.
	//
	// Deprecated: use Input.
	RawInput               interface{}
	ErrorText              string
	Preliminary            *bool
	CallProviderMetadata   map[string]interface{}
	ResultProviderMetadata map[string]interface{}
	Approval               *ToolUIPartApproval
}

// DynamicToolUIPart is the TS name for a ToolUIPart whose Type is
// `dynamic-tool`.
type DynamicToolUIPart = ToolUIPart

// ToolOutputErrorUIPart is the TS name for a (static or dynamic) tool part in
// the output-error state. Use IsToolOutputErrorUIPart to identify it.
type ToolOutputErrorUIPart = ToolUIPart

func (TextUIPart) UIPartType() string           { return "text" }
func (CustomContentUIPart) UIPartType() string  { return "custom" }
func (ReasoningUIPart) UIPartType() string      { return "reasoning" }
func (SourceURLUIPart) UIPartType() string      { return "source-url" }
func (SourceDocumentUIPart) UIPartType() string { return "source-document" }
func (FileUIPart) UIPartType() string           { return "file" }
func (ReasoningFileUIPart) UIPartType() string  { return "reasoning-file" }
func (StepStartUIPart) UIPartType() string      { return "step-start" }
func (p DataUIPart) UIPartType() string         { return p.Type }
func (p ToolUIPart) UIPartType() string         { return p.Type }

// ---------------------------------------------------------------------------
// Type guards (TS isTextUIPart, isToolUIPart, ...)
// ---------------------------------------------------------------------------

// IsTextUIPart reports whether part is a text part.
func IsTextUIPart(part UIMessagePart) bool { return partType(part) == "text" }

// IsCustomContentUIPart reports whether part is a custom part.
func IsCustomContentUIPart(part UIMessagePart) bool { return partType(part) == "custom" }

// IsFileUIPart reports whether part is a file part.
func IsFileUIPart(part UIMessagePart) bool { return partType(part) == "file" }

// IsReasoningFileUIPart reports whether part is a reasoning file part.
func IsReasoningFileUIPart(part UIMessagePart) bool { return partType(part) == "reasoning-file" }

// IsReasoningUIPart reports whether part is a reasoning part.
func IsReasoningUIPart(part UIMessagePart) bool { return partType(part) == "reasoning" }

// IsDataUIPart reports whether part is a data part.
func IsDataUIPart(part UIMessagePart) bool { return strings.HasPrefix(partType(part), "data-") }

// IsStaticToolUIPart reports whether part is a static tool part (`tool-*`).
func IsStaticToolUIPart(part UIMessagePart) bool {
	return strings.HasPrefix(partType(part), "tool-")
}

// IsDynamicToolUIPart reports whether part is a dynamic tool part.
func IsDynamicToolUIPart(part UIMessagePart) bool { return partType(part) == "dynamic-tool" }

// IsToolUIPart reports whether part is a static or dynamic tool part.
func IsToolUIPart(part UIMessagePart) bool {
	return IsStaticToolUIPart(part) || IsDynamicToolUIPart(part)
}

// IsToolOutputErrorUIPart reports whether part is a static or dynamic tool
// part in the output-error state.
func IsToolOutputErrorUIPart(part UIMessagePart) bool {
	tool, ok := AsToolUIPart(part)
	return ok && tool.State == ToolStateOutputError
}

// rawInputDeprecationWarning mirrors TS
// warn-if-ui-message-has-deprecated-raw-input.ts's rawInputDeprecationWarning.
var rawInputDeprecationWarning = types.Warning{
	Type:    "deprecated",
	Setting: "rawInput in output-error UI message parts",
	Message: `Use the "input" field instead. The "rawInput" field will be removed in the next major version.`,
}

// warnIfUIMessagesHaveDeprecatedRawInput logs a deprecation warning (via
// LogWarnings) when any message has an output-error tool part still carrying
// the deprecated RawInput field. Mirrors TS
// warnIfUIMessageHasDeprecatedRawInput.
func warnIfUIMessagesHaveDeprecatedRawInput(messages []UIMessage) {
	for _, message := range messages {
		for _, part := range message.Parts {
			tool, ok := AsToolUIPart(part)
			if ok && tool.State == ToolStateOutputError && tool.RawInput != nil {
				LogWarnings(LogWarningsOptions{Warnings: []types.Warning{rawInputDeprecationWarning}})
				return
			}
		}
	}
}

// AsToolUIPart returns the tool part behind part (value or pointer).
func AsToolUIPart(part UIMessagePart) (*ToolUIPart, bool) {
	switch p := part.(type) {
	case *ToolUIPart:
		if p != nil && IsToolUIPart(p) {
			return p, true
		}
	case ToolUIPart:
		if IsToolUIPart(p) {
			return &p, true
		}
	}
	return nil, false
}

// GetStaticToolName returns the tool name of a static tool part.
func GetStaticToolName(part *ToolUIPart) string {
	parts := strings.Split(part.Type, "-")
	return strings.Join(parts[1:], "-")
}

// GetToolName returns the tool name of a static or dynamic tool part.
func GetToolName(part *ToolUIPart) string {
	if part.Type == "dynamic-tool" {
		return part.ToolName
	}
	return GetStaticToolName(part)
}

// GetToolOrDynamicToolName is a deprecated alias for GetToolName.
//
// Deprecated: use GetToolName.
func GetToolOrDynamicToolName(part *ToolUIPart) string { return GetToolName(part) }

func partType(part UIMessagePart) string {
	if part == nil {
		return ""
	}
	return part.UIPartType()
}

// ---------------------------------------------------------------------------
// JSON
// ---------------------------------------------------------------------------

// orderedJSON writes a JSON object with keys in insertion order so the output
// matches the TypeScript object key order.
type orderedJSON struct {
	buf bytes.Buffer
	err error
	n   int
}

func (o *orderedJSON) field(key string, value interface{}) {
	if o.err != nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		o.err = err
		return
	}
	if o.n == 0 {
		o.buf.WriteByte('{')
	} else {
		o.buf.WriteByte(',')
	}
	k, _ := json.Marshal(key)
	o.buf.Write(k)
	o.buf.WriteByte(':')
	o.buf.Write(data)
	o.n++
}

func (o *orderedJSON) str(key, value string) {
	if value != "" {
		o.field(key, value)
	}
}

func (o *orderedJSON) mapField(key string, value map[string]interface{}) {
	if value != nil {
		o.field(key, value)
	}
}

func (o *orderedJSON) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	if o.n == 0 {
		return []byte("{}"), nil
	}
	o.buf.WriteByte('}')
	return o.buf.Bytes(), nil
}

// MarshalJSON emits the TypeScript UIMessage shape.
func (m UIMessage) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("id", m.ID)
	o.field("role", m.Role)
	if m.Metadata != nil {
		o.field("metadata", m.Metadata)
	}
	parts := m.Parts
	if parts == nil {
		parts = []UIMessagePart{}
	}
	o.field("parts", parts)
	return o.bytes()
}

// UnmarshalJSON decodes the TypeScript UIMessage shape. Unknown keys are
// ignored; unknown part types are an error.
func (m *UIMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       string            `json:"id"`
		Role     UIMessageRole     `json:"role"`
		Metadata interface{}       `json:"metadata"`
		Parts    []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.Role = raw.Role
	m.Metadata = raw.Metadata
	m.Parts = make([]UIMessagePart, 0, len(raw.Parts))
	for i, rawPart := range raw.Parts {
		part, err := UnmarshalUIMessagePart(rawPart)
		if err != nil {
			return fmt.Errorf("parts[%d]: %w", i, err)
		}
		m.Parts = append(m.Parts, part)
	}
	return nil
}

// UnmarshalUIMessagePart decodes a single UI message part into a pointer to
// its concrete type.
func UnmarshalUIMessagePart(data []byte) (UIMessagePart, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	var part UIMessagePart
	switch {
	case head.Type == "text":
		part = &TextUIPart{}
	case head.Type == "custom":
		part = &CustomContentUIPart{}
	case head.Type == "reasoning":
		part = &ReasoningUIPart{}
	case head.Type == "source-url":
		part = &SourceURLUIPart{}
	case head.Type == "source-document":
		part = &SourceDocumentUIPart{}
	case head.Type == "file":
		part = &FileUIPart{}
	case head.Type == "reasoning-file":
		part = &ReasoningFileUIPart{}
	case head.Type == "step-start":
		return &StepStartUIPart{}, nil
	case strings.HasPrefix(head.Type, "data-"):
		part = &DataUIPart{}
	case head.Type == "dynamic-tool" || strings.HasPrefix(head.Type, "tool-"):
		part = &ToolUIPart{}
	default:
		return nil, fmt.Errorf("unsupported UI message part type %q", head.Type)
	}
	if err := json.Unmarshal(data, part); err != nil {
		return nil, err
	}
	return part, nil
}

func (p TextUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "text")
	o.field("text", p.Text)
	o.str("state", string(p.State))
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *TextUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		Text             string                 `json:"text"`
		State            UIPartState            `json:"state"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = TextUIPart(raw)
	return nil
}

func (p CustomContentUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "custom")
	o.field("kind", p.Kind)
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *CustomContentUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind             string                 `json:"kind"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = CustomContentUIPart(raw)
	return nil
}

func (p ReasoningUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "reasoning")
	o.str("id", p.ID)
	o.field("text", p.Text)
	o.str("state", string(p.State))
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *ReasoningUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID               string                 `json:"id"`
		Text             string                 `json:"text"`
		State            UIPartState            `json:"state"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = ReasoningUIPart(raw)
	return nil
}

func (p SourceURLUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "source-url")
	o.field("sourceId", p.SourceID)
	o.field("url", p.URL)
	o.str("title", p.Title)
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *SourceURLUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceID         string                 `json:"sourceId"`
		URL              string                 `json:"url"`
		Title            string                 `json:"title"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = SourceURLUIPart(raw)
	return nil
}

func (p SourceDocumentUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "source-document")
	o.field("sourceId", p.SourceID)
	o.field("mediaType", p.MediaType)
	o.field("title", p.Title)
	o.str("filename", p.Filename)
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *SourceDocumentUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceID         string                 `json:"sourceId"`
		MediaType        string                 `json:"mediaType"`
		Title            string                 `json:"title"`
		Filename         string                 `json:"filename"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = SourceDocumentUIPart(raw)
	return nil
}

func (p FileUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "file")
	o.field("mediaType", p.MediaType)
	o.str("filename", p.Filename)
	o.field("url", p.URL)
	if p.ProviderReference != nil {
		o.field("providerReference", p.ProviderReference)
	}
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *FileUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		MediaType         string                 `json:"mediaType"`
		Filename          string                 `json:"filename"`
		URL               string                 `json:"url"`
		ProviderReference map[string]string      `json:"providerReference"`
		ProviderMetadata  map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = FileUIPart(raw)
	return nil
}

func (p ReasoningFileUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", "reasoning-file")
	o.field("mediaType", p.MediaType)
	o.field("url", p.URL)
	o.mapField("providerMetadata", p.ProviderMetadata)
	return o.bytes()
}

func (p *ReasoningFileUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		MediaType        string                 `json:"mediaType"`
		URL              string                 `json:"url"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = ReasoningFileUIPart(raw)
	return nil
}

func (StepStartUIPart) MarshalJSON() ([]byte, error) {
	return []byte(`{"type":"step-start"}`), nil
}

func (p DataUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", p.Type)
	o.str("id", p.ID)
	o.field("data", p.Data)
	return o.bytes()
}

func (p *DataUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type string      `json:"type"`
		ID   string      `json:"id"`
		Data interface{} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = DataUIPart(raw)
	return nil
}

func (a ToolUIPartApproval) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("id", a.ID)
	if a.Approved != nil {
		o.field("approved", *a.Approved)
	}
	if a.Descriptor != nil {
		o.field("descriptor", a.Descriptor)
	}
	o.str("requestReason", a.RequestReason)
	o.str("reason", a.Reason)
	if a.IsAutomatic != nil {
		o.field("isAutomatic", *a.IsAutomatic)
	}
	o.str("signature", a.Signature)
	if a.InputSchemaInput != nil {
		o.field("inputSchemaInput", a.InputSchemaInput)
	}
	return o.bytes()
}

func (a *ToolUIPartApproval) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID               string      `json:"id"`
		Approved         *bool       `json:"approved"`
		Descriptor       interface{} `json:"descriptor"`
		RequestReason    string      `json:"requestReason"`
		Reason           string      `json:"reason"`
		IsAutomatic      *bool       `json:"isAutomatic"`
		Signature        string      `json:"signature"`
		InputSchemaInput interface{} `json:"inputSchemaInput"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = ToolUIPartApproval(raw)
	return nil
}

// toolStateRequiresInput reports whether the TS type declares `input` as a
// required key for the state (so a nil Input is serialized as null).
func toolStateRequiresInput(state ToolUIPartState) bool {
	switch state {
	case ToolStateInputStreaming, ToolStateOutputError, "":
		return false
	default:
		return true
	}
}

func (p ToolUIPart) MarshalJSON() ([]byte, error) {
	var o orderedJSON
	o.field("type", p.Type)
	if p.Type == "dynamic-tool" {
		o.field("toolName", p.ToolName)
	}
	o.field("toolCallId", p.ToolCallID)
	o.field("state", p.State)
	o.str("title", p.Title)
	o.mapField("toolMetadata", p.ToolMetadata)
	if p.Input != nil || toolStateRequiresInput(p.State) {
		o.field("input", p.Input)
	}
	if p.Output != nil || p.State == ToolStateOutputAvailable {
		o.field("output", p.Output)
	}
	if p.RawInput != nil {
		o.field("rawInput", p.RawInput)
	}
	if p.ErrorText != "" || p.State == ToolStateOutputError {
		o.field("errorText", p.ErrorText)
	}
	if p.ProviderExecuted != nil {
		o.field("providerExecuted", *p.ProviderExecuted)
	}
	if p.Preliminary != nil {
		o.field("preliminary", *p.Preliminary)
	}
	o.mapField("callProviderMetadata", p.CallProviderMetadata)
	o.mapField("resultProviderMetadata", p.ResultProviderMetadata)
	if p.Approval != nil {
		o.field("approval", p.Approval)
	}
	return o.bytes()
}

func (p *ToolUIPart) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type                   string                 `json:"type"`
		ToolName               string                 `json:"toolName"`
		ToolCallID             string                 `json:"toolCallId"`
		State                  ToolUIPartState        `json:"state"`
		Title                  string                 `json:"title"`
		ToolMetadata           map[string]interface{} `json:"toolMetadata"`
		ProviderExecuted       *bool                  `json:"providerExecuted"`
		Input                  interface{}            `json:"input"`
		Output                 interface{}            `json:"output"`
		RawInput               interface{}            `json:"rawInput"`
		ErrorText              string                 `json:"errorText"`
		Preliminary            *bool                  `json:"preliminary"`
		CallProviderMetadata   map[string]interface{} `json:"callProviderMetadata"`
		ResultProviderMetadata map[string]interface{} `json:"resultProviderMetadata"`
		Approval               *ToolUIPartApproval    `json:"approval"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = ToolUIPart(raw)
	if p.Type != "dynamic-tool" {
		p.ToolName = ""
	}
	return nil
}

// ---------------------------------------------------------------------------
// Conversion between UIMessage and the map-based UIMessageChunk shape used by
// the UI message stream reducer and callbacks.
// ---------------------------------------------------------------------------

// UIMessageFromChunk converts a map-shaped UI message (as produced by the UI
// message stream callbacks, e.g. `responseMessage`) into a typed UIMessage.
func UIMessageFromChunk(message UIMessageChunk) (UIMessage, error) {
	var out UIMessage
	data, err := json.Marshal(message)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}

// UIMessagesFromChunks converts map-shaped UI messages into typed UIMessages.
func UIMessagesFromChunks(messages []UIMessageChunk) ([]UIMessage, error) {
	out := make([]UIMessage, 0, len(messages))
	for i, message := range messages {
		converted, err := UIMessageFromChunk(message)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		out = append(out, converted)
	}
	return out, nil
}

// ToChunk converts a typed UIMessage into the map-shaped UI message used by
// UIMessageStreamOptions.OriginalMessages.
func (m UIMessage) ToChunk() (UIMessageChunk, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out UIMessageChunk
	err = json.Unmarshal(data, &out)
	return out, err
}

// UIMessagesToChunks converts typed UIMessages into map-shaped UI messages.
func UIMessagesToChunks(messages []UIMessage) ([]UIMessageChunk, error) {
	out := make([]UIMessageChunk, 0, len(messages))
	for _, message := range messages {
		chunk, err := message.ToChunk()
		if err != nil {
			return nil, err
		}
		out = append(out, chunk)
	}
	return out, nil
}
