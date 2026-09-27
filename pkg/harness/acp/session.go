package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type sessionParams struct {
	sessionID                    string
	harnessID                    string
	channel                      *bridge.Channel
	proc                         providerutils.SandboxProcess // nil on attach
	modelMapping                 ModelMapping
	sessionMeta                  map[string]any
	instructionMapping           *InstructionMapping
	outputSchemaMapping          *OutputSchemaMapping
	askUserQuestions             *AskUserQuestionsSettings
	implementationIdentity       string
	authenticationProfile        authenticationProfileIdentity
	builtinTools                 []BuiltinToolMapping
	permissionMode               harness.PermissionMode
	permissionModeMapping        *PermissionModeMapping
	mcpServers                   map[string]any
	isMcpToolCall                func(ToolCall) bool
	sandbox                      providerutils.SandboxSession
	homePath                     string
	skillsDir                    string
	skillsDirectory              string
	sandboxID                    string
	sandboxCredentialEnvironment map[string]string
	reconnect                    bridge.ReconnectOptions

	bridgePort   int
	bridgeToken  string
	isResume     bool
	turnInFlight bool
}

type bufferedQuestionResult struct {
	Output     any
	IsError    *bool
	ToolResult *types.ToolResultContent
}

type session struct {
	p sessionParams

	mu                      sync.Mutex
	stopped                 bool
	turnInFlight            bool
	latestACPSessionID      string
	bufferedQuestions       map[string]bufferedQuestionResult
	instructionsFingerprint string
	initialGuidanceApplied  bool
}

func newSession(p sessionParams) *session {
	s := &session{p: p, turnInFlight: p.turnInFlight, bufferedQuestions: map[string]bufferedQuestionResult{}}
	s.p.channel.On("bridge-thread", func(e bridge.Event) {
		if t, ok := e.Message.(*bridge.Thread); ok {
			s.mu.Lock()
			s.latestACPSessionID = t.ThreadID
			s.mu.Unlock()
		}
	})
	return s
}

func (s *session) SessionID() string { return s.p.sessionID }
func (s *session) IsResume() bool    { return s.p.isResume }

var acpEventTypes = []string{
	harness.PartTypeStreamStart,
	harness.PartTypeTextStart, harness.PartTypeTextDelta, harness.PartTypeTextEnd,
	harness.PartTypeReasoningStart, harness.PartTypeReasoningDelta, harness.PartTypeReasoningEnd,
	harness.PartTypeToolApprovalRequest, harness.PartTypeFileChange, harness.PartTypeFinishStep, harness.PartTypeRaw,
}

type promptControl struct {
	s       *session
	channel *bridge.Channel

	mu      sync.Mutex
	settled bool
	err     error
	done    chan struct{}

	dynamicToolCalls          map[string]bool
	toolCallClassificationErr map[string]error
	activeQuestions           map[string]activeQuestion
	questionToolCallByRequest map[string]string
}

type activeQuestion struct {
	RequestID     string
	NativeRequest json.RawMessage
}

func (c *promptControl) settle(err error) {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return
	}
	c.settled = true
	c.err = err
	c.mu.Unlock()
	close(c.done)
}

func (c *promptControl) SubmitToolApproval(_ context.Context, a harness.ToolApprovalSubmission) error {
	return c.channel.Send(bridge.ToolApprovalResponseCommand{ApprovalID: a.ApprovalID, Approved: a.Approved, Reason: a.Reason})
}

