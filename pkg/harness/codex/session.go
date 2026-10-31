package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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
	debug                        *harness.DebugConfig
}

type session struct {
	opts sessionOptions

	mu                                 sync.Mutex
	stopped                            bool
	pendingResumeThreadID              string
	latestThreadID                     string
	latestTurnConfigurationFingerprint string
	// destroyed is closed when the first DoDestroy finishes its teardown.
	destroyed chan struct{}
	// rerunContinue is the live copy of opts.rerunContinue: true until the
	// single recovery start has been sent. Mirrors TS `rerunContinue`.
	rerunContinue bool
}

func newSession(opts sessionOptions) *session {
	s := &session{
		opts:                               opts,
		latestThreadID:                     opts.resumeThreadID,
		latestTurnConfigurationFingerprint: opts.turnConfigurationFingerprint,
		rerunContinue:                      opts.rerunContinue,
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
		(result.Changed || (strings.HasPrefix(s.latestTurnConfigurationFingerprint, fingerprintVersionPrefix) && s.latestTurnConfigurationFingerprint != fingerprint))
	s.latestTurnConfigurationFingerprint = fingerprint
	if restart {
		s.pendingResumeThreadID = ""
	}
	return restart, nil
}

// fingerprintVersionPrefix marks fingerprints computed over canonical
// (key-sorted) JSON. Legacy fingerprints (bare hex) are replaced once rather
// than treated as a configuration change. Mirrors TS `v2:`.
const fingerprintVersionPrefix = "v2:"

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
	// Round-trip through a generic value so every object (including raw JSON
	// and struct-typed schemas) is re-encoded with sorted keys: value-equivalent
	// tool definitions with reordered keys must hash identically.
	raw, _ := json.Marshal(struct {
		Instructions any        `json:"instructions"`
		Tools        []wireTool `json:"tools"`
	}{instructionsValue, wireTools})
	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&generic)
	var buf bytes.Buffer
	writeJSCanonicalJSON(&buf, generic)
	canonical := buf.Bytes()
	sum := sha256.Sum256(canonical)
	return fingerprintVersionPrefix + hex.EncodeToString(sum[:])
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON && opts.ResponseFormat.Schema == nil {
		return nil, harness.NewCapabilityUnsupportedError("Harness 'codex' requires a JSON schema for structured output.", HarnessID, nil)
	}
	restartThread, err := s.synchronizeTurnConfiguration(ctx, opts.Skills, opts.Instructions, opts.Tools)
	if err != nil {
		return nil, err
	}
	content, err := harnessutil.ExtractPromptContent(opts.Prompt)
	if err != nil {
		return nil, err
	}

	control := s.wireTurn(ctx, opts.Emit)
	// TS builds the start message (consuming the pending resume thread id) and
	// clears the recovery flag whether or not the start is still sent: an
	// already-aborted turn settled in wireTurn and sendStart skips the frame.
	frame := s.buildStartFrame(content, opts.TurnSettings, opts.ResponseFormat, restartThread, true)
	// Recovery starts a native turn once. Later continuations attach to it.
	s.mu.Lock()
	s.rerunContinue = false
	s.mu.Unlock()
	if ctx.Err() != nil {
		return control, nil
	}
	if err := s.opts.channel.Send(frame); err != nil {
		control.settleError(err)
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
	// TS consumes the recovery flag (and the pending resume thread id) whether
	// or not the start is still sent: an already-aborted turn settled in
	// wireTurn and sendStart skips the frame.
	s.mu.Lock()
	rerun := s.rerunContinue
	if rerun {
		s.rerunContinue = false
	}
	s.mu.Unlock()
	if rerun {
		frame := s.buildStartFrame(harnessutil.PromptContent{Text: "Continue."}, opts.TurnSettings, opts.ResponseFormat, restartThread, false)
		if ctx.Err() == nil {
			if err := s.opts.channel.Send(frame); err != nil {
				control.settleError(err)
				return nil, err
			}
		}
	}
	return control, nil
}

