package harness

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// AgentVersion is the agent-interface specification version HarnessAgent
// implements (TS `HarnessAgent.version`).
const AgentVersion = "agent-v1"

// Agent satisfies agent.Agent, so a harness.Agent can be used anywhere an
// agent.Agent is expected (e.g. pkg/tui's AgentTUIRunner), once a session has
// been threaded through AgentGenerateOptions.HarnessSession.
var _ agent.Agent = (*Agent)(nil)

// Agent is the consumer-facing runtime that drives a Harness adapter as an
// agent.Agent: it merges the harness's builtin tools with user tools,
// resolves a per-session sandbox, and runs prompt turns via runPrompt built
// on ai.NewStreamTextResultFromParts. Mirrors TS `HarnessAgent`
// (@ai-sdk/harness/agent).
//
// Adapter interface (WG7-WG12 plug in here): an adapter package (e.g.
// pkg/harness/claudecode) exposes a constructor returning a Harness — the
// only interface Agent depends on. A Harness implementation typically:
//  1. Declares BuiltinTools() and optionally GetBootstrap() (the recipe Agent
//     applies via WG2's ApplyBootstrapRecipe/PrepareSandboxForHarness before
//     DoStart).
//  2. Implements DoStart(ctx, StartOptions) (Session, error): for a
//     bridge-backed adapter, this spawns `node bridge.mjs` in the sandbox
//     (WG6's embedded assets), waits for readiness (pkg/harness/bridge:
//     WaitForBridgeReady), opens a SandboxChannel, and wraps it in a Session
//     whose DoPromptTurn/DoContinueTurn translate the harness-v1 bridge wire
//     protocol (pkg/harness/bridge: protocol.go) into StreamPart Emit calls
//     and return a PromptControl that forwards SubmitToolResult/
//     SubmitToolApproval as bridge frames.
//  3. Session/PromptControl never touch Agent, run_prompt.go or translate.go
//     directly — those only see the harness.Harness/Session/PromptControl
//     interfaces in spec.go, so a fake/test harness (see run_prompt_test.go)
//     is a first-class substitute for a real bridge adapter.
//
// Out of scope for WG4 (left for WG7-WG12/WG13): the bridge-backed Harness
// implementations themselves, the ACP meta-adapter, and
// pkg/workflow/harness.go's suspend/continue slicing helpers.
type Agent struct {
	settings AgentSettings

	tools                map[string]types.Tool
	stopConditions       []ai.StopCondition
	sandboxConfig        AgentSandboxConfig
	builtinToolFiltering *BuiltinToolFiltering
	permissionMode       PermissionMode
	headers              map[string]string
}

// NewAgent validates settings and constructs a HarnessAgent. Mirrors the TS
// `HarnessAgent` constructor.
func NewAgent(settings AgentSettings) (*Agent, error) {
	if settings.Harness == nil {
		return nil, errors.New("HarnessAgent: `harness` is required.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if err := ValidateSandboxBootstrapSettings(settings.SandboxConfig); err != nil {
		return nil, err
	}
	headers, err := normalizeAgentHeaders(settings.Headers)
	if err != nil {
		return nil, err
	}
	userTools := settings.UserTools
	if userTools == nil {
		userTools = map[string]types.Tool{}
	}
	if err := assertNoReservedQuestionTool(settings.Harness, userTools); err != nil {
		return nil, err
	}
	permissionMode := ResolvePermissionMode(settings.PermissionMode)

	tools := make(map[string]types.Tool, len(settings.Harness.BuiltinTools())+len(userTools))
	for name, bt := range settings.Harness.BuiltinTools() {
		tools[name] = bt.Tool
	}
	for name, t := range userTools {
		tools[name] = t
	}

	filtering, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: settings.Harness, UserTools: userTools, AllTools: tools,
		ActiveTools: settings.ActiveTools, InactiveTools: settings.InactiveTools,
	})
	if err != nil {
		return nil, err
	}

	if len(settings.Harness.BuiltinTools()) > 0 &&
		PermissionModeNeedsBuiltinSupport(permissionMode) &&
		!SupportsBuiltinToolApprovals(settings.Harness) {
		return nil, NewCapabilityUnsupportedError(
			fmt.Sprintf("Harness '%s' does not support built-in tool approval requests; use permissionMode: 'allow-all'.", settings.Harness.HarnessID()),
			settings.Harness.HarnessID(), nil,
		)
	}

	var stopConditions []ai.StopCondition
	stopConditions = append(stopConditions, settings.StopWhen...)

	return &Agent{
		settings:             settings,
		tools:                tools,
		stopConditions:       stopConditions,
		sandboxConfig:        settings.SandboxConfig,
		builtinToolFiltering: filtering.BuiltinToolFiltering,
		permissionMode:       permissionMode,
		headers:              headers,
	}, nil
}

