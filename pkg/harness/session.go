package harness

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SessionState is the lifecycle state of an AgentSession's underlying
// sandbox/runtime attachment.
type SessionState string

const (
	SessionStateActive    SessionState = "active"
	SessionStateDetached  SessionState = "detached"
	SessionStateStopped   SessionState = "stopped"
	SessionStateDestroyed SessionState = "destroyed"
)

// TurnState is the lifecycle state of the turn currently associated with an
// AgentSession. Mirrors TS `HarnessAgentTurnState`.
type TurnState string

const (
	TurnStateIdle             TurnState = "idle"
	TurnStateRunning          TurnState = "running"
	TurnStateAwaitingApproval TurnState = "awaiting-approval"
	TurnStateAwaitingResult   TurnState = "awaiting-tool-result"
	TurnStateSuspended        TurnState = "suspended"
)

// AgentSession is the consumer handle for one harness session, returned by
// HarnessAgent.CreateSession. Mirrors TS `HarnessAgentSession`.
//
// Exactly one turn may be in flight at a time. Generate/Stream (fresh
// prompts) require TurnState idle; ContinueGenerate/ContinueStream require
// awaiting-approval, awaiting-tool-result or suspended.
type AgentSession struct {
	mu sync.Mutex

	sessionID            string
	harness              Harness
	underlying           Session
	sandboxSession       providerutils.SandboxSession
	ownsSandboxLifecycle bool
	sessionWorkDir       string
	toolApproval         ToolApprovalConfiguration

	pendingApprovals map[string]PendingToolApproval
	pendingResults   map[string]PendingToolResult
	turnSettings     *TurnSettings

	sessionState SessionState
	turnState    TurnState

	resumedToolsContext map[string]interface{}
}

// AgentSessionOptions is the input of newAgentSession.
type AgentSessionOptions struct {
	SessionID            string
	Harness              Harness
	Underlying           Session
	SandboxSession       providerutils.SandboxSession
	OwnsSandboxLifecycle bool
	SessionWorkDir       string
	ToolApproval         ToolApprovalConfiguration
	PendingToolApprovals []PendingToolApproval
	PendingToolResults   []PendingToolResult
	TurnSettings         *TurnSettings
	ResumedToolsContext  map[string]interface{}
	TurnState            TurnState
}

func newAgentSession(opts AgentSessionOptions) *AgentSession {
	s := &AgentSession{
		sessionID:            opts.SessionID,
		harness:              opts.Harness,
		underlying:           opts.Underlying,
		sandboxSession:       opts.SandboxSession,
		ownsSandboxLifecycle: opts.OwnsSandboxLifecycle,
		sessionWorkDir:       opts.SessionWorkDir,
		toolApproval:         opts.ToolApproval,
		pendingApprovals:     map[string]PendingToolApproval{},
		pendingResults:       map[string]PendingToolResult{},
		turnSettings:         opts.TurnSettings,
		sessionState:         SessionStateActive,
		turnState:            opts.TurnState,
		resumedToolsContext:  opts.ResumedToolsContext,
	}
	if s.turnState == "" {
		s.turnState = TurnStateIdle
	}
	for _, a := range opts.PendingToolApprovals {
		s.pendingApprovals[a.ApprovalID] = a
	}
	for _, r := range opts.PendingToolResults {
		s.pendingResults[r.ToolCallID] = r
	}
	return s
}

// SessionID returns the stable identifier this session was created or
// resumed with.
func (s *AgentSession) SessionID() string { return s.sessionID }

// GetSandboxSession returns the sandbox session this harness session runs
// in.
func (s *AgentSession) GetSandboxSession() providerutils.SandboxSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sandboxSession
}

// GetSessionWorkDir returns the absolute path the adapter runs the agent in.
func (s *AgentSession) GetSessionWorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionWorkDir
}

// HasUnfinishedTurn reports whether a turn is running or paused (awaiting an
// approval, a tool result, or explicitly suspended).
func (s *AgentSession) HasUnfinishedTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnState != TurnStateIdle
}

// requirePromptableTurn mirrors TS `requirePromptableTurn`.
func (s *AgentSession) requirePromptableTurn() error {
	switch s.turnState {
	case TurnStateIdle:
		return nil
	case TurnStateRunning:
		return fmt.Errorf("harness session '%s' already has a turn in progress", s.sessionID)
	default:
		return fmt.Errorf("harness session '%s' has an unfinished turn; call ContinueGenerate/ContinueStream instead", s.sessionID)
	}
}

// requireContinuableTurn mirrors TS `requireContinuableTurn`.
func (s *AgentSession) requireContinuableTurn() error {
	switch s.turnState {
	case TurnStateAwaitingApproval, TurnStateAwaitingResult, TurnStateSuspended:
		return nil
	case TurnStateRunning:
		return fmt.Errorf("harness session '%s' already has a turn in progress", s.sessionID)
	default:
		return fmt.Errorf("harness session '%s' has no unfinished turn to continue", s.sessionID)
	}
}

// startTrackedTurn transitions the session to "running" for the duration of
// one runPrompt call.
func (s *AgentSession) startTrackedTurn() {
	s.mu.Lock()
	s.turnState = TurnStateRunning
	s.mu.Unlock()
}

// finishTrackedTurn records the outcome of a completed runPrompt call:
// TurnStateIdle when the turn naturally finished, or the state matching
// whatever it is now waiting on.
func (s *AgentSession) finishTrackedTurn(next TurnState) {
	s.mu.Lock()
	s.turnState = next
	s.mu.Unlock()
}

