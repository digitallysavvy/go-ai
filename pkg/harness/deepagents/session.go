package deepagents

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// sessionParams collects every value createSession/attach need. Mirrors the
// parameter object of TS `createSession`.
type sessionParams struct {
	sessionID                    string
	channel                      *bridge.Channel
	proc                         providerutils.SandboxProcess // nil on attach
	thinking                     *ThinkingConfig
	effort                       string
	bridgePort                   int
	bridgeToken                  string
	sandboxID                    string
	sandboxCredentialEnvironment map[string]string
	isResume                     bool
	sandbox                      providerutils.SandboxSession
	homeDir                      string
	skillsPaths                  []string
	permissionMode               harness.PermissionMode
	builtinToolFiltering         *harness.BuiltinToolFiltering
	recursionLimit               *int
	mcpServers                   map[string]any
	headers                      map[string]string
	reconnect                    bridge.ReconnectOptions
}

type session struct {
	p sessionParams

	mu      sync.Mutex
	stopped bool
}

func newSession(p sessionParams) *session { return &session{p: p} }

func (s *session) SessionID() string { return s.p.sessionID }
func (s *session) IsResume() bool    { return s.p.isResume }

// eventTypes are the harness stream-part types the deepagents bridge emits.
// Mirrors TS `wireTurn`'s `eventTypes` array.
var eventTypes = []string{
	harness.PartTypeStreamStart,
	harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
	harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
	harness.PartTypeToolCall, harness.PartTypeToolApprovalRequest, harness.PartTypeToolResult,
	harness.PartTypeFileChange, harness.PartTypeFinishStep, harness.PartTypeRaw,
}

// promptControl implements harness.PromptControl and harness.ToolApprovalSubmitter,
// wired to one live bridge channel/turn. Mirrors TS `wireTurn`'s returned
// HarnessV1PromptControl.
type promptControl struct {
	channel *bridge.Channel

	mu        sync.Mutex
	settled   bool
	err       error
	done      chan struct{}
	unsub     []func()
	onceClose sync.Once
}

func (c *promptControl) settleSuccess() {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return
	}
	c.settled = true
	unsub := c.unsub
	c.mu.Unlock()
	for _, u := range unsub {
		u()
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
	unsub := c.unsub
	c.mu.Unlock()
	for _, u := range unsub {
		u()
	}
	close(c.done)
}

func (c *promptControl) SubmitToolResult(_ context.Context, r harness.ToolResultSubmission) error {
	return c.channel.Send(bridge.ToolResultCommand{ToolCallID: r.ToolCallID, Output: r.Output, IsError: r.IsError})
}

func (c *promptControl) SubmitToolApproval(_ context.Context, a harness.ToolApprovalSubmission) error {
	return c.channel.Send(bridge.ToolApprovalResponseCommand{ApprovalID: a.ApprovalID, Approved: a.Approved, Reason: a.Reason})
}

func (c *promptControl) Done() <-chan struct{} { return c.done }
func (c *promptControl) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// wireTurn subscribes to one live turn's events, forwarding every
// harness.StreamPart to emit until `finish`/`error` settle the turn or the
// channel closes. Mirrors TS `wireTurn`.
func wireTurn(ctx context.Context, channel *bridge.Channel, emit harness.EmitFunc) harness.PromptControl {
	c := &promptControl{channel: channel, done: make(chan struct{})}

	forward := func(part harness.StreamPart) {
		defer func() { _ = recover() }()
		emit(part)
	}

	for _, t := range eventTypes {
		t := t
		unsub := channel.On(t, func(e bridge.Event) {
			if f, ok := e.Message.(bridge.StreamPartFrame); ok {
				forward(f.Part)
			}
		})
		c.unsub = append(c.unsub, unsub)
	}
	c.unsub = append(c.unsub, channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			forward(f.Part)
		}
		c.settleSuccess()
	}))
	c.unsub = append(c.unsub, channel.On(harness.PartTypeError, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			c.settleError(errors.New("deepagents: malformed error frame"))
			return
		}
		forward(f.Part)
		errPart, _ := f.Part.(*harness.ErrorPart)
		var err error
		if errPart != nil {
			err = fmt.Errorf("%v", errPart.Error)
		} else {
			err = errors.New("deepagents: bridge reported an error")
		}
		c.settleError(err)
	}))

	// A "suspended" close is a graceful slice-boundary freeze (suspend/detach
	// keep the bridge alive for continuation); end the turn cleanly. Any
	// other close is an unexpected bridge failure.
	channel.OnClose(func(_ int, reason string) {
		if reason == bridge.CloseReasonSuspended {
			c.settleSuccess()
			return
		}
		c.settleError(errors.New("deepagents bridge closed before the turn finished."))
	})

	go func() {
		select {
		case <-ctx.Done():
			_ = channel.Send(bridge.AbortCommand{})
			c.settleError(ctx.Err())
		case <-c.done:
		}
	}()

	return c
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, harness.NewCapabilityUnsupportedError(
			"Harness 'deepagents' requires a JSON schema for structured output.", HarnessID, nil)
	}
	promptText, err := extractUserText(opts.Prompt)
	if err != nil {
		return nil, err
	}
	skillResult, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.p.sandbox, HomePath: s.p.homeDir, SkillsDir: ".agents/skills", Skills: opts.Skills,
		SkillNamePattern:            skillNamePattern,
		InvalidSkillNameMessage:     invalidSkillNameMessage,
		FilePathMode:                harnessutil.SkillFilePathStripLeadingSlashes,
		InvalidSkillFilePathMessage: invalidSkillFilePathMessage,
	})
	if err != nil {
		return nil, err
	}

	control := wireTurn(ctx, s.p.channel, opts.Emit)

	msg := StartMessage{
		StartBase: bridge.StartBase{
			Prompt: promptText, Tools: opts.Tools, Model: opts.Model,
			ResponseFormat: opts.ResponseFormat, PermissionMode: s.p.permissionMode,
			BuiltinToolFiltering: s.p.builtinToolFiltering,
		},
		Instructions: opts.Instructions, Thinking: s.p.thinking, Effort: s.p.effort,
		SkillsPaths: s.p.skillsPaths, SkillsChanged: skillResult.Changed,
		RecursionLimit: s.p.recursionLimit, MCPServers: s.p.mcpServers, Headers: s.p.headers,
	}
	if err := s.p.channel.Send(msg); err != nil {
		return nil, err
	}
	return control, nil
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, harness.NewCapabilityUnsupportedError(
			"Harness 'deepagents' requires a JSON schema for structured output.", HarnessID, nil)
	}
	// Attach/replay: DoStart opened with resume=true so the bridge replays
	// past the cursor; no `start` is sent (that would clear the replay log).
	return wireTurn(ctx, s.p.channel, opts.Emit), nil
}

