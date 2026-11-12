package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var _ harness.Session = (*session)(nil)
var _ harness.PromptControl = (*promptControl)(nil)
var _ harness.ToolApprovalSubmitter = (*promptControl)(nil)
var _ harness.UserMessageSubmitter = (*promptControl)(nil)
var _ harness.CheckpointPinner = (*promptControl)(nil)

// sessionOptions is the input of newSession. Mirrors the object TS
// `createSession` closes over.
type sessionOptions struct {
	sessionID string
	channel   *bridge.Channel
	// proc is nil when attached to a bridge spawned by another process (not
	// reachable from this port — see package doc: DoStart never attaches).
	proc providerutils.SandboxProcess

	maxTurns               int
	startSettings          startSettings
	agentProgressSummaries bool
	forwardSubagentText    bool
	env                    map[string]string
	thinking               ThinkingConfig
	effort                 string

	isResume              bool
	continueOnFirstPrompt bool
	rerunContinue         bool
	resumeSessionID       string
	// startID is the start of the attached or replayed turn (TS
	// `createSession({startId})`); "" otherwise.
	startID string
	// deferredToolUseID is sent on the rerun-continue start (TS
	// `deferredToolUseId`).
	deferredToolUseID string

	bridgePort                   int
	bridgePrivateKey             string
	sandboxID                    string
	sandboxCredentialEnvironment map[string]string

	permissionMode       harness.PermissionMode
	builtinToolFiltering *harness.BuiltinToolFiltering
	mcpServers           map[string]any

	sandbox        providerutils.SandboxSession
	sandboxHomeDir string

	// supportsUserMessages is whether the bridge advertised acknowledged
	// mid-turn user messages (experimental_userMessageResponses) on the
	// hello it sent during the connection this session was built from.
	// DoStart captures it via OpenBridgeWebSocket's OnHello — the hello
	// frame is consumed by the dial/hello-wait handshake itself and never
	// reaches the Channel's normal dispatch, so it cannot be observed via
	// channel.On. Mirrors TS `supportsUserMessageResponses()`, captured once
	// at channel-build (session) time, the same way.
	supportsUserMessages func() bool

	// finishListenerAttachment releases the channel's buffered events once
	// the first turn has registered its listeners (attach/replay hold them
	// back until then). Mirrors TS `finishListenerAttachment`.
	finishListenerAttachment func()

	// debug is the observability debug config forwarded on `start`.
	debug *harness.DebugConfig

	// bridgeHello returns the latest bridge-hello of the channel's current
	// connection (nil before any). Mirrors TS `getBridgeHello`.
	bridgeHello func() *bridge.Hello
	// turnStartTimeout bounds the wait for a turn's first event (0 only in
	// internal tests: no deadline). Mirrors TS `turnStartTimeoutMs`.
	turnStartTimeout time.Duration
}

// startSettings are the Settings forwarded unchanged on every start frame.
// Mirrors the TS `createSession` parameters skillOverrides, maxBudgetUsd,
// strictMcpConfig, autoMode, questionTimeoutMs, excludeDynamicSections and
// toolHints.
type startSettings struct {
	skillOverrides         map[string]SkillOverride
	maxBudgetUSD           *float64
	additionalDirectories  []string
	strictMCPConfig        *bool
	autoMode               *bool
	questionTimeoutMs      *int
	excludeDynamicSections *bool
	toolHints              map[string]ToolHint
}

func startSettingsFrom(settings Settings) startSettings {
	return startSettings{
		skillOverrides: settings.SkillOverrides, maxBudgetUSD: settings.MaxBudgetUSD,
		additionalDirectories: settings.AdditionalDirectories,
		strictMCPConfig:       settings.StrictMCPConfig, autoMode: settings.AutoMode, questionTimeoutMs: settings.QuestionTimeoutMs,
		excludeDynamicSections: settings.ExcludeDynamicSections, toolHints: settings.ToolHints,
	}
}

