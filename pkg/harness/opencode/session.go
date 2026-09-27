package opencode

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

type sessionParams struct {
	sessionID                      string
	channel                        *bridge.Channel
	proc                           providerutils.SandboxProcess // nil on attach
	provider                       string
	reasoningVariant               string
	openCodeConfig                 map[string]any
	mcpServers                     map[string]any
	headers                        map[string]string
	openCodeSessionID              string
	isResume                       bool
	seedResumeSessionOnFirstPrompt bool
	rerunContinue                  bool
	bridgePort                     int
	bridgeToken                    string
	sandboxID                      string
	sandboxCredentialEnvironment   map[string]string
	debug                          *harness.DebugConfig
	permissionMode                 harness.PermissionMode
	builtinToolFiltering           *harness.BuiltinToolFiltering
	sandbox                        providerutils.SandboxSession
	sandboxHomeDir                 string
	reconnect                      bridge.ReconnectOptions
	supportsUserMessageResponses   func() bool
	// transformModel remaps the wire `model` field when native GitLab
	// subscription credentials are brokered (resolveOpenCodeGitLabSubscriptionModel).
	// Nil otherwise. Mirrors TS `transformModel`.
	transformModel func(model string) (string, error)
}

type session struct {
	p sessionParams

	mu                      sync.Mutex
	stopped                 bool
	stopDone                chan struct{}
	latestOpenCodeSessionID string
	pendingResumeSessionID  string
	selectedModel           string
	activeTurn              bool
	pendingCompaction       []harness.StreamPart
}

func newSession(p sessionParams) *session {
	s := &session{p: p, latestOpenCodeSessionID: p.openCodeSessionID}
	if p.seedResumeSessionOnFirstPrompt {
		s.pendingResumeSessionID = p.openCodeSessionID
	}
	s.p.channel.On("bridge-thread", func(e bridge.Event) {
		if t, ok := e.Message.(*bridge.Thread); ok {
			s.mu.Lock()
			s.latestOpenCodeSessionID = t.ThreadID
			s.mu.Unlock()
		}
	})
	return s
}

func (s *session) SessionID() string { return s.p.sessionID }
func (s *session) IsResume() bool    { return s.p.isResume }

var eventTypes = []string{
	harness.PartTypeStreamStart,
	harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
	harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
	harness.PartTypeToolCall, harness.PartTypeToolApprovalRequest, harness.PartTypeToolResult,
	harness.PartTypeFileChange, harness.PartTypeFinishStep, harness.PartTypeCompaction, harness.PartTypeRaw,
}

type promptControl struct {
	channel   *bridge.Channel
	submitter *bridge.ExperimentalUserMessageSubmitter

	mu      sync.Mutex
	settled bool
	err     error
	done    chan struct{}
}

func (c *promptControl) settleSuccess() {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return
	}
	c.settled = true
	c.mu.Unlock()
	if c.submitter != nil {
		c.submitter.Close(nil)
	}
	close(c.done)
}

func (c *promptControl) settleError(err error) {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return
	}
	c.settled = true
	c.err = err
	c.mu.Unlock()
	if c.submitter != nil {
		c.submitter.Close(err)
	}
	close(c.done)
}

func (c *promptControl) SubmitToolResult(_ context.Context, r harness.ToolResultSubmission) error {
	return c.channel.Send(bridge.ToolResultCommand{ToolCallID: r.ToolCallID, Output: r.Output, IsError: r.IsError, ToolResult: r.ToolResult})
}

func (c *promptControl) SubmitToolApproval(_ context.Context, a harness.ToolApprovalSubmission) error {
	return c.channel.Send(bridge.ToolApprovalResponseCommand{ApprovalID: a.ApprovalID, Approved: a.Approved, Reason: a.Reason})
}

func (c *promptControl) SubmitUserMessage(ctx context.Context, text string) error {
	if c.submitter == nil {
		return harness.NewCapabilityUnsupportedError("Harness 'opencode' does not support experimental steering with this bridge version.", HarnessID, nil)
	}
	return c.submitter.Submit(ctx, text)
}

