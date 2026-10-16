package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/intsafe"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// StreamPart is one event emitted by a harness adapter during a prompt turn.
// Mirrors TS `HarnessV1StreamPart`; every variant marshals to exactly the JSON
// shape validated by the corresponding `harnessV1*PartSchema` zod schema.
//
// Variants: *StreamStartPart, *TextStartPart, *TextDeltaPart, *TextEndPart,
// *ReasoningStartPart, *ReasoningDeltaPart, *ReasoningEndPart,
// *ToolInputStartPart, *ToolInputDeltaPart, *ToolInputEndPart, *ToolCallPart,
// *ToolApprovalRequestPart, *ToolResultPart, *FinishStepPart, *FinishPart,
// *FileChangePart, *CompactionPart, *ErrorPart, *RawPart.
type StreamPart interface {
	// PartType returns the wire discriminator ("text-delta", ...).
	PartType() string
	isStreamPart()
}

// Stream part discriminators.
const (
	PartTypeStreamStart         = "stream-start"
	PartTypeTextStart           = "text-start"
	PartTypeTextDelta           = "text-delta"
	PartTypeTextEnd             = "text-end"
	PartTypeReasoningStart      = "reasoning-start"
	PartTypeReasoningDelta      = "reasoning-delta"
	PartTypeReasoningEnd        = "reasoning-end"
	PartTypeToolInputStart      = "tool-input-start"
	PartTypeToolInputDelta      = "tool-input-delta"
	PartTypeToolInputEnd        = "tool-input-end"
	PartTypeToolCall            = "tool-call"
	PartTypeToolApprovalRequest = "tool-approval-request"
	PartTypeToolResult          = "tool-result"
	PartTypeFinishStep          = "finish-step"
	PartTypeFinish              = "finish"
	PartTypeFileChange          = "file-change"
	PartTypeCompaction          = "compaction"
	PartTypeError               = "error"
	PartTypeRaw                 = "raw"
)

// ProviderMetadata mirrors `SharedV4ProviderMetadata` on tool parts.
type ProviderMetadata map[string]map[string]any

// InputTokenUsage is the input-token breakdown of LanguageModelV4Usage.
type InputTokenUsage struct {
	Total      *int `json:"total,omitempty"`
	NoCache    *int `json:"noCache,omitempty"`
	CacheRead  *int `json:"cacheRead,omitempty"`
	CacheWrite *int `json:"cacheWrite,omitempty"`
}

// OutputTokenUsage is the output-token breakdown of LanguageModelV4Usage.
type OutputTokenUsage struct {
	Total     *int `json:"total,omitempty"`
	Text      *int `json:"text,omitempty"`
	Reasoning *int `json:"reasoning,omitempty"`
}

// Usage is the harness wire encoding of `LanguageModelV4Usage`.
type Usage struct {
	InputTokens  InputTokenUsage  `json:"inputTokens"`
	OutputTokens OutputTokenUsage `json:"outputTokens"`
	Raw          map[string]any   `json:"raw,omitempty"`
}

// Unified finish reasons (LanguageModelV4FinishReason.unified).
const (
	FinishReasonStop          = "stop"
	FinishReasonLength        = "length"
	FinishReasonContentFilter = "content-filter"
	FinishReasonToolCalls     = "tool-calls"
	FinishReasonError         = "error"
	FinishReasonOther         = "other"
)

// FinishReason is the harness wire encoding of `LanguageModelV4FinishReason`.
type FinishReason struct {
	Unified string `json:"unified"`
	Raw     string `json:"raw,omitempty"`
}

func (r FinishReason) validate() error {
	switch r.Unified {
	case FinishReasonStop, FinishReasonLength, FinishReasonContentFilter,
		FinishReasonToolCalls, FinishReasonError, FinishReasonOther:
		return nil
	}
	return fmt.Errorf("invalid finish reason %q", r.Unified)
}

// StreamStartPart is the `stream-start` part.
type StreamStartPart struct {
	Warnings []CallWarning `json:"warnings,omitempty"`
	// ModelID is the model the runtime resolved to for this turn, when known.
	ModelID string `json:"modelId,omitempty"`
}

