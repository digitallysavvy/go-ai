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

	// turnSeq/activeTurnID/activeHandoff support ExperimentalSteerTurn.
	// turnSeq/activeTurnID mirror TS `turnSequence`/`activeTurnSequence`:
	// every startTrackedTurn call mints a new turn id, and every later
	// callback (setActivePromptControl/finishTrackedTurn) is a no-op once a
	// newer turn has started or the tracked turn has already ended. Mirrors
	// TS AgentSession's private `turnSequence`/`activeTurnSequence` fields.
	turnSeq       int
	activeTurnID  int
	activeHandoff *steerHandoff

	// suspendedState memoizes DoSuspendTurn's result for the currently
	// running/suspended turn, mirroring TS AgentSession's private
	// `suspendedTurnState` promise cache: captureStopConditionBoundary (a
	// StopWhen early-stop, run_prompt.go's suspendOrFinishNow) and a later
	// explicit SuspendTurn call for the *same* turn must not call the
	// underlying adapter's DoSuspendTurn twice — the second caller simply
	// receives the first call's already-resolved state. Cleared whenever a
	// fresh turn starts (startTrackedTurn).
	suspendedState *ContinueTurnState
}

// steerHandoff carries the in-flight turn's PromptControl to a caller
// blocked in ExperimentalSteerTurn, once runPrompt's DoPromptTurn/
// DoContinueTurn call returns it (or reports that the turn ended, or moved
// out of "running", before ever producing one — ready closes with control
// left nil). Mirrors TS AgentSession's `activePromptControl` promise
// (startTrackedTurn/setPromptControl/waitForPromptControl/
// settleActivePromptControl/clearActivePromptControl).
type steerHandoff struct {
	turnID  int
	ready   chan struct{}
	control PromptControl // valid only after ready is closed
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
// one runPrompt call and returns a turn id: every later
// setActivePromptControl/finishTrackedTurn call for this turn must pass it
// back, so a call that arrives after a newer turn has already started (or
// after this one already ended) is a safe no-op. Mirrors TS
// `startTrackedTurn`.
func (s *AgentSession) startTrackedTurn() int {
	s.mu.Lock()
	s.turnState = TurnStateRunning
	s.turnSeq++
	turnID := s.turnSeq
	s.activeTurnID = turnID
	s.clearActiveHandoffLocked()
	s.activeHandoff = &steerHandoff{turnID: turnID, ready: make(chan struct{})}
	s.suspendedState = nil
	s.mu.Unlock()
	return turnID
}

// setActivePromptControl hands the running turn's PromptControl to any
// caller blocked in ExperimentalSteerTurn. Wired as runPrompt's
// OnPromptControlAvailable callback via a turnID-scoped closure (see
// Agent.startTurn). A no-op once the turn is no longer the active
// "running" one. Mirrors TS `setPromptControl`.
func (s *AgentSession) setActivePromptControl(turnID int, control PromptControl) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnState != TurnStateRunning || s.activeTurnID != turnID || s.activeHandoff == nil || s.activeHandoff.turnID != turnID {
		return
	}
	select {
	case <-s.activeHandoff.ready:
	default:
		s.activeHandoff.control = control
		close(s.activeHandoff.ready)
	}
}

// clearActiveHandoffLocked settles any pending handoff (unblocking a caller
// waiting in ExperimentalSteerTurn with a nil control, which resolves to the
// "no longer targeted" error) and clears it. Callers must hold s.mu. Mirrors
// TS `clearActivePromptControl`.
func (s *AgentSession) clearActiveHandoffLocked() {
	if s.activeHandoff == nil {
		return
	}
	select {
	case <-s.activeHandoff.ready:
	default:
		close(s.activeHandoff.ready)
	}
	s.activeHandoff = nil
}

