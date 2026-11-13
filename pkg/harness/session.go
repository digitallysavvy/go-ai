package harness

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	// approvalOrder/resultOrder keep insertion order (TS Maps iterate in
	// insertion order), so persisted pending lists are deterministic.
	approvalOrder []string
	resultOrder   []string
	// turnSettings are the persisted settings of the unfinished turn
	// (TS `persistedTurnSettings`).
	turnSettings *TurnSettings
	// activeTurn caches the resolved settings (tools, contexts) of the
	// unfinished turn (TS `activeTurnSettings`).
	activeTurn *activeTurnSettings
	// activePromptDone is closed when the driver of the current turn has
	// finished (TS `activePromptDone`).
	activePromptDone <-chan struct{}

	sessionState SessionState
	turnState    TurnState

	resumedToolsContext   map[string]interface{}
	resumedRuntimeContext interface{}

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
	suspend *suspendCall
}

// suspendCall memoizes one DoSuspendTurn invocation (TS
// `suspendedTurnState`, a cached promise). The error is memoized too, like a
// rejected promise.
type suspendCall struct {
	done  chan struct{}
	state *ContinueTurnState
	err   error
}

// activeTurnSettings are the resolved settings of the unfinished turn. Mirrors
// TS `ActiveTurnSettings`.
type activeTurnSettings struct {
	persisted            TurnSettings
	tools                map[string]types.Tool
	toolsContext         map[string]interface{}
	runtimeContext       interface{}
	activeTools          map[string]types.Tool
	builtinToolFiltering *BuiltinToolFiltering
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
	SessionID             string
	Harness               Harness
	Underlying            Session
	SandboxSession        providerutils.SandboxSession
	OwnsSandboxLifecycle  bool
	SessionWorkDir        string
	ToolApproval          ToolApprovalConfiguration
	PendingToolApprovals  []PendingToolApproval
	PendingToolResults    []PendingToolResult
	TurnSettings          *TurnSettings
	ResumedToolsContext   map[string]interface{}
	ResumedRuntimeContext interface{}
	TurnState             TurnState
}