// TextStartPart is the `text-start` part.
type TextStartPart struct {
	ID              string   `json:"id"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// TextDeltaPart is the `text-delta` part.
type TextDeltaPart struct {
	ID              string   `json:"id"`
	Delta           string   `json:"delta"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// TextEndPart is the `text-end` part.
type TextEndPart struct {
	ID              string   `json:"id"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ReasoningStartPart is the `reasoning-start` part.
type ReasoningStartPart struct {
	ID              string   `json:"id"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ReasoningDeltaPart is the `reasoning-delta` part.
type ReasoningDeltaPart struct {
	ID              string   `json:"id"`
	Delta           string   `json:"delta"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ReasoningEndPart is the `reasoning-end` part.
type ReasoningEndPart struct {
	ID              string   `json:"id"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ToolInputStartPart is the `tool-input-start` part (V4 primitive).
type ToolInputStartPart struct {
	ID               string           `json:"id"`
	ToolName         string           `json:"toolName"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	ProviderExecuted bool             `json:"providerExecuted,omitempty"`
	Dynamic          bool             `json:"dynamic,omitempty"`
	Title            string           `json:"title,omitempty"`
}

// ToolInputDeltaPart is the `tool-input-delta` part (V4 primitive).
type ToolInputDeltaPart struct {
	ID               string           `json:"id"`
	Delta            string           `json:"delta"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

// ToolInputEndPart is the `tool-input-end` part (V4 primitive).
type ToolInputEndPart struct {
	ID               string           `json:"id"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

// ToolCallPart is the `tool-call` part: LanguageModelV4ToolCall plus
// `nativeName` and `stepToolCallCount`.
type ToolCallPart struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	// Input is the stringified JSON tool input.
	Input string `json:"input"`
	// ProviderExecuted is true for runtime-executed builtins.
	ProviderExecuted bool             `json:"providerExecuted,omitempty"`
	Dynamic          bool             `json:"dynamic,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	// NativeName is the runtime's native name when it differs from ToolName.
	NativeName string `json:"nativeName,omitempty"`
	// StepToolCallCount is the total tool calls in the current model step,
	// when known up front. Must be a positive integer when set.
	//
	// Deprecated: ignored by the host since TS #23260. Parallel approvals and
	// client tool results are collected by pausing at the step's next
	// finish-step instead, so adapters no longer need to populate it.
	StepToolCallCount *int `json:"stepToolCallCount,omitempty"`
	// ProviderExecutedOmitted is set by DecodeStreamPart when the wire frame
	// carried no `providerExecuted` key. A Go bool cannot tell "absent" from
	// false, but TS defaults a builtin approval's providerExecuted to true
	// when the tool call left it undefined (`rawToolCall?.providerExecuted
	// ?? true`). Never serialized.
	ProviderExecutedOmitted bool `json:"-"`
}

// ToolApprovalRequestPart is the `tool-approval-request` part.
type ToolApprovalRequestPart struct {
	ApprovalID       string           `json:"approvalId"`
	ToolCallID       string           `json:"toolCallId"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

// ToolResultPart is the `tool-result` part. Result is always serialized,
// including a JSON null for tools that produced no output (TS leniency).
type ToolResultPart struct {
	ToolCallID       string           `json:"toolCallId"`
	ToolName         string           `json:"toolName"`
	Result           any              `json:"result"`
	IsError          bool             `json:"isError,omitempty"`
	Preliminary      bool             `json:"preliminary,omitempty"`
	Dynamic          bool             `json:"dynamic,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

// FinishStepPart is the `finish-step` part: a step boundary inside a turn.
type FinishStepPart struct {
	FinishReason    FinishReason `json:"finishReason"`
	Usage           Usage        `json:"usage"`
	HarnessMetadata Metadata     `json:"harnessMetadata,omitempty"`
}

// FinishPart is the `finish` part: turn end.
type FinishPart struct {
	FinishReason    FinishReason `json:"finishReason"`
	TotalUsage      Usage        `json:"totalUsage"`
	HarnessMetadata Metadata     `json:"harnessMetadata,omitempty"`
}

// File change events.
const (
	FileChangeCreate = "create"
	FileChangeModify = "modify"
	FileChangeDelete = "delete"
)

// FileChangePart is the `file-change` part: a workspace mutation through an
// opaque mechanism.
type FileChangePart struct {
	Event           string   `json:"event"`
	Path            string   `json:"path"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// Compaction triggers.
const (
	CompactionTriggerManual = "manual"
	CompactionTriggerAuto   = "auto"
)

// CompactionPart is the `compaction` part: context compaction performed by the
// runtime.
type CompactionPart struct {
	Trigger         string   `json:"trigger"`
	Summary         string   `json:"summary"`
	TokensBefore    *float64 `json:"tokensBefore,omitempty"`
	TokensAfter     *float64 `json:"tokensAfter,omitempty"`
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ErrorPart is the `error` part. Error is an arbitrary JSON value.
type ErrorPart struct {
	Error any `json:"error"`

	// errorText is JSON.stringify of the wire error object, with the wire key
	// order (a Go map would sort the keys). Set only when decoded from JSON.
	errorText string
	// errorTextFor is the Error value errorText was computed for; a caller
	// that replaces Error invalidates the cached text.
	errorTextFor any
}

// UnmarshalJSON decodes the part and keeps the wire key order of an object
// or array error, like the JS value TS rejects with.
func (p *ErrorPart) UnmarshalJSON(data []byte) error {
	var aux struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.Error, p.errorText, p.errorTextFor = nil, "", nil
	if len(aux.Error) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(aux.Error, &v); err != nil {
		return err
	}
	p.Error = v
	switch v.(type) {
	case map[string]any, []any:
		p.errorText = jsonorder.Text(aux.Error, nil)
		p.errorTextFor = v
	}
	return nil
}

// Message renders the error value like TS getErrorMessage: an object error is
// JSON.stringify of the wire object, keys in wire order.
func (p *ErrorPart) Message() string {
	if p.errorText != "" && sameErrorValue(p.Error, p.errorTextFor) {
		return p.errorText
	}
	return ErrorValueMessage(p.Error)
}

// AsError converts the part's error value to a Go error: an error passes
// through, anything else carries Message().
func (p *ErrorPart) AsError() error {
	if err, ok := p.Error.(error); ok {
		return err
	}
	return &rawValueError{message: p.Message()}
}

func sameErrorValue(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && reflect.ValueOf(x).Pointer() == reflect.ValueOf(y).Pointer()
	case []any:
		y, ok := b.([]any)
		return ok && len(x) == len(y) && (len(x) == 0 || &x[0] == &y[0])
	}
	return false
}

// RawPart is the `raw` part: adapter-specific passthrough.
type RawPart struct {
	RawValue any `json:"rawValue"`
}

func (*StreamStartPart) PartType() string         { return PartTypeStreamStart }
func (*TextStartPart) PartType() string           { return PartTypeTextStart }
func (*TextDeltaPart) PartType() string           { return PartTypeTextDelta }
func (*TextEndPart) PartType() string             { return PartTypeTextEnd }
func (*ReasoningStartPart) PartType() string      { return PartTypeReasoningStart }
func (*ReasoningDeltaPart) PartType() string      { return PartTypeReasoningDelta }
func (*ReasoningEndPart) PartType() string        { return PartTypeReasoningEnd }
func (*ToolInputStartPart) PartType() string      { return PartTypeToolInputStart }
func (*ToolInputDeltaPart) PartType() string      { return PartTypeToolInputDelta }
func (*ToolInputEndPart) PartType() string        { return PartTypeToolInputEnd }
func (*ToolCallPart) PartType() string            { return PartTypeToolCall }
func (*ToolApprovalRequestPart) PartType() string { return PartTypeToolApprovalRequest }
func (*ToolResultPart) PartType() string          { return PartTypeToolResult }
func (*FinishStepPart) PartType() string          { return PartTypeFinishStep }
func (*FinishPart) PartType() string              { return PartTypeFinish }
func (*FileChangePart) PartType() string          { return PartTypeFileChange }
func (*CompactionPart) PartType() string          { return PartTypeCompaction }
func (*ErrorPart) PartType() string               { return PartTypeError }
func (*RawPart) PartType() string                 { return PartTypeRaw }

func (*StreamStartPart) isStreamPart()         {}
func (*TextStartPart) isStreamPart()           {}
func (*TextDeltaPart) isStreamPart()           {}
func (*TextEndPart) isStreamPart()             {}
func (*ReasoningStartPart) isStreamPart()      {}
func (*ReasoningDeltaPart) isStreamPart()      {}
func (*ReasoningEndPart) isStreamPart()        {}
func (*ToolInputStartPart) isStreamPart()      {}
func (*ToolInputDeltaPart) isStreamPart()      {}
func (*ToolInputEndPart) isStreamPart()        {}
func (*ToolCallPart) isStreamPart()            {}
func (*ToolApprovalRequestPart) isStreamPart() {}
func (*ToolResultPart) isStreamPart()          {}
func (*FinishStepPart) isStreamPart()          {}
func (*FinishPart) isStreamPart()              {}
func (*FileChangePart) isStreamPart()          {}
func (*CompactionPart) isStreamPart()          {}
func (*ErrorPart) isStreamPart()               {}
func (*RawPart) isStreamPart()                 {}

// MarshalStreamPart encodes a part with its `type` discriminator first.
func MarshalStreamPart(part StreamPart) ([]byte, error) {
	if part == nil {
		return nil, errors.New("harness: nil stream part")
	}
	return MarshalTagged(part.PartType(), part)
}

// MarshalTagged encodes v (a struct) as a JSON object whose first key is
// `"type": typ`. It is shared with the bridge frame encoders.
func MarshalTagged(typ string, v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	typeJSON, _ := json.Marshal(typ)
	if len(body) < 2 || body[0] != '{' {
		return nil, fmt.Errorf("harness: %T does not encode to a JSON object", v)
	}
	out := make([]byte, 0, intsafe.AddCap(intsafe.AddCap(len(body), len(typeJSON)), 10))
	out = append(out, `{"type":`...)
	out = append(out, typeJSON...)
	if len(body) > 2 {
		out = append(out, ',')
	}
	out = append(out, body[1:]...)
	return out, nil
}

// streamPartSpec describes how to decode one variant: a constructor and the
// keys zod requires to be present (non-undefined) on the wire.
type streamPartSpec struct {
	newPart  func() StreamPart
	required []string
}

var streamPartSpecs = map[string]streamPartSpec{
	PartTypeStreamStart:         {func() StreamPart { return &StreamStartPart{} }, nil},
	PartTypeTextStart:           {func() StreamPart { return &TextStartPart{} }, []string{"id"}},
	PartTypeTextDelta:           {func() StreamPart { return &TextDeltaPart{} }, []string{"id", "delta"}},
	PartTypeTextEnd:             {func() StreamPart { return &TextEndPart{} }, []string{"id"}},
	PartTypeReasoningStart:      {func() StreamPart { return &ReasoningStartPart{} }, []string{"id"}},
	PartTypeReasoningDelta:      {func() StreamPart { return &ReasoningDeltaPart{} }, []string{"id", "delta"}},
	PartTypeReasoningEnd:        {func() StreamPart { return &ReasoningEndPart{} }, []string{"id"}},
	PartTypeToolInputStart:      {func() StreamPart { return &ToolInputStartPart{} }, []string{"id", "toolName"}},
	PartTypeToolInputDelta:      {func() StreamPart { return &ToolInputDeltaPart{} }, []string{"id", "delta"}},
	PartTypeToolInputEnd:        {func() StreamPart { return &ToolInputEndPart{} }, []string{"id"}},
	PartTypeToolCall:            {func() StreamPart { return &ToolCallPart{} }, []string{"toolCallId", "toolName", "input"}},
	PartTypeToolApprovalRequest: {func() StreamPart { return &ToolApprovalRequestPart{} }, []string{"approvalId", "toolCallId"}},
	// `result` may be JSON null (runtime leniency), but the key must exist.
	PartTypeToolResult: {func() StreamPart { return &ToolResultPart{} }, []string{"toolCallId", "toolName", "result?"}},
	PartTypeFinishStep: {func() StreamPart { return &FinishStepPart{} }, []string{"finishReason", "usage"}},
	PartTypeFinish:     {func() StreamPart { return &FinishPart{} }, []string{"finishReason", "totalUsage"}},
	PartTypeFileChange: {func() StreamPart { return &FileChangePart{} }, []string{"event", "path"}},
	PartTypeCompaction: {func() StreamPart { return &CompactionPart{} }, []string{"trigger", "summary"}},
	PartTypeError:      {func() StreamPart { return &ErrorPart{} }, nil},
	PartTypeRaw:        {func() StreamPart { return &RawPart{} }, nil},
}

// IsStreamPartType reports whether typ is a stream-part discriminator.
func IsStreamPartType(typ string) bool {
	_, ok := streamPartSpecs[typ]
	return ok
}

// ErrUnknownPartType is returned when a JSON value carries an unknown `type`.
var ErrUnknownPartType = errors.New("harness: unknown part type")

// DecodeStreamPart parses and validates one JSON stream part, mirroring
// `harnessV1StreamPartSchema.parse`. Unknown keys (such as the bridge `seq`
// cursor) are ignored, like zod's default object stripping.
func DecodeStreamPart(data []byte) (StreamPart, error) {
	typ, fields, err := ReadTagged(data)
	if err != nil {
		return nil, err
	}
	spec, ok := streamPartSpecs[typ]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownPartType, typ)
	}
	if err := RequireKeys(typ, fields, spec.required); err != nil {
		return nil, err
	}
	part := spec.newPart()
	if tc, ok := part.(*ToolCallPart); ok {
		// TS dropped `stepToolCallCount` from the tool-call schema, so zod
		// strips it whatever its value: an unusable count is ignored, never a
		// reason to reject the frame.
		type toolCallAlias ToolCallPart
		wire := struct {
			*toolCallAlias
			StepToolCallCount json.RawMessage `json:"stepToolCallCount"`
		}{toolCallAlias: (*toolCallAlias)(tc)}
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, fmt.Errorf("harness: invalid %s part: %w", typ, err)
		}
		var n int
		if json.Unmarshal(wire.StepToolCallCount, &n) == nil && n > 0 {
			tc.StepToolCallCount = &n
		}
	} else if err := json.Unmarshal(data, part); err != nil {
		return nil, fmt.Errorf("harness: invalid %s part: %w", typ, err)
	}
	if err := validateStreamPart(part); err != nil {
		return nil, fmt.Errorf("harness: invalid %s part: %w", typ, err)
	}
	if tc, ok := part.(*ToolCallPart); ok {
		_, present := fields["providerExecuted"]
		tc.ProviderExecutedOmitted = !present
	}
	return part, nil
}

// ReadTagged returns the `type` discriminator and the raw top-level fields of
// a JSON object.
func ReadTagged(data []byte) (string, map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", nil, fmt.Errorf("harness: frame is not a JSON object: %w", err)
	}
	if fields == nil {
		return "", nil, errors.New("harness: frame is not a JSON object")
	}
	rawType, ok := fields["type"]
	if !ok {
		return "", nil, errors.New("harness: frame is missing a type discriminator")
	}
	var typ string
	if err := json.Unmarshal(rawType, &typ); err != nil {
		return "", nil, errors.New("harness: frame type must be a string")
	}
	return typ, fields, nil
}

// RequireKeys verifies required keys are present. A key suffixed with "?" may
// be JSON null; other keys must be non-null.
func RequireKeys(typ string, fields map[string]json.RawMessage, required []string) error {
	for _, key := range required {
		nullable := false
		if n := len(key); n > 0 && key[n-1] == '?' {
			key, nullable = key[:n-1], true
		}
		raw, ok := fields[key]
		if !ok || (!nullable && string(raw) == "null") {
			return fmt.Errorf("harness: invalid %s frame: missing required field %q", typ, key)
		}
	}
	return nil
}

func validateStreamPart(part StreamPart) error {
	switch p := part.(type) {
	case *StreamStartPart:
		for _, w := range p.Warnings {
			switch w.Type {
			case CallWarningUnsupportedSetting, CallWarningUnsupportedTool, CallWarningOther:
			default:
				return fmt.Errorf("invalid warning type %q", w.Type)
			}
		}
	case *FinishStepPart:
		return p.FinishReason.validate()
	case *FinishPart:
		return p.FinishReason.validate()
	case *FileChangePart:
		switch p.Event {
		case FileChangeCreate, FileChangeModify, FileChangeDelete:
		default:
			return fmt.Errorf("invalid file-change event %q", p.Event)
		}
	case *CompactionPart:
		switch p.Trigger {
		case CompactionTriggerManual, CompactionTriggerAuto:
		default:
			return fmt.Errorf("invalid compaction trigger %q", p.Trigger)
		}
	}
	return nil
}
