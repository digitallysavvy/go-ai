package fx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// Port of TS fx-session.ts: a harness session on the libfx runtime. Each
// turn starts a fresh core from the session checkpoint, records every model
// response and tool outcome in a private state file in the sandbox, and
// checkpoints the core when the turn finishes. A suspended or interrupted
// turn continues cold from that state: recorded model responses replay
// without network requests, and a tool call that was executing when the
// turn stopped is answered with a fixed "outcome unknown" error instead of
// running again.

const interruptedToolText = "This tool execution was interrupted and its outcome is unknown. It was not executed again. Inspect the sandbox before retrying."

type libfxSession struct {
	h        *libfxHarness
	start    harness.StartOptions
	isResume bool
	store    *stateStore
	remote   *remoteOps
	auth     fxAuthentication
	broker   *credentialBroker

	saveMu sync.Mutex // orders snapshots with their writes

	mu      sync.Mutex // guards everything below and every turn's state
	state   *fxStoredState
	stopped bool
	active  *activeTurn
}

func newPlaceholderCredential() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return "fx-brokered-" + hex.EncodeToString(b[:])
}

func startLibfxSession(ctx context.Context, h *libfxHarness, opts harness.StartOptions) (harness.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	var resume *resumeRef
	switch {
	case opts.ContinueFrom != nil:
		resume = &resumeRef{opts.ContinueFrom.HarnessID, opts.ContinueFrom.SpecificationVersion, opts.ContinueFrom.Data}
	case opts.ResumeFrom != nil && opts.ResumeFrom.ContinueFrom != nil:
		c := opts.ResumeFrom.ContinueFrom
		resume = &resumeRef{c.HarnessID, c.SpecificationVersion, c.Data}
	case opts.ResumeFrom != nil:
		resume = &resumeRef{opts.ResumeFrom.HarnessID, opts.ResumeFrom.SpecificationVersion, opts.ResumeFrom.Data}
	}
	if resume != nil && (resume.harnessID != HarnessID || resume.spec != harness.SpecificationVersion) {
		return nil, errors.New("Cannot import lifecycle state from a different harness.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	var stateFileName string
	if resume != nil {
		d, err := parseLibfxResumeData(resume.data)
		if err != nil {
			return nil, err
		}
		stateFileName = d.StateFileName
	}
	var auth fxAuthentication
	var authErr error
	if resume == nil {
		if auth, authErr = h.resolve(ctx, h.settings.Auth, ""); authErr != nil {
			return nil, authErr
		}
	}
	sandbox := harnessutil.GetRestrictedSandboxSession(opts.SandboxSession)
	if sandbox == nil {
		return nil, errors.New("fx requires a sandbox session")
	}
	store, err := newStateStore(ctx, sandbox, opts.SessionID, opts.SessionWorkDir, stateFileName)
	if err != nil {
		return nil, err
	}
	state := &fxStoredState{Version: 1, SDKVersion: libfx.Version}
	if resume != nil {
		if state, err = store.load(ctx); err != nil {
			return nil, err
		}
		required := state.Provider
		if required == "" {
			required = "gateway"
		}
		if auth, err = h.resolve(ctx, h.settings.Auth, required); err != nil {
			return nil, err
		}
	}
	state.Provider = auth.Provider
	state.SDKVersion = libfx.Version
	s := &libfxSession{
		h:        h,
		start:    opts,
		isResume: resume != nil,
		store:    store,
		remote:   newRemoteOps(sandbox, opts.SessionWorkDir),
		auth:     auth,
		broker:   &credentialBroker{placeholder: newPlaceholderCredential(), real: auth.APIKey},
		state:    state,
	}
	if err := s.save(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

type resumeRef struct {
	harnessID string
	spec      string
	data      json.RawMessage
}

// save writes a snapshot of the state. Callers must not hold s.mu.
func (s *libfxSession) save(ctx context.Context) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	data, err := jsstr.Marshal(s.state)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.store.save(ctx, data)
}

func (s *libfxSession) SessionID() string { return s.start.SessionID }
func (s *libfxSession) IsResume() bool    { return s.isResume }

func (s *libfxSession) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	return s.runTurn(ctx, opts.TurnSettings, &opts.Prompt, opts.ResponseFormat, opts.Emit)
}

func (s *libfxSession) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	return s.runTurn(ctx, opts.TurnSettings, nil, opts.ResponseFormat, opts.Emit)
}

func (s *libfxSession) DoCompact(context.Context, string) error {
	return harness.NewCapabilityUnsupportedError("Stable libfx does not expose manual context compaction.", HarnessID, nil)
}

func (s *libfxSession) continuation() *harness.ContinueTurnState {
	st, _ := harness.NewContinueTurnState(HarnessID, libfxResumeData{Version: 1, StateFileName: s.store.fileName})
	return st
}

func (s *libfxSession) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	s.mu.Lock()
	stopped, active := s.stopped, s.active
	s.mu.Unlock()
	if stopped {
		return nil, errors.New("fx session has been stopped.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if active != nil {
		active.suspend()
	}
	if err := s.save(ctx); err != nil {
		return nil, err
	}
	return s.continuation(), nil
}

func (s *libfxSession) stop(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	stopped, active := s.stopped, s.active
	s.mu.Unlock()
	if !stopped {
		if active != nil {
			active.suspend()
		}
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		if err := s.save(ctx); err != nil {
			return nil, err
		}
	}
	st, err := harness.NewResumeSessionState(HarnessID, libfxResumeData{Version: 1, StateFileName: s.store.fileName})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	open := s.state.Turn != nil
	s.mu.Unlock()
	if open {
		st.ContinueFrom = s.continuation()
	}
	return st, nil
}

func (s *libfxSession) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	return s.stop(ctx)
}

func (s *libfxSession) DoStop(ctx context.Context) (*harness.ResumeSessionState, error) {
	return s.stop(ctx)
}

func (s *libfxSession) DoDestroy(ctx context.Context) error {
	_, err := s.stop(ctx)
	return err
}

