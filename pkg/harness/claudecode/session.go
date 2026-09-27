package claudecode

import (
	"context"
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

// sessionOptions is the input of newSession. Mirrors the object TS
// `createSession` closes over.
type sessionOptions struct {
	sessionID string
	channel   *bridge.Channel
	// proc is nil when attached to a bridge spawned by another process (not
	// reachable from this port — see package doc: DoStart never attaches).
	proc providerutils.SandboxProcess

	maxTurns int
	env      map[string]string
	thinking ThinkingConfig
	effort   string

	isResume              bool
	continueOnFirstPrompt bool
	rerunContinue         bool
	resumeSessionID       string

	bridgePort                   int
	bridgeToken                  string
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
	supportsUserMessages bool
}

type session struct {
	opts sessionOptions

	mu                sync.Mutex
	stopped           bool
	pendingResumeFlag bool
	lastSessionID     string
}

func newSession(opts sessionOptions) *session {
	return &session{opts: opts, pendingResumeFlag: opts.continueOnFirstPrompt, lastSessionID: opts.resumeSessionID}
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
	control := s.wireTurn(ctx, opts.Emit)
	if ctx.Err() != nil {
		return control, nil
	}

	text, err := extractUserText(opts.Prompt)
	if err != nil {
		return nil, err
	}
	frame := s.buildStartFrame(text, opts.TurnSettings, opts.ResponseFormat)
	s.mu.Lock()
	frame.Continue, frame.ResumeSessionID = s.resumeFields()
	s.pendingResumeFlag = false
	s.mu.Unlock()
	if err := s.opts.channel.Send(frame); err != nil {
		return nil, err
	}
	return control, nil
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	if err := s.prepareTurn(ctx, opts.Skills, opts.ResponseFormat); err != nil {
		return nil, err
	}
	control := s.wireTurn(ctx, opts.Emit)
	if s.opts.rerunContinue && ctx.Err() == nil {
		frame := s.buildStartFrame("Continue.", opts.TurnSettings, opts.ResponseFormat)
		s.mu.Lock()
		frame.Continue, frame.ResumeSessionID = s.resumeFields()
		s.pendingResumeFlag = false
		s.mu.Unlock()
		if err := s.opts.channel.Send(frame); err != nil {
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

func (s *session) buildStartFrame(prompt string, ts harness.TurnSettings, rf *harness.ResponseFormat) *StartFrame {
	frame := &StartFrame{
		StartBase: bridge.StartBase{
			Prompt: prompt, Tools: ts.Tools, Model: ts.Model, ResponseFormat: rf,
			PermissionMode: s.opts.permissionMode, BuiltinToolFiltering: s.opts.builtinToolFiltering,
		},
		Instructions: ts.Instructions,
		Thinking:     &s.opts.thinking,
		Effort:       s.opts.effort,
	}
	if s.opts.maxTurns > 0 {
		frame.MaxTurns = &s.opts.maxTurns
	}
	if len(s.opts.env) > 0 {
		frame.Env = s.opts.env
	}
	for _, sk := range ts.Skills {
		frame.Skills = append(frame.Skills, sk.Name)
	}
	frame.MCPServers = s.opts.mcpServers
	return frame
}

// DoCompact rides the `/compact` slash command over the user-message rail.
// Mirrors TS `doCompact`.
func (s *session) DoCompact(ctx context.Context, customInstructions string) error {
	text := "/compact"
	if trimmed := strings.TrimSpace(customInstructions); trimmed != "" {
		text = "/compact " + trimmed
	}
	return s.opts.channel.Send(bridge.UserMessageCommand{Text: text})
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("claude-code session %s is already stopped; cannot detach.", s.opts.sessionID)
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
		return nil, fmt.Errorf("claude-code session %s is stopped; cannot suspend.", s.opts.sessionID)
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
		data.Bridge = &bridgeCoords{Port: s.opts.bridgePort, Token: s.opts.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.opts.sandboxID}
	}
	return data
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
		return nil, fmt.Errorf("claude-code session %s is already stopped; cannot stop.", s.opts.sessionID)
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
			return nil, fmt.Errorf("claude-code session %s did not reply to stop within 5s.", s.opts.sessionID)
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
// control surface. Mirrors TS `wireTurn`.
func (s *session) wireTurn(ctx context.Context, emit harness.EmitFunc) *promptControl {
	pc := &promptControl{channel: s.opts.channel, done: make(chan struct{})}
	forward := func(part harness.StreamPart) {
		defer func() { _ = recover() }()
		emit(part)
	}

	// Mid-turn steering: only when the bridge advertised acknowledged user
	// messages on its hello (captured once at DoStart time — see
	// sessionOptions.supportsUserMessages). Mirrors TS `wireTurn`'s
	// `supportsUserMessageResponses() ? experimental_createBridgeUserMessageSubmitter(...) : undefined`.
	if s.opts.supportsUserMessages {
		pc.userMessages = bridge.NewChannelUserMessageSubmitter(s.opts.channel)
	}

	eventTypes := []string{
		harness.PartTypeStreamStart, harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
		harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
		harness.PartTypeToolInputStart, harness.PartTypeToolInputDelta, harness.PartTypeToolInputEnd,
		harness.PartTypeToolCall, harness.PartTypeToolApprovalRequest, harness.PartTypeToolResult,
		harness.PartTypeFinishStep, harness.PartTypeCompaction, harness.PartTypeRaw,
	}
	var unsubs []func()
	for _, t := range eventTypes {
		t := t
		unsubs = append(unsubs, s.opts.channel.On(t, func(e bridge.Event) {
			if f, ok := e.Message.(bridge.StreamPartFrame); ok {
				forward(f.Part)
			}
		}))
	}
	unsubs = append(unsubs, s.opts.channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
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
	unsubs = append(unsubs, s.opts.channel.On(harness.PartTypeError, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			return
		}
		forward(f.Part)
		var cause error = errors.New("claude-code bridge reported an error")
		if ep, ok := f.Part.(*harness.ErrorPart); ok && ep.Error != nil {
			cause = fmt.Errorf("%v", ep.Error)
		}
		pc.settleError(cause)
	}))
	pc.unsubs = unsubs

	s.opts.channel.OnClose(func(_ int, reason string) {
		if reason == bridge.CloseReasonSuspended {
			pc.settleSuccess()
			return
		}
		pc.settleError(errors.New("claude-code bridge closed before the turn finished."))
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
// UserMessageSubmitter (when the bridge advertises support).
type promptControl struct {
	channel      *bridge.Channel
	unsubs       []func()
	userMessages *bridge.ExperimentalUserMessageSubmitter

	once sync.Once
	done chan struct{}
	err  error
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

// SubmitUserMessage steers the in-flight turn with an acknowledged mid-turn
// user message. It is only reachable when the bridge advertised
// experimental_userMessageResponses on hello (see wireTurn); c.userMessages
// is nil otherwise, and this type does not satisfy
// harness.UserMessageSubmitter for that turn (the interface is checked with
// a type assertion by callers, mirroring TS's `submitUserMessage` being
// absent from the returned object rather than throwing).
func (c *promptControl) SubmitUserMessage(ctx context.Context, text string) error {
	if c.userMessages == nil {
		return errors.New("claude-code: the connected bridge does not support mid-turn user messages.")
	}
	return c.userMessages.Submit(ctx, text)
}

// extractUserText mirrors TS `extractUserText`: a bare string prompt, or a
// user message whose content is only text parts.
func extractUserText(prompt harness.Prompt) (string, error) {
	if prompt.Message == nil {
		return prompt.Text, nil
	}
	var parts []string
	for _, part := range prompt.Message.Content {
		text, ok := part.(types.TextContent)
		if !ok {
			return "", harness.NewCapabilityUnsupportedError(
				fmt.Sprintf("The claude-code harness does not yet support user message parts of type '%s'. Pass a string or a user message whose content contains only text parts.", part.ContentType()),
				HarnessID, nil)
		}
		parts = append(parts, text.Text)
	}
	return joinLinesDouble(parts), nil
}

func joinLinesDouble(parts []string) string { return strings.Join(parts, "\n\n") }
