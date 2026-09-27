package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AgentSandboxConfig is the sandbox working-directory and lifecycle-hook
// configuration for a HarnessAgent. Mirrors TS `HarnessAgentSandboxConfig`
// (alias of the WG2 SandboxConfig type; `OnSession` is honored here, unlike
// the WG2-only helpers PrepareSandboxForHarness/PrepareHarnessSandboxTemplate
// which ignore it).
type AgentSandboxConfig = SandboxConfig

// AgentToolApprovalConfiguration is the per-tool static approval
// configuration. Mirrors TS `HarnessAgentToolApprovalConfiguration`.
type AgentToolApprovalConfiguration = ToolApprovalConfiguration

// PrepareCallResult is what Settings.PrepareCall returns: the turn-scoped
// fields it may override, plus the (possibly rewritten) prompt. Mirrors the
// return type of TS `HarnessAgentSettings.prepareCall`.
type PrepareCallResult struct {
	Model  string
	Skills []Skill
	// Instructions is either a string or a *types.Message (a system message;
	// only its Content text is used), mirroring TS `HarnessAgentSettings`'s
	// `instructions?: string | SystemModelMessage` (TS 4d1bf28). Extracted to
	// a plain string via instructionsText before being handed to the harness
	// adapter.
	Instructions interface{}
	// HasInstructions distinguishes "clear the instructions" (Instructions
	// == "", HasInstructions == true) from "leave them as configured"
	// (HasInstructions == false). Go has no `undefined`, so this flag plays
	// its role for prepareCall's rest-spread removal pattern.
	HasInstructions bool
	Tools           map[string]types.Tool
	ToolsContext    map[string]interface{}
	Prompt          Prompt
}

// PrepareCallOptions is passed to Settings.PrepareCall.
type PrepareCallOptions struct {
	CallOptions interface{}
	Prompt      Prompt
	Model       string
	Skills      []Skill
	// Instructions is either a string or a *types.Message, exactly as
	// configured on AgentSettings.Instructions (or overridden by the
	// per-call Instructions option). See PrepareCallResult.Instructions.
	Instructions interface{}
	Tools        map[string]types.Tool
	ToolsContext map[string]interface{}
}

// Callbacks are the lifecycle callbacks a HarnessAgent invokes for every
// turn, reusing pkg/ai's canonical event structs so a harness turn appears in
// callback-driven tooling the same way a ToolLoopAgent/StreamText call does.
// Mirrors TS `fe86f8f` (established ToolLoopAgent lifecycle callbacks ported
// to HarnessAgent).
type Callbacks struct {
	OnStart                  func(ctx context.Context, e ai.OnStartEvent)
	OnStepStart              func(ctx context.Context, e ai.OnStepStartEvent)
	OnLanguageModelCallStart ai.OnLanguageModelCallStartCallback
	OnLanguageModelCallEnd   ai.OnLanguageModelCallEndCallback
	OnToolExecutionStart     func(ctx context.Context, e ai.OnToolCallStartEvent)
	OnToolExecutionEnd       func(ctx context.Context, e ai.OnToolCallFinishEvent)
	OnStepEnd                func(ctx context.Context, e ai.OnStepFinishEvent)
	OnEnd                    func(ctx context.Context, e ai.OnFinishEvent)
}