// turnSignature hashes the settings a continued turn must not change. Go
// computes it its own way, so a turn suspended by a TS host continues only
// there.
func (s *libfxSession) turnSignature(settings harness.TurnSettings, responseFormat *harness.ResponseFormat, fast, ultrafast *bool) string {
	sig := struct {
		Model          string                        `json:"model,omitempty"`
		Skills         []harness.Skill               `json:"skills"`
		Instructions   string                        `json:"instructions,omitempty"`
		Tools          []harness.ToolSpec            `json:"tools"`
		ResponseFormat *harness.ResponseFormat       `json:"responseFormat,omitempty"`
		Filtering      *harness.BuiltinToolFiltering `json:"filtering,omitempty"`
		PermissionMode harness.PermissionMode        `json:"permissionMode,omitempty"`
		MCPServers     map[string]any                `json:"mcpServers,omitempty"`
		Effort         string                        `json:"effort,omitempty"`
		Fast           *bool                         `json:"fast,omitempty"`
		Ultrafast      *bool                         `json:"ultrafast,omitempty"`
	}{settings.Model, settings.Skills, settings.Instructions, settings.Tools, responseFormat, s.start.BuiltinToolFiltering,
		s.start.PermissionMode, s.h.settings.MCPServers, s.h.settings.Effort, fast, ultrafast}
	data, _ := jsstr.Marshal(sig)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// promptJSON converts a harness prompt to the libfx prompt (TS toPrompt).
func promptJSON(p harness.Prompt) (json.RawMessage, error) {
	content, err := harnessutil.ExtractPromptContent(p)
	if err != nil {
		return nil, err
	}
	if content.IsText() {
		return jsstr.Marshal(content.Text)
	}
	type block struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		Data     string `json:"data,omitempty"`
		MimeType string `json:"mimeType,omitempty"`
	}
	blocks := make([]block, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part.Type == "text" {
			blocks = append(blocks, block{Type: "text", Text: part.Text})
		} else {
			blocks = append(blocks, block{Type: "image", Data: part.Data, MimeType: part.MediaType})
		}
	}
	return jsstr.Marshal(blocks)
}

func promptBlocks(raw json.RawMessage) ([]libfx.PromptBlock, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []libfx.PromptBlock{{Type: "text", Text: text}}, nil
	}
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("invalid saved fx prompt: %w", err)
	}
	out := make([]libfx.PromptBlock, len(blocks))
	for i, b := range blocks {
		out[i] = libfx.PromptBlock{Type: b.Type, Text: b.Text, Data: b.Data, MimeType: b.MimeType}
	}
	return out, nil
}

func (s *libfxSession) serviceTier() (fast, ultrafast *bool) {
	switch s.h.settings.ServiceTier {
	case ServiceTierFast:
		return boolPtr(true), boolPtr(false)
	case ServiceTierUltrafast:
		return boolPtr(false), boolPtr(true)
	}
	return nil, nil
}

