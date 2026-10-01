package harness

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Lifecycle state discriminators.
const (
	LifecycleStateResumeSession = "resume-session"
	LifecycleStateContinueTurn  = "continue-turn"
)

// Pending tool approval kinds.
const (
	PendingToolApprovalBuiltin = "builtin"
	PendingToolApprovalCustom  = "custom"
)

// PendingToolApproval is a framework-owned pending approval record. Mirrors TS
// `HarnessV1PendingToolApproval`.
type PendingToolApproval struct {
	ApprovalID       string `json:"approvalId"`
	ToolCallID       string `json:"toolCallId"`
	ToolName         string `json:"toolName"`
	Input            string `json:"input"`
	Kind             string `json:"kind"` // "builtin" | "custom"
	ProviderExecuted *bool  `json:"providerExecuted,omitempty"`
	NativeName       string `json:"nativeName,omitempty"`
}

// CompletedToolResult is a client tool result executed while suspending, to be
// submitted on resume without running the tool again.
type CompletedToolResult struct {
	Output     any   `json:"output"`
	IsError    *bool `json:"isError,omitempty"`
	ToolResult any   `json:"toolResult,omitempty"`
}

// PendingToolResult is a framework-owned client tool call waiting for a
// caller-provided result. Mirrors TS `HarnessV1PendingToolResult`.
type PendingToolResult struct {
	ToolCallID      string                    `json:"toolCallId"`
	ToolName        string                    `json:"toolName"`
	Input           string                    `json:"input"`
	ProviderOptions map[string]map[string]any `json:"providerOptions,omitempty"`
	CompletedResult *CompletedToolResult      `json:"completedResult,omitempty"`
}

// TurnSettings are framework-owned settings captured when a turn begins.
// Mirrors TS `HarnessV1TurnSettings`. Skills and Tools always serialize as
// arrays.
type TurnSettings struct {
	Model        string     `json:"model,omitempty"`
	Skills       []Skill    `json:"skills"`
	Instructions string     `json:"instructions,omitempty"`
	Tools        []ToolSpec `json:"tools"`
}

// MarshalJSON emits skills/tools as arrays even when nil.
func (s TurnSettings) MarshalJSON() ([]byte, error) {
	type alias TurnSettings
	a := alias(s)
	if a.Skills == nil {
		a.Skills = []Skill{}
	}
	if a.Tools == nil {
		a.Tools = []ToolSpec{}
	}
	return json.Marshal(a)
}

// ResumeSessionState is the opaque payload returned by between-turn lifecycle
// methods and accepted by DoStart{ResumeFrom}. Mirrors TS
// `HarnessV1ResumeSessionState`. Type is always "resume-session".
type ResumeSessionState struct {
	Type                 string          `json:"type"`
	HarnessID            string          `json:"harnessId"`
	SpecificationVersion string          `json:"specificationVersion"`
	Data                 json.RawMessage `json:"data"`
	// ContinueFrom is optional unfinished-turn state.
	ContinueFrom *ContinueTurnState `json:"continueFrom,omitempty"`
}

// ContinueTurnState is the opaque payload returned by DoSuspendTurn and
// accepted by DoStart{ContinueFrom}. Mirrors TS `HarnessV1ContinueTurnState`.
// Type is always "continue-turn".
type ContinueTurnState struct {
	Type                 string                `json:"type"`
	HarnessID            string                `json:"harnessId"`
	SpecificationVersion string                `json:"specificationVersion"`
	Data                 json.RawMessage       `json:"data"`
	PendingToolApprovals []PendingToolApproval `json:"pendingToolApprovals,omitempty"`
	PendingToolResults   []PendingToolResult   `json:"pendingToolResults,omitempty"`
	TurnSettings         *TurnSettings         `json:"turnSettings,omitempty"`
}

// NewResumeSessionState builds a resume-session state; data is marshaled to
// JSON.
func NewResumeSessionState(harnessID string, data any) (*ResumeSessionState, error) {
	raw, err := marshalData(data)
	if err != nil {
		return nil, err
	}
	return &ResumeSessionState{
		Type:                 LifecycleStateResumeSession,
		HarnessID:            harnessID,
		SpecificationVersion: SpecificationVersion,
		Data:                 raw,
	}, nil
}