type session struct {
	opts sessionOptions

	mu                sync.Mutex
	stopped           bool
	pendingResumeFlag bool
	finishAttachment  func()
	lastSessionID     string
	// destroyed is closed when the first DoDestroy finishes its teardown.
	destroyed chan struct{}
	// rerunContinue is the live copy of opts.rerunContinue: true until the
	// single recovery start has been sent. Mirrors TS `rerunContinue`.
	rerunContinue bool
	// activeStartID is the start of the current (or last) turn, persisted on
	// detach / suspend. Mirrors TS `activeStartId`.
	activeStartID string
}

func newSession(opts sessionOptions) *session {
	return &session{opts: opts, pendingResumeFlag: opts.continueOnFirstPrompt, lastSessionID: opts.resumeSessionID, rerunContinue: opts.rerunContinue, finishAttachment: opts.finishListenerAttachment, activeStartID: opts.startID}
}

func (s *session) SessionID() string { return s.opts.sessionID }
func (s *session) IsResume() bool    { return s.opts.isResume }

func (s *session) prepareTurn(ctx context.Context, skills []harness.Skill, responseFormat *harness.ResponseFormat) error {
	if responseFormat != nil && responseFormat.Type == harness.ResponseFormatJSON && responseFormat.Schema == nil {
		return harness.NewCapabilityUnsupportedError("Harness 'claude-code' requires a JSON schema for structured output.", HarnessID, nil)
	}
	_, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.opts.sandbox, HomePath: s.opts.sandboxHomeDir, SkillsDir: ".claude/skills", Skills: skills,
		InvalidSkillNameMessage: func(name string) string { return fmt.Sprintf("Invalid Claude Code skill name: %s", name) },
		InvalidSkillFilePathMessage: func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid Claude Code skill file path for %s: %s", skillName, filePath)
		},
		TrailingNewline: true,
	})
	return err
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	if err := s.prepareTurn(ctx, opts.Skills, opts.ResponseFormat); err != nil {
		return nil, err
	}
	// An unsupported prompt is rejected before the turn is wired, so the
	// failure leaves no listeners or abort watcher behind (an already-aborted
	// call never builds the start message, like TS).
	var content harnessutil.PromptContent
	if ctx.Err() == nil {
		var err error
		if content, err = harnessutil.ExtractPromptContent(opts.Prompt); err != nil {
			return nil, err
		}
	}
	control := s.wireTurn(ctx, opts.Emit, newStartID())
	if ctx.Err() != nil {
		return control, nil
	}

	frame := s.buildStartFrame(content, opts.TurnSettings, opts.ResponseFormat)
	s.mu.Lock()
	frame.Continue, frame.ResumeSessionID = s.resumeFields()
	s.pendingResumeFlag = false
	// Recovery starts a native turn once. Later continuations attach to it.
	s.rerunContinue = false
	s.mu.Unlock()
	if err := control.sendStart(frame); err != nil {
		return nil, err
	}
	return control, nil
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	if err := s.prepareTurn(ctx, opts.Skills, opts.ResponseFormat); err != nil {
		return nil, err
	}
	// TS `startId: rerunContinue ? generateId() : activeStartId`: an attached
	// or replayed turn keeps its own start.
	s.mu.Lock()
	startID := s.activeStartID
	if s.rerunContinue {
		startID = newStartID()
	}
	s.mu.Unlock()
	control := s.wireTurn(ctx, opts.Emit, startID)
	s.mu.Lock()
	rerun := s.rerunContinue && ctx.Err() == nil
	if rerun {
		s.rerunContinue = false
	}
	s.mu.Unlock()
	if rerun {
		frame := s.buildStartFrame(harnessutil.PromptContent{Text: "Continue."}, opts.TurnSettings, opts.ResponseFormat)
		s.mu.Lock()
		// Unlike a prompt turn, a rerun continuation always rehydrates: the
		// exact conversation when known, else `continue: true`.
		if s.lastSessionID != "" {
			frame.ResumeSessionID = s.lastSessionID
		} else {
			frame.Continue = true
		}
		frame.DeferredToolUseID = s.opts.deferredToolUseID
		s.pendingResumeFlag = false
		s.mu.Unlock()
		if err := control.sendStart(frame); err != nil {
			return nil, err
		}
	}
	return control, nil
}