func (s *libfxSession) runTurn(ctx context.Context, settings harness.TurnSettings, prompt *harness.Prompt, responseFormat *harness.ResponseFormat, emit harness.EmitFunc) (harness.PromptControl, error) {
	continuing := prompt == nil
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, errors.New("fx session has been stopped.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if active := s.active; active != nil && s.state.Turn == nil {
		_ = active.controlWaitLocked(func() error {
			s.mu.Unlock()
			<-active.settled
			s.mu.Lock()
			return nil
		})
	}
	fast, ultrafast := s.serviceTier()
	signature := s.turnSignature(settings, responseFormat, fast, ultrafast)
	if continuing {
		if s.state.Turn == nil {
			s.mu.Unlock()
			return nil, errors.New("There is no suspended fx turn to continue.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if s.state.Turn.Signature != signature {
			s.mu.Unlock()
			return nil, errors.New("Cannot change fx settings while continuing an unfinished turn.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if a := s.active; a != nil {
			a.emitTarget = emit
			s.mu.Unlock()
			return a, nil
		}
	} else {
		if s.state.Turn != nil {
			s.mu.Unlock()
			return nil, errors.New("Continue the unfinished fx turn before prompting again.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		s.mu.Unlock()
		promptRaw, err := promptJSON(*prompt)
		if err != nil {
			return nil, err
		}
		var rf json.RawMessage
		if responseFormat != nil {
			rf, _ = jsstr.Marshal(responseFormat)
		}
		s.mu.Lock()
		if s.state.Turn != nil {
			s.mu.Unlock()
			return nil, errors.New("Continue the unfinished fx turn before prompting again.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		s.state.Turn = &fxTurnState{
			ID: randomUUID(), Prompt: promptRaw, Signature: signature, ResponseFormat: rf,
			Models: []*fxModelRecord{}, Tools: []*fxToolRecord{}, EmittedKeys: []string{}, Steers: []fxSteer{},
		}
	}
	turn := s.state.Turn
	a := newActiveTurn(ctx, s, turn, emit, settings, fast, ultrafast)
	s.active = a
	s.mu.Unlock()
	go a.run()
	return a, nil
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type registeredTool struct {
	providerExecuted bool
	kind             harness.BuiltinToolUseKind
}

type toolIdentity struct{ id, name string }

// pendingOutputEntry is a model request context for the next delta, or a
// deferred flush (TS pendingOutput).
type pendingOutputEntry struct {
	requestIndex int
	record       *fxModelRecord
	flush        func()
}

// activeTurn is one running turn and its prompt control. All fields after
// the immutable ones are guarded by s.mu.
type activeTurn struct {
	s         *libfxSession
	turn      *fxTurnState
	settings  harness.TurnSettings
	fast      *bool
	ultrafast *bool

	// caller is the DoPromptTurn/DoContinueTurn ctx: its cancellation is
	// the caller's abort, read synchronously at every abort check. ctx is
	// the turn's own ctx, cancelled by that abort or by a suspension.
	caller context.Context
	ctx    context.Context
	cancel context.CancelCauseFunc
	// settled closes when the turn body and its cleanup have finished;
	// suspend and the next turn wait for it. done (PromptControl.Done)
	// closes after that, once every queued part has been delivered.
	settled chan struct{}
	done    chan struct{}
	err     error

	// Parts reach emitTarget from one dispatcher goroutine, in order, so no
	// goroutine that runs the turn ever blocks in the caller's Emit. An Emit
	// may therefore call DoSuspendTurn, DoStop or DoPromptTurn synchronously
	// (TS emit is synchronous and those calls return promises).
	emitTarget harness.EmitFunc
	emitQueue  []queuedPart
	emitSignal chan struct{} // capacity 1: wakes the dispatcher
	emitClosed bool          // set once settled; the dispatcher then drains and closes done
	enqueued   int           // parts queued so far
	delivered  int           // parts the dispatcher has handed to Emit
	// controlWaiters counts control calls blocked on turn progress. While
	// one waits, producers stop waiting for delivery, since that call may
	// be running inside Emit.
	controlWaiters int

	suspending     bool
	emitted        map[string]bool
	translator     *translator
	engine         fxEngine
	runtimeTurn    fxTurn
	transport      *transport
	credentialFail error
	mcp            *fxMCPRuntime
	mcpTools       []fxMCPTool
	results        map[string]harness.ToolResultSubmission
	approvals      map[string]fxApproval
	registered     map[string]registeredTool
	toolRequests   map[string]int
	wake           chan struct{}
	identities     []toolIdentity
	seenIdentities map[string]bool
	callbackIndex  int
	replaySteers   []fxSteer
	pendingSteers  []string
	pendingOutput  []pendingOutputEntry
}

func newActiveTurn(ctx context.Context, s *libfxSession, turn *fxTurnState, emit harness.EmitFunc, settings harness.TurnSettings, fast, ultrafast *bool) *activeTurn {
	tctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	a := &activeTurn{
		s: s, turn: turn, settings: settings, fast: fast, ultrafast: ultrafast,
		caller: ctx, ctx: tctx, cancel: cancel, settled: make(chan struct{}), done: make(chan struct{}),
		emitTarget: emit, emitSignal: make(chan struct{}, 1), emitted: map[string]bool{},
		results: map[string]harness.ToolResultSubmission{}, approvals: map[string]fxApproval{},
		registered: map[string]registeredTool{}, toolRequests: map[string]int{},
		wake: make(chan struct{}), seenIdentities: map[string]bool{},
		replaySteers: append([]fxSteer(nil), turn.Steers...),
	}
	for _, key := range turn.EmittedKeys {
		a.emitted[key] = true
	}
	a.translator = newTranslator(turn.ID, a.emitLocked, a.emitOnceLocked)
	go func() {
		<-a.settled
		stop()
	}()
	go a.dispatch()
	return a
}

// queuedPart is a part waiting for the dispatcher, with the emitted key
// that emitOnceLocked recorded for it.
type queuedPart struct {
	part harness.StreamPart
	key  string
}

// emitLocked queues a part; the caller holds s.mu and calls deliver after
// unlocking.
func (a *activeTurn) emitLocked(part harness.StreamPart) {
	a.enqueueLocked(part, "")
}

func (a *activeTurn) enqueueLocked(part harness.StreamPart, key string) {
	if !a.suspending {
		a.emitQueue = append(a.emitQueue, queuedPart{part, key})
		a.enqueued++
	}
}

func (a *activeTurn) emitOnceLocked(key string, part harness.StreamPart) {
	if a.suspending || a.emitted[key] {
		return
	}
	a.emitted[key] = true
	a.turn.EmittedKeys = append(a.turn.EmittedKeys, key)
	a.enqueueLocked(part, key)
}

// discardQueueLocked drops the parts the caller has not received when the
// turn starts suspending, as TS drops every emit after `suspending = true`.
// Their keys are unrecorded so the continuation emits them again.
func (a *activeTurn) discardQueueLocked() {
	dropped := map[string]bool{}
	for _, q := range a.emitQueue {
		if q.key != "" {
			dropped[q.key] = true
			delete(a.emitted, q.key)
		}
	}
	a.emitQueue = nil
	if len(dropped) == 0 {
		return
	}
	keys := a.turn.EmittedKeys[:0]
	for _, k := range a.turn.EmittedKeys {
		if !dropped[k] {
			keys = append(keys, k)
		}
	}
	a.turn.EmittedKeys = keys
}

// deliver wakes the dispatcher. It never blocks.
func (a *activeTurn) deliver() {
	select {
	case a.emitSignal <- struct{}{}:
	default:
	}
}

// deliverWait wakes the dispatcher and waits until it has handed every part
// queued so far to Emit, so the turn does not move on before the caller
// has seen its parts (TS emit is synchronous; an emit handler that suspends
// stops the turn before the core continues). The wait ends early once the
// turn is suspending or aborted, or while a control call is waiting on the
// turn, so an Emit that calls back into the session cannot deadlock. The
// caller must not hold s.mu.
func (a *activeTurn) deliverWait() {
	a.deliver()
	s := a.s
	s.mu.Lock()
	seq := a.enqueued
	for a.delivered < seq && !a.suspending && a.controlWaiters == 0 && a.ctx.Err() == nil {
		wake := a.wake
		s.mu.Unlock()
		select {
		case <-wake:
		case <-a.ctx.Done():
		}
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// controlWaitLocked marks a control call as waiting on the turn for the
// duration of wait (see controlWaiters). Called and returns with s.mu held.
func (a *activeTurn) controlWaitLocked(wait func() error) error {
	a.controlWaiters++
	a.notifyLocked()
	defer func() { a.controlWaiters-- }()
	return wait()
}

// dispatch hands queued parts to the caller in order, then closes done
// once the turn has settled and the queue is empty.
func (a *activeTurn) dispatch() {
	s := a.s
	for {
		s.mu.Lock()
		if len(a.emitQueue) == 0 {
			closed := a.emitClosed
			s.mu.Unlock()
			if closed {
				close(a.done)
				return
			}
			<-a.emitSignal
			continue
		}
		q := a.emitQueue[0]
		a.emitQueue[0] = queuedPart{}
		a.emitQueue = a.emitQueue[1:]
		target := a.emitTarget
		s.mu.Unlock()
		if target != nil {
			target(q.part)
		}
		s.mu.Lock()
		a.delivered++
		a.notifyLocked()
		s.mu.Unlock()
	}
}

// notifyLocked wakes every waitFor.
func (a *activeTurn) notifyLocked() {
	close(a.wake)
	a.wake = make(chan struct{})
}

// waitFor waits until get reports ok (called with s.mu held, returns with
// it held) or the turn is aborted.
func (a *activeTurn) waitForLocked(get func() bool) error {
	for {
		if err := a.abortErr(); err != nil {
			return err
		}
		if get() {
			return nil
		}
		wake := a.wake
		a.s.mu.Unlock()
		select {
		case <-wake:
		case <-a.ctx.Done():
		}
		a.s.mu.Lock()
	}
}

// abortErr is the turn's abort reason once the caller aborted or the turn
// is suspending (TS signal.throwIfAborted).
func (a *activeTurn) abortErr() error {
	if a.caller.Err() != nil {
		cause := context.Cause(a.caller)
		a.cancel(cause)
		return cause
	}
	if a.ctx.Err() != nil {
		return context.Cause(a.ctx)
	}
	return nil
}

func (a *activeTurn) flushStepLocked(requestIndex int) {
	if step, ok := a.translator.steps[requestIndex]; ok {
		a.emitOnceLocked(fmt.Sprintf("step:%d", requestIndex), &harness.FinishStepPart{FinishReason: step.finishReason, Usage: step.usage})
	}
}

func (a *activeTurn) flushOutputLocked() {
	for len(a.pendingOutput) > 0 && a.pendingOutput[0].flush != nil {
		f := a.pendingOutput[0].flush
		a.pendingOutput = a.pendingOutput[1:]
		if !a.suspending {
			f()
		}
	}
}

func (a *activeTurn) recordSteerLocked(text string) {
	for i, t := range a.pendingSteers {
		if t == text {
			a.pendingSteers = append(a.pendingSteers[:i], a.pendingSteers[i+1:]...)
			index := -1
			if a.transport != nil {
				index = a.transport.requestIndex
			}
			a.turn.Steers = append(a.turn.Steers, fxSteer{Text: text, BeforeRequest: index + 1})
			return
		}
	}
}

func (a *activeTurn) earlyStepLocked(record *fxToolRecord) {
	key := "waiting:" + record.ID
	if index, ok := a.toolRequests[record.ID]; ok {
		key = fmt.Sprintf("waiting-step:%d", index)
	}
	a.emitOnceLocked(key, &harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonToolCalls}})
}

// sameJSON compares two JSON texts by value.
func sameJSON(x, y json.RawMessage) bool {
	var vx, vy any
	if json.Unmarshal(x, &vx) != nil || json.Unmarshal(y, &vy) != nil {
		return string(x) == string(y)
	}
	bx, _ := jsstr.Marshal(vx)
	by, _ := jsstr.Marshal(vy)
	return string(bx) == string(by)
}

// orderedJSON re-serializes JSON text in its own key order.
func orderedJSON(raw json.RawMessage) json.RawMessage {
	v, ok := jsonorder.Parse(raw)
	if !ok {
		return jsonValue(raw)
	}
	return json.RawMessage(jsonorder.Stringify(v))
}

func (a *activeTurn) toolRecordLocked(id, name string, value json.RawMessage) (*fxToolRecord, error) {
	for _, r := range a.turn.Tools {
		if r.ID == id {
			if r.Name != name || !sameJSON(r.Input, value) {
				return nil, errors.New("fx continuation changed a recorded tool call.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			return r, nil
		}
	}
	r := &fxToolRecord{ID: id, Name: name, Input: orderedJSON(value), Status: toolWaiting}
	a.turn.Tools = append(a.turn.Tools, r)
	return r, nil
}

func (a *activeTurn) emitToolCallLocked(r *fxToolRecord, providerExecuted bool) {
	part := &harness.ToolCallPart{ToolCallID: r.ID, ToolName: r.Name, Input: string(r.Input), ProviderExecuted: providerExecuted, Dynamic: strings.HasPrefix(r.Name, "mcp_")}
	if providerExecuted {
		part.NativeName = r.Name
	}
	a.emitOnceLocked("call:"+r.ID, part)
}

func (a *activeTurn) emitToolApprovalLocked(r *fxToolRecord) {
	if r.ApprovalID == "" {
		r.ApprovalID = "fx:" + r.ID
	}
	if r.Approval != nil {
		return
	}
	a.emitOnceLocked("approval:"+r.ID, &harness.ToolApprovalRequestPart{ApprovalID: r.ApprovalID, ToolCallID: r.ID})
}

func (a *activeTurn) emitToolResultLocked(r *fxToolRecord) {
	var result any = ""
	if len(r.Output) > 0 && !isNull(r.Output) {
		result = decodeJSONValue(r.Output)
	}
	part := &harness.ToolResultPart{ToolCallID: r.ID, ToolName: r.Name, Result: result, Dynamic: strings.HasPrefix(r.Name, "mcp_")}
	if r.Result != nil && r.Result.IsError != nil {
		part.IsError = *r.Result.IsError
	}
	a.emitOnceLocked("result:"+r.ID, part)
}

func (a *activeTurn) permissionMode() harness.PermissionMode {
	if a.s.start.PermissionMode == "" {
		return harness.PermissionModeAllowAll
	}
	return a.s.start.PermissionMode
}

// publishToolBatch emits every call of a recorded model response, and the
// approvals it needs, before the core's sequential tool callbacks start
// (TS publishToolBatch).
func (a *activeTurn) publishToolBatch(ctx context.Context, requestIndex int) error {
	if err := a.abortErr(); err != nil {
		return err
	}
	type entry struct {
		record *fxToolRecord
		tool   registeredTool
	}
	var batch []entry
	s := a.s
	calls := a.transport.toolCallsAt(requestIndex)
	s.mu.Lock()
	for _, call := range calls {
		tool, ok := a.registered[call.ToolName]
		if !ok {
			continue
		}
		var value json.RawMessage
		if str, isStr := call.Input.(string); isStr {
			if securejson.Check([]byte(str)) != nil || !json.Valid([]byte(str)) {
				continue
			}
			value = json.RawMessage(str)
		} else {
			value = jsonValue(call.Input)
		}
		trimmed := strings.TrimSpace(string(value))
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		record, err := a.toolRecordLocked(call.ToolCallID, call.ToolName, value)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		a.toolRequests[record.ID] = requestIndex
		batch = append(batch, entry{record, tool})
	}
	s.mu.Unlock()
	if err := s.save(ctx); err != nil {
		return err
	}
	if err := a.abortErr(); err != nil {
		return err
	}
	s.mu.Lock()
	for _, e := range batch {
		a.emitToolCallLocked(e.record, e.tool.providerExecuted)
		if e.record.Status != toolCompleted && e.tool.providerExecuted && toolRequiresApproval(a.permissionMode(), e.tool.kind) {
			a.emitToolApprovalLocked(e.record)
		}
	}
	s.mu.Unlock()
	a.deliverWait()
	return nil
}

// executeTool is every host tool's executor (TS executeTool).
func (a *activeTurn) executeTool(name string, impl func(ctx context.Context, input json.RawMessage) (any, error), providerExecuted bool, kind harness.BuiltinToolUseKind, value json.RawMessage) (any, error) {
	s := a.s
	if err := a.abortErr(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	var identity *toolIdentity
	if a.callbackIndex < len(a.identities) {
		identity = &a.identities[a.callbackIndex]
	}
	a.callbackIndex++
	if identity == nil || identity.name != name {
		s.mu.Unlock()
		return nil, errors.New("fx tool callback could not be matched to its original call ID.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	current, err := a.toolRecordLocked(identity.id, name, value)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	a.emitToolCallLocked(current, providerExecuted)
	status := current.Status
	s.mu.Unlock()
	a.deliverWait()

	if status != toolCompleted {
		if err := s.save(a.ctx); err != nil {
			return nil, err
		}
		s.mu.Lock()
		switch {
		case current.Status == toolExecuting:
			current.Result = &fxToolResult{Type: "libfx.tool-result", Text: interruptedToolText, Images: []fxToolImage{}, IsError: boolPtr(true)}
			current.Output = jsonValue(interruptedToolText)
		case !providerExecuted:
			if _, ok := a.results[current.ID]; !ok {
				a.earlyStepLocked(current)
			}
			s.mu.Unlock()
			a.deliverWait()
			s.mu.Lock()
			var submission harness.ToolResultSubmission
			if err := a.waitForLocked(func() bool {
				sub, ok := a.results[current.ID]
				submission = sub
				return ok
			}); err != nil {
				s.mu.Unlock()
				return nil, err
			}
			result, err := toToolResult(submission)
			if err != nil {
				s.mu.Unlock()
				return nil, err
			}
			if current.Status != toolCompleted {
				current.Result = result
				current.Output = jsonValue(submission.Output)
			}
		default:
			if toolRequiresApproval(a.permissionMode(), kind) {
				a.emitToolApprovalLocked(current)
				if current.Approval == nil {
					if ap, ok := a.approvals[current.ApprovalID]; ok {
						current.Approval = &ap
					}
				}
				if current.Approval == nil {
					a.earlyStepLocked(current)
				}
				s.mu.Unlock()
				a.deliverWait()
				if err := s.save(a.ctx); err != nil {
					return nil, err
				}
				s.mu.Lock()
				if current.Approval == nil {
					if err := a.waitForLocked(func() bool {
						if ap, ok := a.approvals[current.ApprovalID]; ok {
							current.Approval = &ap
							return true
						}
						return current.Approval != nil
					}); err != nil {
						s.mu.Unlock()
						return nil, err
					}
				}
				if !current.Approval.Approved {
					text := "Tool execution denied."
					if current.Approval.Reason != nil {
						text = *current.Approval.Reason
					}
					current.Result = &fxToolResult{Type: "libfx.tool-result", Text: text, Images: []fxToolImage{}, IsError: boolPtr(true)}
					current.Output = jsonValue(text)
				}
			}
			if current.Result == nil {
				if err := a.abortErr(); err != nil {
					s.mu.Unlock()
					return nil, err
				}
				current.Status = toolExecuting
				s.mu.Unlock()
				if err := s.save(a.ctx); err != nil {
					return nil, err
				}
				if err := a.abortErr(); err != nil {
					return nil, err
				}
				output, execErr := impl(a.ctx, value)
				if execErr != nil {
					if aerr := a.abortErr(); aerr != nil {
						return nil, aerr
					}
				}
				s.mu.Lock()
				var richErr *libfx.ToolResultError
				if execErr != nil && errors.As(execErr, &richErr) {
					// A failure carrying a rich result (TS error.toolResult).
					current.Result = fromTypedResult(richErr.Result)
					current.Result.IsError = boolPtr(true)
					current.Output = jsonValue(current.Result.Text)
				} else if execErr != nil {
					current.Result = &fxToolResult{Type: "libfx.tool-result", Text: execErr.Error(), Images: []fxToolImage{}, IsError: boolPtr(true)}
					current.Output = jsonValue(current.Result.Text)
				} else if rich, ok := output.(*libfx.TypedToolResult); ok && rich != nil && rich.Type == "libfx.tool-result" {
					current.Result = fromTypedResult(*rich)
					current.Output = jsonValue(current.Result)
				} else {
					current.Output = jsonValue(output)
					if text, ok := output.(string); ok {
						current.Result = &fxToolResult{Type: "libfx.tool-result", Text: text, Images: []fxToolImage{}}
					} else {
						current.Result = &fxToolResult{Type: "libfx.tool-result", Text: string(current.Output), Images: []fxToolImage{}}
					}
				}
			}
		}
		current.Status = toolCompleted
		s.mu.Unlock()
	}
	if err := s.save(a.ctx); err != nil {
		return nil, err
	}
	if err := a.abortErr(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	a.emitToolResultLocked(current)
	result := current.Result
	s.mu.Unlock()
	a.deliverWait()
	if err := s.save(a.ctx); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("Recorded fx tool result is missing.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	typed := &libfx.TypedToolResult{Type: result.Type, Text: result.Text, IsError: result.IsError, Images: make([]libfx.ToolImage, len(result.Images))}
	for i, img := range result.Images {
		typed.Images[i] = libfx.ToolImage(img)
	}
	if result.IsError != nil && *result.IsError {
		return nil, &libfx.ToolResultError{Result: *typed}
	}
	return typed, nil
}

// fromTypedResult converts a host tool's rich result for the record.
func fromTypedResult(r libfx.TypedToolResult) *fxToolResult {
	out := &fxToolResult{Type: r.Type, Text: r.Text, Images: make([]fxToolImage, len(r.Images)), IsError: r.IsError}
	for i, img := range r.Images {
		out.Images[i] = fxToolImage(img)
	}
	return out
}

// suspend freezes the turn (TS active.suspend).
func (a *activeTurn) suspend() {
	s := a.s
	s.mu.Lock()
	a.suspending = true
	a.discardQueueLocked()
	rt := a.runtimeTurn
	a.notifyLocked()
	s.mu.Unlock()
	a.cancel(errTurnSuspended)
	if rt != nil {
		rt.Cancel()
	}
	<-a.settled
}

func (a *activeTurn) buildTools(ctx context.Context) ([]libfx.Tool, error) {
	s := a.s
	custom := map[string]bool{}
	for _, spec := range a.settings.Tools {
		custom[spec.Name] = true
	}
	var tools []libfx.Tool
	for _, name := range libfxBuiltinToolNames {
		if custom[name] || !harness.IsBuiltinToolIncluded(name, s.start.BuiltinToolFiltering) {
			continue
		}
		builtin := LibfxBuiltinTools[name]
		kind := builtin.ToolUseKind
		if kind == "" {
			kind = harness.BuiltinToolUseKindReadonly
		}
		description := builtin.Description
		if description == "" {
			description = name
		}
		providerExecuted := name != "askUserQuestions"
		toolName := name
		impl := func(ctx context.Context, input json.RawMessage) (any, error) {
			return s.remote.execute(ctx, toolName, input)
		}
		a.registered[name] = registeredTool{providerExecuted: providerExecuted, kind: kind}
		tools = append(tools, libfx.Tool{
			Name: name, Description: description, InputSchema: builtin.Parameters,
			Execute: func(callCtx context.Context, input json.RawMessage) (any, error) {
				// The sandbox command stops when libfx aborts the call as
				// well as when the turn ends.
				run := func(ctx context.Context, input json.RawMessage) (any, error) {
					ctx, cancel := context.WithCancel(ctx)
					defer cancel()
					stop := context.AfterFunc(callCtx, cancel)
					defer stop()
					return impl(ctx, input)
				}
				return a.executeTool(toolName, run, providerExecuted, kind, input)
			},
		})
	}
	for _, spec := range a.settings.Tools {
		description := spec.Description
		if description == "" {
			description = spec.Name
		}
		schema := spec.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		toolName := spec.Name
		a.registered[toolName] = registeredTool{kind: harness.BuiltinToolUseKindReadonly}
		tools = append(tools, libfx.Tool{
			Name: toolName, Description: description, InputSchema: schema,
			Execute: func(_ context.Context, input json.RawMessage) (any, error) {
				return a.executeTool(toolName, func(context.Context, json.RawMessage) (any, error) { return nil, nil }, false, harness.BuiltinToolUseKindReadonly, input)
			},
		})
	}
	for _, tool := range a.mcpTools {
		name := tool.name
		for suffix := 2; hasTool(tools, name); suffix++ {
			name = fmt.Sprintf("%s_%d", truncate(tool.name, 58), suffix)
		}
		toolName := name
		impl := tool.execute
		a.registered[toolName] = registeredTool{providerExecuted: true, kind: harness.BuiltinToolUseKindBash}
		tools = append(tools, libfx.Tool{
			Name: toolName, Description: tool.description, InputSchema: tool.inputSchema,
			Execute: func(callCtx context.Context, input json.RawMessage) (any, error) {
				run := func(ctx context.Context, input json.RawMessage) (any, error) {
					ctx, cancel := context.WithCancel(ctx)
					defer cancel()
					stop := context.AfterFunc(callCtx, cancel)
					defer stop()
					return impl(ctx, input)
				}
				return a.executeTool(toolName, run, true, harness.BuiltinToolUseKindBash, input)
			},
		})
	}
	_ = ctx
	return tools, nil
}

func hasTool(tools []libfx.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func (a *activeTurn) run() {
	s := a.s
	err := a.runBody()
	s.mu.Lock()
	suspending := a.suspending
	s.mu.Unlock()
	if err != nil && !suspending {
		s.mu.Lock()
		s.state.Turn = nil
		failure := err
		if a.credentialFail != nil {
			failure = a.credentialFail
		}
		a.emitLocked(&harness.ErrorPart{Error: failure})
		s.mu.Unlock()
		a.deliver()
		a.err = failure
	}
	if cerr := a.cleanup(); cerr != nil {
		a.err = cerr
	}
	s.mu.Lock()
	if s.active == a {
		s.active = nil
	}
	a.emitClosed = true
	s.mu.Unlock()
	a.cancel(context.Canceled)
	close(a.settled)
	a.deliver()
}

// cleanup closes the runtime and the transport and saves (TS finally).
func (a *activeTurn) cleanup() error {
	var first error
	a.s.mu.Lock()
	engine := a.engine
	tr := a.transport
	a.s.mu.Unlock()
	if engine != nil {
		if err := engine.Close(context.Background()); err != nil {
			first = err
		}
	}
	if tr != nil {
		tr.stop()
	}
	a.s.mu.Lock()
	mcpRuntime := a.mcp
	a.mcp = nil
	a.s.mu.Unlock()
	if err := mcpRuntime.close(); err != nil && first == nil {
		first = err
	}
	if err := a.s.save(context.Background()); err != nil && first == nil {
		first = err
	}
	return first
}

func (a *activeTurn) runBody() error {
	s := a.s
	if err := s.save(a.ctx); err != nil {
		return err
	}
	skills, err := skillsInstructions(a.settings.Skills)
	if err != nil {
		return err
	}
	mcpRuntime, err := createFxMCPRuntime(a.ctx, s.h.settings.MCPServers)
	if err != nil {
		return err
	}
	s.mu.Lock()
	a.mcp = mcpRuntime
	a.mcpTools = mcpRuntime.tools
	s.mu.Unlock()
	tools, err := a.buildTools(a.ctx)
	if err != nil {
		return err
	}
	network, err := s.h.networkFetch(s.auth.BaseURL)
	if err != nil {
		return err
	}
	headers := map[string]string{}
	for k, v := range s.h.settings.Headers {
		headers[k] = v
	}
	for k, v := range s.start.Headers {
		headers[k] = v
	}
	headers["user-agent"] = clientApp
	headers["x-client-app"] = clientApp
	var protocol transportProtocol = gatewayProtocol{}
	if s.auth.Subscription != nil {
		protocol = responsesProtocol{provider: s.auth.Provider}
	}
	tr := newTransport(transportOptions{
		protocol:       protocol,
		baseURL:        s.auth.BaseURL,
		headers:        headers,
		responseFormat: a.turn.ResponseFormat,
		mu:             &s.mu,
		turn:           a.turn,
		save:           s.save,
		network:        network,
		broker:         s.broker,
		onRequest: func(requestIndex int, modelID *string) {
			s.mu.Lock()
			if requestIndex > 0 {
				prev := requestIndex - 1
				a.pendingOutput = append(a.pendingOutput, pendingOutputEntry{flush: func() { a.flushStepLocked(prev) }})
			}
			part := &harness.StreamStartPart{}
			if modelID != nil {
				part.ModelID = *modelID
			}
			a.emitOnceLocked("start", part)
			s.mu.Unlock()
			a.deliverWait()
		},
		beforeRelease: func(ctx context.Context, requestIndex int, _ bool) error {
			if err := a.publishToolBatch(ctx, requestIndex); err != nil {
				return err
			}
			s.mu.Lock()
			var steers []string
			for _, st := range a.replaySteers {
				if st.BeforeRequest == requestIndex+1 {
					steers = append(steers, st.Text)
				}
			}
			// The core can fetch before Prompt has returned the turn a
			// replayed steer must reach.
			if len(steers) > 0 {
				if err := a.waitForLocked(func() bool { return a.runtimeTurn != nil }); err != nil {
					s.mu.Unlock()
					return err
				}
			}
			rt := a.runtimeTurn
			a.notifyLocked()
			s.mu.Unlock()
			for _, text := range steers {
				_, _ = rt.Steer(text)
			}
			return nil
		},
	})
	s.mu.Lock()
	a.transport = tr
	s.mu.Unlock()
	if err := a.abortErr(); err != nil {
		return err
	}
	instructions := []string{}
	for _, part := range []string{a.settings.Instructions, skills, mcpRuntime.instructions, fmt.Sprintf("All filesystem and shell tools operate in the sandbox workspace %s.", s.start.SessionWorkDir)} {
		if part != "" {
			instructions = append(instructions, part)
		}
	}
	s.mu.Lock()
	var checkpoint []byte
	if s.state.Checkpoint != "" {
		checkpoint, err = base64.StdEncoding.DecodeString(s.state.Checkpoint)
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("invalid saved fx checkpoint: %w", err)
	}
	opts := runtimeOptions{
		APIKey:       s.broker.placeholder,
		Model:        a.settings.Model,
		Effort:       s.h.settings.Effort,
		Fast:         a.fast,
		Ultrafast:    a.ultrafast,
		Instructions: strings.Join(instructions, "\n\n"),
		Tools:        tools,
		Checkpoint:   checkpoint,
		Fetch:        tr.fetch,
		OnHook:       a.onHook,
		OnReceive:    a.onReceive,
	}
	if sub := s.auth.Subscription; sub != nil {
		opts.APIKey = ""
		opts.Provider = s.auth.Provider
		opts.Subscription = a.subscriptionAuth(sub)
	}
	engine, err := s.h.newEngine(a.ctx, opts)
	if err != nil {
		return err
	}
	s.mu.Lock()
	a.engine = engine
	s.mu.Unlock()
	if err := a.abortErr(); err != nil {
		return err
	}
	s.mu.Lock()
	needCheckpoint := s.state.Checkpoint == ""
	s.mu.Unlock()
	if needCheckpoint {
		cp, err := engine.Checkpoint(context.WithoutCancel(a.ctx))
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.state.Checkpoint = base64.StdEncoding.EncodeToString(cp)
		s.mu.Unlock()
		if err := s.save(a.ctx); err != nil {
			return err
		}
	}
	blocks, err := promptBlocks(a.turn.Prompt)
	if err != nil {
		return err
	}
	rt, err := engine.Prompt(a.ctx, blocks)
	if err != nil {
		return err
	}
	s.mu.Lock()
	a.runtimeTurn = rt
	a.notifyLocked()
	s.mu.Unlock()

	drainErr := a.drain(rt)
	result, waitErr := rt.Wait(context.Background())
	if drainErr != nil {
		return drainErr
	}
	if waitErr != nil {
		return waitErr
	}
	s.mu.Lock()
	if a.suspending {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := a.abortErr(); err != nil {
		return err
	}
	s.mu.Lock()
	index := tr.requestIndex
	a.flushStepLocked(index)
	finishReason := harness.FinishReason{Unified: harness.FinishReasonStop, Raw: result.StopReason}
	if result.StopReason == "max_output_tokens" {
		finishReason.Unified = harness.FinishReasonLength
	}
	if step, ok := a.translator.steps[index]; ok {
		finishReason = step.finishReason
	}
	s.mu.Unlock()
	a.deliverWait()
	cp, err := engine.Checkpoint(context.WithoutCancel(a.ctx))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.state.Checkpoint = base64.StdEncoding.EncodeToString(cp)
	s.state.Turn = nil
	s.mu.Unlock()
	if err := s.save(a.ctx); err != nil {
		return err
	}
	s.mu.Lock()
	a.emitLocked(&harness.FinishPart{FinishReason: finishReason, TotalUsage: a.translator.totalUsage()})
	s.mu.Unlock()
	a.deliver()
	return nil
}

// subscriptionAuth hands libfx the selected store. A store failure is
// what the turn reports, rather than libfx's wrapped transport error.
func (a *activeTurn) subscriptionAuth(sub *fxSubscription) *libfx.SubscriptionAuth {
	fail := func(err error) error {
		a.s.mu.Lock()
		if a.credentialFail == nil {
			a.credentialFail = err
		}
		a.s.mu.Unlock()
		return err
	}
	return &libfx.SubscriptionAuth{
		Credentials: func(ctx context.Context, _ libfx.CredentialRequest) (libfx.Credentials, error) {
			c, err := sub.read(ctx)
			if err != nil {
				return libfx.Credentials{}, fail(err)
			}
			return c, nil
		},
		OnCredentials: func(ctx context.Context, next libfx.Credentials, _ libfx.CredentialRequest) error {
			if err := sub.write(ctx, next); err != nil {
				return fail(err)
			}
			return nil
		},
	}
}

// drain reads the runtime turn's events (TS draining).
func (a *activeTurn) drain(rt fxTurn) error {
	s := a.s
	for {
		ev, err := rt.Next(a.ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if aerr := a.abortErr(); aerr != nil {
				return aerr
			}
			return err
		}
		s.mu.Lock()
		a.flushOutputLocked()
		if ev.Type == "text_delta" || ev.Type == "reasoning_delta" {
			var entry *pendingOutputEntry
			if len(a.pendingOutput) > 0 {
				e := a.pendingOutput[0]
				a.pendingOutput = a.pendingOutput[1:]
				entry = &e
			}
			if entry == nil || entry.flush != nil {
				index := a.transport.requestIndex
				s.mu.Unlock()
				if rej, ok := a.transport.rejectedAt(index + 1); ok {
					model := "(unknown model)"
					if rej.modelID != nil {
						model = *rej.modelID
					}
					return fmt.Errorf("fx %s model request %s failed with HTTP %d.", s.auth.Provider, model, rej.status) //nolint:staticcheck // matches TS SDK's exact error text
				}
				return errors.New("fx parsed output could not be matched to its model request.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			if !a.suspending {
				kind := "text"
				if ev.Type == "reasoning_delta" {
					kind = "reasoning"
				}
				a.translator.onDelta(kind, ev.Delta, entry.requestIndex, entry.record)
			}
		}
		if ev.Type == "user_message" {
			a.recordSteerLocked(ev.Text)
		}
		s.mu.Unlock()
		a.deliverWait()
	}
	s.mu.Lock()
	a.flushOutputLocked()
	s.mu.Unlock()
	a.deliverWait()
	return nil
}

// onHook handles semantic hooks on the guest goroutine.
func (a *activeTurn) onHook(e libfx.HookEvent) {
	if e.Type != "model.end" {
		return
	}
	s := a.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.transport == nil {
		return
	}
	index := a.transport.requestIndex
	if index < 0 || index >= len(a.turn.Models) || a.turn.Models[index] == nil {
		return
	}
	record := a.turn.Models[index]
	a.translator.onModelEnd(e.Data, index)
	a.pendingOutput = append(a.pendingOutput, pendingOutputEntry{flush: func() { a.translator.endOutput(index, record) }})
}

// onReceive sees core notifications on the guest goroutine, before the
// turn queues them (TS onEvent acp.receive).
func (a *activeTurn) onReceive(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var p struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			ToolCallID    string `json:"toolCallId"`
			Name          string `json:"name"`
			ToolName      string `json:"toolName"`
			Title         string `json:"title"`
			Content       *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	u := p.Update
	s := a.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.transport != nil {
		index := a.transport.requestIndex
		if index >= 0 && index < len(a.turn.Models) && a.turn.Models[index] != nil && u.Content != nil && u.Content.Text != "" &&
			(u.SessionUpdate == "agent_thought_chunk" || (u.SessionUpdate == "agent_message_chunk" && !strings.HasPrefix(u.Content.Text, "[context]"))) {
			a.pendingOutput = append(a.pendingOutput, pendingOutputEntry{requestIndex: index, record: a.turn.Models[index]})
		}
	}
	if u.SessionUpdate == "user_message_chunk" && u.Content != nil && u.Content.Type == "text" {
		a.recordSteerLocked(u.Content.Text)
	}
	if u.SessionUpdate == "tool_call" && u.ToolCallID != "" && !a.seenIdentities[u.ToolCallID] {
		a.seenIdentities[u.ToolCallID] = true
		a.identities = append(a.identities, toolIdentity{id: u.ToolCallID, name: firstNonEmptyString(u.Name, u.ToolName, u.Title, "tool")})
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// PromptControl.

func (a *activeTurn) Done() <-chan struct{} { return a.done }

func (a *activeTurn) Err() error {
	select {
	case <-a.done:
		return a.err
	default:
		return nil
	}
}

func (a *activeTurn) SubmitToolResult(ctx context.Context, sub harness.ToolResultSubmission) error {
	s := a.s
	s.mu.Lock()
	a.results[sub.ToolCallID] = sub
	var record *fxToolRecord
	for _, r := range a.turn.Tools {
		if r.ID == sub.ToolCallID {
			record = r
		}
	}
	if record != nil && record.Status != toolCompleted {
		// A later call can settle while libfx is blocked on an earlier
		// approval; its durable result and echo must not wait for libfx to
		// reach its executor.
		result, err := toToolResult(sub)
		if err != nil {
			a.notifyLocked()
			s.mu.Unlock()
			return err
		}
		record.Result = result
		record.Output = jsonValue(sub.Output)
		record.Status = toolCompleted
		s.mu.Unlock()
		if err := s.save(ctx); err != nil {
			return err
		}
		s.mu.Lock()
		a.emitToolResultLocked(record)
		s.mu.Unlock()
		a.deliver()
		if err := s.save(ctx); err != nil {
			return err
		}
		s.mu.Lock()
	}
	a.notifyLocked()
	s.mu.Unlock()
	return nil
}

func (a *activeTurn) SubmitToolApproval(ctx context.Context, approval harness.ToolApprovalSubmission) error {
	s := a.s
	ap := fxApproval{Approved: approval.Approved}
	if approval.Reason != "" {
		reason := approval.Reason
		ap.Reason = &reason
	}
	s.mu.Lock()
	a.approvals[approval.ApprovalID] = ap
	var record *fxToolRecord
	for _, r := range a.turn.Tools {
		if r.ApprovalID == approval.ApprovalID {
			record = r
		}
	}
	if record != nil {
		cp := ap
		record.Approval = &cp
		s.mu.Unlock()
		if err := s.save(ctx); err != nil {
			return err
		}
		s.mu.Lock()
	}
	a.notifyLocked()
	s.mu.Unlock()
	return nil
}

func (a *activeTurn) SubmitUserMessage(ctx context.Context, text string) error {
	s := a.s
	s.mu.Lock()
	err := a.controlWaitLocked(func() error {
		return a.waitForLocked(func() bool {
			return a.runtimeTurn != nil && a.transport != nil && a.transport.requestIndex >= len(a.turn.Models)-1
		})
	})
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	a.pendingSteers = append(a.pendingSteers, text)
	rt := a.runtimeTurn
	s.mu.Unlock()
	if _, err := rt.Steer(text); err != nil {
		return err
	}
	s.mu.Lock()
	a.recordSteerLocked(text)
	s.mu.Unlock()
	return s.save(ctx)
}

// Interface checks.
var (
	_ harness.Session               = (*libfxSession)(nil)
	_ harness.PromptControl         = (*activeTurn)(nil)
	_ harness.ToolApprovalSubmitter = (*activeTurn)(nil)
	_ harness.UserMessageSubmitter  = (*activeTurn)(nil)
)