func (c *promptControl) Done() <-chan struct{} { return c.done }
func (c *promptControl) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (s *session) wireTurn(ctx context.Context, emit harness.EmitFunc) *promptControl {
	s.mu.Lock()
	s.activeTurn = true
	s.mu.Unlock()

	c := &promptControl{channel: s.p.channel, done: make(chan struct{})}
	if s.p.supportsUserMessageResponses != nil && s.p.supportsUserMessageResponses() {
		c.submitter = bridge.NewChannelUserMessageSubmitter(s.p.channel)
	}

	var unsub []func()
	forward := func(part harness.StreamPart) {
		defer func() { _ = recover() }()
		emit(part)
	}
	settleSuccess := func() {
		s.mu.Lock()
		s.activeTurn = false
		s.mu.Unlock()
		for _, u := range unsub {
			u()
		}
		c.settleSuccess()
	}
	settleError := func(err error) {
		s.mu.Lock()
		s.activeTurn = false
		s.mu.Unlock()
		for _, u := range unsub {
			u()
		}
		c.settleError(err)
	}

	for _, t := range eventTypes {
		unsub = append(unsub, s.p.channel.On(t, func(e bridge.Event) {
			if f, ok := e.Message.(bridge.StreamPartFrame); ok {
				forward(f.Part)
			}
		}))
	}
	unsub = append(unsub, s.p.channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			forward(f.Part)
		}
		settleSuccess()
	}))
	unsub = append(unsub, s.p.channel.On(harness.PartTypeError, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			settleError(errors.New("opencode: malformed error frame"))
			return
		}
		forward(f.Part)
		errPart, _ := f.Part.(*harness.ErrorPart)
		if errPart != nil {
			settleError(fmt.Errorf("%v", errPart.Error))
		} else {
			settleError(errors.New("opencode: bridge reported an error"))
		}
	}))

	s.p.channel.OnClose(func(_ int, reason string) {
		if reason == bridge.CloseReasonSuspended {
			settleSuccess()
			return
		}
		settleError(errors.New("OpenCode bridge closed before the turn finished."))
	})

	go func() {
		select {
		case <-ctx.Done():
			_ = s.p.channel.Send(bridge.AbortCommand{})
			settleError(ctx.Err())
		case <-c.done:
		}
	}()

	s.mu.Lock()
	pending := s.pendingCompaction
	s.pendingCompaction = nil
	s.mu.Unlock()
	for _, p := range pending {
		forward(p)
	}

	return c
}

func (s *session) startBase(turnModel string) (StartMessage, error) {
	if s.p.transformModel != nil {
		transformed, err := s.p.transformModel(turnModel)
		if err != nil {
			return StartMessage{}, err
		}
		turnModel = transformed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := StartMessage{
		StartBase: bridge.StartBase{
			Model: turnModel, PermissionMode: s.p.permissionMode, BuiltinToolFiltering: s.p.builtinToolFiltering,
		},
		Provider: s.p.provider, Variant: s.p.reasoningVariant, OpenCodeConfig: s.p.openCodeConfig,
		MCPServers: s.p.mcpServers, Headers: s.p.headers,
	}
	switch {
	case s.pendingResumeSessionID != "":
		msg.ResumeSessionID = s.pendingResumeSessionID
	case s.latestOpenCodeSessionID != "":
		msg.ResumeSessionID = s.latestOpenCodeSessionID
	}
	if s.p.debug != nil {
		msg.StartBase.Debug = s.p.debug
	}
	return msg, nil
}

func (s *session) prepareTurn(ctx context.Context, opts turnPrepareOptions) (*promptControl, *harnessutil.WriteSkillsResult, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, nil, harness.NewCapabilityUnsupportedError(
			"Harness 'opencode' requires a JSON schema for structured output.", HarnessID, nil)
	}
	result, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.p.sandbox, HomePath: s.p.sandboxHomeDir, SkillsDir: ".agents/skills", Skills: opts.Skills,
		InvalidSkillNameMessage: func(name string) string { return "Invalid OpenCode skill name: " + name },
		InvalidSkillFilePathMessage: func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid OpenCode skill file path for %s: %s", skillName, filePath)
		},
	})
	if err != nil {
		return nil, nil, err
	}
	control := s.wireTurn(ctx, opts.Emit)
	return control, result, nil
}