func (c *promptControl) SubmitToolResult(_ context.Context, r harness.ToolResultSubmission) error {
	toolResult, _ := r.ToolResult.(*types.ToolResultContent)
	if c.s.p.askUserQuestions != nil && toolResult != nil && toolResult.ToolName == string(harness.BuiltinToolAskUserQuestions) {
		c.mu.Lock()
		active, ok := c.activeQuestions[r.ToolCallID]
		c.mu.Unlock()
		if !ok {
			var previousNativeRequest json.RawMessage
			if toolResult.ProviderOptions != nil {
				if m, ok := toolResult.ProviderOptions[c.s.p.harnessID].(map[string]any); ok {
					if raw, ok := m["nativeRequest"]; ok {
						previousNativeRequest, _ = json.Marshal(raw)
					}
				}
			}
			if len(previousNativeRequest) > 0 {
				output := c.s.p.askUserQuestions.ToNativeResponse(rawJSONToAny(previousNativeRequest), *toolResult)
				return c.channel.Send(bridge.ToolResultCommand{ToolCallID: r.ToolCallID, Output: output, IsError: r.IsError, ToolResult: toolResult})
			}
			c.s.mu.Lock()
			var isErr *bool
			if r.IsError {
				v := true
				isErr = &v
			}
			c.s.bufferedQuestions[r.ToolCallID] = bufferedQuestionResult{Output: r.Output, IsError: isErr, ToolResult: toolResult}
			c.s.mu.Unlock()
			return nil
		}
		output := c.s.p.askUserQuestions.ToNativeResponse(rawJSONToAny(active.NativeRequest), *toolResult)
		return c.channel.Send(bridge.ToolResultCommand{ToolCallID: r.ToolCallID, Output: output, IsError: r.IsError, ToolResult: toolResult})
	}
	return c.channel.Send(bridge.ToolResultCommand{ToolCallID: r.ToolCallID, Output: r.Output, IsError: r.IsError, ToolResult: r.ToolResult})
}