func (s *session) DoCompact(context.Context, string) error { return unsupported("manual compaction") }

func (s *session) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("deepagents session %s is stopped; cannot suspend.", s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := resumeStateData{
		SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment,
		Bridge: &bridgeCoords{
			Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.p.sandboxID,
		},
	}
	return harness.NewContinueTurnState(HarnessID, data)
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("deepagents session %s is already stopped; cannot detach.", s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := resumeStateData{
		SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment,
		Bridge: &bridgeCoords{
			Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.p.sandboxID,
		},
	}
	return harness.NewResumeSessionState(HarnessID, data)
}

func (s *session) DoStop(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("deepagents session %s is already stopped; cannot stop.", s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	teardown(s.p.channel, s.p.proc, bridge.TypeStopCommand)
	data := resumeStateData{SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment}
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
	teardown(s.p.channel, s.p.proc, bridge.TypeDestroyCommand)
	return nil
}

// teardown mirrors TS `teardown`: begin-close, ask the bridge to stop/destroy,
// wait briefly for the process to exit, then kill it and close the channel.
func teardown(channel *bridge.Channel, proc providerutils.SandboxProcess, operation string) {
	channel.BeginClose()
	if !channel.IsClosed() {
		if operation == bridge.TypeStopCommand {
			_ = channel.Send(bridge.StopCommand{})
		} else {
			_ = channel.Send(bridge.DestroyCommand{})
		}
	}
	if proc != nil {
		done := make(chan struct{})
		go func() { _, _ = proc.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		_ = proc.Kill()
	}
	channel.Close()
}

// attachToRunningBridge mirrors TS's `if (coords) { try { ... } catch {} }`
// block: it attaches to a still-running bridge at coords and, on success,
// returns the resulting session. On any failure it returns ok=false so the
// caller falls through to a fresh spawn.
func attachToRunningBridge(ctx context.Context, sandboxSession providerutils.SandboxSession, settings Settings, coords *bridgeCoords, isContinue bool, onBridgeErr func(*harness.ErrorPart), p sessionParams) (*session, bool) {
	endpoint, err := resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, coords.Port)
	if err != nil {
		return nil, false
	}
	endpoint, err = bridge.WithBridgeToken(endpoint, coords.Token)
	if err != nil {
		return nil, false
	}
	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect:                bridge.NewConnectFunc(endpoint, bridge.DialOptions{Name: "deepagents bridge"}),
		InitialLastSeenEventID: coords.LastSeenEventID,
		OnBridgeError:          onBridgeErr,
		Reconnect:              settings.Reconnect,
	})
	if err := channel.Open(ctx, isContinue); err != nil {
		return nil, false
	}
	p.channel = channel
	p.proc = nil
	p.bridgePort = coords.Port
	p.bridgeToken = coords.Token
	p.isResume = true
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
				fmt.Sprintf("The deepagents harness does not yet support user message parts of type '%s'. Pass a string or a user message whose content contains only text parts.", part.ContentType()),
				HarnessID, nil)
		}
		parts = append(parts, text.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

func invalidSkillNameMessage(name string) string {
	return fmt.Sprintf("Invalid deepagents skill name '%s': must be lowercase alphanumeric with hyphens, 1-64 chars.", name)
}

func invalidSkillFilePathMessage(skillName, filePath string) string {
	return fmt.Sprintf("Invalid skill file path for '%s': %s", skillName, filePath)
}