// NewContinueTurnState builds a continue-turn state; data is marshaled to
// JSON.
func NewContinueTurnState(harnessID string, data any) (*ContinueTurnState, error) {
	raw, err := marshalData(data)
	if err != nil {
		return nil, err
	}
	return &ContinueTurnState{
		Type:                 LifecycleStateContinueTurn,
		HarnessID:            harnessID,
		SpecificationVersion: SpecificationVersion,
		Data:                 raw,
	}, nil
}

func marshalData(data any) (json.RawMessage, error) {
	if raw, ok := data.(json.RawMessage); ok {
		if raw == nil {
			return json.RawMessage("null"), nil
		}
		return raw, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("harness: lifecycle data is not JSON-serializable: %w", err)
	}
	return raw, nil
}

// MarshalJSON guarantees `data` is always present (null when unset).
func (s ResumeSessionState) MarshalJSON() ([]byte, error) {
	type alias ResumeSessionState
	a := alias(s)
	if a.Type == "" {
		a.Type = LifecycleStateResumeSession
	}
	if a.Data == nil {
		a.Data = json.RawMessage("null")
	}
	return json.Marshal(a)
}

// MarshalJSON guarantees `data` is always present (null when unset).
func (s ContinueTurnState) MarshalJSON() ([]byte, error) {
	type alias ContinueTurnState
	a := alias(s)
	if a.Type == "" {
		a.Type = LifecycleStateContinueTurn
	}
	if a.Data == nil {
		a.Data = json.RawMessage("null")
	}
	return json.Marshal(a)
}

// Validate checks the structural invariants of a resume-session state.
func (s *ResumeSessionState) Validate() error {
	if s == nil {
		return errors.New("harness: nil resume-session state")
	}
	if s.Type != LifecycleStateResumeSession {
		return fmt.Errorf("harness: expected lifecycle state type %q, got %q", LifecycleStateResumeSession, s.Type)
	}
	if err := validateLifecycleBase(s.HarnessID, s.SpecificationVersion); err != nil {
		return err
	}
	if s.ContinueFrom != nil {
		return s.ContinueFrom.Validate()
	}
	return nil
}

// Validate checks the structural invariants of a continue-turn state.
func (s *ContinueTurnState) Validate() error {
	if s == nil {
		return errors.New("harness: nil continue-turn state")
	}
	if s.Type != LifecycleStateContinueTurn {
		return fmt.Errorf("harness: expected lifecycle state type %q, got %q", LifecycleStateContinueTurn, s.Type)
	}
	if err := validateLifecycleBase(s.HarnessID, s.SpecificationVersion); err != nil {
		return err
	}
	for _, approval := range s.PendingToolApprovals {
		if approval.Kind != PendingToolApprovalBuiltin && approval.Kind != PendingToolApprovalCustom {
			return fmt.Errorf("harness: invalid pending tool approval kind %q", approval.Kind)
		}
	}
	return nil
}

func validateLifecycleBase(harnessID, specificationVersion string) error {
	if harnessID == "" {
		return errors.New("harness: lifecycle state is missing harnessId")
	}
	if specificationVersion != SpecificationVersion {
		return fmt.Errorf("harness: unsupported lifecycle state specificationVersion %q", specificationVersion)
	}
	return nil
}

// LifecycleState is either a *ResumeSessionState or a *ContinueTurnState
// (TS `HarnessV1LifecycleState`).
type LifecycleState interface {
	lifecycleStateType() string
}

func (*ResumeSessionState) lifecycleStateType() string { return LifecycleStateResumeSession }
func (*ContinueTurnState) lifecycleStateType() string  { return LifecycleStateContinueTurn }

// DecodeLifecycleState parses persisted lifecycle JSON (produced by a Go or
// TS host) into its concrete variant and validates it.
func DecodeLifecycleState(data []byte) (LifecycleState, error) {
	typ, _, err := ReadTagged(data)
	if err != nil {
		return nil, err
	}
	switch typ {
	case LifecycleStateResumeSession:
		var s ResumeSessionState
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, s.Validate()
	case LifecycleStateContinueTurn:
		var s ContinueTurnState
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, s.Validate()
	}
	return nil, fmt.Errorf("harness: unknown lifecycle state type %q", typ)
}
