// Package bridge contains the host side of the harness-v1 bridge wire
// protocol (TS `harness-v1-bridge-protocol.ts`): the JSON frames exchanged
// between a Go host and the unchanged TypeScript in-sandbox `bridge.mjs` over
// a WebSocket, plus the stdout `bridge-ready` handshake line.
//
// Outbound frames (bridge -> host) are every harness.StreamPart plus the
// transport/control frames defined here. Inbound frames (host -> bridge) are
// the adapter `start` frame (which embeds StartBase) and the shared commands.
package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Outbound transport/control frame discriminators.
const (
	TypeHello               = "bridge-hello"
	TypeUserMessageResponse = "user-message-response"
	TypeStop                = "bridge-stop"
	TypeThread              = "bridge-thread"
	TypeSandboxLog          = "sandbox-log"
	TypeDebugEvent          = "debug-event"
	TypeReady               = "bridge-ready"
)

// Inbound command discriminators. `stop`/`destroy` replaced the former
// `detach`/`shutdown` names in 8f10600.
const (
	TypeStart                = "start"
	TypeToolResult           = "tool-result"
	TypeToolApprovalResponse = "tool-approval-response"
	TypeUserMessage          = "user-message"
	TypeAbort                = "abort"
	TypeDestroyCommand       = "destroy"
	TypeResume               = "resume"
	TypeStopCommand          = "stop"
)

// ---------------------------------------------------------------------------
// Outbound (bridge -> host)
// ---------------------------------------------------------------------------

// OutboundMessage is any frame a bridge can send to the host: a
// harness.StreamPart or one of the control frames below. Mirrors
// `harnessV1BridgeOutboundMessageSchema`.
type OutboundMessage interface {
	FrameType() string
}

// StreamPartFrame wraps a stream part received from the bridge.
type StreamPartFrame struct {
	Part harness.StreamPart
}

// FrameType returns the stream part discriminator.
func (f StreamPartFrame) FrameType() string { return f.Part.PartType() }

// HelloCapabilities are the capabilities announced on bridge-hello.
type HelloCapabilities struct {
	ExperimentalUserMessageResponses *bool `json:"experimental_userMessageResponses,omitempty"`
}

// Hello is sent the instant the bridge accepts an authenticated connection.
// The host waits for it before sending start/resume.
type Hello struct {
	State        string             `json:"state,omitempty"`
	LastSeq      *float64           `json:"lastSeq,omitempty"`
	Capabilities *HelloCapabilities `json:"capabilities,omitempty"`
}

// UserMessageResponseError is the error payload of UserMessageResponse.
type UserMessageResponseError struct {
	Message string `json:"message"`
}

// UserMessageResponse acknowledges an experimental user-message (TS
// `experimental_harnessV1BridgeUserMessageResponseSchema`).
type UserMessageResponse struct {
	MessageID string                    `json:"messageId"`
	Accepted  bool                      `json:"accepted"`
	Error     *UserMessageResponseError `json:"error,omitempty"`
}

// Stop is the bridge's reply to an inbound `stop`; Data is serialized into
// lifecycle state `data`.
type Stop struct {
	Data json.RawMessage `json:"data"`
}

// Thread is a resume coordinate the bridge proactively announces.
type Thread struct {
	ThreadID string `json:"threadId"`
}

// SandboxLog is one captured console line from inside the sandbox.
type SandboxLog struct {
	Source string `json:"source"`
	Stream string `json:"stream"` // "stdout" | "stderr"
	Line   string `json:"line"`
}

// DebugEvent is a structured diagnostic emitted from inside the bridge.
type DebugEvent struct {
	Level     harness.DebugLevel       `json:"level"`
	Subsystem string                   `json:"subsystem"`
	Message   string                   `json:"message"`
	Attrs     map[string]any           `json:"attrs,omitempty"`
	Error     *harness.DiagnosticError `json:"error,omitempty"`
}

func (*Hello) FrameType() string               { return TypeHello }
func (*UserMessageResponse) FrameType() string { return TypeUserMessageResponse }
func (*Stop) FrameType() string                { return TypeStop }
func (*Thread) FrameType() string              { return TypeThread }
func (*SandboxLog) FrameType() string          { return TypeSandboxLog }
func (*DebugEvent) FrameType() string          { return TypeDebugEvent }

// MarshalJSON guarantees `data` is present.
func (s Stop) MarshalJSON() ([]byte, error) {
	type alias Stop
	a := alias(s)
	if a.Data == nil {
		a.Data = json.RawMessage("null")
	}
	return json.Marshal(a)
}

