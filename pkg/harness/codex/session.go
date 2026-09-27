package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	proc      providerutils.SandboxProcess

	model                string
	reasoningEffort      string
	webSearch            *bool
	builtinToolFiltering *harness.BuiltinToolFiltering
	codexConfig          map[string]any
	mcpServers           map[string]any
	headers              map[string]string

	isResume                      bool
	seedResumeThreadOnFirstPrompt bool
	rerunContinue                 bool
	resumeThreadID                string

	bridgePort                   int
	bridgeToken                  string
	sandboxID                    string
	sandboxCredentialEnvironment map[string]string

	permissionMode harness.PermissionMode
	sandbox        providerutils.SandboxSession
	sandboxHomeDir string

	turnConfigurationFingerprint string
}

type session struct {
	opts sessionOptions

	mu                                 sync.Mutex
	stopped                            bool
	pendingResumeThreadID              string
	latestThreadID                     string
	latestTurnConfigurationFingerprint string
}

func newSession(opts sessionOptions) *session {
	s := &session{
		opts:                               opts,
		latestThreadID:                     opts.resumeThreadID,
		latestTurnConfigurationFingerprint: opts.turnConfigurationFingerprint,
	}
	if opts.seedResumeThreadOnFirstPrompt {
		s.pendingResumeThreadID = opts.resumeThreadID
	}
	opts.channel.On("bridge-thread", func(e bridge.Event) {
		if t, ok := e.Message.(*bridge.Thread); ok {
			s.mu.Lock()
			s.latestThreadID = t.ThreadID
			s.mu.Unlock()
		}
	})
	return s
}

func (s *session) SessionID() string { return s.opts.sessionID }
func (s *session) IsResume() bool    { return s.opts.isResume }

// synchronizeTurnConfiguration mirrors TS `synchronizeTurnConfiguration`.
func (s *session) synchronizeTurnConfiguration(ctx context.Context, skills []harness.Skill, instructions string, tools []harness.ToolSpec) (restartThread bool, err error) {
	result, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.opts.sandbox, HomePath: s.opts.sandboxHomeDir, SkillsDir: ".agents/skills", Skills: skills,
		InvalidSkillNameMessage: func(name string) string { return fmt.Sprintf("Invalid Codex skill name: %s", name) },
		InvalidSkillFilePathMessage: func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid Codex skill file path for %s: %s", skillName, filePath)
		},
	})
	if err != nil {
		return false, err
	}
	fingerprint := fingerprintTurnConfiguration(instructions, tools)

	s.mu.Lock()
	defer s.mu.Unlock()
	restart := s.latestThreadID != "" &&
		(result.Changed || (s.latestTurnConfigurationFingerprint != "" && s.latestTurnConfigurationFingerprint != fingerprint))
	s.latestTurnConfigurationFingerprint = fingerprint
	if restart {
		s.pendingResumeThreadID = ""
	}
	return restart, nil
}

func fingerprintTurnConfiguration(instructions string, tools []harness.ToolSpec) string {
	type wireTool struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		InputSchema any    `json:"inputSchema,omitempty"`
	}
	wireTools := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		wireTools = append(wireTools, wireTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	var instructionsValue any
	if instructions != "" {
		instructionsValue = instructions
	}
	payload, _ := json.Marshal(struct {
		Instructions any        `json:"instructions"`
		Tools        []wireTool `json:"tools"`
	}{instructionsValue, wireTools})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, harness.NewCapabilityUnsupportedError("Harness 'codex' requires a JSON schema for structured output.", HarnessID, nil)
	}
	restartThread, err := s.synchronizeTurnConfiguration(ctx, opts.Skills, opts.Instructions, opts.Tools)
	if err != nil {
		return nil, err
	}
	text, err := extractUserText(opts.Prompt)
	if err != nil {
		return nil, err
	}

	control := s.wireTurn(ctx, opts.Emit)
	if ctx.Err() != nil {
		return control, nil
	}
	frame := s.buildStartFrame(text, opts.TurnSettings, opts.ResponseFormat, restartThread, true)
	if err := s.opts.channel.Send(frame); err != nil {
		return nil, err
	}
	return control, nil
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, harness.NewCapabilityUnsupportedError("Harness 'codex' requires a JSON schema for structured output.", HarnessID, nil)
	}
	restartThread, err := s.synchronizeTurnConfiguration(ctx, opts.Skills, opts.Instructions, opts.Tools)
	if err != nil {
		return nil, err
	}
	control := s.wireTurn(ctx, opts.Emit)
	if s.opts.rerunContinue && ctx.Err() == nil {
		frame := s.buildStartFrame("Continue.", opts.TurnSettings, opts.ResponseFormat, restartThread, false)
		if err := s.opts.channel.Send(frame); err != nil {
			return nil, err
		}
	}
	return control, nil
}