type turnPrepareOptions struct {
	ResponseFormat *harness.ResponseFormat
	Skills         []harness.Skill
	Emit           harness.EmitFunc
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	control, skillResult, err := s.prepareTurn(ctx, turnPrepareOptions{ResponseFormat: opts.ResponseFormat, Skills: opts.Skills, Emit: opts.Emit})
	if err != nil {
		return nil, err
	}
	promptText, err := extractUserText(opts.Prompt)
	if err != nil {
		return nil, err
	}
	turnModel := opts.Model
	s.mu.Lock()
	if turnModel == "" {
		turnModel = s.selectedModel
	} else {
		s.selectedModel = turnModel
	}
	s.mu.Unlock()

	msg, err := s.startBase(turnModel)
	if err != nil {
		return nil, err
	}
	msg.Operation = OperationPrompt
	msg.Prompt = promptText
	msg.Tools = opts.Tools
	msg.ResponseFormat = opts.ResponseFormat
	msg.Instructions = opts.Instructions
	msg.SkillsChanged = skillResult.Changed
	if err := s.p.channel.Send(msg); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.pendingResumeSessionID = ""
	s.mu.Unlock()
	return control, nil
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	control, skillResult, err := s.prepareTurn(ctx, turnPrepareOptions{ResponseFormat: opts.ResponseFormat, Skills: opts.Skills, Emit: opts.Emit})
	if err != nil {
		return nil, err
	}
	if s.p.rerunContinue {
		turnModel := opts.Model
		s.mu.Lock()
		if turnModel == "" {
			turnModel = s.selectedModel
		} else {
			s.selectedModel = turnModel
		}
		s.mu.Unlock()

		msg, err := s.startBase(turnModel)
		if err != nil {
			return nil, err
		}
		msg.Operation = OperationPrompt
		msg.Prompt = "Continue."
		msg.Tools = opts.Tools
		msg.ResponseFormat = opts.ResponseFormat
		msg.Instructions = opts.Instructions
		msg.SkillsChanged = skillResult.Changed
		if err := s.p.channel.Send(msg); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.pendingResumeSessionID = ""
		s.mu.Unlock()
	}
	return control, nil
}

func (s *session) DoCompact(ctx context.Context, customInstructions string) error {
	if strings.TrimSpace(customInstructions) != "" {
		return harness.NewCapabilityUnsupportedError(
			"Harness 'opencode' supports native manual compaction, but OpenCode does not expose custom compaction instructions through the supported API.", HarnessID, nil)
	}
	s.mu.Lock()
	active := s.activeTurn
	s.mu.Unlock()
	if active {
		return harness.NewCapabilityUnsupportedError(
			"Harness 'opencode' supports manual compaction between turns; compacting during an active turn is not supported by the bridge transport.", HarnessID, nil)
	}
	model := s.currentModel()
	msg, err := s.startBase(model)
	if err != nil {
		return err
	}
	msg.Operation = OperationCompact
	msg.Prompt = ""
	msg.Tools = nil
	return s.runCompactOperation(msg)
}

func (s *session) currentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedModel
}

func (s *session) runCompactOperation(msg StartMessage) error {
	done := make(chan error, 1)
	var unsub []func()
	settle := func(err error) {
		for _, u := range unsub {
			u()
		}
		select {
		case done <- err:
		default:
		}
	}
	unsub = append(unsub, s.p.channel.On(harness.PartTypeCompaction, func(e bridge.Event) {
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			s.mu.Lock()
			s.pendingCompaction = append(s.pendingCompaction, f.Part)
			s.mu.Unlock()
		}
	}))
	unsub = append(unsub, s.p.channel.On(harness.PartTypeFinish, func(bridge.Event) { settle(nil) }))
	unsub = append(unsub, s.p.channel.On(harness.PartTypeError, func(e bridge.Event) {
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			if ep, ok := f.Part.(*harness.ErrorPart); ok {
				settle(fmt.Errorf("%v", ep.Error))
				return
			}
		}
		settle(errors.New("opencode: bridge reported an error"))
	}))
	if err := s.p.channel.Send(msg); err != nil {
		settle(err)
	}
	return <-done
}

func (s *session) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("OpenCode session %s is stopped; cannot suspend.", s.p.sessionID)
	}
	s.stopped = true
	latest := s.latestOpenCodeSessionID
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := resumeStateData{
		OpenCodeSessionID: latest, SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment,
		Bridge: &bridgeCoords{Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.p.sandboxID},
	}
	return harness.NewContinueTurnState(HarnessID, data)
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("OpenCode session %s is already stopped; cannot detach.", s.p.sessionID)
	}
	s.stopped = true
	latest := s.latestOpenCodeSessionID
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := resumeStateData{
		OpenCodeSessionID: latest, SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment,
		Bridge: &bridgeCoords{Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.p.sandboxID},
	}
	return harness.NewResumeSessionState(HarnessID, data)
}