// resumeFields mirrors TS's `lastClaudeSessionId ? {resumeSessionId} :
// pendingResumeFlag ? {continue:true} : {}`. s.mu must be held.
func (s *session) resumeFields() (continueFlag bool, resumeSessionID string) {
	if s.lastSessionID != "" {
		return false, s.lastSessionID
	}
	if s.pendingResumeFlag {
		return true, ""
	}
	return false, ""
}

func (s *session) buildStartFrame(prompt harnessutil.PromptContent, ts harness.TurnSettings, rf *harness.ResponseFormat) *StartFrame {
	frame := &StartFrame{
		StartBase: bridge.StartBase{
			Prompt: prompt.Text, Tools: ts.Tools, Model: ts.Model, ResponseFormat: rf,
			PermissionMode: s.opts.permissionMode, BuiltinToolFiltering: s.opts.builtinToolFiltering,
		},
		Instructions: ts.Instructions,
		Thinking:     &s.opts.thinking,
		Effort:       s.opts.effort,
	}
	if s.opts.maxTurns > 0 {
		frame.MaxTurns = &s.opts.maxTurns
	}
	extra := s.opts.startSettings
	frame.SkillOverrides = extra.skillOverrides
	frame.MaxBudgetUSD = extra.maxBudgetUSD
	if extra.additionalDirectories != nil {
		dirs := append([]string{}, extra.additionalDirectories...)
		frame.AdditionalDirectories = &dirs
	}
	frame.StrictMCPConfig = extra.strictMCPConfig
	frame.AutoMode = extra.autoMode
	frame.QuestionTimeoutMs = extra.questionTimeoutMs
	frame.ExcludeDynamicSections = extra.excludeDynamicSections
	frame.ToolHints = extra.toolHints
	if !prompt.IsText() {
		frame.PromptContent = &prompt
	}
	frame.AgentProgressSummaries = s.opts.agentProgressSummaries
	frame.ForwardSubagentText = s.opts.forwardSubagentText
	if len(s.opts.env) > 0 {
		frame.Env = s.opts.env
	}
	for _, sk := range ts.Skills {
		frame.Skills = append(frame.Skills, sk.Name)
		// The bridge trusts reads under each skill's directory. Built like
		// TS (`${sandboxHomeDir}/.claude/skills/${name}`, not path-cleaned).
		frame.ReadableSkillRoots = append(frame.ReadableSkillRoots, s.opts.sandboxHomeDir+"/.claude/skills/"+sk.Name)
	}
	frame.MCPServers = s.opts.mcpServers
	frame.Debug = s.opts.debug
	return frame
}

// DoCompact rides the `/compact` slash command over the user-message rail.
// Mirrors TS `doCompact`.
func (s *session) DoCompact(ctx context.Context, customInstructions string) error {
	text := "/compact"
	if trimmed := providerutils.JSTrim(customInstructions); trimmed != "" {
		text = "/compact " + trimmed
	}
	return s.opts.channel.Send(bridge.UserMessageCommand{Text: text})
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("claude-code session %s is already stopped; cannot detach.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.opts.channel.Suspend()
	return harness.NewResumeSessionState(HarnessID, s.resumeData(lastSeenEventID))
}

func (s *session) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("claude-code session %s is stopped; cannot suspend.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.opts.channel.Suspend()
	return harness.NewContinueTurnState(HarnessID, s.resumeData(lastSeenEventID))
}

func (s *session) resumeData(lastSeenEventID float64) resumeStateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := resumeStateData{
		SandboxCredentialEnvironment: s.opts.sandboxCredentialEnvironment,
		ClaudeSessionID:              s.lastSessionID,
	}
	if lastSeenEventID >= 0 {
		data.Bridge = &bridgeCoords{Port: s.opts.bridgePort, PrivateKey: s.opts.bridgePrivateKey, LastSeenEventID: lastSeenEventID, StartID: s.activeStartID, SandboxID: s.opts.sandboxID}
	}
	return data
}