func (s *session) buildStartFrame(prompt string, ts harness.TurnSettings, rf *harness.ResponseFormat, restartThread, useFirstPromptResume bool) *StartFrame {
	frame := &StartFrame{
		StartBase: bridge.StartBase{
			Prompt: prompt, Tools: ts.Tools, Model: firstNonEmpty(ts.Model, s.opts.model), ResponseFormat: rf,
			PermissionMode: s.opts.permissionMode, BuiltinToolFiltering: s.opts.builtinToolFiltering,
		},
		Instructions:    ts.Instructions,
		ReasoningEffort: s.opts.reasoningEffort,
		WebSearch:       s.opts.webSearch,
		CodexConfig:     s.opts.codexConfig,
		MCPServers:      s.opts.mcpServers,
		Headers:         s.opts.headers,
		RestartThread:   restartThread,
	}
	s.mu.Lock()
	if useFirstPromptResume {
		frame.ResumeThreadID = s.pendingResumeThreadID
		s.pendingResumeThreadID = ""
	} else {
		frame.ResumeThreadID = s.pendingResumeThreadID
		if frame.ResumeThreadID == "" {
			frame.ResumeThreadID = s.latestThreadID
		}
		s.pendingResumeThreadID = ""
	}
	s.mu.Unlock()
	return frame
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// DoCompact is unsupported: `codex exec` exposes no manual compaction
// trigger. Mirrors TS `doCompact`.
func (s *session) DoCompact(ctx context.Context, customInstructions string) error {
	return harness.NewCapabilityUnsupportedError(
		"Harness 'codex' does not support manual compaction; Codex auto-compacts its context internally.", HarnessID, nil)
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("codex session %s is already stopped; cannot detach.", s.opts.sessionID)
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
		return nil, fmt.Errorf("codex session %s is stopped; cannot suspend.", s.opts.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()
	lastSeenEventID := <-s.opts.channel.Suspend()
	return harness.NewContinueTurnState(HarnessID, s.resumeData(lastSeenEventID))
}

func (s *session) resumeData(lastSeenEventID float64) resumeStateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return resumeStateData{
		ThreadID: s.latestThreadID, TurnConfigurationFingerprint: s.latestTurnConfigurationFingerprint,
		SandboxCredentialEnvironment: s.opts.sandboxCredentialEnvironment,
		Bridge:                       &bridgeCoords{Port: s.opts.bridgePort, Token: s.opts.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.opts.sandboxID},
	}
}

func (s *session) DoDestroy(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

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
		return nil, fmt.Errorf("codex session %s is already stopped; cannot stop.", s.opts.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	s.opts.channel.BeginClose()
	var data json.RawMessage = json.RawMessage("{}")
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
			return nil, fmt.Errorf("codex session %s did not reply to stop within 5s.", s.opts.sessionID)
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
	if s.latestTurnConfigurationFingerprint != "" {
		lifecycleData["turnConfigurationFingerprint"] = s.latestTurnConfigurationFingerprint
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
// control surface. Mirrors TS `wireTurn`. TS defers sending `start` by one
// macrotask (`setTimeout(0)`) purely to let its own synchronous listener
// registration finish first; that ordering constraint does not apply here
// because Go's Channel already buffers inbound frames in arrival order and
// replays them to a listener regardless of when it subscribes relative to
// when the bridge started sending (see bridge.Channel's doc comment), so
// DoPromptTurn/DoContinueTurn call wireTurn and then Send directly.
func (s *session) wireTurn(ctx context.Context, emit harness.EmitFunc) *promptControl {
	pc := &promptControl{channel: s.opts.channel, done: make(chan struct{}), emit: emit}
	pc.checkpoint = bridge.NewCheckpointRecorder(s.opts.channel)
	// Mid-turn steering: unconditionally wired, mirroring TS `wireTurn`'s
	// unconditional `experimental_createBridgeUserMessageSubmitter(...)`
	// (unlike claude-code/opencode, codex does not gate this on a
	// bridge-advertised hello capability — the bridge always accepts
	// `turn/steer` once a turn is active).
	pc.userMessages = bridge.NewChannelUserMessageSubmitter(s.opts.channel)

	eventTypes := []string{
		harness.PartTypeStreamStart, harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
		harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
		harness.PartTypeToolCall, harness.PartTypeToolApprovalRequest, harness.PartTypeToolResult,
		harness.PartTypeFileChange, harness.PartTypeFinishStep, harness.PartTypeRaw,
	}
	var unsubs []func()
	for _, t := range eventTypes {
		unsubs = append(unsubs, s.opts.channel.On(t, func(e bridge.Event) {
			if t == harness.PartTypeFinishStep {
				pc.checkpoint.Record(e)
			}
			if f, ok := e.Message.(bridge.StreamPartFrame); ok {
				pc.forward(f.Part)
			}
		}))
	}
	unsubs = append(unsubs, s.opts.channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			pc.forward(f.Part)
		}
		pc.settleSuccess()
	}))
	unsubs = append(unsubs, s.opts.channel.On(harness.PartTypeError, func(e bridge.Event) {
		var cause error = errors.New("codex bridge reported an error")
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			pc.forward(f.Part)
			if ep, ok := f.Part.(*harness.ErrorPart); ok && ep.Error != nil {
				cause = fmt.Errorf("%v", ep.Error)
			}
		}
		pc.settleError(cause)
	}))
	pc.unsubs = unsubs

	s.opts.channel.OnClose(func(_ int, reason string) {
		if reason == bridge.CloseReasonSuspended {
			pc.settleSuccess()
			return
		}
		pc.settleError(errors.New("codex bridge closed before the turn finished."))
	})

	go func() {
		select {
		case <-ctx.Done():
			_ = s.opts.channel.Send(bridge.AbortCommand{})
			pc.settleError(ctx.Err())
		case <-pc.done:
		}
	}()

	return pc
}