// AgentSettings are the construction-time settings for a HarnessAgent.
// Mirrors TS `HarnessAgentSettings`.
//
// Prompt and per-call abort/cancellation belong on the options passed to
// Generate/Stream. Lifecycle callbacks are configured here for every call.
// PrepareCall can derive turn-scoped model, skills, instructions and tools
// from custom call options.
type AgentSettings struct {
	// Harness is the adapter driving the underlying agent runtime. Its
	// builtin tools are merged with UserTools and exposed via Agent.Tools().
	Harness Harness

	// ID is exposed via HarnessAgent.ID().
	ID string

	// Model is the model identifier used by the harness adapter.
	// PrepareCall can replace it between completed turns.
	Model string

	// UserTools are tools available to the runtime in addition to the
	// harness's own builtins. User tools take precedence over harness
	// builtins on key collision.
	UserTools map[string]types.Tool

	// ToolsContext is per-tool context passed to host-executed tools.
	ToolsContext map[string]interface{}

	// Skills made available to the underlying runtime.
	Skills []Skill

	// Instructions for the underlying agent runtime. Adapters append these
	// to a native system/developer prompt when supported. Accepts either a
	// plain string or a *types.Message (a system message; only its Content
	// text is forwarded to the harness adapter) for parity with
	// ToolLoopAgent's Instructions field, mirroring TS
	// `HarnessAgentSettings.instructions: string | SystemModelMessage` (TS
	// 4d1bf28). PrepareCall can replace it (with the same two shapes)
	// between completed turns.
	Instructions interface{}

	// Headers are additional HTTP headers sent with every model request.
	// "authorization", "x-api-key", "user-agent" and "x-client-app" are
	// managed by the harness and must not be set here.
	Headers map[string]string

	// PrepareCall derives the prompt and the settings that may vary between
	// completed turns. See PrepareCallOptions/PrepareCallResult.
	PrepareCall func(ctx context.Context, opts PrepareCallOptions) (PrepareCallResult, error)

	// StopWhen are the conditions that stop the current result after a
	// completed harness tool step that could continue into another model
	// step. The underlying turn remains unfinished and can be suspended and
	// continued. Nil means the harness runs until the turn naturally
	// finishes or pauses.
	StopWhen []ai.StopCondition

	Callbacks Callbacks

	// PermissionMode is the baseline permission mode for adapter-native
	// built-in tools. Defaults to PermissionModeAllowAll.
	PermissionMode PermissionMode

	// ToolApproval is the per-tool static approval configuration for
	// host-executed tools.
	ToolApproval AgentToolApprovalConfiguration

	// ActiveTools / InactiveTools limit the tools available to the harness
	// without changing the tool call/result types in the result. Mutually
	// exclusive; leave both nil to allow every tool.
	ActiveTools   []string
	InactiveTools []string

	// Sandbox is the provider used to create or resume network sandbox
	// sessions. When nil, every CreateSession call must provide an existing
	// sandbox session.
	Sandbox SandboxProvider

	// SandboxConfig is the sandbox working-directory and lifecycle-hook
	// configuration.
	SandboxConfig AgentSandboxConfig
}

var forbiddenAgentHeaders = map[string]struct{}{
	"authorization": {},
	"x-api-key":     {},
	"user-agent":    {},
	"x-client-app":  {},
}

// normalizeAgentHeaders validates and lower-cases header names, mirroring
// TS's `normalizeHeaders` call in the HarnessAgent constructor.
func normalizeAgentHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		lower := strings.ToLower(name)
		if _, forbidden := forbiddenAgentHeaders[lower]; forbidden {
			return nil, fmt.Errorf("HarnessAgent: `headers` must not include the managed header `%s`.", lower)
		}
		out[lower] = value
	}
	return out, nil
}

// instructionsText extracts a plain instructions string from a value that is
// either nil, a string, or a *types.Message/types.Message (a system
// message). Mirrors TS `_prepareTurnSettings`'s
// `typeof options.instructions === 'string' ? options.instructions :
// options.instructions?.content` (TS 4d1bf28): a SystemModelMessage's
// content is always a single string there, while Go's types.Message
// generalizes content to []ContentPart, so this concatenates the text parts.
// Every harness-v1 adapter only understands a plain string, so this
// extraction happens once, after PrepareCall has had a chance to replace
// Instructions, exactly like TS.
func instructionsText(v interface{}) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	case types.Message:
		return harnessSystemMessageText(t), nil
	case *types.Message:
		if t == nil {
			return "", nil
		}
		return harnessSystemMessageText(*t), nil
	default:
		return "", fmt.Errorf("harness: Instructions must be a string or *types.Message, got %T", v)
	}
}

// harnessSystemMessageText concatenates a message's text content parts.
func harnessSystemMessageText(msg types.Message) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if text, ok := part.(types.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// assertNoReservedQuestionTool mirrors TS `assertNoReservedQuestionTool`.
func assertNoReservedQuestionTool(harness Harness, userTools map[string]types.Tool) error {
	_, harnessHasIt := harness.BuiltinTools()[string(BuiltinToolAskUserQuestions)]
	_, userHasIt := userTools[string(BuiltinToolAskUserQuestions)]
	if harnessHasIt && userHasIt {
		return errors.New("HarnessAgent tool name 'askUserQuestions' is reserved for harness question requests.")
	}
	return nil
}