func (s *session) buildStartFrame(prompt harnessutil.PromptContent, ts harness.TurnSettings, rf *harness.ResponseFormat, restartThread, useFirstPromptResume bool) *StartFrame {
	frame := &StartFrame{
		StartBase: bridge.StartBase{
			Prompt: prompt.Text, Tools: ts.Tools, Model: firstNonEmpty(ts.Model, s.opts.model), ResponseFormat: rf,
			PermissionMode: s.opts.permissionMode, BuiltinToolFiltering: s.opts.builtinToolFiltering, Debug: s.opts.debug,
		},
		Instructions:    ts.Instructions,
		ReasoningEffort: s.opts.reasoningEffort,
		WebSearch:       s.opts.webSearch,
		CodexConfig:     s.opts.codexConfig,
		MCPServers:      s.opts.mcpServers,
		Headers:         s.opts.headers,
		RestartThread:   restartThread,
	}
	if !prompt.IsText() {
		frame.PromptContent = &prompt
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
		return nil, fmt.Errorf("codex session %s is already stopped; cannot detach.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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
		return nil, fmt.Errorf("codex session %s is stopped; cannot suspend.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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
		return nil, fmt.Errorf("codex session %s is already stopped; cannot stop.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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
			return nil, fmt.Errorf("codex session %s did not reply to stop within 5s.", s.opts.sessionID) //nolint:staticcheck // matches TS SDK's exact error text
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
		var cause = errors.New("codex bridge reported an error")
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			pc.forward(f.Part)
			if ep, ok := f.Part.(*harness.ErrorPart); ok && ep.Error != nil {
				cause = ep.AsError()
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
		pc.settleError(errors.New("codex bridge closed before the turn finished.")) //nolint:staticcheck // matches TS SDK's exact error text
	})

	go bridge.WatchAbort(ctx, pc.done, func() {
		_ = s.opts.channel.Send(bridge.AbortCommand{})
		pc.settleError(context.Cause(ctx))
	})

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
	return c.channel.Send(bridge.ToolResultCommand{ToolCallID: result.ToolCallID, Output: result.Output, IsError: result.IsError, ToolResult: result.ToolResult})
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

// writeJSCanonicalJSON serializes v byte-for-byte like the TS fingerprint's
// JSON.stringify with a key-sorting replacer: keys sorted by UTF-16 code unit,
// no HTML escaping, U+2028/U+2029 and non-ASCII emitted raw, JS number
// formatting, and JS short escapes (\b \f \n \r \t, lowercase \u00xx).
func writeJSCanonicalJSON(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			buf.WriteString("null")
			return
		}
		buf.WriteString(jsNumber(f))
	case string:
		writeJSString(buf, x)
	case []any:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSCanonicalJSON(buf, e)
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return slices.Compare(utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))) < 0
		})
		// Object.fromEntries builds a JS object, which enumerates integer-like
		// keys first in ascending numeric order regardless of insertion order.
		keys = jsonorder.JSKeyOrder(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSString(buf, k)
			buf.WriteByte(':')
			writeJSCanonicalJSON(buf, x[k])
		}
		buf.WriteByte('}')
	default:
		buf.WriteString("null")
	}
}

// jsNumber formats f like ECMAScript Number::toString.
func jsNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde+XX
	mant, exp, _ := strings.Cut(s, "e")
	sign := exp[0]
	exp = strings.TrimLeft(exp[1:], "0")
	return mant + "e" + string(sign) + exp
}

func writeJSString(buf *bytes.Buffer, s string) {
	const hexDigits = "0123456789abcdef"
	buf.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r == '\b':
			buf.WriteString(`\b`)
		case r == '\f':
			buf.WriteString(`\f`)
		case r == '\n':
			buf.WriteString(`\n`)
		case r == '\r':
			buf.WriteString(`\r`)
		case r == '\t':
			buf.WriteString(`\t`)
		case r < 0x20:
			buf.WriteString(`\u00`)
			buf.WriteByte(hexDigits[r>>4])
			buf.WriteByte(hexDigits[r&0xf])
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}