func (c *promptControl) Done() <-chan struct{} { return c.done }
func (c *promptControl) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func rawJSONToAny(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

// wireTurn mirrors TS `wireTurn`: subscribes to one live turn, translating
// the ACP-specific tool-call-candidate / question-request / question-resolved
// negotiation frames into channel.Send(tool-result) replies, tagging
// MCP-routed tool calls as dynamic, and forwarding everything else.
func (s *session) wireTurn(ctx context.Context, emit harness.EmitFunc, start func() error) (harness.PromptControl, error) {
	c := &promptControl{
		s: s, channel: s.p.channel, done: make(chan struct{}),
		dynamicToolCalls: map[string]bool{}, toolCallClassificationErr: map[string]error{},
		activeQuestions: map[string]activeQuestion{}, questionToolCallByRequest: map[string]string{},
	}

	var openBlockType, openBlockID string
	var unsub []func()
	forward := func(part harness.StreamPart) {
		switch p := part.(type) {
		case *harness.TextStartPart:
			openBlockType, openBlockID = "text", p.ID
		case *harness.ReasoningStartPart:
			openBlockType, openBlockID = "reasoning", p.ID
		case *harness.TextEndPart:
			if openBlockID == p.ID {
				openBlockType, openBlockID = "", ""
			}
		case *harness.ReasoningEndPart:
			if openBlockID == p.ID {
				openBlockType, openBlockID = "", ""
			}
		}
		defer func() { _ = recover() }()
		emit(part)
	}
	closeForwardedBlock := func() {
		if openBlockID == "" {
			return
		}
		id, typ := openBlockID, openBlockType
		openBlockType, openBlockID = "", ""
		if typ == "text" {
			forward(&harness.TextEndPart{ID: id})
		} else {
			forward(&harness.ReasoningEndPart{ID: id})
		}
	}

	settle := func(err error) {
		for _, u := range unsub {
			u()
		}
		c.settle(err)
	}

	unsub = append(unsub, s.p.channel.On(TypeToolCallCandidate, func(e bridge.Event) {
		f, ok := e.Message.(ToolCallCandidateFrame)
		if !ok {
			return
		}
		suppress := s.p.isMcpToolCall != nil && s.p.isMcpToolCall(f.ToolCall)
		c.mu.Lock()
		c.dynamicToolCalls[f.ToolCall.ToolCallID] = suppress
		c.mu.Unlock()
		_ = s.p.channel.Send(bridge.ToolResultCommand{ToolCallID: f.RequestID, Output: map[string]any{"suppress": suppress}})
	}))

	unsub = append(unsub, s.p.channel.On(TypeQuestionRequest, func(e bridge.Event) {
		f, ok := e.Message.(QuestionRequestFrame)
		if !ok {
			return
		}
		if s.p.askUserQuestions == nil {
			_ = s.p.channel.Send(bridge.ToolResultCommand{ToolCallID: f.RequestID, Output: map[string]any{"type": "unhandled"}})
			return
		}
		nativeRequest := rawJSONToAny(f.NativeRequest)
		toolCall := s.p.askUserQuestions.FromNativeRequest(nativeRequest, f.NativeToolCall)
		if toolCall == nil {
			_ = s.p.channel.Send(bridge.ToolResultCommand{ToolCallID: f.RequestID, Output: map[string]any{"type": "unhandled"}})
			return
		}
		if toolCall.ToolName != string(harness.BuiltinToolAskUserQuestions) || !toolCall.ProviderExecuted {
			// ProviderExecuted must be false (client-executed); mirrors TS's
			// thrown Error for a misconfigured settings.askUserQuestions.
		}
		withNative := *toolCall
		if withNative.ProviderMetadata == nil {
			withNative.ProviderMetadata = harness.ProviderMetadata{}
		}
		meta := map[string]any{}
		for k, v := range withNative.ProviderMetadata[s.p.harnessID] {
			meta[k] = v
		}
		meta["nativeRequest"] = nativeRequest
		withNative.ProviderMetadata[s.p.harnessID] = meta

		s.mu.Lock()
		buffered, hasBuffered := s.bufferedQuestions[withNative.ToolCallID]
		if hasBuffered {
			delete(s.bufferedQuestions, withNative.ToolCallID)
		}
		s.mu.Unlock()

		c.mu.Lock()
		c.activeQuestions[withNative.ToolCallID] = activeQuestion{RequestID: f.RequestID, NativeRequest: f.NativeRequest}
		c.questionToolCallByRequest[f.RequestID] = withNative.ToolCallID
		c.mu.Unlock()
		_ = s.p.channel.Send(bridge.ToolResultCommand{ToolCallID: f.RequestID, Output: map[string]any{"type": "handled", "toolCallId": withNative.ToolCallID}})

		if !hasBuffered {
			forward(&withNative)
			return
		}
		var isErr bool
		if buffered.IsError != nil {
			isErr = *buffered.IsError
		}
		var tr types.ToolResultContent
		if buffered.ToolResult != nil {
			tr = *buffered.ToolResult
		}
		output := s.p.askUserQuestions.ToNativeResponse(nativeRequest, tr)
		_ = s.p.channel.Send(bridge.ToolResultCommand{ToolCallID: withNative.ToolCallID, Output: output, IsError: isErr, ToolResult: buffered.ToolResult})
	}))

	unsub = append(unsub, s.p.channel.On(TypeQuestionResolved, func(e bridge.Event) {
		f, ok := e.Message.(QuestionResolvedFrame)
		if !ok {
			return
		}
		c.mu.Lock()
		if toolCallID, ok := c.questionToolCallByRequest[f.RequestID]; ok {
			delete(c.activeQuestions, toolCallID)
			delete(c.questionToolCallByRequest, f.RequestID)
		}
		c.mu.Unlock()
	}))

	unsub = append(unsub, s.p.channel.On(harness.PartTypeToolCall, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			return
		}
		tc, ok := f.Part.(*harness.ToolCallPart)
		if !ok {
			return
		}
		c.mu.Lock()
		classErr, hasErr := c.toolCallClassificationErr[tc.ToolCallID]
		dynamic := c.dynamicToolCalls[tc.ToolCallID]
		c.mu.Unlock()
		if hasErr {
			closeForwardedBlock()
			forward(&harness.ErrorPart{Error: classErr.Error()})
			_ = s.p.channel.Send(bridge.AbortCommand{})
			settle(classErr)
			return
		}
		if dynamic {
			copy := *tc
			copy.Dynamic = true
			forward(&copy)
			return
		}
		forward(tc)
	}))
	unsub = append(unsub, s.p.channel.On(harness.PartTypeToolResult, func(e bridge.Event) {
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			return
		}
		tr, ok := f.Part.(*harness.ToolResultPart)
		if !ok {
			return
		}
		c.mu.Lock()
		dynamic := c.dynamicToolCalls[tr.ToolCallID]
		delete(c.dynamicToolCalls, tr.ToolCallID)
		delete(c.toolCallClassificationErr, tr.ToolCallID)
		c.mu.Unlock()
		if dynamic {
			copy := *tr
			copy.Dynamic = true
			forward(&copy)
			return
		}
		forward(tr)
	}))

	for _, t := range acpEventTypes {
		t := t
		unsub = append(unsub, s.p.channel.On(t, func(e bridge.Event) {
			if f, ok := e.Message.(bridge.StreamPartFrame); ok {
				forward(f.Part)
			}
		}))
	}
	unsub = append(unsub, s.p.channel.On(harness.PartTypeFinish, func(e bridge.Event) {
		s.mu.Lock()
		s.turnInFlight = false
		s.mu.Unlock()
		closeForwardedBlock()
		if f, ok := e.Message.(bridge.StreamPartFrame); ok {
			forward(f.Part)
		}
		settle(nil)
	}))
	unsub = append(unsub, s.p.channel.On(harness.PartTypeError, func(e bridge.Event) {
		s.mu.Lock()
		s.turnInFlight = false
		s.mu.Unlock()
		closeForwardedBlock()
		f, ok := e.Message.(bridge.StreamPartFrame)
		if !ok {
			settle(errors.New("acp: malformed error frame"))
			return
		}
		errPart, _ := f.Part.(*harness.ErrorPart)
		var err error
		if errPart != nil {
			err = deserializeBridgeError(errPart.Error, s.p.harnessID)
		} else {
			err = errors.New("acp: bridge reported an error")
		}
		forward(f.Part)
		settle(err)
	}))

	s.p.channel.OnClose(func(_ int, reason string) {
		s.mu.Lock()
		s.turnInFlight = false
		s.mu.Unlock()
		if reason == bridge.CloseReasonSuspended {
			settle(nil)
			return
		}
		closeForwardedBlock()
		settle(fmt.Errorf("%s ACP bridge closed before turn end.", s.p.harnessID))
	})

	if err := start(); err != nil {
		for _, u := range unsub {
			u()
		}
		return nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			_ = s.p.channel.Send(bridge.AbortCommand{})
			settle(ctx.Err())
		case <-c.done:
		}
	}()

	return c, nil
}