// DecodeOutbound parses and validates one bridge->host frame. The optional
// `seq` cursor is returned separately (zod strips it from the parsed value).
func DecodeOutbound(data []byte) (msg OutboundMessage, seq *float64, err error) {
	typ, fields, err := harness.ReadTagged(data)
	if err != nil {
		return nil, nil, err
	}
	if rawSeq, ok := fields["seq"]; ok {
		var s float64
		if json.Unmarshal(rawSeq, &s) == nil {
			seq = &s
		}
	}
	if harness.IsStreamPartType(typ) {
		part, err := harness.DecodeStreamPart(data)
		if err != nil {
			return nil, seq, err
		}
		return StreamPartFrame{Part: part}, seq, nil
	}

	var target OutboundMessage
	var required []string
	switch typ {
	case TypeHello:
		target = &Hello{}
	case TypeUserMessageResponse:
		target = &UserMessageResponse{}
		required = []string{"messageId", "accepted"}
	case TypeStop:
		target = &Stop{}
	case TypeThread:
		target = &Thread{}
		required = []string{"threadId"}
	case TypeSandboxLog:
		target = &SandboxLog{}
		required = []string{"source", "stream", "line"}
	case TypeDebugEvent:
		target = &DebugEvent{}
		required = []string{"level", "subsystem", "message"}
	default:
		return nil, seq, fmt.Errorf("%w %q", harness.ErrUnknownPartType, typ)
	}
	if err := harness.RequireKeys(typ, fields, required); err != nil {
		return nil, seq, err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return nil, seq, fmt.Errorf("harness bridge: invalid %s frame: %w", typ, err)
	}
	switch m := target.(type) {
	case *SandboxLog:
		if m.Stream != "stdout" && m.Stream != "stderr" {
			return nil, seq, fmt.Errorf("harness bridge: invalid sandbox-log stream %q", m.Stream)
		}
	case *DebugEvent:
		if !m.Level.Valid() {
			return nil, seq, fmt.Errorf("harness bridge: invalid debug-event level %q", m.Level)
		}
		if raw, ok := fields["error"]; ok && string(raw) != "null" {
			if err := harness.RequireKeys("debug-event error", readObject(raw), []string{"message"}); err != nil {
				return nil, seq, err
			}
		}
	case *Stop:
		if raw, ok := fields["data"]; ok {
			m.Data = raw
		}
	}
	return target, seq, nil
}

// MarshalOutbound encodes a bridge->host frame (used by fake bridges in
// tests and by tooling).
func MarshalOutbound(msg OutboundMessage) ([]byte, error) {
	if f, ok := msg.(StreamPartFrame); ok {
		return harness.MarshalStreamPart(f.Part)
	}
	return harness.MarshalTagged(msg.FrameType(), msg)
}

// DiagnosticContext supplies the fields DiagnosticFromFrame cannot derive.
type DiagnosticContext struct {
	SessionID string
	// Timestamp in epoch milliseconds.
	Timestamp int64
}

// DiagnosticFromFrame normalizes a sandbox-log or debug-event frame into a
// harness.Diagnostic. A console line maps stderr -> warn and stdout -> info.
// Mirrors TS `harnessV1DiagnosticFromBridgeFrame`.
func DiagnosticFromFrame(frame OutboundMessage, dc DiagnosticContext) (harness.Diagnostic, bool) {
	switch f := frame.(type) {
	case *SandboxLog:
		level := harness.DebugLevelInfo
		if f.Stream == "stderr" {
			level = harness.DebugLevelWarn
		}
		return harness.Diagnostic{
			Level:     level,
			Message:   f.Line,
			Subsystem: "sandbox.log." + f.Source,
			Kind:      "log",
			Source:    f.Source,
			Stream:    f.Stream,
			SessionID: dc.SessionID,
			Timestamp: dc.Timestamp,
		}, true
	case *DebugEvent:
		return harness.Diagnostic{
			Level:     f.Level,
			Message:   f.Message,
			Subsystem: f.Subsystem,
			Kind:      "event",
			Attrs:     f.Attrs,
			Error:     f.Error,
			SessionID: dc.SessionID,
			Timestamp: dc.Timestamp,
		}, true
	}
	return harness.Diagnostic{}, false
}

// ReportDiagnostic builds a ChannelOptions.OnDiagnostic callback that
// normalizes sandbox-log/debug-event frames via DiagnosticFromFrame and
// forwards them to report (e.g. harness.Observability.Report), stamping
// each with sessionID and the time it was observed. Returns nil when report
// is nil, so callers can assign the result directly without a nil check.
// Mirrors TS adapters' `onDiagnostic` closure (built from
// `harnessV1DiagnosticFromBridgeFrame`).
func ReportDiagnostic(report func(harness.Diagnostic), sessionID string) func(OutboundMessage) {
	if report == nil {
		return nil
	}
	return func(frame OutboundMessage) {
		if d, ok := DiagnosticFromFrame(frame, DiagnosticContext{SessionID: sessionID, Timestamp: time.Now().UnixMilli()}); ok {
			report(d)
		}
	}
}

// Ready is the JSON line the bridge writes to stdout once its WebSocket
// server is bound (`harnessV1BridgeReadySchema`).
type Ready struct {
	Port int `json:"port"`
}