func (s *session) DoDestroy(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		// A second destroy awaits the first one's teardown (TS returns the
		// same `stopPromise`). A destroy after stop/detach has nothing to do.
		done := s.destroyed
		s.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	s.stopped = true
	s.destroyed = make(chan struct{})
	done := s.destroyed
	s.mu.Unlock()
	defer close(done)

	s.opts.channel.BeginClose()
	if !s.opts.channel.IsClosed() {
		_ = s.opts.channel.Send(bridge.DestroyCommand{})
	}
	waitForProcOrTimeout(s.opts.proc, 5*time.Second)
	if s.opts.proc != nil {
		_ = s.opts.proc.Kill()
	}
	s.opts.channel.Close()
	return nil
}

func (s *session) DoStop(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("claude-code session %s is already stopped; cannot stop.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	s.stopped = true
	s.mu.Unlock()

	s.opts.channel.BeginClose()
	var data = json.RawMessage("{}")
	if !s.opts.channel.IsClosed() {
		replied := make(chan json.RawMessage, 1)
		unsub := s.opts.channel.On("bridge-stop", func(e bridge.Event) {
			if stop, ok := e.Message.(*bridge.Stop); ok {
				select {
				case replied <- stop.Data:
				default:
				}
			}
		})
		if err := s.opts.channel.Send(bridge.StopCommand{}); err != nil {
			unsub()
			return nil, err
		}
		select {
		case d := <-replied:
			unsub()
			if len(d) > 0 {
				data = d
			}
		case <-time.After(5 * time.Second):
			unsub()
			return nil, fmt.Errorf("claude-code session %s did not reply to stop within 5s.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}

	waitForProcOrTimeout(s.opts.proc, 5*time.Second)
	if s.opts.proc != nil {
		_ = s.opts.proc.Kill()
	}
	s.opts.channel.Close()

	var lifecycleData map[string]any
	_ = json.Unmarshal(data, &lifecycleData)
	if lifecycleData == nil {
		lifecycleData = map[string]any{}
	}
	s.mu.Lock()
	if s.lastSessionID != "" {
		if _, ok := lifecycleData["claudeSessionId"]; !ok {
			lifecycleData["claudeSessionId"] = s.lastSessionID
		}
	}
	if s.opts.sandboxCredentialEnvironment != nil {
		lifecycleData["sandboxCredentialEnvironment"] = s.opts.sandboxCredentialEnvironment
	}
	s.mu.Unlock()
	return harness.NewResumeSessionState(HarnessID, lifecycleData)
}

func waitForProcOrTimeout(proc providerutils.SandboxProcess, d time.Duration) {
	if proc == nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// wireTurn subscribes to one turn's worth of bridge events and returns the
// control surface. startID is the turn's start ("" adopts the first one an
// event carries). Mirrors TS `wireTurn`.
func (s *session) wireTurn(ctx context.Context, emit harness.EmitFunc, startID string) *promptControl {
	pc := &promptControl{channel: s.opts.channel, done: make(chan struct{}), bridgeHello: s.opts.bridgeHello, turnStartTimeout: s.opts.turnStartTimeout, expectedStartID: startID}
	s.mu.Lock()
	s.activeStartID = startID
	s.mu.Unlock()
	// belongsToTurn drops late events of another (aborted) turn and every
	// event after this turn settled. An event without a startId (an older
	// bridge) belongs. Mirrors TS `belongsToTurn` (5a05735ed0).
	belongsToTurn := func(f bridge.StreamPartFrame) bool {
		pc.mu.Lock()
		defer pc.mu.Unlock()
		if pc.settled {
			return false
		}
		if f.StartID == "" {
			return true
		}
		if pc.expectedStartID == "" {
			pc.expectedStartID = f.StartID
			s.mu.Lock()
			s.activeStartID = f.StartID
			s.mu.Unlock()
		}
		return f.StartID == pc.expectedStartID
	}
	pc.checkpoint = bridge.NewCheckpointRecorder(s.opts.channel)
	forward := func(part harness.StreamPart) {
		pc.clearPendingStart()
		defer func() { _ = recover() }()
		emit(part)
	}

	// Registered before the user-message submitter's own reconnect handler,
	// so a lost turn start is retried before pending steering is replayed.
	pc.subs.Add(s.opts.channel.OnReconnect(pc.retryStartAfterReconnect))

	// Mid-turn steering: only when the bridge advertised acknowledged user
	// messages on its hello (captured once at DoStart time — see
	// sessionOptions.supportsUserMessages). Mirrors TS `wireTurn`'s
	// `supportsUserMessageResponses() ? experimental_createBridgeUserMessageSubmitter(...) : undefined`.
	// The submitter owns replay after the turn start has been retried, so
	// its requests are not queued while disconnected (TS
	// `queueIfDisconnected: false`).
	if s.opts.supportsUserMessages != nil && s.opts.supportsUserMessages() {
		channel := s.opts.channel
		noQueue := false
		pc.userMessages = bridge.NewExperimentalUserMessageSubmitter(bridge.ExperimentalUserMessageSubmitterOptions{
			Send: func(cmd bridge.UserMessageCommand) error {
				return channel.SendWithOptions(cmd, bridge.SendOptions{QueueIfDisconnected: &noQueue})
			},
			OnResponse: func(fn func(*bridge.UserMessageResponse)) func() {
				return channel.On(bridge.TypeUserMessageResponse, func(ev bridge.Event) {
					if r, ok := ev.Message.(*bridge.UserMessageResponse); ok {
						fn(r)
					}
				})
			},
			OnReconnect: channel.OnReconnect,
		})
	}

	eventTypes := []string{
		harness.PartTypeStreamStart, harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
		harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
		harness.PartTypeToolInputStart, harness.PartTypeToolInputDelta, harness.PartTypeToolInputEnd,
		harness.PartTypeToolCall, harness.PartTypeToolApprovalRequest, harness.PartTypeToolResult,
		harness.PartTypeFinishStep, harness.PartTypeCompaction, harness.PartTypeRaw,
	}
	for _, t := range eventTypes {
		t := t
		pc.subs.Add(s.opts.channel.On(t, func(e bridge.Event) {
			f, ok := e.Message.(bridge.StreamPartFrame)
			if !ok || !belongsToTurn(f) {
				return
			}
			if t == harness.PartTypeFinishStep {
				pc.checkpoint.Record(e)
			}
			forward(f.Part)
		}))
	}
	pc.subs.Add(s.opts.channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok || !belongsToTurn(f) {
			return
		}
		if fp, ok := f.Part.(*harness.FinishPart); ok {
			if cc, ok := fp.HarnessMetadata[HarnessID]; ok {
				if id, ok := cc["sessionId"].(string); ok && id != "" {
					s.mu.Lock()
					s.lastSessionID = id
					s.mu.Unlock()
				}
			}
		}
		forward(f.Part)
		pc.settleSuccess()
	}))
	// A buffered finish or error can settle the turn while its listeners are
	// still being registered (the channel replays it from On): Subscriptions
	// then removes each later listener as soon as it is added.
	pc.subs.Add(s.opts.channel.On(harness.PartTypeError, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok || !belongsToTurn(f) {
			return
		}
		forward(f.Part)
		var cause = errors.New("claude-code bridge reported an error")
		if ep, ok := f.Part.(*harness.ErrorPart); ok && ep.Error != nil {
			cause = ep.AsError()
		}
		pc.settleError(cause)
	}))
	// Listeners are registered: release events the channel buffered while an
	// attach/replay was in flight (first turn only).
	s.mu.Lock()
	finish := s.finishAttachment
	s.finishAttachment = nil
	s.mu.Unlock()
	if finish != nil {
		finish()
	}

	s.opts.channel.OnClose(func(_ int, reason string) {
		if reason == bridge.CloseReasonSuspended {
			pc.settleSuccess()
			return
		}
		pc.settleError(errors.New("claude-code bridge closed before the turn finished.")) //nolint:staticcheck // matches TS SDK's exact error text
	})

	go bridge.WatchAbort(ctx, pc.done, func() {
		_ = s.opts.channel.Send(bridge.AbortCommand{})
		pc.settleError(context.Cause(ctx))
	})

	return pc
}

// promptControl implements harness.PromptControl + ToolApprovalSubmitter +
// UserMessageSubmitter (when the bridge advertises support).
type promptControl struct {
	channel      *bridge.Channel
	userMessages *bridge.ExperimentalUserMessageSubmitter
	checkpoint   *bridge.CheckpointRecorder

	bridgeHello      func() *bridge.Hello
	turnStartTimeout time.Duration

	// subs holds the turn's bridge listener unsubscribes; settle runs them.
	subs harnessutil.Subscriptions

	// mu guards the fields below.
	mu      sync.Mutex
	settled bool
	// pendingStart is the start frame until the turn's first event (or its
	// settlement); awaitingStartSend is true while it is queued but not yet
	// written; startCursor is the event cursor when it was first sent.
	// Mirrors TS `pendingStart`, `awaitingStartSend`, `startCursor`.
	pendingStart      *StartFrame
	awaitingStartSend bool
	// expectedStartID is the start this turn's events must carry (TS
	// `expectedStartId`); "" until the first tagged event when unknown.
	expectedStartID string
	startCursor     float64
	startTimer      *time.Timer

	once sync.Once
	done chan struct{}
	err  error
}

func (c *promptControl) settleSuccess() { c.settle(nil) }

func (c *promptControl) settleError(err error) { c.settle(err) }

func (c *promptControl) settle(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.settled = true
		c.clearPendingStartLocked()
		c.mu.Unlock()
		c.err = err
		c.subs.UnsubscribeAll()
		if c.userMessages != nil {
			c.userMessages.Close(err)
		}
		close(c.done)
	})
}