// Version returns "agent-v1".
func (a *Agent) Version() string { return AgentVersion }

// ID returns the configured agent id, or "".
func (a *Agent) ID() string { return a.settings.ID }

// HarnessID returns the identifier of the harness backing this agent.
func (a *Agent) HarnessID() string { return a.settings.Harness.HarnessID() }

// HasOutput reports whether this agent parses completed turns with a
// configured output specification. Mirrors TS `HarnessAgent.hasOutput`.
func (a *Agent) HasOutput() bool { return hasOutputSpec(a.settings.Output) }

// Tools returns the merged harness-builtin + user tool set.
func (a *Agent) Tools() []types.Tool {
	out := make([]types.Tool, 0, len(a.tools))
	for _, t := range a.tools {
		out = append(out, t)
	}
	return out
}

// CreateSessionOptions is the input of Agent.CreateSession. Mirrors TS
// `HarnessAgent.createSession`'s options.
type CreateSessionOptions struct {
	// SessionID is the stable identifier for the underlying sandbox/session.
	// Generated when empty.
	SessionID string
	// ResumeFrom is the payload from a prior Detach/Stop. Mutually exclusive
	// with ContinueFrom.
	ResumeFrom *ResumeSessionState
	// ContinueFrom is the payload from a prior SuspendTurn. Mutually
	// exclusive with ResumeFrom.
	ContinueFrom *ContinueTurnState
	// ToolsContext rebinds host-only tool context for an unfinished turn
	// resumed with ContinueFrom (directly or nested in ResumeFrom). Only
	// valid together with one of those.
	ToolsContext map[string]interface{}
	// RuntimeContext rebinds host-only runtime context for an unfinished
	// turn resumed with ContinueFrom (directly or nested in ResumeFrom).
	// Runtime context is not serialized into lifecycle state. Mirrors TS
	// `HarnessAgent.createSession`'s `options.runtimeContext`.
	RuntimeContext interface{}
	// SandboxSession is a caller-owned sandbox session. When set, the caller
	// retains ownership of its lifecycle; Agent.Sandbox is not consulted.
	SandboxSession providerutils.SandboxSession
}