func (s *session) DoStop(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("OpenCode session %s is already stopped; cannot stop.", s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	s.p.channel.BeginClose()
	var stopData map[string]any
	if !s.p.channel.IsClosed() {
		replyCh := make(chan map[string]any, 1)
		unsub := s.p.channel.On(bridge.TypeStop, func(e bridge.Event) {
			if stop, ok := e.Message.(*bridge.Stop); ok {
				var m map[string]any
				_ = json.Unmarshal(stop.Data, &m)
				select {
				case replyCh <- m:
				default:
				}
			}
		})
		if err := s.p.channel.Send(bridge.StopCommand{}); err != nil {
			unsub()
			return nil, err
		}
		select {
		case stopData = <-replyCh:
			unsub()
		case <-time.After(5 * time.Second):
			unsub()
			return nil, fmt.Errorf("OpenCode session %s did not reply to stop within 5s.", s.p.sessionID)
		}
	}

	teardownProc(s.p.proc)
	s.p.channel.Close()

	data := map[string]any{}
	for k, v := range stopData {
		data[k] = v
	}
	if s.p.sandboxCredentialEnvironment != nil {
		data["sandboxCredentialEnvironment"] = s.p.sandboxCredentialEnvironment
	}
	return harness.NewResumeSessionState(HarnessID, data)
}

func (s *session) DoDestroy(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

	s.p.channel.BeginClose()
	if !s.p.channel.IsClosed() {
		_ = s.p.channel.Send(bridge.DestroyCommand{})
	}
	teardownProc(s.p.proc)
	s.p.channel.Close()
	return nil
}

func teardownProc(proc providerutils.SandboxProcess) {
	if proc == nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = proc.Kill()
}

func attachToRunningBridge(ctx context.Context, sandboxSession providerutils.SandboxSession, settings Settings, coords *bridgeCoords, isContinue bool, resumeSessionID string, helloTimeout time.Duration, onBridgeErr func(*harness.ErrorPart), onDiagnostic func(bridge.OutboundMessage), p sessionParams) (*session, bool) {
	endpoint, err := resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, coords.Port)
	if err != nil {
		return nil, false
	}
	endpoint, err = bridge.WithBridgeToken(endpoint, coords.Token)
	if err != nil {
		return nil, false
	}
	var supportsUserMessageResponses bool
	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect: bridge.NewConnectFunc(endpoint, bridge.DialOptions{
			Name: "OpenCode bridge", WaitForHello: true, HelloTimeout: helloTimeout,
			OnHello: func(h *bridge.Hello) {
				if h.Capabilities != nil && h.Capabilities.ExperimentalUserMessageResponses != nil {
					supportsUserMessageResponses = *h.Capabilities.ExperimentalUserMessageResponses
				}
			},
		}),
		InitialLastSeenEventID: coords.LastSeenEventID, OnBridgeError: onBridgeErr, OnDiagnostic: onDiagnostic, Reconnect: settings.Reconnect,
	})
	if err := channel.Open(ctx, isContinue); err != nil {
		return nil, false
	}
	p.channel = channel
	p.proc = nil
	p.openCodeSessionID = resumeSessionID
	p.isResume = true
	p.seedResumeSessionOnFirstPrompt = false
	p.rerunContinue = false
	p.bridgePort = coords.Port
	p.bridgeToken = coords.Token
	p.supportsUserMessageResponses = func() bool { return supportsUserMessageResponses }
	return newSession(p), true
}

// extractUserText reduces the prompt to plain user text; non-text parts are
// unsupported. Mirrors TS `extractUserText`.
func extractUserText(p harness.Prompt) (string, error) {
	if p.Message == nil {
		return p.Text, nil
	}
	var parts []string
	for _, part := range p.Message.Content {
		text, ok := part.(types.TextContent)
		if !ok {
			return "", harness.NewCapabilityUnsupportedError(
				fmt.Sprintf("The OpenCode harness does not yet support user message parts of type '%s'. Pass a string or a user message whose content contains only text parts.", part.ContentType()),
				HarnessID, nil)
		}
		parts = append(parts, text.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}