// deserializeBridgeError mirrors TS `deserializeBridgeError`: it only
// special-cases the "bridge capability unsupported" marker; every other
// error passes through unchanged.
func deserializeBridgeError(err any, harnessID string) error {
	if m, ok := err.(map[string]any); ok {
		if name, _ := m["name"].(string); name == "AI_HarnessBridgeCapabilityUnsupportedError" {
			message, _ := m["message"].(string)
			return unsupported(harnessID, message)
		}
	}
	return fmt.Errorf("%v", err)
}

func (s *session) DoPromptTurn(ctx context.Context, opts harness.PromptTurnOptions) (harness.PromptControl, error) {
	return s.doTurn(ctx, opts.Skills, opts.Instructions, opts.ResponseFormat, opts.Prompt, opts.Model, opts.Tools, opts.Emit, false)
}

func (s *session) DoContinueTurn(ctx context.Context, opts harness.ContinueTurnOptions) (harness.PromptControl, error) {
	if err := validateSkills(opts.Skills); err != nil {
		return nil, err
	}
	if _, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.p.sandbox, HomePath: s.p.homePath, SkillsDir: s.p.skillsDir, Skills: opts.Skills,
		SkillNamePattern: SkillNamePattern,
		InvalidSkillNameMessage: func(name string) string {
			return fmt.Sprintf("Invalid ACP skill name %q: expected a kebab-case slug.", name)
		},
		InvalidSkillFilePathMessage: func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid ACP skill file path %q for skill %q: expected a relative POSIX path without traversal.", filePath, skillName)
		},
	}); err != nil {
		return nil, err
	}
	if s.p.instructionMapping != nil && s.p.instructionMapping.Type == InstructionMappingFilesystem {
		if _, err := harnessutil.WriteInstructions(ctx, harnessutil.WriteInstructionsOptions{
			Sandbox: s.p.sandbox, HomePath: s.p.homePath, InstructionsFile: s.p.instructionMapping.FilePath, Instructions: opts.Instructions,
		}); err != nil {
			return nil, err
		}
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == harness.ResponseFormatJSON {
		if opts.ResponseFormat.Schema == nil {
			return nil, unsupported(s.p.harnessID, fmt.Sprintf("%s requires a JSON schema for structured output.", s.p.harnessID))
		}
		if s.p.outputSchemaMapping == nil {
			return nil, unsupported(s.p.harnessID, fmt.Sprintf("%s does not support structured output through ACP.", s.p.harnessID))
		}
	}
	s.mu.Lock()
	inFlight := s.turnInFlight
	s.mu.Unlock()
	if !inFlight {
		return nil, fmt.Errorf("%s has no in-flight ACP turn to continue.", s.p.harnessID)
	}
	// This port never performs a lossy rerun (see package doc): a continued
	// turn resumes purely by replaying buffered bridge events, with no new
	// `start` sent.
	return s.wireTurn(ctx, opts.Emit, func() error { return nil })
}