// finishTrackedTurn returns the session to idle, but only if turnID is still
// the active turn (a stale call — from a turn that a newer startTrackedTurn
// has since superseded — is a no-op, mirroring TS's `activeTurnSequence !==
// options.turnId` guard). Called from runPrompt's OnTurnFinished/OnTurnFailed
// callbacks (via a turnID-scoped closure — see Agent.startTurn) —
// synchronously, from the turn driver's own goroutine, before it signals
// Done — for both a natural finish and a failure. Mirrors TS
// `HarnessAgentSession`'s private `finishTrackedTurn`, which likewise always
// sets `turnState = 'idle'` (when the turnId still matches) regardless of
// what markAwaitingApprovalIfActive/markAwaitingToolResultIfActive set
// earlier: those calls only fire when a turn *pauses* for host input, a case
// in which runPrompt never calls OnTurnFinished/OnTurnFailed (see
// pauseForHostInput), so there is no ordering conflict between the two.
func (s *AgentSession) finishTrackedTurn(turnID int) {
	s.mu.Lock()
	if s.activeTurnID == turnID {
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateIdle
	}
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

// recordPendingApproval records a newly pending approval and — mirroring TS
// `markAwaitingApprovalIfActive` — immediately marks the turn as
// awaiting-approval if it is currently running. It is wired as runPrompt's
// OnPendingToolApproval callback, so this happens synchronously as the turn
// discovers it needs one, not retrospectively once the turn settles.
func (s *AgentSession) recordPendingApproval(a PendingToolApproval) {
	s.mu.Lock()
	s.pendingApprovals[a.ApprovalID] = a
	if s.turnState == TurnStateRunning {
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateAwaitingApproval
	}
	s.mu.Unlock()
}

func (s *AgentSession) settleApproval(approvalID string) {
	s.mu.Lock()
	delete(s.pendingApprovals, approvalID)
	s.mu.Unlock()
}

// recordPendingResult records a newly pending client tool result and marks
// the turn as awaiting-tool-result if it is currently running. Mirrors TS
// `markAwaitingToolResultIfActive`; see recordPendingApproval.
func (s *AgentSession) recordPendingResult(r PendingToolResult) {
	s.mu.Lock()
	s.pendingResults[r.ToolCallID] = r
	if s.turnState == TurnStateRunning {
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateAwaitingResult
	}
	s.mu.Unlock()
}

func (s *AgentSession) settleResult(toolCallID string) {
	s.mu.Lock()
	delete(s.pendingResults, toolCallID)
	s.mu.Unlock()
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

// ExperimentalSteerTurn submits an additional user message to the active
// (running) turn. The underlying runtime accepts the message at its next
// safe input boundary; any output it causes remains part of the active
// turn's result stream, i.e. the caller does not receive a separate result
// for it — it surfaces through the *ai.StreamTextResult already returned by
// the Stream/Generate call that started this turn. Returns
// CapabilityUnsupportedError if the harness's PromptControl does not
// implement UserMessageSubmitter. Mirrors TS
// `HarnessAgentSession.experimental_steerTurn`.
func (s *AgentSession) ExperimentalSteerTurn(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.turnState != TurnStateRunning || s.activeHandoff == nil {
		s.mu.Unlock()
		return fmt.Errorf("harness session '%s' has no running turn to steer", s.sessionID)
	}
	handoff := s.activeHandoff
	s.mu.Unlock()

	var control PromptControl
	select {
	case <-handoff.ready:
		control = handoff.control
	case <-ctx.Done():
		return ctx.Err()
	}

	s.mu.Lock()
	stillTargeted := s.sessionState == SessionStateActive && s.turnState == TurnStateRunning && s.activeHandoff == handoff
	s.mu.Unlock()
	if !stillTargeted || control == nil {
		return fmt.Errorf("harness session '%s' no longer has the running turn targeted for steering", s.sessionID)
	}

	submitter, ok := control.(UserMessageSubmitter)
	if !ok {
		return NewCapabilityUnsupportedError(
			fmt.Sprintf("Harness '%s' does not support steering active turns.", s.harness.HarnessID()),
			s.harness.HarnessID(), nil,
		)
	}
	return submitter.SubmitUserMessage(ctx, text)
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
	// A StopWhen early-stop (suspendOrFinishNow -> captureStopConditionBoundary)
	// may have already suspended this exact turn and cached the result —
	// reuse it instead of calling the underlying adapter's DoSuspendTurn a
	// second time, mirroring TS `suspendCurrentTurn`'s
	// `this.suspendedTurnState ??= ...` memoization.
	if s.suspendedState != nil {
		state := s.suspendedState
		s.turnState = TurnStateSuspended
		s.mu.Unlock()
		return state, nil
	}
	s.clearActiveHandoffLocked()
	s.mu.Unlock()

	state, err := s.underlying.DoSuspendTurn(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.suspendedState = state
	s.turnState = TurnStateSuspended
	s.mu.Unlock()
	return state, nil
}

// captureStopConditionBoundary suspends the underlying harness session's
// turn in place when a StopWhen condition matches after a completed step,
// keeping it resumable via ContinueGenerate/ContinueStream instead of
// discarding it. Wired as runPrompt's OnStopConditionMet callback
// (turnID-scoped, like OnTurnFinished/OnPromptControlAvailable — see
// Agent.startTurn). A no-op (returns nil, nil) once turnID is no longer the
// active "running" turn, mirroring TS's identical guard in
// `AgentSession.captureStopConditionBoundary`: by the time this runs, a
// concurrent Detach/Stop/Destroy (or, in principle, a fresher turn) may
// already have superseded it. See run_prompt.go's suspendOrFinishNow doc
// for what TS behavior around this is deferred to WG13/bridge adapters.
func (s *AgentSession) captureStopConditionBoundary(ctx context.Context, turnID int) (*ContinueTurnState, error) {
	s.mu.Lock()
	if s.sessionState != SessionStateActive || s.activeTurnID != turnID {
		s.mu.Unlock()
		return nil, nil
	}
	if s.suspendedState != nil {
		state := s.suspendedState
		s.mu.Unlock()
		return state, nil
	}
	s.mu.Unlock()

	state, err := s.underlying.DoSuspendTurn(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	// Re-check after the (possibly slow) adapter call: a concurrent
	// Detach/Stop/Destroy or a newer turn may have superseded this one while
	// DoSuspendTurn was in flight. The adapter has already frozen its side
	// regardless, so the state is still returned to the caller (matching TS,
	// which awaits and returns `state` from `suspendCurrentTurn` even when
	// its own later steps no-op) — only this session's own bookkeeping is
	// skipped.
	if s.sessionState == SessionStateActive && s.activeTurnID == turnID {
		s.suspendedState = state
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateSuspended
	}
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
	s.clearActiveHandoffLocked()
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
	s.clearActiveHandoffLocked()
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
	s.clearActiveHandoffLocked()
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