// snapshotPendingState returns copies of the pending approval/result maps
// (safe to read after the mutex is released) plus the persisted turn
// settings for a resumed turn.
func (s *AgentSession) snapshotPendingState() ([]PendingToolApproval, []PendingToolResult, *TurnSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	approvals := make([]PendingToolApproval, 0, len(s.pendingApprovals))
	for _, a := range s.pendingApprovals {
		approvals = append(approvals, a)
	}
	results := make([]PendingToolResult, 0, len(s.pendingResults))
	for _, r := range s.pendingResults {
		results = append(results, r)
	}
	return approvals, results, s.turnSettings
}

func (s *AgentSession) recordPendingApproval(a PendingToolApproval) {
	s.mu.Lock()
	s.pendingApprovals[a.ApprovalID] = a
	s.mu.Unlock()
}

func (s *AgentSession) settleApproval(approvalID string) {
	s.mu.Lock()
	delete(s.pendingApprovals, approvalID)
	s.mu.Unlock()
}

func (s *AgentSession) recordPendingResult(r PendingToolResult) {
	s.mu.Lock()
	s.pendingResults[r.ToolCallID] = r
	s.mu.Unlock()
}

func (s *AgentSession) settleResult(toolCallID string) {
	s.mu.Lock()
	delete(s.pendingResults, toolCallID)
	s.mu.Unlock()
}

// resolveTurnState computes the TurnState a paused/finished turn leaves the
// session in, from its own pending maps. Mirrors the ternary in TS
// `createSession`'s initial turnState computation, reused here after every
// turn.
func (s *AgentSession) resolveTurnState(finished bool) TurnState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if finished {
		return TurnStateIdle
	}
	if len(s.pendingApprovals) > 0 {
		return TurnStateAwaitingApproval
	}
	if len(s.pendingResults) > 0 {
		return TurnStateAwaitingResult
	}
	return TurnStateSuspended
}

// Compact requests that the runtime compact its context. Mirrors TS
// `HarnessAgentSession.compact`.
func (s *AgentSession) Compact(ctx context.Context, customInstructions string) error {
	s.mu.Lock()
	turnState := s.turnState
	s.mu.Unlock()
	if turnState != TurnStateIdle {
		return fmt.Errorf("harness session '%s': cannot compact while a turn is in progress or unfinished", s.sessionID)
	}
	return s.underlying.DoCompact(ctx, customInstructions)
}

// SuspendTurn freezes the active turn at a precise cursor while keeping the
// runtime alive, and returns the continuation payload the caller must
// persist to resume it later (directly, or embedded in the ResumeSessionState
// returned by Detach/Stop). Mirrors TS `HarnessAgentSession.suspendTurn`.
func (s *AgentSession) SuspendTurn(ctx context.Context) (*ContinueTurnState, error) {
	s.mu.Lock()
	if s.turnState == TurnStateIdle {
		s.mu.Unlock()
		return nil, fmt.Errorf("harness session '%s': no unfinished turn to suspend", s.sessionID)
	}
	s.mu.Unlock()

	state, err := s.underlying.DoSuspendTurn(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.turnState = TurnStateSuspended
	s.mu.Unlock()
	return state, nil
}

// Detach detaches from the runtime without tearing it down, returning a
// payload that can later be passed to HarnessAgent.CreateSession's
// ResumeFrom. Mirrors TS `HarnessAgentSession.detach`.
func (s *AgentSession) Detach(ctx context.Context) (*ResumeSessionState, error) {
	s.mu.Lock()
	if s.turnState != TurnStateIdle {
		s.mu.Unlock()
		return nil, fmt.Errorf("harness session '%s': cannot detach with an unfinished turn; suspend it first", s.sessionID)
	}
	s.mu.Unlock()

	state, err := s.underlying.DoDetach(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sessionState = SessionStateDetached
	s.mu.Unlock()
	return state, nil
}

// Stop persists enough state to resume later, then stops the runtime.
// Mirrors TS `HarnessAgentSession.stop`.
func (s *AgentSession) Stop(ctx context.Context) (*ResumeSessionState, error) {
	s.mu.Lock()
	if s.turnState != TurnStateIdle {
		s.mu.Unlock()
		return nil, fmt.Errorf("harness session '%s': cannot stop with an unfinished turn; suspend it first", s.sessionID)
	}
	s.mu.Unlock()

	state, err := s.underlying.DoStop(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sessionState = SessionStateStopped
	sandbox, owns := s.sandboxSession, s.ownsSandboxLifecycle
	s.mu.Unlock()
	if owns {
		stopSandbox(ctx, sandbox)
	}
	return state, nil
}

// Destroy stops the runtime without returning lifecycle state. Mirrors TS
// `HarnessAgentSession.destroy`.
func (s *AgentSession) Destroy(ctx context.Context) error {
	err := s.underlying.DoDestroy(ctx)
	s.mu.Lock()
	s.sessionState = SessionStateDestroyed
	sandbox, owns := s.sandboxSession, s.ownsSandboxLifecycle
	s.mu.Unlock()
	if owns {
		stopSandbox(ctx, sandbox)
	}
	return err
}

func stopSandbox(ctx context.Context, sandbox providerutils.SandboxSession) {
	if network, ok := AsNetworkSandboxSession(sandbox); ok {
		_ = network.Stop(ctx)
	}
}