// promptControl implements harness.PromptControl + ToolApprovalSubmitter +
// UserMessageSubmitter.
type promptControl struct {
	channel      *bridge.Channel
	emit         harness.EmitFunc
	unsubs       []func()
	userMessages *bridge.ExperimentalUserMessageSubmitter
	checkpoint   *bridge.CheckpointRecorder

	once sync.Once
	done chan struct{}
	err  error
}

func (c *promptControl) forward(part harness.StreamPart) {
	defer func() { _ = recover() }()
	c.emit(part)
}

func (c *promptControl) settleSuccess() {
	c.once.Do(func() {
		for _, u := range c.unsubs {
			u()
		}
		if c.userMessages != nil {
			c.userMessages.Close(nil)
		}
		close(c.done)
	})
}

func (c *promptControl) settleError(err error) {
	c.once.Do(func() {
		c.err = err
		for _, u := range c.unsubs {
			u()
		}
		if c.userMessages != nil {
			c.userMessages.Close(err)
		}
		close(c.done)
	})
}

func (c *promptControl) SubmitToolResult(ctx context.Context, result harness.ToolResultSubmission) error {
	return c.channel.Send(bridge.ToolResultCommand{ToolCallID: result.ToolCallID, Output: result.Output, IsError: result.IsError})
}

func (c *promptControl) SubmitToolApproval(ctx context.Context, approval harness.ToolApprovalSubmission) error {
	return c.channel.Send(bridge.ToolApprovalResponseCommand{ApprovalID: approval.ApprovalID, Approved: approval.Approved, Reason: approval.Reason})
}

func (c *promptControl) Done() <-chan struct{} { return c.done }
func (c *promptControl) Err() error            { return c.err }

// SubmitUserMessage steers the in-flight turn with an acknowledged mid-turn
// user message. Mirrors TS `control.submitUserMessage`, which is present
// unconditionally on the returned control object (see wireTurn).
func (c *promptControl) SubmitUserMessage(ctx context.Context, text string) error {
	return c.userMessages.Submit(ctx, text)
}

// PinCheckpoint implements harness.CheckpointPinner by pinning the bridge
// channel's replay checkpoint to the finish-step event's own seq, recorded
// synchronously in wireTurn's listener (WG13: run_prompt.go's StopWhen
// early-stop path pins this while deciding whether to suspend, but only
// after that finish-step's StreamPart has already crossed run_prompt.go's
// buffered parts channel — the live cursor may have moved on by then, so
// this must not pin "now").
func (c *promptControl) PinCheckpoint() (release func()) { return c.checkpoint.Pin() }

// extractUserText mirrors TS `extractUserText`.
func extractUserText(prompt harness.Prompt) (string, error) {
	if prompt.Message == nil {
		return prompt.Text, nil
	}
	var parts []string
	for _, part := range prompt.Message.Content {
		text, ok := part.(types.TextContent)
		if !ok {
			return "", harness.NewCapabilityUnsupportedError(
				fmt.Sprintf("The codex harness does not yet support user message parts of type '%s'. Pass a string or a user message whose content contains only text parts.", part.ContentType()),
				HarnessID, nil)
		}
		parts = append(parts, text.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}