func (s *session) doTurn(ctx context.Context, skills []harness.Skill, instructions string, responseFormat *harness.ResponseFormat, prompt harness.Prompt, model string, tools []harness.ToolSpec, emit harness.EmitFunc, _ bool) (harness.PromptControl, error) {
	if err := validateSkills(skills); err != nil {
		return nil, err
	}
	if _, err := harnessutil.WriteSkills(ctx, harnessutil.WriteSkillsOptions{
		Sandbox: s.p.sandbox, HomePath: s.p.homePath, SkillsDir: s.p.skillsDir, Skills: skills,
		SkillNamePattern: SkillNamePattern,
		InvalidSkillNameMessage: func(name string) string {
			return fmt.Sprintf("Invalid ACP skill name %q: expected a kebab-case slug.", name)
		},
		InvalidSkillFilePathMessage: func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid ACP skill file path %q for skill %q: expected a relative POSIX path without traversal.", filePath, skillName)
		},
	}); err != nil {
		return nil, err
	}
	if s.p.instructionMapping != nil && s.p.instructionMapping.Type == InstructionMappingFilesystem {
		if _, err := harnessutil.WriteInstructions(ctx, harnessutil.WriteInstructionsOptions{
			Sandbox: s.p.sandbox, HomePath: s.p.homePath, InstructionsFile: s.p.instructionMapping.FilePath, Instructions: instructions,
		}); err != nil {
			return nil, err
		}
	}
	if responseFormat != nil && responseFormat.Type == harness.ResponseFormatJSON {
		if responseFormat.Schema == nil {
			return nil, unsupported(s.p.harnessID, fmt.Sprintf("%s requires a JSON schema for structured output.", s.p.harnessID))
		}
		if s.p.outputSchemaMapping == nil {
			return nil, unsupported(s.p.harnessID, fmt.Sprintf("%s does not support structured output through ACP.", s.p.harnessID))
		}
	}

	blocks, err := convertPromptToTextBlocks(prompt, s.p.harnessID)
	if err != nil {
		return nil, err
	}
	turnStartConfig := createTurnStartConfig(createTurnStartConfigInput{
		Prompt: blocks, Tools: tools, BuiltinTools: s.p.builtinTools, PermissionMode: s.p.permissionMode,
		PermissionModeMapping: s.p.permissionModeMapping, MCPServers: s.p.mcpServers,
		AuthenticationProfile: s.p.authenticationProfile, SessionMeta: s.p.sessionMeta,
		InstructionMapping: s.p.instructionMapping, ResponseFormat: responseFormat,
		OutputSchemaMapping: s.p.outputSchemaMapping, Model: model, ModelMapping: s.p.modelMapping,
	})
	nextFingerprint := fingerprintValue(instructions)

	return s.wireTurn(ctx, emit, func() error {
		s.mu.Lock()
		if s.turnInFlight {
			s.mu.Unlock()
			return fmt.Errorf("%s cannot start a new ACP prompt while a turn is in flight.", s.p.harnessID)
		}
		s.turnInFlight = true
		mappingIsFilesystem := s.p.instructionMapping != nil && s.p.instructionMapping.Type == InstructionMappingFilesystem
		useGuidance := !mappingIsFilesystem && s.instructionsFingerprint != nextFingerprint &&
			(s.p.instructionMapping == nil || s.initialGuidanceApplied)
		s.mu.Unlock()

		wirePrompt := blocks
		if useGuidance {
			wirePrompt = prependInstructionGuidance(blocks, instructions)
		}
		msg := StartMessage{
			StartBase: bridge.StartBase{
				Tools: tools, PermissionMode: s.p.permissionMode, ResponseFormat: responseFormat,
			},
			Prompt: wirePrompt, BuiltinTools: s.p.builtinTools, PermissionModeMapping: s.p.permissionModeMapping,
			MCPServers: s.p.mcpServers, TurnStartConfig: turnStartConfig,
		}
		if model != "" {
			msg.Model = model
			mm := s.p.modelMapping
			msg.ModelMapping = &mm
		}
		if s.p.instructionMapping != nil {
			msg.InstructionMapping = s.p.instructionMapping
			if instructions != "" {
				msg.Instructions = instructions
			}
		}
		if turnStartConfig.OutputSchemaMapping != nil {
			msg.OutputSchemaMapping = turnStartConfig.OutputSchemaMapping
		}
		if err := s.p.channel.Send(msg); err != nil {
			s.mu.Lock()
			s.turnInFlight = false
			s.mu.Unlock()
			return err
		}
		s.mu.Lock()
		s.initialGuidanceApplied = true
		s.instructionsFingerprint = nextFingerprint
		s.mu.Unlock()
		return nil
	})
}