func newAgentSession(opts AgentSessionOptions) *AgentSession {
	s := &AgentSession{
		sessionID:             opts.SessionID,
		harness:               opts.Harness,
		underlying:            opts.Underlying,
		sandboxSession:        opts.SandboxSession,
		ownsSandboxLifecycle:  opts.OwnsSandboxLifecycle,
		sessionWorkDir:        opts.SessionWorkDir,
		toolApproval:          opts.ToolApproval,
		pendingApprovals:      map[string]PendingToolApproval{},
		pendingResults:        map[string]PendingToolResult{},
		turnSettings:          opts.TurnSettings,
		sessionState:          SessionStateActive,
		turnState:             opts.TurnState,
		resumedToolsContext:   opts.ResumedToolsContext,
		resumedRuntimeContext: opts.ResumedRuntimeContext,
	}
	if s.turnState == "" {
		s.turnState = TurnStateIdle
	}
	for _, a := range opts.PendingToolApprovals {
		s.setApprovalLocked(a)
	}
	for _, r := range opts.PendingToolResults {
		s.setResultLocked(r)
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

// requirePromptableTurnLocked mirrors TS `requirePromptableTurn`. Callers
// must hold s.mu.
func (s *AgentSession) requirePromptableTurnLocked() error {
	switch s.turnState {
	case TurnStateIdle:
		return nil
	case TurnStateRunning:
		return fmt.Errorf("Harness session %s already has a turn in progress.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	default:
		return fmt.Errorf("Harness session %s has an unfinished turn and must be continued before accepting a new prompt.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
}

// requireContinuableTurnLocked mirrors TS `requireContinuableTurn`. Callers
// must hold s.mu.
func (s *AgentSession) requireContinuableTurnLocked() error {
	switch s.turnState {
	case TurnStateAwaitingApproval, TurnStateAwaitingResult, TurnStateSuspended:
		return nil
	case TurnStateRunning:
		return fmt.Errorf("Harness session %s already has a turn in progress.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	default:
		return fmt.Errorf("Harness session %s has no unfinished turn to continue.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
}

// startTrackedTurn atomically checks that the session may accept this turn
// (requirePromptableTurnLocked for a fresh prompt, requireContinuableTurnLocked
// for a continuation — selected by mode == "continue") and, only if the guard
// passes, transitions the session to "running" and returns a turn id: every
// later setActivePromptControl/finishTrackedTurn call for this turn must pass
// it back, so a call that arrives after a newer turn has already started (or
// after this one already ended) is a safe no-op.
//
// The guard check and the state transition happen under a single critical
// section (s.mu held throughout) so two concurrent callers can never both
// observe an idle/continuable turnState and both start a turn: whichever
// caller acquires s.mu first flips turnState to "running", and the other
// caller's guard check (now serialized behind the same lock) necessarily
// observes "running" and is rejected. This mirrors TS `promptTurn`/
// `continueTurn`, where `requirePromptableTurn()`/`requireContinuableTurn()`
// and `startTrackedTurn()` are adjacent synchronous statements with no
// `await` between them — JS's run-to-completion semantics make that pairing
// atomic for free. Go has real concurrency, so the equivalent atomicity must
// be provided explicitly, by performing both steps while holding s.mu rather
// than checking early (e.g. before PrepareCall/tool-filtering work) and
// transitioning only afterward, which leaves a window where two calls can
// both pass the guard.
func (s *AgentSession) startTrackedTurn(mode string, cur *activeTurnSettings) (int, *activeTurnSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireReusableLocked(); err != nil {
		return 0, nil, err
	}
	var err error
	var active *activeTurnSettings
	if mode == "continue" {
		if err = s.requireContinuableTurnLocked(); err == nil {
			active, err = s.resolveActiveTurnSettingsLocked(cur)
		}
	} else {
		if err = s.requirePromptableTurnLocked(); err == nil {
			// TS promptTurn: the turn's settings are persisted so a suspend,
			// detach or stop can carry them in the continuation.
			persisted := cur.persisted
			s.turnSettings = &persisted
			s.activeTurn = cur
			active = cur
		}
	}
	if err != nil {
		return 0, nil, err
	}
	s.turnState = TurnStateRunning
	s.turnSeq++
	turnID := s.turnSeq
	s.activeTurnID = turnID
	s.clearActiveHandoffLocked()
	s.activeHandoff = &steerHandoff{turnID: turnID, ready: make(chan struct{})}
	s.suspend = nil
	return turnID, active, nil
}

// setActivePromptDone records the done channel of turn turnID's driver, unless
// a newer turn has started meanwhile.
func (s *AgentSession) setActivePromptDone(turnID int, done <-chan struct{}) {
	s.mu.Lock()
	if s.activeTurnID == turnID {
		s.activePromptDone = done
	}
	s.mu.Unlock()
}

// isTurnSuspending reports whether turnID is the active turn and the host
// has suspended it. Mirrors TS `isTurnSuspending`.
func (s *AgentSession) isTurnSuspending(turnID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTurnID == turnID && s.suspend != nil
}

// requireReusableLocked mirrors TS `requireReusableSession`.
func (s *AgentSession) requireReusableLocked() error {
	if s.sessionState != SessionStateActive || s.underlying == nil {
		return fmt.Errorf("Harness session %s has ended and cannot be reused.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// resolveActiveTurnSettingsLocked mirrors TS `resolveActiveTurnSettings`: a
// continuation reuses the settings of the turn started in this process; a
// session resumed from persisted state rebuilds them from the persisted
// turn settings and requires every persisted tool to still be available.
func (s *AgentSession) resolveActiveTurnSettingsLocked(cur *activeTurnSettings) (*activeTurnSettings, error) {
	if s.activeTurn != nil {
		return s.activeTurn, nil
	}
	persisted := s.turnSettings
	if persisted == nil {
		p := cur.persisted
		persisted = &p
	}
	s.turnSettings = persisted
	activeTools := map[string]types.Tool{}
	for _, spec := range persisted.Tools {
		t, ok := cur.activeTools[spec.Name]
		if !ok {
			return nil, fmt.Errorf("HarnessAgent cannot continue turn because tool '%s' is no longer available.", spec.Name) //nolint:staticcheck // matches TS SDK's exact error text
		}
		activeTools[spec.Name] = t
	}
	toolsContext, runtimeContext := cur.toolsContext, cur.runtimeContext
	if s.resumedToolsContext != nil {
		toolsContext = s.resumedToolsContext
	}
	if s.resumedRuntimeContext != nil {
		runtimeContext = s.resumedRuntimeContext
	}
	s.activeTurn = &activeTurnSettings{
		persisted: *persisted, tools: cur.tools, toolsContext: toolsContext, runtimeContext: runtimeContext,
		activeTools: activeTools, builtinToolFiltering: cur.builtinToolFiltering,
	}
	return s.activeTurn, nil
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

// finishTrackedTurn returns the session to idle, but only if the session is
// still active and turnID is still the active turn (a stale call — from a
// turn that a newer startTrackedTurn has since superseded, or one that
// arrives after Detach/Stop/Destroy — is a no-op, mirroring TS's
// `sessionState !== 'active'` and `activeTurnSequence !== options.turnId`
// guards). Called from runPrompt's OnTurnFinished/OnTurnFailed callbacks
// (via a turnID-scoped closure — see Agent.startTurn) — synchronously, from
// the turn driver's own goroutine, before it signals Done — for both a
// natural finish and a failure. Mirrors TS `HarnessAgentSession`'s private
// `finishTrackedTurn`, which likewise always sets `turnState = 'idle'` (when
// still active and the turnId still matches) regardless of what
// markAwaitingApprovalIfActive/markAwaitingToolResultIfActive set earlier:
// those calls only fire when a turn *pauses* for host input, a case in
// which runPrompt never calls OnTurnFinished/OnTurnFailed (see
// pauseForHostInput), so there is no ordering conflict between the two.
// Also drops any leftover pending approvals/results and the memoized
// suspend state, exactly like TS clearing `pendingToolApprovals`/
// `pendingToolResults`/`suspendedTurnState` — a completed turn must not
// leave orphaned entries a later turn's snapshotPendingState could pick up.
func (s *AgentSession) finishTrackedTurn(turnID int) {
	s.mu.Lock()
	if s.sessionState == SessionStateActive && s.activeTurnID == turnID {
		s.clearActiveHandoffLocked()
		s.pendingApprovals = map[string]PendingToolApproval{}
		s.pendingResults = map[string]PendingToolResult{}
		s.approvalOrder, s.resultOrder = nil, nil
		s.suspend = nil
		s.activeTurn = nil
		s.turnSettings = nil
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
	return s.pendingApprovalsLocked(), s.pendingResultsLocked(), s.turnSettings
}

func (s *AgentSession) pendingApprovalsLocked() []PendingToolApproval {
	approvals := make([]PendingToolApproval, 0, len(s.approvalOrder))
	for _, id := range s.approvalOrder {
		approvals = append(approvals, s.pendingApprovals[id])
	}
	return approvals
}

func (s *AgentSession) pendingResultsLocked() []PendingToolResult {
	results := make([]PendingToolResult, 0, len(s.resultOrder))
	for _, id := range s.resultOrder {
		results = append(results, s.pendingResults[id])
	}
	return results
}

// setApprovalLocked/setResultLocked mirror a TS Map `set`: re-setting an
// existing key keeps its position.
func (s *AgentSession) setApprovalLocked(a PendingToolApproval) {
	if _, ok := s.pendingApprovals[a.ApprovalID]; !ok {
		s.approvalOrder = append(s.approvalOrder, a.ApprovalID)
	}
	s.pendingApprovals[a.ApprovalID] = a
}

func (s *AgentSession) setResultLocked(r PendingToolResult) {
	if _, ok := s.pendingResults[r.ToolCallID]; !ok {
		s.resultOrder = append(s.resultOrder, r.ToolCallID)
	}
	s.pendingResults[r.ToolCallID] = r
}

func removeOrdered(order []string, id string) []string {
	for i, v := range order {
		if v == id {
			return append(order[:i:i], order[i+1:]...)
		}
	}
	return order
}

// recordPendingApproval records a newly pending approval and — mirroring TS
// `markAwaitingApprovalIfActive` — immediately marks the turn as
// awaiting-approval if it is currently running. It is wired as runPrompt's
// OnPendingToolApproval callback, so this happens synchronously as the turn
// discovers it needs one, not retrospectively once the turn settles.
func (s *AgentSession) recordPendingApproval(a PendingToolApproval) {
	s.mu.Lock()
	s.setApprovalLocked(a)
	if s.sessionState == SessionStateActive {
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateAwaitingApproval
	}
	s.mu.Unlock()
}

func (s *AgentSession) settleApproval(approvalID string) {
	s.mu.Lock()
	delete(s.pendingApprovals, approvalID)
	s.approvalOrder = removeOrdered(s.approvalOrder, approvalID)
	s.mu.Unlock()
}

// recordPendingResult records a newly pending client tool result and marks
// the turn as awaiting-tool-result if it is currently running. Mirrors TS
// `markAwaitingToolResultIfActive`; see recordPendingApproval.
func (s *AgentSession) recordPendingResult(r PendingToolResult) {
	s.mu.Lock()
	s.setResultLocked(r)
	if s.sessionState == SessionStateActive {
		s.clearActiveHandoffLocked()
		s.turnState = TurnStateAwaitingResult
	}
	s.mu.Unlock()
}

func (s *AgentSession) settleResult(toolCallID string) {
	s.mu.Lock()
	delete(s.pendingResults, toolCallID)
	s.resultOrder = removeOrdered(s.resultOrder, toolCallID)
	s.mu.Unlock()
}

// Compact requests that the runtime compact its context. Mirrors TS
// `HarnessAgentSession.compact`.
func (s *AgentSession) Compact(ctx context.Context, customInstructions string) error {
	s.mu.Lock()
	err := s.requireReusableLocked()
	underlying := s.underlying
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return underlying.DoCompact(ctx, customInstructions)
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
	if err := s.requireReusableLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.turnState != TurnStateRunning || s.suspend != nil || s.activeHandoff == nil {
		s.mu.Unlock()
		return fmt.Errorf("Harness session %s has no running turn to steer.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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
	stillTargeted := s.sessionState == SessionStateActive && s.turnState == TurnStateRunning && s.suspend == nil && s.activeHandoff == handoff
	s.mu.Unlock()
	if !stillTargeted || control == nil {
		return fmt.Errorf("Harness session %s no longer has the running turn targeted for steering.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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

// SuspendTurn freezes the active turn at a precise cursor and returns the
// continuation payload the caller must persist to resume it later (directly,
// or embedded in the ResumeSessionState returned by Detach/Stop). The
// sandbox/runtime keep running, but this call always leaves the local
// session handle detached afterward (like Detach, but without a separate
// DoDetach call — the adapter's cursor is already frozen by DoSuspendTurn):
// this in-process handle no longer drives turns, and a caller must create a
// fresh session from the returned state to continue. Mirrors TS
// `HarnessAgentSession.suspendTurn`'s `finally { endLocalHandle(...) }`,
// which unconditionally detaches even when the memoized suspend state below
// is reused rather than freshly captured — unlike the StopWhen early-stop
// path (captureStopConditionBoundary), which suspends the turn in place
// without detaching so ContinueGenerate/ContinueStream can resume it on this
// same session.
func (s *AgentSession) SuspendTurn(ctx context.Context) (*ContinueTurnState, error) {
	s.mu.Lock()
	if s.sessionState != SessionStateActive || s.underlying == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("Harness session %s is not active and cannot be suspended.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if s.turnState == TurnStateIdle {
		s.mu.Unlock()
		return nil, fmt.Errorf("Harness session %s has no unfinished turn to suspend.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	s.mu.Unlock()
	// Like TS, the local handle is detached whether or not suspending succeeded.
	defer s.endLocalHandle(SessionStateDetached)
	return s.finalizeCurrentTurnSuspension(ctx)
}

// endLocalHandle ends this in-process handle. Mirrors TS `endLocalHandle`.
func (s *AgentSession) endLocalHandle(state SessionState) {
	s.mu.Lock()
	s.clearActiveHandoffLocked()
	s.sessionState = state
	s.mu.Unlock()
}

// suspendCurrentTurn suspends the underlying turn once and memoizes the
// result for the lifetime of the turn: a StopWhen early-stop and a later
// explicit SuspendTurn must not call the adapter's DoSuspendTurn twice. The
// adapter's state is validated and extended with the session's pending tool
// approvals/results and turn settings. Mirrors TS `suspendCurrentTurn`.
func (s *AgentSession) suspendCurrentTurn(ctx context.Context) (*ContinueTurnState, error) {
	s.mu.Lock()
	s.clearActiveHandoffLocked()
	call := s.suspend
	owner := call == nil
	underlying := s.underlying
	if owner {
		call = &suspendCall{done: make(chan struct{})}
		s.suspend = call
	}
	s.mu.Unlock()

	if owner {
		raw, err := underlying.DoSuspendTurn(ctx)
		if err == nil && raw == nil {
			err = NewHarnessError("Harness session returned no state when suspending the turn.", nil)
		}
		if err == nil {
			err = validateLifecycleStateData(s.harness, raw)
		}
		if err == nil {
			call.state = s.addPendingToolState(raw)
		}
		call.err = err
		close(call.done)
	} else {
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if call.err != nil {
		return nil, call.err
	}
	s.mu.Lock()
	s.turnState = TurnStateSuspended
	s.mu.Unlock()
	return call.state, nil
}

// finalizeCurrentTurnSuspension suspends the turn, waits for the in-flight
// prompt driver to wind down, then re-reads the pending tool state so host
// work that completed meanwhile is included. Mirrors TS
// `finalizeCurrentTurnSuspension`.
func (s *AgentSession) finalizeCurrentTurnSuspension(ctx context.Context) (*ContinueTurnState, error) {
	state, err := s.suspendCurrentTurn(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	done := s.activePromptDone
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.addPendingToolState(state), nil
}

// addPendingToolState returns state extended with the session's turn
// settings and pending tool approvals/results. Mirrors TS
// `addPendingToolState`.
func (s *AgentSession) addPendingToolState(state *ContinueTurnState) *ContinueTurnState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := &ContinueTurnState{
		Type: state.Type, HarnessID: state.HarnessID, SpecificationVersion: state.SpecificationVersion, Data: state.Data,
	}
	if s.turnSettings != nil {
		ts := *s.turnSettings
		out.TurnSettings = &ts
	}
	approvals, results := s.pendingApprovalsLocked(), s.pendingResultsLocked()
	if len(approvals) > 0 {
		out.PendingToolApprovals = approvals
	}
	if len(results) > 0 {
		out.PendingToolResults = results
	}
	return out
}

// toResumeStateWithContinuation wraps an unfinished turn's state in a
// resume-session state. Mirrors TS `toResumeStateWithContinuation`.
func toResumeStateWithContinuation(continueFrom *ContinueTurnState) *ResumeSessionState {
	return &ResumeSessionState{
		Type: LifecycleStateResumeSession, HarnessID: continueFrom.HarnessID,
		SpecificationVersion: continueFrom.SpecificationVersion, Data: continueFrom.Data, ContinueFrom: continueFrom,
	}
}

// captureStopConditionBoundary suspends the underlying harness session's
// turn in place when a StopWhen condition matches after a completed step,
// keeping it resumable via ContinueGenerate/ContinueStream instead of
// discarding it. Wired as runPrompt's OnStopConditionMet callback
// (turnID-scoped, like OnTurnFinished/OnPromptControlAvailable). A no-op
// (returns nil, nil) once turnID is no longer the active turn of an active
// session, mirroring TS's identical guard in `captureStopConditionBoundary`.
func (s *AgentSession) captureStopConditionBoundary(ctx context.Context, turnID int) (*ContinueTurnState, error) {
	s.mu.Lock()
	if s.sessionState != SessionStateActive || s.activeTurnID != turnID {
		s.mu.Unlock()
		return nil, nil
	}
	s.mu.Unlock()
	return s.suspendCurrentTurn(ctx)
}

// Detach detaches from the runtime without tearing it down, returning a
// payload that can later be passed to HarnessAgent.CreateSession's
// ResumeFrom. Mirrors TS `HarnessAgentSession.detach`.
func (s *AgentSession) Detach(ctx context.Context) (*ResumeSessionState, error) {
	s.mu.Lock()
	if s.sessionState != SessionStateActive || s.underlying == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("Harness session %s is not active and cannot be detached.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	unfinished := s.turnState != TurnStateIdle
	underlying := s.underlying
	s.mu.Unlock()

	var state *ResumeSessionState
	if unfinished {
		continueFrom, err := s.finalizeCurrentTurnSuspension(ctx)
		if err != nil {
			return nil, err
		}
		state = toResumeStateWithContinuation(continueFrom)
	} else {
		var err error
		if state, err = s.doResumeState(ctx, underlying.DoDetach); err != nil {
			return nil, err
		}
	}
	s.endLocalHandle(SessionStateDetached)
	return state, nil
}

// doResumeState runs an adapter lifecycle call and validates the returned
// resume-session state (TS `validateLifecycleStateData` with
// expectedType 'resume-session').
func (s *AgentSession) doResumeState(ctx context.Context, call func(context.Context) (*ResumeSessionState, error)) (*ResumeSessionState, error) {
	state, err := call(ctx)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, NewHarnessError("Harness session returned no resume state.", nil)
	}
	if err := validateLifecycleStateData(s.harness, state); err != nil {
		return nil, err
	}
	return state, nil
}

// Stop persists enough state to resume later, then stops the runtime and
// any harness-owned sandbox. An unfinished turn is suspended and returned as
// the state's ContinueFrom. Mirrors TS `HarnessAgentSession.stop`.
func (s *AgentSession) Stop(ctx context.Context) (*ResumeSessionState, error) {
	s.mu.Lock()
	if s.sessionState != SessionStateActive || s.underlying == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("Harness session %s is not active and cannot be stopped.", s.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	unfinished := s.turnState != TurnStateIdle
	underlying := s.underlying
	sandbox, owns := s.sandboxSession, s.ownsSandboxLifecycle
	s.mu.Unlock()

	// The local handle ends and an owned sandbox stops whether or not
	// persisting the state succeeded (TS `finally`).
	defer func() {
		s.endLocalHandle(SessionStateStopped)
		if owns {
			stopSandbox(ctx, sandbox)
		}
	}()
	if unfinished {
		continueFrom, err := s.finalizeCurrentTurnSuspension(ctx)
		if err != nil {
			return nil, err
		}
		return toResumeStateWithContinuation(continueFrom), nil
	}
	return s.doResumeState(ctx, underlying.DoStop)
}

// Destroy stops the runtime without returning lifecycle state and discards
// resumability. A harness-owned network sandbox is destroyed. Calling it on
// an ended session is a no-op. Teardown is best-effort, exactly as in TS
// `HarnessAgentSession.destroy`: an adapter or sandbox destroy failure is
// swallowed, so the returned error is always nil (kept for source
// compatibility).
func (s *AgentSession) Destroy(ctx context.Context) error {
	s.mu.Lock()
	if s.sessionState != SessionStateActive {
		s.mu.Unlock()
		return nil
	}
	underlying := s.underlying
	sandbox, owns := s.sandboxSession, s.ownsSandboxLifecycle
	s.mu.Unlock()

	s.endLocalHandle(SessionStateDestroyed)
	if underlying != nil {
		// TS awaits `doDestroy()` and swallows its rejection
		// (`.catch(() => {})`): destroying is best-effort teardown and the
		// owned sandbox below is destroyed regardless.
		_ = underlying.DoDestroy(ctx)
	}
	if owns {
		destroySandbox(ctx, sandbox)
	}
	return nil
}

// destroySandbox destroys an owned network sandbox, swallowing errors like
// TS (`destroy()` followed by `.catch(() => {})`).
func destroySandbox(ctx context.Context, sandbox providerutils.SandboxSession) {
	if network, ok := AsNetworkSandboxSession(sandbox); ok {
		_ = network.Destroy(ctx)
	}
}

func stopSandbox(ctx context.Context, sandbox providerutils.SandboxSession) {
	if network, ok := AsNetworkSandboxSession(sandbox); ok {
		_ = network.Stop(ctx)
	}
}