// clearPendingStart forgets the start frame and stops its deadline: the
// bridge emitted a turn event, so the start arrived. Mirrors TS
// `clearPendingStart`.
func (c *promptControl) clearPendingStart() {
	c.mu.Lock()
	c.clearPendingStartLocked()
	c.mu.Unlock()
}

func (c *promptControl) clearPendingStartLocked() {
	c.pendingStart = nil
	if c.startTimer != nil {
		c.startTimer.Stop()
		c.startTimer = nil
	}
}

// sendStart sends the turn's start frame with the turn's StartID (TS
// `startId: expectedStartId`) and arms the first-event deadline. A send error settles the turn and is returned.
// Mirrors TS `sendStart`.
func (c *promptControl) sendStart(frame *StartFrame) error {
	cursor := c.channel.LastSeenEventID()
	if hello := c.hello(); hello != nil && hello.LastSeq != nil && *hello.LastSeq > cursor {
		cursor = *hello.LastSeq
	}
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return nil
	}
	c.startCursor = cursor
	if c.expectedStartID == "" {
		c.expectedStartID = newStartID()
	}
	frame.StartID = c.expectedStartID
	c.pendingStart = frame
	if timeout := c.turnStartTimeout; timeout != 0 {
		// Node's setTimeout runs a delay below 1 ms (negative included) after
		// 1 ms, so the start is sent before the deadline can fire.
		delay := timeout
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		// TS `within ${turnStartTimeoutMs}ms`: the configured milliseconds
		// as a JS number.
		ms := providerutils.JSNumberString(float64(timeout) / float64(time.Millisecond))
		c.startTimer = time.AfterFunc(delay, func() {
			c.mu.Lock()
			fire := !c.settled && c.pendingStart != nil
			c.mu.Unlock()
			if fire {
				c.failStart(fmt.Errorf("claude-code bridge did not emit a turn event within %sms.", ms)) //nolint:staticcheck // matches TS SDK's exact error text
			}
		})
	}
	c.mu.Unlock()
	if err := c.sendPendingStart(); err != nil {
		c.settleError(err)
		return err
	}
	return nil
}