// DecodeReady parses a `bridge-ready` stdout line.
func DecodeReady(line []byte) (*Ready, error) {
	typ, fields, err := harness.ReadTagged(line)
	if err != nil {
		return nil, err
	}
	if typ != TypeReady {
		return nil, fmt.Errorf("harness bridge: expected %s, got %q", TypeReady, typ)
	}
	if err := harness.RequireKeys(typ, fields, []string{"port"}); err != nil {
		return nil, err
	}
	var r Ready
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// MarshalReady encodes a `bridge-ready` line.
func MarshalReady(r Ready) ([]byte, error) { return harness.MarshalTagged(TypeReady, r) }

// ---------------------------------------------------------------------------
// Inbound (host -> bridge)
// ---------------------------------------------------------------------------

// InboundCommand is a host->bridge frame. Encode with MarshalInbound.
type InboundCommand interface {
	FrameType() string
}

// StartBase holds the common fields of the inbound `start` frame
// (`harnessV1BridgeStartBaseSchema`). Adapter start frames embed it and add
// their runtime-specific fields; encoding/json flattens embedded structs so
// the wire shape matches the TS `.extend(...)` schema.
type StartBase struct {
	Prompt               string                        `json:"prompt"`
	Tools                []harness.ToolSpec            `json:"tools,omitempty"`
	Model                string                        `json:"model,omitempty"`
	Debug                *harness.DebugConfig          `json:"debug,omitempty"`
	PermissionMode       harness.PermissionMode        `json:"permissionMode,omitempty"`
	BuiltinToolFiltering *harness.BuiltinToolFiltering `json:"builtinToolFiltering,omitempty"`
	ResponseFormat       *harness.ResponseFormat       `json:"responseFormat,omitempty"`
}

// FrameType returns "start".
func (StartBase) FrameType() string { return TypeStart }

// ToolResultCommand submits a host tool result.
type ToolResultCommand struct {
	ToolCallID string `json:"toolCallId"`
	Output     any    `json:"output"`
	IsError    bool   `json:"isError,omitempty"`
	ToolResult any    `json:"toolResult,omitempty"`
}

// ToolApprovalResponseCommand answers a tool-approval-request.
type ToolApprovalResponseCommand struct {
	ApprovalID string `json:"approvalId"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"`
}

// UserMessageCommand injects a mid-turn user message. MessageID is required
// for the experimental acknowledged variant.
type UserMessageCommand struct {
	MessageID string `json:"messageId,omitempty"`
	Text      string `json:"text"`
}

// AbortCommand aborts the in-flight turn.
type AbortCommand struct{}

// DestroyCommand tears the bridge down without returning state.
type DestroyCommand struct{}

// ResumeCommand asks the bridge to replay every buffered event with
// seq > LastSeenEventID after a reconnect.
type ResumeCommand struct {
	LastSeenEventID float64 `json:"lastSeenEventId"`
}

// StopCommand asks the bridge to reply with bridge-stop and exit.
type StopCommand struct{}

func (ToolResultCommand) FrameType() string           { return TypeToolResult }
func (ToolApprovalResponseCommand) FrameType() string { return TypeToolApprovalResponse }
func (UserMessageCommand) FrameType() string          { return TypeUserMessage }
func (AbortCommand) FrameType() string                { return TypeAbort }
func (DestroyCommand) FrameType() string              { return TypeDestroyCommand }
func (ResumeCommand) FrameType() string               { return TypeResume }
func (StopCommand) FrameType() string                 { return TypeStopCommand }

// MarshalInbound encodes a host->bridge frame with its `type` first. Adapter
// start frames (structs embedding StartBase) inherit FrameType "start".
func MarshalInbound(cmd InboundCommand) ([]byte, error) {
	return harness.MarshalTagged(cmd.FrameType(), cmd)
}

// ValidateExperimentalUserMessage mirrors
// `experimental_harnessV1BridgeUserMessageInboundSchema`: messageId is
// required.
func ValidateExperimentalUserMessage(cmd UserMessageCommand) error {
	if cmd.MessageID == "" {
		return errors.New("harness bridge: experimental user-message requires a messageId")
	}
	return nil
}

// DecodeInbound parses one shared host->bridge command (not `start`, whose
// shape is adapter-specific). Useful for fake bridges in tests.
func DecodeInbound(data []byte) (InboundCommand, error) {
	typ, fields, err := harness.ReadTagged(data)
	if err != nil {
		return nil, err
	}
	var required []string
	var cmd InboundCommand
	switch typ {
	case TypeToolResult:
		cmd, required = &ToolResultCommand{}, []string{"toolCallId"}
	case TypeToolApprovalResponse:
		cmd, required = &ToolApprovalResponseCommand{}, []string{"approvalId", "approved"}
	case TypeUserMessage:
		cmd, required = &UserMessageCommand{}, []string{"text"}
	case TypeAbort:
		return AbortCommand{}, nil
	case TypeDestroyCommand:
		return DestroyCommand{}, nil
	case TypeStopCommand:
		return StopCommand{}, nil
	case TypeResume:
		cmd, required = &ResumeCommand{}, []string{"lastSeenEventId"}
	case TypeStart:
		cmd, required = &StartBase{}, []string{"prompt"}
	default:
		return nil, fmt.Errorf("harness bridge: unknown inbound command %q", typ)
	}
	if err := harness.RequireKeys(typ, fields, required); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cmd); err != nil {
		return nil, fmt.Errorf("harness bridge: invalid %s command: %w", typ, err)
	}
	return cmd, nil
}

func readObject(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	return m
}