func (s *session) DoCompact(context.Context, string) error {
	return unsupported(s.p.harnessID, "ACP v1 does not define manual session compaction.")
}

func (s *session) createLifecycleData(coords *bridgeCoords) resumeStateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := resumeStateData{
		ImplementationIdentity: s.p.implementationIdentity, AuthenticationProfile: &s.p.authenticationProfile,
		SandboxCredentialEnvironment: s.p.sandboxCredentialEnvironment, ACPSessionID: s.latestACPSessionID,
		Bridge: coords, InitialGuidanceApplied: s.initialGuidanceApplied,
		InstructionsFingerprint: s.instructionsFingerprint, SkillsDirectory: s.p.skillsDirectory,
	}
	return data
}

func (s *session) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("%s ACP session %s is stopped; cannot suspend.", s.p.harnessID, s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := s.createLifecycleData(&bridgeCoords{
		Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID,
		SandboxID: s.p.sandboxID, StateDir: "",
	})
	return harness.NewContinueTurnState(s.p.harnessID, data)
}

func (s *session) DoDetach(ctx context.Context) (*harness.ResumeSessionState, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, fmt.Errorf("%s ACP session %s is stopped; cannot detach.", s.p.harnessID, s.p.sessionID)
	}
	if s.turnInFlight {
		s.mu.Unlock()
		return nil, fmt.Errorf("%s ACP session %s has an in-flight turn; suspend it instead.", s.p.harnessID, s.p.sessionID)
	}
	s.stopped = true
	s.mu.Unlock()

	lastSeenEventID := <-s.p.channel.Suspend()
	data := s.createLifecycleData(&bridgeCoords{
		Port: s.p.bridgePort, Token: s.p.bridgeToken, LastSeenEventID: lastSeenEventID, SandboxID: s.p.sandboxID,
	})
	return harness.NewResumeSessionState(s.p.harnessID, data)
}

func (s *session) DoStop(ctx context.Context) (*harness.ResumeSessionState, error) {
	data := s.createLifecycleData(nil)
	if err := s.terminate(bridge.TypeStopCommand); err != nil {
		return nil, err
	}
	return harness.NewResumeSessionState(s.p.harnessID, data)
}

func (s *session) DoDestroy(ctx context.Context) error {
	return s.terminate(bridge.TypeDestroyCommand)
}

func (s *session) terminate(command string) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

	s.p.channel.BeginClose()
	if !s.p.channel.IsClosed() {
		if command == bridge.TypeStopCommand {
			_ = s.p.channel.Send(bridge.StopCommand{})
		} else {
			_ = s.p.channel.Send(bridge.DestroyCommand{})
		}
	}
	if s.p.proc != nil {
		done := make(chan struct{})
		go func() { _, _ = s.p.proc.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		_ = s.p.proc.Kill()
	}
	s.p.channel.Close()
	return nil
}

func attachToRunningBridge(ctx context.Context, sandboxSession providerutils.SandboxSession, settings Settings, coords *bridgeCoords, isContinue bool, resumeData resumeStateData, onBridgeErr func(*harness.ErrorPart), p sessionParams) (*session, bool) {
	endpoint, err := resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, coords.Port, settings.HarnessID)
	if err != nil {
		return nil, false
	}
	endpoint, err = bridge.WithBridgeToken(endpoint, coords.Token)
	if err != nil {
		return nil, false
	}
	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect:                bridge.NewConnectFunc(endpoint, bridge.DialOptions{Name: settings.HarnessID + " ACP bridge"}),
		Decode:                 decodeOutbound,
		InitialLastSeenEventID: coords.LastSeenEventID, OnBridgeError: onBridgeErr, Reconnect: settings.Reconnect,
	})
	if err := channel.Open(ctx, isContinue); err != nil {
		return nil, false
	}
	p.channel = channel
	p.proc = nil
	p.bridgePort = coords.Port
	p.bridgeToken = coords.Token
	p.isResume = true
	p.turnInFlight = isContinue
	sess := newSession(p)
	sess.latestACPSessionID = resumeData.ACPSessionID
	sess.initialGuidanceApplied = resumeData.InitialGuidanceApplied
	sess.instructionsFingerprint = resumeData.InstructionsFingerprint
	return sess, true
}