// sendPendingStart (re)sends the pending start frame. awaitingStartSend
// stays true until the frame was handed to the socket, so a reconnect that
// flushes the queued frame does not send it a second time. Mirrors TS
// `sendPendingStart`.
func (c *promptControl) sendPendingStart() error {
	c.mu.Lock()
	frame := c.pendingStart
	if frame == nil {
		c.mu.Unlock()
		return nil
	}
	c.awaitingStartSend = true
	c.mu.Unlock()
	err := c.channel.SendWithOptions(frame, bridge.SendOptions{OnSent: func() {
		c.mu.Lock()
		c.awaitingStartSend = false
		c.mu.Unlock()
	}})
	if err != nil {
		c.mu.Lock()
		c.awaitingStartSend = false
		c.mu.Unlock()
	}
	return err
}

// retryStartAfterReconnect resends a start the bridge never received: only
// when the reconnected bridge reports it is waiting for a turn and has not
// emitted an event past the start's cursor. A bridge without
// idempotentStart cannot tell a resend from a new turn, so the turn fails
// instead. Mirrors the TS `channel.onReconnect` handler in `wireTurn`.
func (c *promptControl) retryStartAfterReconnect() {
	c.mu.Lock()
	if c.pendingStart == nil || c.settled || c.awaitingStartSend {
		c.mu.Unlock()
		return
	}
	cursor := c.startCursor
	c.mu.Unlock()
	hello := c.hello()
	// TS 5a05735ed0: a bridge that drops a repeated startId always gets the
	// pending start again; only one without that capability goes through
	// the waiting / cursor check, and then fails.
	if hello != nil && hello.SupportsIdempotentStart() {
		_ = c.sendPendingStart()
		return
	}
	if hello == nil || hello.State != "waiting" || hello.LastSeq == nil || *hello.LastSeq > cursor {
		return
	}
	c.failStart(errors.New("claude-code bridge is waiting for a start after reconnect and does not support safe start retries.")) //nolint:staticcheck // matches TS SDK's exact error text
}