// CreateSession starts a fresh session, or resumes one from state previously
// returned by AgentSession.Detach/.Stop or .SuspendTurn. The returned
// AgentSession must be passed as AgentGenerateOptions.HarnessSession to
// subsequent Generate/Stream/ContinueGenerate/ContinueStream calls, and ended
// with Detach, Stop, or Destroy. Mirrors TS `HarnessAgent.createSession`.
func (a *Agent) CreateSession(ctx context.Context, opts CreateSessionOptions) (*AgentSession, error) {
	if opts.ResumeFrom != nil && opts.ContinueFrom != nil {
		return nil, errors.New("HarnessAgent.CreateSession: pass either ResumeFrom or ContinueFrom, not both.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = newID()
	}

	var validatedResumeFrom *ResumeSessionState
	if opts.ResumeFrom != nil {
		if err := validateLifecycleStateData(a.settings.Harness, opts.ResumeFrom); err != nil {
			return nil, err
		}
		validatedResumeFrom = opts.ResumeFrom
	}
	var validatedContinueFrom *ContinueTurnState
	if opts.ContinueFrom != nil {
		if err := validateLifecycleStateData(a.settings.Harness, opts.ContinueFrom); err != nil {
			return nil, err
		}
		validatedContinueFrom = opts.ContinueFrom
	}
	effectiveContinueFrom := validatedContinueFrom
	if effectiveContinueFrom == nil && validatedResumeFrom != nil {
		effectiveContinueFrom = validatedResumeFrom.ContinueFrom
	}
	if opts.ToolsContext != nil && effectiveContinueFrom == nil {
		return nil, errors.New("HarnessAgent.CreateSession: `ToolsContext` can only rebind an unfinished turn from ContinueFrom or ResumeFrom.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	isResumedSession := validatedResumeFrom != nil || effectiveContinueFrom != nil

	ownsSandboxLifecycle := opts.SandboxSession == nil
	sandboxSession, sessionWorkDir, err := a.acquireSandbox(ctx, opts, isResumedSession)
	if err != nil {
		return nil, err
	}

	if err := EnsureSandboxDirectory(ctx, sandboxSession, sessionWorkDir); err != nil {
		a.cleanupAfterStartFailure(ctx, sandboxSession, ownsSandboxLifecycle)
		return nil, err
	}
	if a.sandboxConfig.OnSession != nil {
		if err := a.sandboxConfig.OnSession(ctx, SandboxSessionContext{Session: GetRestrictedSandboxSession(sandboxSession), SessionWorkDir: sessionWorkDir}); err != nil {
			a.cleanupAfterStartFailure(ctx, sandboxSession, ownsSandboxLifecycle)
			return nil, err
		}
	}

	startOpts := StartOptions{
		Headers: a.headers, SessionID: sessionID, ResumeFrom: validatedResumeFrom, ContinueFrom: effectiveContinueFrom,
		PermissionMode: a.permissionMode, BuiltinToolFiltering: a.builtinToolFiltering,
		SandboxSession: sandboxSession, SessionWorkDir: sessionWorkDir,
	}
	underlying, err := a.settings.Harness.DoStart(ctx, startOpts)
	if err != nil {
		a.cleanupAfterStartFailure(ctx, sandboxSession, ownsSandboxLifecycle)
		return nil, err
	}

	var pendingApprovals []PendingToolApproval
	var pendingResults []PendingToolResult
	var turnSettings *TurnSettings
	turnState := TurnStateIdle
	if effectiveContinueFrom != nil {
		pendingApprovals = effectiveContinueFrom.PendingToolApprovals
		pendingResults = effectiveContinueFrom.PendingToolResults
		turnSettings = effectiveContinueFrom.TurnSettings
		switch {
		case len(pendingApprovals) > 0:
			turnState = TurnStateAwaitingApproval
		case len(pendingResults) > 0:
			turnState = TurnStateAwaitingResult
		default:
			turnState = TurnStateSuspended
		}
	}

	return newAgentSession(AgentSessionOptions{
		SessionID: sessionID, Harness: a.settings.Harness, Underlying: underlying,
		SandboxSession: sandboxSession, OwnsSandboxLifecycle: ownsSandboxLifecycle,
		SessionWorkDir: sessionWorkDir, ToolApproval: a.settings.ToolApproval,
		PendingToolApprovals: pendingApprovals, PendingToolResults: pendingResults,
		TurnSettings: turnSettings, ResumedToolsContext: opts.ToolsContext, ResumedRuntimeContext: opts.RuntimeContext, TurnState: turnState,
	}), nil
}

func (a *Agent) acquireSandbox(ctx context.Context, opts CreateSessionOptions, isResumedSession bool) (providerutils.SandboxSession, string, error) {
	sandboxSession, sessionWorkDir, err := a.acquireSandboxSession(ctx, opts, isResumedSession)
	if err != nil {
		return nil, "", err
	}

	// Unconditionally ensures sandboxConfig.OnBootstrap has run against this
	// physical sandbox, regardless of which branch above produced it —
	// including a caller-supplied SandboxSession (31742b9a1b), which
	// previously never ran OnBootstrap at all, and a freshly-created
	// sandbox whose provider may not honor CreateSandboxSessionOptions.
	// OnFirstCreate. SkipOnBootstrapIfMarked makes the common case (a
	// provider that already ran it via OnFirstCreate) a cheap marker-file
	// read. Mirrors TS `HarnessAgent.createSession`'s unconditional
	// post-branch `runSandboxBootstrap` call.
	if err := RunSandboxBootstrap(ctx, RunSandboxBootstrapOptions{
		Session: GetRestrictedSandboxSession(sandboxSession), WorkDir: a.sandboxConfig.WorkDir,
		OnBootstrap: a.sandboxConfig.OnBootstrap, BootstrapHash: a.sandboxConfig.BootstrapHash,
		SkipOnBootstrapIfMarked: true,
	}); err != nil {
		return nil, "", err
	}
	return sandboxSession, sessionWorkDir, nil
}

// acquireSandboxSession resolves the concrete sandbox session (caller-owned,
// resumed, or freshly created) and applies the harness's own bootstrap
// recipe. It does not run sandboxConfig.OnBootstrap — see acquireSandbox's
// unconditional post-branch call for that.
func (a *Agent) acquireSandboxSession(ctx context.Context, opts CreateSessionOptions, isResumedSession bool) (providerutils.SandboxSession, string, error) {
	harness := a.settings.Harness

	if opts.SandboxSession != nil {
		restricted := GetRestrictedSandboxSession(opts.SandboxSession)
		defaultWD, err := ResolveSandboxDefaultWorkingDirectory(ctx, opts.SandboxSession)
		if err != nil {
			return nil, "", err
		}
		sessionWorkDir := ResolveSessionWorkDir(defaultWD, harness.HarnessID(), opts.SessionID, a.sandboxConfig.WorkDir)
		if recipe, err := GetBootstrap(ctx, harness); err != nil {
			return nil, "", err
		} else if recipe != nil {
			identity := HashHarnessBootstrap(*recipe)
			home, err := ResolveSandboxHomeDir(ctx, restricted)
			if err != nil {
				return nil, "", err
			}
			if err := ApplyBootstrapRecipe(ctx, restricted, *recipe, identity, StateDirectoryPath(home)); err != nil {
				return nil, "", err
			}
		}
		return opts.SandboxSession, sessionWorkDir, nil
	}

	if a.settings.Sandbox == nil {
		return nil, "", errors.New("HarnessAgent.CreateSession: configure `Sandbox` on the agent or pass `SandboxSession`.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	recipe, err := GetBootstrap(ctx, harness)
	if err != nil {
		return nil, "", err
	}

	if isResumedSession {
		resumer, ok := a.settings.Sandbox.(SandboxSessionResumer)
		if !ok {
			return nil, "", NewCapabilityUnsupportedError(
				fmt.Sprintf("Sandbox provider '%s' does not support resume.", a.settings.Sandbox.ProviderID()),
				harness.HarnessID(), nil)
		}
		resumed, err := resumer.ResumeSession(ctx, opts.SessionID)
		if err != nil {
			return nil, "", err
		}
		sessionWorkDir := ResolveSessionWorkDir(resumed.DefaultWorkingDirectory(), harness.HarnessID(), opts.SessionID, a.sandboxConfig.WorkDir)
		if recipe != nil {
			identity := HashHarnessBootstrap(*recipe)
			home, err := ResolveSandboxHomeDir(ctx, resumed)
			if err != nil {
				return nil, "", err
			}
			if err := ApplyBootstrapRecipe(ctx, resumed.Restricted(), *recipe, identity, StateDirectoryPath(home)); err != nil {
				return nil, "", err
			}
		}
		return resumed, sessionWorkDir, nil
	}

	plan, err := CreateSandboxBootstrapPlan(recipe, a.sandboxConfig)
	if err != nil {
		return nil, "", err
	}
	created, err := a.settings.Sandbox.CreateSession(ctx, CreateSandboxSessionOptions{
		SessionID: opts.SessionID, Identity: plan.Identity, OnFirstCreate: plan.OnFirstCreate,
	})
	if err != nil {
		return nil, "", err
	}
	sessionWorkDir := ResolveSessionWorkDir(created.DefaultWorkingDirectory(), harness.HarnessID(), opts.SessionID, plan.WorkDir)
	if plan.Recipe != nil && plan.RecipeIdentity != "" {
		home, err := ResolveSandboxHomeDir(ctx, created)
		if err != nil {
			return nil, "", err
		}
		if err := ApplyBootstrapRecipe(ctx, created.Restricted(), *plan.Recipe, plan.RecipeIdentity, StateDirectoryPath(home)); err != nil {
			return nil, "", err
		}
	}
	return created, sessionWorkDir, nil
}

func (a *Agent) cleanupAfterStartFailure(ctx context.Context, sandboxSession providerutils.SandboxSession, ownsSandboxLifecycle bool) {
	if !ownsSandboxLifecycle {
		return
	}
	stopSandbox(context.WithoutCancel(ctx), sandboxSession)
}

func validateLifecycleStateData(h Harness, state LifecycleState) error {
	switch s := state.(type) {
	case *ResumeSessionState:
		if err := s.Validate(); err != nil {
			return err
		}
		if s.HarnessID != h.HarnessID() {
			return fmt.Errorf("harness: lifecycle state was produced by harness '%s' but this agent uses '%s'", s.HarnessID, h.HarnessID())
		}
	case *ContinueTurnState:
		if err := s.Validate(); err != nil {
			return err
		}
		if s.HarnessID != h.HarnessID() {
			return fmt.Errorf("harness: lifecycle state was produced by harness '%s' but this agent uses '%s'", s.HarnessID, h.HarnessID())
		}
	}
	if v, ok := h.(LifecycleStateValidator); ok {
		var data []byte
		switch s := state.(type) {
		case *ResumeSessionState:
			data = s.Data
		case *ContinueTurnState:
			data = s.Data
		}
		if err := v.ValidateLifecycleStateData(data); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) sessionFromOptions(opts agent.AgentGenerateOptions) (*AgentSession, error) {
	session, ok := opts.HarnessSession.(*AgentSession)
	if !ok || session == nil {
		return nil, errors.New("HarnessAgent: AgentGenerateOptions.HarnessSession must be a *harness.AgentSession created via Agent.CreateSession.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return session, nil
}

// Generate runs one fresh prompt turn to completion and returns the buffered
// result. Requires opts.HarnessSession (a *AgentSession from CreateSession)
// with no unfinished turn. Mirrors TS `HarnessAgent.generate`.
func (a *Agent) Generate(ctx context.Context, opts agent.AgentGenerateOptions) (*ai.GenerateTextResult, error) {
	session, err := a.sessionFromOptions(opts)
	if err != nil {
		return nil, err
	}
	result, err := a.startTurn(ctx, session, opts, "prompt", nil, nil)
	if err != nil {
		return nil, err
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	return streamResultToGenerateResult(result), nil
}

// Stream runs one fresh prompt turn and returns the streaming result.
// Requires opts.HarnessSession with no unfinished turn. Mirrors TS
// `HarnessAgent.stream`.
func (a *Agent) Stream(ctx context.Context, opts agent.AgentStreamOptions) (*ai.StreamTextResult, error) {
	session, err := a.sessionFromOptions(opts.AgentGenerateOptions)
	if err != nil {
		return nil, err
	}
	return a.startTurn(ctx, session, opts.AgentGenerateOptions, "prompt", nil, nil)
}

// ContinueGenerate resumes a paused turn (awaiting an approval, a tool
// result, or explicitly suspended) to completion and returns the buffered
// result. toolApprovalContinuations/toolResultContinuations are typically
// produced by CollectToolApprovalContinuations/CollectToolResultContinuations
// over the caller's UI messages. Mirrors TS `HarnessAgent.continueGenerate`.
func (a *Agent) ContinueGenerate(ctx context.Context, opts agent.AgentGenerateOptions, toolApprovalContinuations []types.ToolApprovalResponseContent, toolResultContinuations []types.ToolResultContent) (*ai.GenerateTextResult, error) {
	session, err := a.sessionFromOptions(opts)
	if err != nil {
		return nil, err
	}
	result, err := a.startTurn(ctx, session, opts, "continue", toolApprovalContinuations, toolResultContinuations)
	if err != nil {
		return nil, err
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	return streamResultToGenerateResult(result), nil
}

// ContinueStream resumes a paused turn and returns the streaming result.
// Mirrors TS `HarnessAgent.continueStream`.
func (a *Agent) ContinueStream(ctx context.Context, opts agent.AgentStreamOptions, toolApprovalContinuations []types.ToolApprovalResponseContent, toolResultContinuations []types.ToolResultContent) (*ai.StreamTextResult, error) {
	session, err := a.sessionFromOptions(opts.AgentGenerateOptions)
	if err != nil {
		return nil, err
	}
	return a.startTurn(ctx, session, opts.AgentGenerateOptions, "continue", toolApprovalContinuations, toolResultContinuations)
}

// ExperimentalSteer submits an additional user message to session's active
// (running) turn, if the harness adapter supports it. Mirrors TS
// `HarnessAgent.experimental_steer`. See AgentSession.ExperimentalSteerTurn.
func (a *Agent) ExperimentalSteer(ctx context.Context, session *AgentSession, text string) error {
	return session.ExperimentalSteerTurn(ctx, text)
}

func (a *Agent) startTurn(ctx context.Context, session *AgentSession, opts agent.AgentGenerateOptions, mode string, toolApprovalContinuations []types.ToolApprovalResponseContent, toolResultContinuations []types.ToolResultContent) (*ai.StreamTextResult, error) {
	// No early idle/continuable check here: the guard against a turn already
	// in flight is performed atomically with the state transition in
	// session.startTrackedTurn below, immediately before runPrompt is
	// invoked. Checking here instead (before PrepareCall/tool-filtering,
	// which can take non-zero time and does not hold session.mu) would leave
	// a TOCTOU window in which two concurrent callers on the same session
	// could both observe an idle/continuable turnState and both proceed to
	// start a turn. See session.startTrackedTurn's doc comment and TS
	// `HarnessAgentSession.promptTurn`/`continueTurn`, which likewise call
	// requirePromptableTurn/requireContinuableTurn only immediately before
	// startTrackedTurn (after any async prepareCall work has already
	// resolved), not before it.

	model := a.settings.Model
	// instructionsRaw carries either a string or a *types.Message (system
	// message) through PrepareCall exactly as TS does, extracted to a plain
	// string only once, below, after PrepareCall has had a chance to replace
	// it (TS 4d1bf28: `HarnessAgentSettings.instructions: string |
	// SystemModelMessage`).
	var instructionsRaw = a.settings.Instructions
	toolsContext := a.settings.ToolsContext
	if opts.ToolsContext != nil {
		toolsContext = opts.ToolsContext
	}
	// RuntimeContext mirrors TS `this.settings.runtimeContext ?? {}`
	// (HarnessAgent.generate/stream/continueGenerate/continueStream): the
	// agent-level default, overridden by a per-call value when the caller
	// supplies one.
	runtimeContext := a.settings.RuntimeContext
	if opts.RuntimeContext != nil {
		runtimeContext = opts.RuntimeContext
	}
	skills := a.settings.Skills
	tools := a.tools

	var prompt Prompt
	if mode != "continue" {
		if len(opts.Messages) > 0 {
			last := opts.Messages[len(opts.Messages)-1]
			m := last
			prompt = Prompt{Message: &m}
		} else {
			prompt = TextPrompt(opts.Prompt)
		}
	}
	if opts.Instructions != nil {
		instructionsRaw = *opts.Instructions
	}

	// PrepareCall (TS 14d4fc0/8961fde/7608210) derives the turn-scoped
	// model/skills/instructions/tools/prompt from the caller's custom
	// CallOptions. Its result is authoritative over both the agent-level
	// defaults above and the per-call Instructions/ToolsContext already
	// applied, matching TS's "prepared values are frozen for the lifetime of
	// the turn" contract.
	if a.settings.PrepareCall != nil {
		prepared, err := a.settings.PrepareCall(ctx, PrepareCallOptions{
			CallOptions: opts.CallOptions, Prompt: prompt, Model: model,
			Skills: skills, Instructions: instructionsRaw, Tools: tools, ToolsContext: toolsContext,
			RuntimeContext: runtimeContext,
		})
		if err != nil {
			return nil, err
		}
		if prepared.Model != "" {
			model = prepared.Model
		}
		if prepared.Skills != nil {
			skills = prepared.Skills
		}
		if prepared.HasInstructions {
			instructionsRaw = prepared.Instructions
		}
		if prepared.Tools != nil {
			tools = prepared.Tools
		}
		if prepared.ToolsContext != nil {
			toolsContext = prepared.ToolsContext
		}
		if prepared.HasRuntimeContext {
			runtimeContext = prepared.RuntimeContext
		}
		if mode != "continue" && (prepared.Prompt.Text != "" || prepared.Prompt.Message != nil) {
			prompt = prepared.Prompt
		}
	}

	// Extracted once, after PrepareCall has had its say, exactly like TS
	// `_prepareTurnSettings` — every harness-v1 adapter only understands a
	// plain instructions string.
	instructions, err := instructionsText(instructionsRaw)
	if err != nil {
		return nil, err
	}

	// Re-resolve the active *user* tool set for this turn — mirrors TS
	// `_prepareTurnSettings`'s fresh `resolveHarnessAgentToolFiltering` call
	// on every turn (TS 7859cea), applied against whatever PrepareCall may
	// have replaced Tools with. Settings.ActiveTools/InactiveTools are fixed
	// for the agent's lifetime (unlike model/skills/instructions/tools,
	// PrepareCall cannot override them — matches TS, which always reads
	// `this.settings.activeTools`/`inactiveTools` here, never a per-call
	// value). This must NOT be skipped/cached from NewAgent time: NewAgent
	// only used ResolveToolFiltering to validate eagerly and to derive the
	// (turn-invariant) BuiltinToolFiltering — its ActiveUserTools result was
	// deliberately discarded there, exactly as TS's constructor discards
	// `toolFiltering.activeUserTools` too.
	builtinNames := a.settings.Harness.BuiltinTools()
	userTools := make(map[string]types.Tool, len(tools))
	for name, t := range tools {
		if _, isBuiltin := builtinNames[name]; !isBuiltin {
			userTools[name] = t
		}
	}
	toolFiltering, err := ResolveToolFiltering(ResolveToolFilteringOptions{
		Harness: a.settings.Harness, UserTools: userTools, AllTools: tools,
		ActiveTools: a.settings.ActiveTools, InactiveTools: a.settings.InactiveTools,
	})
	if err != nil {
		return nil, err
	}
	activeTools := toolFiltering.ActiveUserTools

	// Wire-format projection of *active user* tools only: harness builtins
	// are executed by the runtime and the adapter already knows about them
	// (never re-declared over the wire), and inactive user tools are
	// excluded too — mirrors TS `_toToolSpecs(toolFiltering.activeUserTools)`.
	toolSpecs := make([]ToolSpec, 0, len(activeTools))
	for name, t := range activeTools {
		schema, _ := t.Parameters.(map[string]any)
		toolSpecs = append(toolSpecs, ToolSpec{Name: name, Description: t.Description, InputSchema: schema})
	}

	pendingApprovals, pendingResults, turnSettings := session.snapshotPendingState()
	if turnSettings != nil {
		if turnSettings.Model != "" {
			model = turnSettings.Model
		}
		if turnSettings.Instructions != "" {
			instructions = turnSettings.Instructions
		}
	}
	// Continuing an unfinished turn from a session created via
	// CreateSession's ContinueFrom/ResumeFrom (rather than one still
	// in-process from an earlier promptTurn/continueTurn call) rebinds
	// host-only tools/runtime context from whatever CreateSessionOptions
	// passed, in preference to this call's own options. Mirrors TS
	// `HarnessAgentSession.resolveActiveTurnSettings`'s
	// `this.resumedToolsContext ?? options.toolsContext` /
	// `this.resumedRuntimeContext ?? options.runtimeContext`, which only
	// ever applies on the continueTurn path (a fresh promptTurn has nothing
	// to resume).
	if mode == "continue" {
		if session.resumedToolsContext != nil {
			toolsContext = session.resumedToolsContext
		}
		if session.resumedRuntimeContext != nil {
			runtimeContext = session.resumedRuntimeContext
		}
	}

	// Resolved fresh for every turn (TS `_resolveResponseFormat`, called from
	// both the fresh-prompt and continue paths) rather than cached at
	// NewAgent time, matching how instructions/tools/etc are re-derived per
	// turn above.
	responseFormat, err := resolveOutputResponseFormat(ctx, a.settings.Output)
	if err != nil {
		return nil, fmt.Errorf("harness: output.ResponseFormat failed: %w", err)
	}

	turnID, err := session.startTrackedTurn(mode)
	if err != nil {
		return nil, err
	}

	out := runPrompt(ctx, runPromptInput{
		Harness: a.settings.Harness, Session: session.underlying,
		Mode: mode, Prompt: prompt,
		Model: model, Skills: skills, Instructions: instructions,
		Tools: tools, ToolsContext: toolsContext, ActiveTools: activeTools, ToolSpecs: toolSpecs,
		BuiltinToolFiltering: a.builtinToolFiltering,
		SandboxSession:       session.sandboxSession, SessionWorkDir: session.sessionWorkDir,
		ResponseFormat: responseFormat, Output: a.settings.Output, Telemetry: a.settings.Telemetry,
		Callbacks: a.settings.Callbacks, StopConditions: a.stopConditions, ToolApproval: a.settings.ToolApproval,
		PendingToolApprovals: pendingApprovals, PendingToolResults: pendingResults,
		ToolApprovalContinuations: toolApprovalContinuations, ToolResultContinuations: toolResultContinuations,
		OnPendingToolApproval: session.recordPendingApproval,
		OnToolApprovalSettled: session.settleApproval,
		OnPendingToolResult:   session.recordPendingResult,
		OnToolResultSettled:   session.settleResult,
		// OnPromptControlAvailable hands the turn's PromptControl to the
		// session (turnID-scoped) as soon as DoPromptTurn/DoContinueTurn
		// returns it, so ExperimentalSteer can reach it while the turn is
		// still running. Mirrors TS AgentSession's `setPromptControl` call
		// site in its own doPromptTurn/doContinueTurn wrappers.
		OnPromptControlAvailable: func(control PromptControl) { session.setActivePromptControl(turnID, control) },
		// OnTurnFinished/OnTurnFailed are called synchronously by the turn
		// driver's own goroutine before it signals Done (see run_prompt.go),
		// so the session's turn state is always settled by the time a
		// caller observes the turn ending — via Generate/ContinueGenerate
		// returning, or via <-out.Done for a Stream/ContinueStream caller
		// that wants to know when it is safe to start another turn. A turn
		// that *pauses* (an approval or a client tool result is needed)
		// calls neither: session.recordPendingApproval/recordPendingResult
		// already transitioned the turn to awaiting-approval/
		// awaiting-tool-result at the moment the pause was discovered.
		OnTurnFinished: func() { session.finishTrackedTurn(turnID) },
		OnTurnFailed:   func() { session.finishTrackedTurn(turnID) },
		// OnStopConditionMet suspends the underlying harness session's turn
		// in place when StopWhen stops the result early, so it stays
		// resumable via ContinueGenerate/ContinueStream instead of being
		// discarded. See run_prompt.go's suspendOrFinishNow and
		// session.go's captureStopConditionBoundary.
		OnStopConditionMet: func(ctx context.Context) (*ContinueTurnState, error) {
			return session.captureStopConditionBoundary(ctx, turnID)
		},
		RuntimeContext: runtimeContext,
	})

	return out.Result, nil
}

// Execute runs a single prompt to completion against an ephemeral session
// (created via CreateSession and destroyed afterward) and returns the
// convenience agent.AgentResult shape. Requires a.settings.Sandbox to be
// configured (there is no caller-provided sandbox session to reuse in this
// call shape). Prefer CreateSession + Generate/Stream (and
// ContinueGenerate/ContinueStream) directly for multi-turn use, where the
// session — and the sandbox it holds open — is reused across calls. Mirrors
// TS's underlying `Agent.execute` used by callers that don't need multi-turn
// harness session management.
func (a *Agent) Execute(ctx context.Context, prompt string) (*agent.AgentResult, error) {
	return a.ExecuteWithMessages(ctx, []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{types.TextContent{Text: prompt}},
	}})
}

// ExecuteWithMessages is Execute with a message history as input.
func (a *Agent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*agent.AgentResult, error) {
	session, err := a.CreateSession(ctx, CreateSessionOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Destroy(context.WithoutCancel(ctx)) }()

	result, err := a.Generate(ctx, agent.AgentGenerateOptions{Messages: messages, HarnessSession: session})
	if err != nil {
		return nil, err
	}
	return &agent.AgentResult{
		Text: result.Text, Output: result.Output, Steps: result.Steps, ToolResults: result.ToolResults,
		FinishReason: result.FinishReason, StopReason: result.StopReason,
		// result.TotalUsage (not result.Usage) is the authoritative total
		// here: streamResultToGenerateResult below sets this harness
		// GenerateTextResult's Usage to the *final step's* usage and
		// TotalUsage to the real cross-step/server-overridden total, unlike
		// pkg/ai's own GenerateTextResult construction where the two are
		// always equal.
		Usage:    result.TotalUsage, //nolint:staticcheck
		Warnings: result.Warnings,
	}, nil
}

// streamResultToGenerateResult drains a completed *ai.StreamTextResult into
// the buffered *ai.GenerateTextResult shape Agent.Generate/.ContinueGenerate
// return, matching how TS's `HarnessGenerateTextResult` derives every field
// from the finished stream's `steps`.
func streamResultToGenerateResult(r *ai.StreamTextResult) *ai.GenerateTextResult {
	steps := r.Steps()
	var finalStep types.StepResult
	if len(steps) > 0 {
		finalStep = steps[len(steps)-1]
	}
	return &ai.GenerateTextResult{
		Content: r.Content(),
		Text:    r.Text(),
		Output:  r.Output(),
		// Reasoning/ReasoningText are deprecated aliases of
		// FinalStep.Reasoning/FinalStep.ReasoningText, set here for callers
		// still reading the old top-level fields (mirrors generate.go's own
		// GenerateTextResult population).
		Reasoning:          finalStep.Reasoning,     //nolint:staticcheck
		ReasoningText:      finalStep.ReasoningText, //nolint:staticcheck
		ToolCalls:          r.ToolCalls(),
		StaticToolCalls:    r.StaticToolCalls(),
		DynamicToolCalls:   r.DynamicToolCalls(),
		ToolResults:        r.ToolResults(),
		StaticToolResults:  r.StaticToolResults(),
		DynamicToolResults: r.DynamicToolResults(),
		Steps:              steps,
		FinalStep:          finalStep,
		FinishReason:       r.FinishReason(),
		// Usage is deliberately the final step's usage, not the total
		// (unlike pkg/ai's own GenerateTextResult, where Usage is already
		// the cross-step total and TotalUsage is just its deprecated
		// alias): TotalUsage below carries the real aggregate/bridge-
		// overridden total, and callers such as Agent.ExecuteWithMessages
		// read TotalUsage, not Usage, for that total. r.Usage() (not the
		// also-deprecated r.TotalUsage(), its pure alias) is used as the
		// source value here.
		Usage:            finalStep.Usage,
		TotalUsage:       r.Usage(), //nolint:staticcheck
		Warnings:         r.Warnings(),
		Sources:          r.Sources(),
		Files:            r.Files(),
		ResponseMessages: r.ResponseMessages(),
	}
}