// failStart fails the turn and tears the bridge connection down. Mirrors TS
// `failStart`.
func (c *promptControl) failStart(err error) {
	c.settleError(err)
	c.channel.BeginClose()
	_ = c.channel.Send(bridge.AbortCommand{})
	c.channel.Close()
}

func (c *promptControl) hello() *bridge.Hello {
	if c.bridgeHello == nil {
		return nil
	}
	return c.bridgeHello()
}

// newStartID returns a random turn start identifier (TS `generateId()`).
func newStartID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (c *promptControl) SubmitToolResult(ctx context.Context, result harness.ToolResultSubmission) error {
	return c.channel.Send(bridge.ToolResultCommand{
		ToolCallID: result.ToolCallID, Output: result.Output, IsError: result.IsError, ToolResult: result.ToolResult,
	})
}

func (c *promptControl) SubmitToolApproval(ctx context.Context, approval harness.ToolApprovalSubmission) error {
	return c.channel.Send(bridge.ToolApprovalResponseCommand{
		ApprovalID: approval.ApprovalID, Approved: approval.Approved, Reason: approval.Reason,
	})
}

func (c *promptControl) Done() <-chan struct{} { return c.done }
func (c *promptControl) Err() error            { return c.err }

// PinCheckpoint implements harness.CheckpointPinner by pinning the bridge
// channel's replay checkpoint to the finish-step event's own seq, recorded
// synchronously in wireTurn's listener (WG13: run_prompt.go's StopWhen
// early-stop path pins this while deciding whether to suspend, but only
// after that finish-step's StreamPart has already crossed run_prompt.go's
// buffered parts channel — the live cursor may have moved on by then, so
// this must not pin "now").
func (c *promptControl) PinCheckpoint() (release func()) { return c.checkpoint.Pin() }

// SubmitUserMessage steers the in-flight turn with an acknowledged mid-turn
// user message. It is only reachable when the bridge advertised
// experimental_userMessageResponses on hello (see wireTurn); c.userMessages
// is nil otherwise, and this type does not satisfy
// harness.UserMessageSubmitter for that turn (the interface is checked with
// a type assertion by callers, mirroring TS's `submitUserMessage` being
// absent from the returned object rather than throwing).
func (c *promptControl) SubmitUserMessage(ctx context.Context, text string) error {
	if c.userMessages == nil {
		return errors.New("claude-code: the connected bridge does not support mid-turn user messages.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return c.userMessages.Submit(ctx, text)
}
