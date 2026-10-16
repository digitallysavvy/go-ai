package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/ordered"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	telemetrypkg "github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// StepEndCallback is called after each completed step.
type StepEndCallback func(ctx context.Context, e ai.OnStepFinishEvent)

// StepFinishCallback is called after each completed step.
//
// Deprecated: use StepEndCallback.
type StepFinishCallback func(ctx context.Context, e ai.OnStepFinishEvent)

// EndCallback is called once after workflow completion. Mirrors TS
// WorkflowAgent's stable `onEnd` (workflow-agent.ts), which shares the same
// OnFinishEvent-shaped event as generateText/streamText/ToolLoopAgent.
type EndCallback func(ctx context.Context, e ai.OnFinishEvent)

// FinishCallback is called once after workflow completion.
//
// Deprecated: use EndCallback.
type FinishCallback func(ctx context.Context, e ai.OnFinishEvent)

// ErrorCallback is called when workflow execution returns an error.
type ErrorCallback func(ctx context.Context, err error)

// AbortCallback is called when workflow execution is aborted by context cancellation.
type AbortCallback func(ctx context.Context, steps []types.StepResult)

// StartCallback fires once before the first step.
type StartCallback func(ctx context.Context, e ai.OnStartEvent)

// StepStartCallback fires before each model step.
type StepStartCallback func(ctx context.Context, e ai.OnStepStartEvent)

// ToolExecutionStartCallback fires before local tool execution.
type ToolExecutionStartCallback func(ctx context.Context, e ai.OnToolCallStartEvent)

// ToolExecutionEndCallback fires after local tool execution.
type ToolExecutionEndCallback func(ctx context.Context, e ai.OnToolCallFinishEvent)

// PrepareCallHook mutates per-step call options before model invocation.
type PrepareCallHook func(ctx context.Context, opts LanguageModelCallOptions) (LanguageModelCallOptions, error)

// PrepareStepHook mutates per-step options using current history.
type PrepareStepHook func(ctx context.Context, opts LanguageModelCallOptions) (LanguageModelCallOptions, error)

// FilterActiveToolsHook can reduce available tools per-step.
type FilterActiveToolsHook func(ctx context.Context, stepNumber int, tools []types.Tool) []types.Tool

// LanguageModelCallOptions mirrors the per-step call config used by ToolLoopAgent.
type LanguageModelCallOptions struct {
	StepNumber            int
	System                string
	AllowSystemInMessages bool
	Messages              []types.Message
	Tools                 []types.Tool
	ToolChoice            types.ToolChoice
	CallOptions           interface{}
	Temperature           *float64
	MaxTokens             *int
	TopP                  *float64
	TopK                  *int
	FrequencyPenalty      *float64
	PresencePenalty       *float64
	StopSequences         []string
	Seed                  *int
	Headers               map[string]string
	Reasoning             *types.ReasoningLevel
	SendReasoning         *bool
	ProviderOptions       map[string]interface{}
	RuntimeContext        interface{}
	ToolsContext          map[string]interface{}
	ExperimentalSandbox   interface{}
	PreviousSteps         []types.StepResult
	AccumulatedUsage      types.Usage
	CustomData            interface{}

	// StopWhen, ActiveTools and ExperimentalDownload mirror TS prepareCall's
	// per-call stopWhen/activeTools/download settings parity (d56638a): a
	// PrepareCall/PrepareStep hook can read the current effective value here
	// and mutate it to override the call.
	StopWhen             []ai.StopCondition
	ActiveTools          []string
	ExperimentalDownload ai.DownloadFunction

	// MaxRetries and Timeout mirror TS's `maxRetries`/`abortSignal` prepareCall
	// parity (419adc7). Timeout is the Go stand-in for TS's AbortSignal.
	MaxRetries *int
	Timeout    *ai.TimeoutConfig

	// InitialInstructions and InitialMessages are the original (unmutated)
	// instructions/messages the call was invoked with, matching TS
	// prepareCall's initial-inputs parity (b666f57). They are read-only: a
	// hook should mutate System/Messages to change what is sent, not these.
	InitialInstructions string
	InitialMessages     []types.Message
}

// WorkflowAgent is a serializable-friendly wrapper around the SDK tool loop.
type WorkflowAgent struct {
	Model  provider.LanguageModel
	System string
	// Instructions is the TypeScript-compatible name for system instructions.
	Instructions interface{}
	// AllowSystemInMessages permits system-role messages in Messages. By
	// default WorkflowAgent rejects system messages; use Instructions/System for
	// default system prompts.
	AllowSystemInMessages bool
	Tools                 []types.Tool
	ToolSet               map[string]types.Tool
	// StopWhen is the default stop condition. When empty NewWorkflowAgent sets
	// IsStepCount(20), which applies to Generate; Stream is unbounded unless
	// StopWhen is set explicitly on the agent or the call (TS: only generate
	// defaults to isStepCount(20)).
	StopWhen  []ai.StopCondition
	Output    interface{}
	Telemetry *ai.TelemetrySettings
	ID        string
	Prompt    string

	// stopWhenDefaulted records that StopWhen was filled in by NewWorkflowAgent.
	stopWhenDefaulted bool

	OnStart              StartCallback
	OnStepStart          StepStartCallback
	OnToolExecutionStart ToolExecutionStartCallback
	OnToolExecutionEnd   ToolExecutionEndCallback
	OnStepEnd            StepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish StepFinishCallback
	OnEnd        EndCallback
	// Deprecated: use OnEnd.
	OnFinish FinishCallback
	OnError  ErrorCallback
	OnAbort  AbortCallback

	PrepareCall       PrepareCallHook
	PrepareStep       PrepareStepHook
	FilterActiveTools FilterActiveToolsHook
	CallOptionsSchema schema.Schema
	CallOptions       interface{}
	ActiveTools       []string

	Temperature                 *float64
	MaxTokens                   *int
	TopP                        *float64
	TopK                        *int
	FrequencyPenalty            *float64
	PresencePenalty             *float64
	StopSequences               []string
	Seed                        *int
	Headers                     map[string]string
	Reasoning                   *types.ReasoningLevel
	SendReasoning               *bool
	ProviderOptions             map[string]interface{}
	RuntimeContext              interface{}
	ToolsContext                map[string]interface{}
	ToolChoice                  types.ToolChoice
	Include                     *ai.IncludeOptions
	ExperimentalSandbox         interface{}
	ExperimentalRefineToolInput map[string]ai.ToolInputRefiner
	// RepairToolCall attempts to repair tool calls that fail to parse.
	RepairToolCall ai.ToolCallRepairFunction
	// ExperimentalRepairToolCall is a deprecated alias for RepairToolCall.
	//
	// Deprecated: use RepairToolCall.
	ExperimentalRepairToolCall ai.ToolCallRepairFunction
	// ExperimentalToolApprovalSecret signs issued approval requests and
	// verifies resumed approvals before approved tools execute.
	ExperimentalToolApprovalSecret []byte

	// MaxRetries controls transient provider call retries for each model
	// call, forwarded to agent.AgentConfig.MaxRetries. Defaults to 2 when
	// nil, matching TS WorkflowAgent's `mergedGenerationSettings.maxRetries
	// ?? 2`.
	MaxRetries *int
	// Timeout provides granular timeout controls, forwarded to
	// agent.AgentConfig.Timeout.
	Timeout *ai.TimeoutConfig
	// ToolCallConcurrency is the maximum number of local tool calls that may
	// execute concurrently. Set to 1 to execute calls sequentially in model
	// order, including approved calls resumed by the agent. nil runs all calls
	// concurrently. Values below 1 return an InvalidArgumentError. Mirrors TS
	// WorkflowAgent's `toolCallConcurrency`.
	ToolCallConcurrency *int
	// ExperimentalDownload customizes remote file URL downloads before model
	// calls, forwarded to agent.AgentConfig.ExperimentalDownload.
	ExperimentalDownload ai.DownloadFunction
}

// WorkflowGenerateOptions configures a single generate invocation.
type WorkflowGenerateOptions struct {
	// Per-call generation settings (TS Partial<GenerationSettings>). A value
	// set here replaces the agent's; Headers and ProviderOptions replace as a
	// whole.
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int
	Headers          map[string]string
	Reasoning        *types.ReasoningLevel
	ProviderOptions  map[string]interface{}

	Prompt                      string
	Messages                    []types.Message
	System                      string
	Instructions                interface{}
	AllowSystemInMessages       bool
	Tools                       []types.Tool
	ToolSet                     map[string]types.Tool
	StopWhen                    []ai.StopCondition
	Telemetry                   *ai.TelemetrySettings
	ActiveTools                 []string
	RuntimeContext              interface{}
	ToolsContext                map[string]interface{}
	Include                     *ai.IncludeOptions
	ExperimentalSandbox         interface{}
	ExperimentalRefineToolInput map[string]ai.ToolInputRefiner

	// ToolChoice overrides the agent's ToolChoice for this call (TS
	// `toolChoice`).
	ToolChoice types.ToolChoice
	// Output overrides the agent's Output specification for this call (TS
	// `output`).
	Output interface{}
	// PrepareStep is a per-call step hook that replaces the agent-level
	// PrepareStep for this call (TS `options.prepareStep ?? this.prepareStep`).
	PrepareStep PrepareStepHook
	// RepairToolCall attempts to repair tool calls that fail to parse.
	RepairToolCall ai.ToolCallRepairFunction
	// ExperimentalRepairToolCall is a deprecated alias for RepairToolCall.
	//
	// Deprecated: use RepairToolCall.
	ExperimentalRepairToolCall ai.ToolCallRepairFunction
	// ExperimentalToolApprovalSecret overrides the agent's approval secret.
	ExperimentalToolApprovalSecret []byte

	// MaxRetries overrides the agent's MaxRetries for this call.
	MaxRetries *int
	// Timeout overrides the agent's Timeout for this call.
	Timeout *ai.TimeoutConfig
	// ToolCallConcurrency is the maximum number of local tool calls that may
	// execute concurrently. Set to 1 to execute calls sequentially in model
	// order, including approved calls resumed by the agent. nil runs all calls
	// concurrently. Values below 1 return an InvalidArgumentError. Mirrors TS
	// WorkflowAgent's `toolCallConcurrency`.
	ToolCallConcurrency *int
	// ExperimentalDownload overrides the agent's ExperimentalDownload for this call.
	ExperimentalDownload ai.DownloadFunction

	OnStart              StartCallback
	OnStepStart          StepStartCallback
	OnToolExecutionStart ToolExecutionStartCallback
	OnToolExecutionEnd   ToolExecutionEndCallback
	OnStepEnd            StepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish StepFinishCallback
	OnEnd        EndCallback
	// Deprecated: use OnEnd.
	OnFinish FinishCallback
	OnError  ErrorCallback
	OnAbort  AbortCallback
}

// WorkflowStreamOptions configures a single stream invocation.
type WorkflowStreamOptions struct {
	// Per-call generation settings (TS Partial<GenerationSettings>). A value
	// set here replaces the agent's; Headers and ProviderOptions replace as a
	// whole.
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int
	Headers          map[string]string
	Reasoning        *types.ReasoningLevel
	ProviderOptions  map[string]interface{}

	Prompt                string
	Messages              []types.Message
	System                string
	Instructions          interface{}
	AllowSystemInMessages bool
	Tools                 []types.Tool
	ToolSet               map[string]types.Tool
	StopWhen              []ai.StopCondition
	Telemetry             *ai.TelemetrySettings

	ActiveTools                 []string
	RuntimeContext              interface{}
	ToolsContext                map[string]interface{}
	Include                     *ai.IncludeOptions
	ExperimentalSandbox         interface{}
	ExperimentalRefineToolInput map[string]ai.ToolInputRefiner

	// ToolChoice overrides the agent's ToolChoice for this call (TS
	// `toolChoice`).
	ToolChoice types.ToolChoice
	// Output overrides the agent's Output specification for this call (TS
	// `output`).
	Output interface{}
	// PrepareStep is a per-call step hook that replaces the agent-level
	// PrepareStep for this call (TS `options.prepareStep ?? this.prepareStep`).
	PrepareStep PrepareStepHook
	// RepairToolCall attempts to repair tool calls that fail to parse.
	RepairToolCall ai.ToolCallRepairFunction
	// ExperimentalRepairToolCall is a deprecated alias for RepairToolCall.
	//
	// Deprecated: use RepairToolCall.
	ExperimentalRepairToolCall ai.ToolCallRepairFunction
	// ExperimentalToolApprovalSecret overrides the agent's approval secret.
	ExperimentalToolApprovalSecret []byte

	// MaxRetries overrides the agent's MaxRetries for this call.
	MaxRetries *int
	// Timeout overrides the agent's Timeout for this call.
	Timeout *ai.TimeoutConfig
	// ToolCallConcurrency is the maximum number of local tool calls that may
	// execute concurrently. Set to 1 to execute calls sequentially in model
	// order, including approved calls resumed by the agent. nil runs all calls
	// concurrently. Values below 1 return an InvalidArgumentError. Mirrors TS
	// WorkflowAgent's `toolCallConcurrency`.
	ToolCallConcurrency *int
	// ExperimentalDownload overrides the agent's ExperimentalDownload for this call.
	ExperimentalDownload ai.DownloadFunction

	// ExperimentalTransform is an ordered list of transforms applied to raw
	// model stream chunks before they reach OnChunk or the returned
	// WorkflowStreamResult, matching TS WorkflowAgent's
	// `experimental_transform` stream option (hash 165455d). Previously
	// declared-but-unused in TS; this forwards it through to the underlying
	// ai.StreamText call so it actually runs.
	ExperimentalTransform []ai.StreamTransformFunc

	OnChunk              func(chunk provider.StreamChunk)
	OnStart              StartCallback
	OnStepStart          StepStartCallback
	OnToolExecutionStart ToolExecutionStartCallback
	OnToolExecutionEnd   ToolExecutionEndCallback
	OnStepEnd            StepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish StepFinishCallback
	OnEnd        EndCallback
	// Deprecated: use OnEnd.
	OnFinish FinishCallback
	OnError  ErrorCallback
	OnAbort  AbortCallback
}

// WorkflowResult is the final non-streaming workflow result.
type WorkflowResult struct {
	*agent.AgentResult

	// ResponseMessages are the messages this run added to the conversation:
	// first the tool message holding the results of any tool approvals the
	// run resumed (executed before the first model call), then each step's
	// assistant and tool messages. Append them to the input messages to
	// continue the conversation (TS result.response.messages).
	ResponseMessages []types.Message
}

// IsLoopFinished reports whether the workflow ended naturally.
func (r *WorkflowResult) IsLoopFinished() bool {
	if r == nil || r.AgentResult == nil {
		return false
	}
	return r.StopReason == ""
}

// WorkflowStreamResult wraps the streaming result.
type WorkflowStreamResult struct{ *ai.StreamTextResult }

// skipOutput reports whether TS WorkflowAgent's stream mode would not parse
// the structured output: it parses the last step's text only when the text is
// non-empty (whatever the finish reason, including tool-calls) and at least one
// step ran. Core streamText parses unconditionally, so an empty final text
// (for example a tool-calls finish) would otherwise surface a parse error the
// workflow agent never produces.
func (r *WorkflowStreamResult) skipOutput() bool {
	steps := r.Steps()
	return len(steps) == 0 || steps[len(steps)-1].Text == ""
}

// Output returns the parsed structured output, nil when TS would not parse it
// (see skipOutput).
func (r *WorkflowStreamResult) Output() any {
	if r.skipOutput() {
		return nil
	}
	return r.StreamTextResult.Output()
}

// OutputErr returns the output parse error, nil when TS would not parse the
// output (see skipOutput).
func (r *WorkflowStreamResult) OutputErr() error {
	if r.skipOutput() {
		return nil
	}
	return r.StreamTextResult.OutputErr()
}

// Err returns the error that ended the stream. Like TS stream mode, where an
// output parse failure fails the run unless an earlier error already did, it
// falls back to the output parse error.
func (r *WorkflowStreamResult) Err() error {
	if err := r.StreamTextResult.Err(); err != nil {
		return err
	}
	return r.OutputErr()
}

// NewWorkflowAgent validates and creates a WorkflowAgent.
func NewWorkflowAgent(cfg WorkflowAgent) (*WorkflowAgent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("workflow: model is required")
	}
	if cfg.System == "" && cfg.Instructions != nil {
		system, err := normalizeInstructions(cfg.Instructions)
		if err != nil {
			return nil, err
		}
		cfg.System = system
	}
	if len(cfg.StopWhen) == 0 {
		cfg.StopWhen = []ai.StopCondition{ai.IsStepCount(20)}
		cfg.stopWhenDefaulted = true
	}
	return &cfg, nil
}

func normalizeInstructions(instructions interface{}) (string, error) {
	switch v := instructions.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case types.Message:
		return systemMessageText(v), nil
	case []types.Message:
		parts := make([]string, 0, len(v))
		for _, msg := range v {
			text := systemMessageText(msg)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n"), nil
	case []string:
		return strings.Join(v, "\n"), nil
	default:
		return "", fmt.Errorf("workflow: unsupported instructions type %T", instructions)
	}
}

func systemMessageText(msg types.Message) string {
	parts := make([]string, 0, len(msg.Content))
	for _, part := range msg.Content {
		if text, ok := part.(types.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func orderedTools(tools []types.Tool, toolSet map[string]types.Tool) []types.Tool {
	if len(toolSet) == 0 {
		return tools
	}
	// Go maps have no insertion order; sort by key so the tool list sent to
	// the model is deterministic across runs (stable prompts, cacheable).
	names := make([]string, 0, len(toolSet))
	for name := range toolSet {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]types.Tool, 0, len(toolSet))
	for _, name := range names {
		tool := toolSet[name]
		if tool.Name == "" {
			tool.Name = name
		}
		out = append(out, tool)
	}
	return out
}

func effectiveWorkflowTools(w *WorkflowAgent, streamOpts WorkflowStreamOptions, generateOpts WorkflowGenerateOptions) []types.Tool {
	tools := orderedTools(w.Tools, w.ToolSet)
	if generateOpts.Tools != nil || generateOpts.ToolSet != nil {
		tools = orderedTools(generateOpts.Tools, generateOpts.ToolSet)
	}
	if streamOpts.Tools != nil || streamOpts.ToolSet != nil {
		tools = orderedTools(streamOpts.Tools, streamOpts.ToolSet)
	}
	return tools
}

// workflowTelemetryRuntimeContext returns the subset of the runtime context
// that Telemetry.IncludeRuntimeContext opts in, so workflow-level telemetry
// events never carry unselected keys (TS #23340). It is always non-nil.
func workflowTelemetryRuntimeContext(settings *ai.TelemetrySettings, runtimeContext interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if settings == nil || len(settings.IncludeRuntimeContext) == 0 || runtimeContext == nil {
		return out
	}
	rv := reflect.ValueOf(runtimeContext)
	for rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return out
	}
	for key, enabled := range settings.IncludeRuntimeContext {
		if !enabled {
			continue
		}
		if v := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key())); v.IsValid() {
			out[key] = v.Interface()
		}
	}
	return out
}

// resolveToolCallConcurrency picks the per-call value over the agent default
// and validates it (TS prepareToolCallConcurrency). It returns 0 for unbounded.
func resolveToolCallConcurrency(values ...*int) (int, error) {
	for _, v := range values {
		if v == nil {
			continue
		}
		if *v < 1 {
			return 0, &providererrors.InvalidArgumentError{Field: "toolCallConcurrency", Message: "toolCallConcurrency must be >= 1"}
		}
		return *v, nil
	}
	return 0, nil
}

// callSetting picks the first non-nil setting: the stream call, then the
// generate call, then the agent default.
func callSetting[T any](values ...*T) *T {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func callSlice[T any](values ...[]T) []T {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func callMap[K comparable, V any](values ...map[K]V) map[K]V {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonNilMap(values ...map[string]interface{}) map[string]interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func workflowToolByName(tools []types.Tool, name string) *types.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func workflowToolNeedsApproval(ctx context.Context, tool *types.Tool, call types.ToolCall, messages []types.Message, toolsContext map[string]interface{}) bool {
	if tool == nil {
		return false
	}
	setting := tool.ToolApproval
	if setting == nil {
		setting = tool.NeedsApproval
	}
	switch v := setting.(type) {
	case bool:
		return v
	case types.ToolApprovalStatus:
		return v == types.ToolApprovalStatusUserApproval || v == types.ToolApprovalStatusApproved || v == types.ToolApprovalStatusDenied
	case string:
		status := types.ToolApprovalStatus(v)
		return status == types.ToolApprovalStatusUserApproval || status == types.ToolApprovalStatusApproved || status == types.ToolApprovalStatusDenied
	case types.ToolNeedsApprovalFunc:
		var toolContext interface{}
		if toolsContext != nil {
			toolContext = toolsContext[call.ToolName]
		}
		return v(ctx, call.Arguments, types.ToolNeedsApprovalOptions{ToolCallID: call.ID, Messages: messages, Context: toolContext})
	case types.NeedsApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		return v(ctx, call.Arguments)
	default:
		return false
	}
}

// workflowApprovalResumeOptions configures processWorkflowApprovalResume.
type workflowApprovalResumeOptions struct {
	messages            []types.Message
	tools               []types.Tool
	runtimeContext      interface{}
	toolsContext        map[string]interface{}
	experimentalSandbox interface{}
	toolApprovalSecret  []byte
	onToolStart         ToolExecutionStartCallback
	onToolEnd           ToolExecutionEndCallback
	// telemetrySettings, when set, wraps each approved tool's execution in an
	// OTel tool call span parented under ctx's current span (27d294d) — the
	// caller is expected to have already started the workflow-level
	// operation span so this has a root to parent under.
	telemetrySettings *telemetrypkg.Options
	// toolCallConcurrency bounds concurrent approved tool executions; 0 means
	// unbounded.
	toolCallConcurrency int
	// initialResponse, when non-nil, receives the tool message holding the
	// resumed approvals' results (TS initialResponseMessages), so generate can
	// report it as the first response message.
	initialResponse *[]types.Message
}

// processWorkflowApprovalResume resolves tool approvals from the last tool
// message before the agent loop starts, mirroring TS WorkflowAgent: it reuses
// the core collector and validator (ai.CollectToolApprovals /
// ai.ValidateApprovedToolApprovals, incl. signature verification when a
// secret is configured), executes approved local tools with tool execution
// callbacks, turns revalidation failures and execution errors into
// model-visible error-text results, synthesizes execution-denied results for
// denials, and strips locally resolved approval parts. Provider-executed
// approvals are preserved (and stamped providerExecuted) for the provider.
func processWorkflowApprovalResume(ctx context.Context, opts workflowApprovalResumeOptions) ([]types.Message, []provider.StreamChunk, error) {
	messages := opts.messages
	if len(messages) == 0 {
		return messages, nil, nil
	}
	collected, err := ai.CollectToolApprovals(messages)
	if err != nil {
		return nil, nil, err
	}
	if len(collected.ApprovedToolApprovals) == 0 && len(collected.DeniedToolApprovals) == 0 {
		return messages, nil, nil
	}

	providerApprovalIDs := map[string]bool{}
	for _, approval := range append(append([]ai.CollectedToolApproval(nil), collected.ApprovedToolApprovals...), collected.DeniedToolApprovals...) {
		if approval.ToolCall.ProviderExecuted {
			providerApprovalIDs[approval.ApprovalResponse.ApprovalID] = true
		}
	}

	localResults := make([]types.ContentPart, 0)
	prefixChunks := make([]provider.StreamChunk, 0)
	errorTextResult := func(call types.ToolCall, text string) types.ToolResultContent {
		return types.ToolResultContent{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Output:     &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: text},
		}
	}

	// Approved calls run with the same concurrency limit as new tool calls
	// (TS #22135): all at once by default, in order with a limit of 1. Results
	// are assembled in model order regardless of completion order.
	type approvedOutcome struct {
		result types.ContentPart
		chunk  *provider.StreamChunk
		err    error
	}
	runApproved := func(approval ai.CollectedToolApproval) approvedOutcome {
		call := approval.ToolCall
		if call.ProviderExecuted {
			return approvedOutcome{}
		}
		tool := workflowToolByName(opts.tools, call.ToolName)
		if tool == nil || tool.Execute == nil {
			return approvedOutcome{}
		}
		if !workflowToolNeedsApproval(ctx, tool, call, messages, opts.toolsContext) {
			return approvedOutcome{result: errorTextResult(call, fmt.Sprintf("Tool %q does not require approval", call.ToolName))}
		}

		// Re-validate through the shared core implementation: signature (when
		// configured), input schema, and approval policy. Failures become a
		// model-visible tool error so the loop can continue.
		revalidationReason := ""
		validated, err := ai.ValidateApprovedToolApprovals(ctx, ai.ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []ai.CollectedToolApproval{approval},
			Tools:                 opts.tools,
			Messages:              messages,
			ToolsContext:          opts.toolsContext,
			RuntimeContext:        opts.runtimeContext,
			ToolApprovalSecret:    opts.toolApprovalSecret,
		})
		switch {
		case err != nil:
			revalidationReason = err.Error()
		case len(validated.InvalidToolApprovals) > 0:
			revalidationReason = validated.InvalidToolApprovals[0].Error.Error()
		case len(validated.DeniedToolApprovals) > 0:
			revalidationReason = validated.DeniedToolApprovals[0].ApprovalResponse.Reason
			if revalidationReason == "" {
				revalidationReason = "Tool approval denied"
			}
		}
		if revalidationReason != "" {
			return approvedOutcome{result: errorTextResult(call, revalidationReason)}
		}

		result, execErr := executeWorkflowApprovedTool(ctx, tool, call, opts)
		if execErr != nil {
			// TS writeApprovalToolResults writes a plain tool-result whose output
			// is the error text (rawOutput), not a tool-error, so the UI stream
			// carries tool-output-available.
			return approvedOutcome{
				result: errorTextResult(call, execErr.Error()),
				chunk: &provider.StreamChunk{
					Type: provider.ChunkTypeToolResult,
					ToolResult: &types.ToolResult{
						ToolCallID: call.ID,
						ToolName:   call.ToolName,
						Input:      call.Arguments,
						Result:     execErr.Error(),
						Dynamic:    call.Dynamic,
					},
				},
			}
		}
		part := types.ToolResultContent{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Input:      call.Arguments,
			Result:     result,
		}
		if tool.ToModelOutput != nil {
			callCopy := call
			output, err := tool.ToModelOutput(ctx, types.ToModelOutputOptions{
				ToolCallID: call.ID,
				Input:      call.Arguments,
				Output:     result,
				// Result is a deprecated alias of Output, kept for
				// ToModelOutput implementations still reading it.
				Result:   result, //nolint:staticcheck
				ToolCall: &callCopy,
			})
			if err != nil {
				return approvedOutcome{err: err}
			}
			part.Output = output
			part.Result = nil
		}
		return approvedOutcome{
			result: part,
			chunk: &provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Result:     result,
					Dynamic:    call.Dynamic,
				},
			},
		}
	}
	outcomes := make([]approvedOutcome, len(collected.ApprovedToolApprovals))
	// Calls are claimed in model order; once ctx is cancelled or a call
	// fails, no further queued call is started (TS mapWithConcurrency). Calls
	// that were never started produce no result.
	ordered.Run(ctx, len(outcomes), opts.toolCallConcurrency, func(i int) error {
		outcomes[i] = runApproved(collected.ApprovedToolApprovals[i])
		return outcomes[i].err
	})
	for _, o := range outcomes {
		if o.err != nil {
			return nil, nil, o.err
		}
		if o.result != nil {
			localResults = append(localResults, o.result)
		}
		if o.chunk != nil {
			prefixChunks = append(prefixChunks, *o.chunk)
		}
	}

	deniedChunks := 0
	for _, approval := range collected.DeniedToolApprovals {
		call := approval.ToolCall
		if call.ProviderExecuted {
			continue
		}
		// A denial result that is already persisted in the conversation is
		// reused (no duplicate result in the next prompt), but the denial
		// stream event is still emitted (TS #22019).
		if approval.ExistingToolResult == nil {
			localResults = append(localResults, types.ToolResultContent{
				ToolCallID: call.ID,
				ToolName:   call.ToolName,
				Input:      call.Arguments,
				Output:     &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: approval.ApprovalResponse.Reason},
			})
		}
		deniedChunks++
		prefixChunks = append(prefixChunks, provider.StreamChunk{
			Type:       provider.ChunkTypeToolOutputDenied,
			ToolResult: &types.ToolResult{ToolCallID: call.ID, ToolName: call.ToolName, Input: call.Arguments},
		})
	}

	cleaned := make([]types.Message, 0, len(messages)+1)
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleAssistant:
			filtered := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				keep := true
				switch p := part.(type) {
				case types.ToolApprovalRequestContent:
					keep = providerApprovalIDs[p.ApprovalID]
				case *types.ToolApprovalRequestContent:
					keep = p != nil && providerApprovalIDs[p.ApprovalID]
				}
				if keep {
					filtered = append(filtered, part)
				}
			}
			if len(filtered) > 0 {
				msg.Content = filtered
				cleaned = append(cleaned, msg)
			}
		case types.RoleTool:
			filtered := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.ToolApprovalResponseContent:
					if providerApprovalIDs[p.ApprovalID] {
						p.ProviderExecuted = true
						filtered = append(filtered, p)
					}
				case *types.ToolApprovalResponseContent:
					if p != nil && providerApprovalIDs[p.ApprovalID] {
						cp := *p
						cp.ProviderExecuted = true
						filtered = append(filtered, cp)
					}
				default:
					filtered = append(filtered, part)
				}
			}
			if len(filtered) > 0 {
				msg.Content = filtered
				cleaned = append(cleaned, msg)
			}
		default:
			cleaned = append(cleaned, msg)
		}
	}
	if len(localResults) > 0 {
		toolMessage := types.Message{Role: types.RoleTool, Content: localResults}
		cleaned = append(cleaned, toolMessage)
		if opts.initialResponse != nil {
			*opts.initialResponse = append(*opts.initialResponse, toolMessage)
		}
	}
	if len(localResults) > 0 || deniedChunks > 0 {
		prefixChunks = append(prefixChunks,
			provider.StreamChunk{Type: provider.ChunkTypeStreamFinish},
			provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
		)
	}
	return cleaned, prefixChunks, nil
}

// executeWorkflowApprovedTool runs an approved tool with the conversation
// messages and fires the tool execution start/end callbacks (TS
// executeToolWithCallbacks).
func executeWorkflowApprovedTool(ctx context.Context, tool *types.Tool, call types.ToolCall, opts workflowApprovalResumeOptions) (interface{}, error) {
	var toolContext interface{}
	if opts.toolsContext != nil {
		toolContext = opts.toolsContext[call.ToolName]
	}
	if opts.onToolStart != nil {
		opts.onToolStart(ctx, ai.OnToolCallStartEvent{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			ToolCall:   call,
			// Args/StepNumber are deprecated, JSON-excluded aliases kept for
			// callbacks still reading the old fields; ToolCall.Arguments is
			// the replacement for Args above.
			Args:                call.Arguments, //nolint:staticcheck
			StepNumber:          0,              //nolint:staticcheck
			Messages:            opts.messages,
			ExperimentalContext: opts.runtimeContext,
			RuntimeContext:      opts.runtimeContext,
			ToolsContext:        opts.toolsContext,
		})
	}
	// Fire the OTel tool call span for this pre-step approved-tool execution
	// under ctx's current span, so it is grouped under the root operation
	// span rather than dropped for lack of a parent (27d294d).
	toolCtx := telemetrypkg.FireOnToolCallStart(ctx, telemetrypkg.TelemetryToolCallStartEvent{
		Settings:   opts.telemetrySettings,
		ToolCallID: call.ID,
		ToolName:   call.ToolName,
		Args:       call.Arguments,
	})
	start := time.Now()
	result, err := tool.Execute(toolCtx, call.Arguments, types.ToolExecutionOptions{
		ToolCallID:          call.ID,
		UserContext:         opts.runtimeContext,
		RuntimeContext:      opts.runtimeContext,
		ToolContext:         toolContext,
		Metadata:            map[string]interface{}{},
		ToolMetadata:        call.ToolMetadata,
		Messages:            opts.messages,
		ExperimentalSandbox: opts.experimentalSandbox,
	})
	telemetrypkg.FireOnToolCallFinish(toolCtx, telemetrypkg.TelemetryToolCallFinishEvent{
		Settings:   opts.telemetrySettings,
		ToolCallID: call.ID,
		ToolName:   call.ToolName,
		Args:       call.Arguments,
		Result:     result,
		Error:      err,
		DurationMs: time.Since(start).Milliseconds(),
	})
	if opts.onToolEnd != nil {
		toolExecutionMs := time.Since(start).Milliseconds()
		toolOutput := types.ToolResult{ToolCallID: call.ID, ToolName: call.ToolName, Input: call.Arguments, Error: err}
		if err == nil {
			toolOutput.Result = result
		}
		finish := ai.OnToolCallFinishEvent{
			ToolCallID:      call.ID,
			ToolName:        call.ToolName,
			ToolCall:        call,
			ToolOutput:      toolOutput,
			ToolExecutionMs: toolExecutionMs,
			// Args/Error/DurationMs/StepNumber are deprecated, JSON-excluded
			// aliases kept for callbacks still reading the old fields;
			// ToolCall/ToolOutput/ToolExecutionMs above are the replacements.
			Args:                call.Arguments,  //nolint:staticcheck
			Error:               err,             //nolint:staticcheck
			DurationMs:          toolExecutionMs, //nolint:staticcheck
			StepNumber:          0,               //nolint:staticcheck
			Messages:            opts.messages,
			ExperimentalContext: opts.runtimeContext,
			RuntimeContext:      opts.runtimeContext,
			ToolsContext:        opts.toolsContext,
		}
		if err == nil {
			finish.Result = result //nolint:staticcheck
		}
		opts.onToolEnd(ctx, finish)
	}
	return result, err
}

func mergeStart(a, b StartCallback) StartCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnStartEvent) { a(ctx, e); b(ctx, e) }
}
func mergeStepStart(a, b StepStartCallback) StepStartCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnStepStartEvent) { a(ctx, e); b(ctx, e) }
}
func mergeToolStart(a, b ToolExecutionStartCallback) ToolExecutionStartCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnToolCallStartEvent) { a(ctx, e); b(ctx, e) }
}
func mergeToolEnd(a, b ToolExecutionEndCallback) ToolExecutionEndCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnToolCallFinishEvent) { a(ctx, e); b(ctx, e) }
}
func mergeStepFinish(a, b StepFinishCallback) StepFinishCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnStepFinishEvent) { a(ctx, e); b(ctx, e) }
}
func resolveStepEnd(onStepEnd StepEndCallback, onStepFinish StepFinishCallback) StepFinishCallback {
	if onStepEnd != nil {
		return func(ctx context.Context, e ai.OnStepFinishEvent) { onStepEnd(ctx, e) }
	}
	return onStepFinish
}

// resolveEnd returns onEnd if set (converted to the FinishCallback shape used
// internally), else its deprecated alias onFinish. Mirrors resolveStepEnd and
// resolveAgentOnEnd (pkg/agent/toolloop.go).
func resolveEnd(onEnd EndCallback, onFinish FinishCallback) FinishCallback {
	if onEnd != nil {
		return func(ctx context.Context, e ai.OnFinishEvent) { onEnd(ctx, e) }
	}
	return onFinish
}
func mergeFinish(a, b FinishCallback) FinishCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e ai.OnFinishEvent) { a(ctx, e); b(ctx, e) }
}

func mergeError(a, b ErrorCallback) ErrorCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, err error) { a(ctx, err); b(ctx, err) }
}

func mergeAbort(a, b AbortCallback) AbortCallback {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, steps []types.StepResult) { a(ctx, steps); b(ctx, steps) }
}

// prepareCallContext bundles per-call values that are known once, at call
// construction time in makeAgent, but that a PrepareCall/PrepareStep hook
// needs to see and may override on the returned LanguageModelCallOptions
// (TS prepareCall setting parity: d56638a stopWhen/activeTools/download,
// 419adc7 maxRetries/abortSignal, b666f57 initial instructions/messages).
type prepareCallContext struct {
	activeTools         []string
	stopWhen            []ai.StopCondition
	download            ai.DownloadFunction
	maxRetries          *int
	timeout             *ai.TimeoutConfig
	initialInstructions string
	initialMessages     []types.Message
	// prepareStep is the per-call PrepareStep hook (runs after w.PrepareStep).
	prepareStep PrepareStepHook
}

// hookFailedModel stands in for the real model on a step whose PrepareCall or
// PrepareStep hook returned an error: the model call fails with that error.
type hookFailedModel struct {
	provider.LanguageModel
	err error
}

func (m *hookFailedModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, m.err
}

func (m *hookFailedModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, m.err
}

// prepareCallState carries what PrepareCall decided at the start of a run so
// later steps keep it (TS applies prepareCall once, before the loop; its
// results are the effective settings of every step).
type prepareCallState struct {
	mu       sync.Mutex
	started  bool
	prepared *LanguageModelCallOptions
	// baseMessages is the number of messages the first step started with;
	// messages after that prefix are the loop's own history.
	baseMessages int
}

func (st *prepareCallState) overlay(opts LanguageModelCallOptions) LanguageModelCallOptions {
	p := st.prepared
	opts.System, opts.AllowSystemInMessages, opts.Tools, opts.ToolChoice, opts.CallOptions = p.System, p.AllowSystemInMessages, p.Tools, p.ToolChoice, p.CallOptions
	opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK = p.Temperature, p.MaxTokens, p.TopP, p.TopK
	opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed = p.FrequencyPenalty, p.PresencePenalty, p.StopSequences, p.Seed
	opts.Headers, opts.Reasoning, opts.SendReasoning, opts.ProviderOptions = p.Headers, p.Reasoning, p.SendReasoning, p.ProviderOptions
	opts.RuntimeContext, opts.ToolsContext, opts.ExperimentalSandbox = p.RuntimeContext, p.ToolsContext, p.ExperimentalSandbox
	opts.StopWhen, opts.ActiveTools, opts.ExperimentalDownload, opts.MaxRetries, opts.Timeout = p.StopWhen, p.ActiveTools, p.ExperimentalDownload, p.MaxRetries, p.Timeout
	if len(opts.Messages) >= st.baseMessages {
		history := opts.Messages[st.baseMessages:]
		opts.Messages = append(append([]types.Message(nil), p.Messages...), history...)
	}
	return opts
}

// stepHooks returns the PrepareStep hooks to run, in order. TS uses the
// per-call prepareStep when given and the constructor one otherwise
// (`options.prepareStep ?? this.prepareStep`).
func (w *WorkflowAgent) stepHook(pctx prepareCallContext) PrepareStepHook {
	if pctx.prepareStep != nil {
		return pctx.prepareStep
	}
	return w.PrepareStep
}

// makePrepareCall builds the agent-level PrepareCall closure. PrepareCall runs
// once, on the first step, before any PrepareStep hook; its results persist for
// the whole run. When stepsInline is set (the legacy generate loop calls this
// closure on every step) the PrepareStep hook also runs here; the streaming
// path runs it per step through makePrepareStep instead.
func (w *WorkflowAgent) makePrepareCall(pctx prepareCallContext, stepsInline bool) func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
	stepHook := w.stepHook(pctx)
	if w.PrepareCall == nil && (stepHook == nil || !stepsInline) && w.FilterActiveTools == nil && pctx.activeTools == nil && w.ActiveTools == nil {
		return nil
	}
	st := &prepareCallState{}
	return func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
		opts := LanguageModelCallOptions{
			StepNumber: c.StepNumber, System: c.System, AllowSystemInMessages: c.AllowSystemInMessages, Messages: c.Messages, Tools: c.Tools, ToolChoice: c.ToolChoice, CallOptions: c.CallOptions,
			Temperature: c.Temperature, MaxTokens: c.MaxTokens, TopP: c.TopP, TopK: c.TopK, FrequencyPenalty: c.FrequencyPenalty, PresencePenalty: c.PresencePenalty, StopSequences: c.StopSequences, Seed: c.Seed,
			Headers: c.Headers, Reasoning: c.Reasoning, SendReasoning: c.SendReasoning, ProviderOptions: c.ProviderOptions,
			RuntimeContext: c.RuntimeContext, ToolsContext: c.ToolsContext, ExperimentalSandbox: c.ExperimentalSandbox,
			PreviousSteps: c.PreviousSteps, AccumulatedUsage: c.AccumulatedUsage, CustomData: c.CustomData,
			StopWhen: pctx.stopWhen, ActiveTools: pctx.activeTools, ExperimentalDownload: pctx.download,
			MaxRetries: pctx.maxRetries, Timeout: pctx.timeout,
			InitialInstructions: pctx.initialInstructions, InitialMessages: pctx.initialMessages,
		}
		// A hook error fails the run (TS prepareStep/prepareCall rejections
		// propagate): PrepareCallConfig cannot carry an error, so the step's
		// model is swapped for one that returns it.
		failStep := func(err error) agent.PrepareCallConfig {
			base := c.Model
			if base == nil {
				base = w.Model
			}
			c.Model = &hookFailedModel{LanguageModel: base, err: err}
			return c
		}
		st.mu.Lock()
		first := !st.started
		st.started = true
		if first {
			if w.PrepareCall != nil {
				// TS hands prepareCall the resolved messages: a plain prompt
				// is its single user message.
				if len(opts.Messages) == 0 && c.Prompt != "" {
					opts.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: c.Prompt}}}}
				}
				st.baseMessages = len(opts.Messages)
				mutated, err := w.PrepareCall(ctx, opts)
				if err != nil {
					st.mu.Unlock()
					return failStep(err)
				}
				opts = mutated
				kept := mutated
				st.prepared = &kept
			}
		} else if st.prepared != nil {
			opts = st.overlay(opts)
		}
		st.mu.Unlock()
		if stepsInline && stepHook != nil {
			mutated, err := stepHook(ctx, opts)
			if err != nil {
				return failStep(err)
			}
			opts = mutated
		}
		c.System, c.AllowSystemInMessages, c.Messages, c.Tools, c.ToolChoice, c.CallOptions = opts.System, opts.AllowSystemInMessages, opts.Messages, opts.Tools, opts.ToolChoice, opts.CallOptions
		c.Temperature, c.MaxTokens, c.TopP, c.TopK = opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK
		c.FrequencyPenalty, c.PresencePenalty, c.StopSequences, c.Seed = opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed
		c.Headers, c.Reasoning, c.SendReasoning, c.ProviderOptions = opts.Headers, opts.Reasoning, opts.SendReasoning, opts.ProviderOptions
		c.RuntimeContext, c.ToolsContext, c.ExperimentalSandbox, c.CustomData = opts.RuntimeContext, opts.ToolsContext, opts.ExperimentalSandbox, opts.CustomData
		c.StopWhen, c.ExperimentalDownload, c.MaxRetries, c.Timeout = withFinishReasonStop(opts.StopWhen), opts.ExperimentalDownload, opts.MaxRetries, opts.Timeout
		if w.FilterActiveTools != nil {
			c.Tools = w.FilterActiveTools(ctx, c.StepNumber, c.Tools)
		}
		effectiveActive := opts.ActiveTools
		if effectiveActive == nil {
			effectiveActive = pctx.activeTools
		}
		if effectiveActive == nil {
			effectiveActive = w.ActiveTools
		}
		c.ActiveTools = effectiveActive
		if effectiveActive != nil {
			c.Tools = ai.FilterActiveTools(c.Tools, effectiveActive)
		}
		return c
	}
}

// makePrepareStep adapts the WorkflowAgent PrepareStep hook to the streaming
// loop's per-step hook so it runs before every step, not only the first.
func (w *WorkflowAgent) makePrepareStep(pctx prepareCallContext, allTools []types.Tool, allowSystem bool) func(context.Context, ai.PrepareStepOptions) ai.PrepareStepOptions {
	hook := w.stepHook(pctx)
	if hook == nil {
		return nil
	}
	return func(ctx context.Context, o ai.PrepareStepOptions) ai.PrepareStepOptions {
		activeTools := o.ActiveTools
		if activeTools == nil {
			activeTools = pctx.activeTools
		}
		opts := LanguageModelCallOptions{
			StepNumber: o.StepNumber, System: o.System, AllowSystemInMessages: allowSystem, Messages: o.Messages, Tools: o.Tools, ToolChoice: o.ToolChoice,
			Temperature: o.Temperature, MaxTokens: o.MaxOutputTokens, TopP: o.TopP, TopK: o.TopK, FrequencyPenalty: o.FrequencyPenalty, PresencePenalty: o.PresencePenalty,
			StopSequences: o.StopSequences, Seed: o.Seed, Reasoning: o.Reasoning, ProviderOptions: o.ProviderOptions,
			RuntimeContext: o.RuntimeContext, ToolsContext: o.ToolsContext, ExperimentalSandbox: o.ExperimentalSandbox,
			PreviousSteps: o.Steps, AccumulatedUsage: o.AccumulatedUsage,
			StopWhen: pctx.stopWhen, ActiveTools: activeTools, ExperimentalDownload: pctx.download,
			MaxRetries: pctx.maxRetries, Timeout: pctx.timeout,
			InitialInstructions: pctx.initialInstructions, InitialMessages: pctx.initialMessages,
		}
		mutated, err := hook(ctx, opts)
		if err != nil {
			base := o.Model
			if base == nil {
				base = w.Model
			}
			o.Model = &hookFailedModel{LanguageModel: base, err: err}
			return o
		}
		o.System = mutated.System
		sys := mutated.System
		o.Instructions = &sys
		o.Messages = mutated.Messages
		o.Tools = mutated.Tools
		o.ToolChoice = mutated.ToolChoice
		o.ProviderOptions = mutated.ProviderOptions
		o.RuntimeContext, o.ToolsContext, o.ExperimentalSandbox = mutated.RuntimeContext, mutated.ToolsContext, mutated.ExperimentalSandbox
		o.MaxOutputTokens, o.Temperature, o.TopP, o.TopK = mutated.MaxTokens, mutated.Temperature, mutated.TopP, mutated.TopK
		o.FrequencyPenalty, o.PresencePenalty, o.StopSequences, o.Seed, o.Reasoning = mutated.FrequencyPenalty, mutated.PresencePenalty, mutated.StopSequences, mutated.Seed, mutated.Reasoning
		if mutated.ActiveTools != nil && !sameStrings(mutated.ActiveTools, activeTools) {
			o.Tools = ai.FilterActiveTools(allTools, mutated.ActiveTools)
		}
		return o
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (w *WorkflowAgent) makeAgent(ovr WorkflowStreamOptions, govr WorkflowGenerateOptions, streaming bool) *agent.ToolLoopAgent {
	system := w.System
	if w.Instructions != nil {
		if normalized, err := normalizeInstructions(w.Instructions); err == nil && normalized != "" {
			system = normalized
		}
	}
	if govr.System != "" {
		system = govr.System
	}
	if govr.Instructions != nil {
		if normalized, err := normalizeInstructions(govr.Instructions); err == nil && normalized != "" {
			system = normalized
		}
	}
	if ovr.System != "" {
		system = ovr.System
	}
	if ovr.Instructions != nil {
		if normalized, err := normalizeInstructions(ovr.Instructions); err == nil && normalized != "" {
			system = normalized
		}
	}
	stopWhen := w.StopWhen
	if streaming && w.stopWhenDefaulted {
		// TS only defaults generate() to isStepCount(20); stream() runs until
		// the model stops calling tools.
		stopWhen = []ai.StopCondition{func(ai.StopConditionState) string { return "" }}
	}
	if len(govr.StopWhen) > 0 {
		stopWhen = govr.StopWhen
	}
	if len(ovr.StopWhen) > 0 {
		stopWhen = ovr.StopWhen
	}
	runtimeContext := w.RuntimeContext
	if govr.RuntimeContext != nil {
		runtimeContext = govr.RuntimeContext
	}
	if ovr.RuntimeContext != nil {
		runtimeContext = ovr.RuntimeContext
	}
	toolsContext := w.ToolsContext
	if govr.ToolsContext != nil {
		toolsContext = govr.ToolsContext
	}
	if ovr.ToolsContext != nil {
		toolsContext = ovr.ToolsContext
	}
	include := w.Include
	if govr.Include != nil {
		include = govr.Include
	}
	if ovr.Include != nil {
		include = ovr.Include
	}
	sandbox := w.ExperimentalSandbox
	if govr.ExperimentalSandbox != nil {
		sandbox = govr.ExperimentalSandbox
	}
	if ovr.ExperimentalSandbox != nil {
		sandbox = ovr.ExperimentalSandbox
	}
	refineToolInput := w.ExperimentalRefineToolInput
	if govr.ExperimentalRefineToolInput != nil {
		refineToolInput = govr.ExperimentalRefineToolInput
	}
	if ovr.ExperimentalRefineToolInput != nil {
		refineToolInput = ovr.ExperimentalRefineToolInput
	}
	repairToolCall := w.RepairToolCall
	if repairToolCall == nil {
		repairToolCall = w.ExperimentalRepairToolCall
	}
	if govr.RepairToolCall != nil {
		repairToolCall = govr.RepairToolCall
	} else if govr.ExperimentalRepairToolCall != nil {
		repairToolCall = govr.ExperimentalRepairToolCall
	}
	if ovr.RepairToolCall != nil {
		repairToolCall = ovr.RepairToolCall
	} else if ovr.ExperimentalRepairToolCall != nil {
		repairToolCall = ovr.ExperimentalRepairToolCall
	}
	approvalSecret := w.effectiveToolApprovalSecret(ovr, govr)
	telemetry := w.Telemetry
	if govr.Telemetry != nil {
		telemetry = govr.Telemetry
	}
	if ovr.Telemetry != nil {
		telemetry = ovr.Telemetry
	}
	// Per-call ToolCallConcurrency wins over the agent default (TS
	// options.toolCallConcurrency ?? this.toolCallConcurrency); it bounds the
	// step loop's tool execution as well as approved-call resume. Already
	// validated by resolveToolCallConcurrency before the agent is built.
	toolCallConcurrency := w.ToolCallConcurrency
	if govr.ToolCallConcurrency != nil {
		toolCallConcurrency = govr.ToolCallConcurrency
	}
	if ovr.ToolCallConcurrency != nil {
		toolCallConcurrency = ovr.ToolCallConcurrency
	}
	if toolCallConcurrency == nil {
		// TS WorkflowAgent runs a step's tool calls concurrently when no
		// limit is set. The AgentResult generate loop only does so when a
		// limit is given, so pass an effectively unbounded one.
		unbounded := math.MaxInt32
		toolCallConcurrency = &unbounded
	}
	tools := orderedTools(w.Tools, w.ToolSet)
	if govr.Tools != nil || govr.ToolSet != nil {
		tools = orderedTools(govr.Tools, govr.ToolSet)
	}
	if ovr.Tools != nil || ovr.ToolSet != nil {
		tools = orderedTools(ovr.Tools, ovr.ToolSet)
	}
	allowSystemInMessages := w.AllowSystemInMessages || govr.AllowSystemInMessages || ovr.AllowSystemInMessages
	maxRetries := w.MaxRetries
	if govr.MaxRetries != nil {
		maxRetries = govr.MaxRetries
	}
	if ovr.MaxRetries != nil {
		maxRetries = ovr.MaxRetries
	}
	timeout := w.Timeout
	if govr.Timeout != nil {
		timeout = govr.Timeout
	}
	if ovr.Timeout != nil {
		timeout = ovr.Timeout
	}
	download := w.ExperimentalDownload
	if govr.ExperimentalDownload != nil {
		download = govr.ExperimentalDownload
	}
	if ovr.ExperimentalDownload != nil {
		download = ovr.ExperimentalDownload
	}
	initialMessages := govr.Messages
	if len(ovr.Messages) > 0 {
		initialMessages = ovr.Messages
	}
	activeTools := govr.ActiveTools
	if ovr.ActiveTools != nil {
		activeTools = ovr.ActiveTools
	}
	prepareStep := govr.PrepareStep
	if ovr.PrepareStep != nil {
		prepareStep = ovr.PrepareStep
	}
	toolChoice := w.ToolChoice
	if govr.ToolChoice.Type != "" {
		toolChoice = govr.ToolChoice
	}
	if ovr.ToolChoice.Type != "" {
		toolChoice = ovr.ToolChoice
	}
	output := w.Output
	if govr.Output != nil {
		output = govr.Output
	}
	if ovr.Output != nil {
		output = ovr.Output
	}
	inputMessages := initialMessages
	if len(inputMessages) == 0 {
		prompt := govr.Prompt
		if ovr.Prompt != "" {
			prompt = ovr.Prompt
		}
		if prompt == "" {
			prompt = w.Prompt
		}
		if prompt != "" {
			inputMessages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt}}}}
		}
	}
	onEnd := mergeFinish(resolveEnd(w.OnEnd, w.OnFinish), mergeFinish(resolveEnd(govr.OnEnd, govr.OnFinish), resolveEnd(ovr.OnEnd, ovr.OnFinish)))
	if onEnd != nil {
		inner := onEnd
		// TS WorkflowAgent's onEnd event carries `messages`: the final
		// conversation (input messages plus every step's response messages).
		onEnd = func(ctx context.Context, e ai.OnFinishEvent) {
			messages := append([]types.Message(nil), inputMessages...)
			for _, step := range e.Steps {
				messages = append(messages, step.Response.Messages...)
			}
			e.Messages = messages
			inner(ctx, e)
		}
	}
	pctx := prepareCallContext{
		prepareStep:         prepareStep,
		activeTools:         activeTools,
		stopWhen:            stopWhen,
		download:            download,
		maxRetries:          maxRetries,
		timeout:             timeout,
		initialInstructions: system,
		initialMessages:     initialMessages,
	}
	var stepAdapter func(context.Context, ai.PrepareStepOptions) ai.PrepareStepOptions
	if streaming {
		stepAdapter = w.makePrepareStep(pctx, tools, allowSystemInMessages)
	}
	return agent.NewToolLoopAgent(agent.AgentConfig{
		ID: w.ID, Model: w.Model, System: system, Prompt: w.Prompt, Tools: tools, StopWhen: withFinishReasonStop(stopWhen),
		AllowSystemInMessages: allowSystemInMessages,
		CallOptionsSchema:     w.CallOptionsSchema, CallOptions: w.CallOptions, PrepareCall: w.makePrepareCall(pctx, !streaming), PrepareStep: stepAdapter,
		Temperature: callSetting(ovr.Temperature, govr.Temperature, w.Temperature), MaxTokens: callSetting(ovr.MaxTokens, govr.MaxTokens, w.MaxTokens),
		TopP: callSetting(ovr.TopP, govr.TopP, w.TopP), TopK: callSetting(ovr.TopK, govr.TopK, w.TopK),
		FrequencyPenalty: callSetting(ovr.FrequencyPenalty, govr.FrequencyPenalty, w.FrequencyPenalty),
		PresencePenalty:  callSetting(ovr.PresencePenalty, govr.PresencePenalty, w.PresencePenalty),
		StopSequences:    callSlice(ovr.StopSequences, govr.StopSequences, w.StopSequences), Seed: callSetting(ovr.Seed, govr.Seed, w.Seed),
		Headers:       version.WithUserAgentSuffix(callMap(ovr.Headers, govr.Headers, w.Headers), "ai-sdk-agent/workflow"),
		Reasoning:     callSetting(ovr.Reasoning, govr.Reasoning, w.Reasoning),
		SendReasoning: w.SendReasoning, ProviderOptions: callMap(ovr.ProviderOptions, govr.ProviderOptions, w.ProviderOptions), RuntimeContext: runtimeContext, ToolsContext: toolsContext,
		ToolChoice:                     toolChoice,
		ToolCallConcurrency:            toolCallConcurrency,
		Output:                         output,
		Telemetry:                      telemetry,
		Include:                        include,
		ExperimentalSandbox:            sandbox,
		ExperimentalRefineToolInput:    refineToolInput,
		ExperimentalDownload:           download,
		RepairToolCall:                 repairToolCall,
		ExperimentalToolApprovalSecret: approvalSecret,
		MaxRetries:                     maxRetries,
		Timeout:                        timeout,
		OnStart:                        mergeStart(w.OnStart, mergeStart(govr.OnStart, ovr.OnStart)),
		OnStepStartEvent:               mergeStepStart(w.OnStepStart, mergeStepStart(govr.OnStepStart, ovr.OnStepStart)),
		OnToolExecutionStart:           mergeToolStart(w.OnToolExecutionStart, mergeToolStart(govr.OnToolExecutionStart, ovr.OnToolExecutionStart)),
		OnToolExecutionEnd:             mergeToolEnd(w.OnToolExecutionEnd, mergeToolEnd(govr.OnToolExecutionEnd, ovr.OnToolExecutionEnd)),
		OnStepEndEvent:                 mergeStepFinish(resolveStepEnd(w.OnStepEnd, w.OnStepFinish), mergeStepFinish(resolveStepEnd(govr.OnStepEnd, govr.OnStepFinish), resolveStepEnd(ovr.OnStepEnd, ovr.OnStepFinish))),
		OnEndEvent:                     onEnd,
	})
}

// withFinishReasonStop adds the WorkflowAgent rule that a step which ended
// with any finish reason other than stop or tool-calls (length, content-filter,
// error, other) is the last one: TS modelCallIterator sets
// `done = !isToolExecutionAllowed` after answering that step's tool calls
// (validation errors and provider results only; nothing is executed). Without
// it the loop asked the model again with the error results and, in stream()
// where no step limit applies, never ended. Empty conditions stay empty so
// the caller's defaulting is unchanged.
func withFinishReasonStop(conditions []ai.StopCondition) []ai.StopCondition {
	if len(conditions) == 0 {
		return conditions
	}
	out := make([]ai.StopCondition, 0, len(conditions)+1)
	out = append(out, func(state ai.StopConditionState) string {
		if n := len(state.Steps); n > 0 {
			switch reason := state.Steps[n-1].FinishReason; reason {
			case "", types.FinishReasonStop, types.FinishReasonToolCalls:
			default:
				return "step finished with finish reason " + string(reason)
			}
		}
		return ""
	})
	return append(out, conditions...)
}

func (w *WorkflowAgent) effectiveToolApprovalSecret(ovr WorkflowStreamOptions, govr WorkflowGenerateOptions) []byte {
	if ovr.ExperimentalToolApprovalSecret != nil {
		return ovr.ExperimentalToolApprovalSecret
	}
	if govr.ExperimentalToolApprovalSecret != nil {
		return govr.ExperimentalToolApprovalSecret
	}
	return w.ExperimentalToolApprovalSecret
}

func validatePromptMessages(prompt string, messages []types.Message) error {
	if prompt != "" && len(messages) > 0 {
		return fmt.Errorf("workflow: use either prompt or messages, not both")
	}
	if prompt == "" && len(messages) == 0 {
		return fmt.Errorf("workflow: either prompt or messages is required")
	}
	return nil
}

// Generate runs the workflow agent and returns final result.
func (w *WorkflowAgent) Generate(ctx context.Context, prompt string, opts *agent.AgentGenerateOptions) (*WorkflowResult, error) {
	legacy := WorkflowGenerateOptions{Prompt: prompt}
	if opts != nil {
		legacy.Prompt = opts.Prompt
		legacy.Messages = opts.Messages
		legacy.System = opts.System
		legacy.AllowSystemInMessages = opts.AllowSystemInMessages
		legacy.StopWhen = opts.StopWhen
		legacy.OnStart = opts.OnStart
		legacy.OnStepStart = opts.OnStepStart
		legacy.OnToolExecutionStart = opts.OnToolExecutionStart
		if legacy.OnToolExecutionStart == nil {
			// opts.OnToolCallStart is a deprecated alias of OnToolExecutionStart, read here as a fallback for callers still setting it.
			legacy.OnToolExecutionStart = opts.OnToolCallStart //nolint:staticcheck
		}
		legacy.OnToolExecutionEnd = opts.OnToolExecutionEnd
		if legacy.OnToolExecutionEnd == nil {
			// opts.OnToolCallFinish is a deprecated alias of OnToolExecutionEnd, read here as a fallback for callers still setting it.
			legacy.OnToolExecutionEnd = opts.OnToolCallFinish //nolint:staticcheck
		}
		legacy.OnStepEnd = opts.OnStepEnd
		legacy.OnStepFinish = opts.OnStepFinish //nolint:staticcheck // forwarding deprecated opts field to legacy's own deprecated field, for callers still using it
		legacy.OnEnd = opts.OnEnd
		legacy.OnFinish = opts.OnFinish //nolint:staticcheck // forwarding deprecated opts field to legacy's own deprecated field, for callers still using it
	}
	return w.GenerateWithOptions(ctx, legacy)
}

// GenerateWithOptions runs the workflow agent using workflow-native call options.
func (w *WorkflowAgent) GenerateWithOptions(ctx context.Context, opts WorkflowGenerateOptions) (*WorkflowResult, error) {
	if w == nil {
		return nil, fmt.Errorf("workflow: nil agent")
	}
	if err := validatePromptMessages(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	telemetrySettings := w.Telemetry
	if opts.Telemetry != nil {
		telemetrySettings = opts.Telemetry
	}
	modelProvider, modelID := workflowModelInfo(w.Model)
	// Start the workflow-level operation span before any pre-step approved
	// tool execution, mirroring TS WorkflowAgent's telemetryDispatcher.onStart
	// for 'ai.workflowAgent.generate' (stream-text-iterator.ts). Without this,
	// approval-resume tool execution has no root span to parent under and its
	// tool span is silently skipped (27d294d).
	callID := newWorkflowCallID()
	ctx = telemetrypkg.FireOnStart(ctx, telemetrypkg.TelemetryStartEvent{
		CallID:         callID,
		OperationType:  "ai.workflowAgent.generate",
		ModelProvider:  modelProvider,
		ModelID:        modelID,
		Settings:       telemetrySettings,
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, firstNonNil(opts.RuntimeContext, w.RuntimeContext)),
	})
	toolCallConcurrency, err := resolveToolCallConcurrency(opts.ToolCallConcurrency, w.ToolCallConcurrency)
	if err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, CallID: callID, Error: err})
		return nil, err
	}
	var initialResponseMessages []types.Message
	opts.Messages, _, err = processWorkflowApprovalResume(ctx, workflowApprovalResumeOptions{
		initialResponse:     &initialResponseMessages,
		messages:            opts.Messages,
		tools:               effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts),
		runtimeContext:      firstNonNil(opts.RuntimeContext, w.RuntimeContext),
		toolsContext:        firstNonNilMap(opts.ToolsContext, w.ToolsContext),
		experimentalSandbox: firstNonNil(opts.ExperimentalSandbox, w.ExperimentalSandbox),
		toolApprovalSecret:  w.effectiveToolApprovalSecret(WorkflowStreamOptions{}, opts),
		onToolStart:         mergeToolStart(w.OnToolExecutionStart, opts.OnToolExecutionStart),
		onToolEnd:           mergeToolEnd(w.OnToolExecutionEnd, opts.OnToolExecutionEnd),
		telemetrySettings:   telemetrySettings,
		toolCallConcurrency: toolCallConcurrency,
	})
	if err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, CallID: callID, Error: err})
		return nil, err
	}
	// TS convertToLanguageModelPrompt rejects a history whose tool calls (or
	// unresolved approval requests) lack tool results.
	if err := validateWorkflowToolResults(opts.Messages, effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts)); err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, CallID: callID, Error: err})
		return nil, err
	}
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if ctx != nil && ctx.Err() != nil {
		telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID})
		if onAbort != nil {
			onAbort(ctx, nil)
		}
		return nil, ctx.Err()
	}
	// Collect completed steps so OnAbort can report them (the agent loop
	// returns no partial result on error).
	var stepsMu sync.Mutex
	var completedSteps []types.StepResult
	prevStepEnd := resolveStepEnd(opts.OnStepEnd, opts.OnStepFinish)
	opts.OnStepFinish = nil
	opts.OnStepEnd = func(c context.Context, e ai.OnStepFinishEvent) {
		stepsMu.Lock()
		completedSteps = append(completedSteps, stepResultFromFinishEvent(e))
		stepsMu.Unlock()
		if prevStepEnd != nil {
			prevStepEnd(c, e)
		}
	}
	a := w.makeAgent(WorkflowStreamOptions{}, opts, false)
	system := opts.System
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
		}
	}
	call := agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: withFinishReasonStop(opts.StopWhen), Output: firstNonNil(opts.Output, w.Output), Telemetry: telemetrySettings, Internal: workflowInternalOptions}
	result, err := a.GenerateAgent(agent.WithTelemetryCallID(ctx, callID), call)
	if err != nil {
		if isWorkflowAbort(ctx, err) {
			stepsMu.Lock()
			steps := append([]types.StepResult(nil), completedSteps...)
			stepsMu.Unlock()
			telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID, Steps: steps})
			if onAbort != nil {
				onAbort(ctx, steps)
			}
		} else {
			telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, CallID: callID, Error: err})
			// An abort reaches onAbort only, never onError (TS).
			if onError := mergeError(w.OnError, opts.OnError); onError != nil {
				onError(ctx, err)
			}
		}
		return nil, err
	}
	telemetrypkg.FireOnEnd(ctx, telemetrypkg.TelemetryFinishEvent{
		CallID:         callID,
		Settings:       telemetrySettings,
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, firstNonNil(opts.RuntimeContext, w.RuntimeContext)),
		FinishReason:   string(result.FinishReason),
		ModelProvider:  modelProvider,
		ModelID:        modelID,
		Text:           result.Text,
		Usage: telemetrypkg.TelemetryUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
	})
	responseMessages := append([]types.Message(nil), initialResponseMessages...)
	for _, step := range result.Steps {
		responseMessages = append(responseMessages, step.Response.Messages...)
	}
	return &WorkflowResult{AgentResult: result, ResponseMessages: responseMessages}, nil
}

// stepResultFromFinishEvent converts a step-end event to the StepResult handed
// to OnAbort.
func stepResultFromFinishEvent(e ai.OnStepFinishEvent) types.StepResult {
	return types.StepResult{
		CallID:             e.CallID,
		StepNumber:         e.StepNumber,
		Model:              e.Model,
		Text:               e.Text,
		Reasoning:          e.Reasoning,
		ReasoningText:      e.ReasoningText,
		Files:              e.Files,
		ToolCalls:          e.ToolCalls,
		StaticToolCalls:    e.StaticToolCalls,
		DynamicToolCalls:   e.DynamicToolCalls,
		ToolResults:        e.ToolResults,
		StaticToolResults:  e.StaticToolResults,
		DynamicToolResults: e.DynamicToolResults,
		FinishReason:       e.FinishReason,
		RawFinishReason:    e.RawFinishReason,
		Usage:              e.Usage,
		Warnings:           e.Warnings,
	}
}

// workflowModelInfo returns the model's provider and model id, or two empty
// strings when model is nil (validated separately by callers).
func workflowModelInfo(model provider.LanguageModel) (string, string) {
	if model == nil {
		return "", ""
	}
	return model.Provider(), model.ModelID()
}

// Stream runs the workflow agent in streaming mode.
func (w *WorkflowAgent) Stream(ctx context.Context, prompt string, opts *agent.AgentStreamOptions) (*WorkflowStreamResult, error) {
	legacy := WorkflowStreamOptions{Prompt: prompt}
	if opts != nil {
		legacy.Prompt = opts.Prompt
		legacy.Messages = opts.Messages
		legacy.System = opts.System
		legacy.AllowSystemInMessages = opts.AllowSystemInMessages
		legacy.StopWhen = opts.StopWhen
		legacy.ExperimentalTransform = opts.ExperimentalTransform
		legacy.OnChunk = opts.OnChunk
		legacy.OnStart = opts.OnStart
		legacy.OnStepStart = opts.OnStepStart
		legacy.OnToolExecutionStart = opts.OnToolExecutionStart
		if legacy.OnToolExecutionStart == nil {
			// opts.OnToolCallStart is a deprecated alias of OnToolExecutionStart, read here as a fallback for callers still setting it.
			legacy.OnToolExecutionStart = opts.OnToolCallStart //nolint:staticcheck
		}
		legacy.OnToolExecutionEnd = opts.OnToolExecutionEnd
		if legacy.OnToolExecutionEnd == nil {
			// opts.OnToolCallFinish is a deprecated alias of OnToolExecutionEnd, read here as a fallback for callers still setting it.
			legacy.OnToolExecutionEnd = opts.OnToolCallFinish //nolint:staticcheck
		}
		legacy.OnStepEnd = opts.OnStepEnd
		legacy.OnStepFinish = opts.OnStepFinish //nolint:staticcheck // forwarding deprecated opts field to legacy's own deprecated field, for callers still using it
		legacy.OnEnd = opts.OnEnd
		legacy.OnFinish = opts.OnFinish //nolint:staticcheck // forwarding deprecated opts field to legacy's own deprecated field, for callers still using it
	}
	return w.StreamWithOptions(ctx, legacy)
}

// StreamWithOptions runs the workflow agent with workflow-native stream options.
func (w *WorkflowAgent) StreamWithOptions(ctx context.Context, opts WorkflowStreamOptions) (*WorkflowStreamResult, error) {
	if w == nil {
		return nil, fmt.Errorf("workflow: nil agent")
	}
	if err := validatePromptMessages(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	telemetrySettings := w.Telemetry
	if opts.Telemetry != nil {
		telemetrySettings = opts.Telemetry
	}
	modelProvider, modelID := workflowModelInfo(w.Model)
	// Start the workflow-level operation span before any pre-step approved
	// tool execution, mirroring TS WorkflowAgent's telemetryDispatcher.onStart
	// for 'ai.workflowAgent.stream' (stream-text-iterator.ts). Without this,
	// approval-resume tool execution has no root span to parent under and its
	// tool span is silently skipped (27d294d). The span is ended as soon as
	// the underlying stream is obtained: the actual streaming happens
	// asynchronously as the caller drains WorkflowStreamResult, under the
	// child span the delegated ai.StreamText call creates for itself.
	ctx = telemetrypkg.FireOnStart(ctx, telemetrypkg.TelemetryStartEvent{
		OperationType:  "ai.workflowAgent.stream",
		ModelProvider:  modelProvider,
		ModelID:        modelID,
		Settings:       telemetrySettings,
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, firstNonNil(opts.RuntimeContext, w.RuntimeContext)),
	})
	toolCallConcurrency, err := resolveToolCallConcurrency(opts.ToolCallConcurrency, w.ToolCallConcurrency)
	if err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		return nil, err
	}
	var prefixChunks []provider.StreamChunk
	opts.Messages, prefixChunks, err = processWorkflowApprovalResume(ctx, workflowApprovalResumeOptions{
		messages:            opts.Messages,
		tools:               effectiveWorkflowTools(w, opts, WorkflowGenerateOptions{}),
		runtimeContext:      firstNonNil(opts.RuntimeContext, w.RuntimeContext),
		toolsContext:        firstNonNilMap(opts.ToolsContext, w.ToolsContext),
		experimentalSandbox: firstNonNil(opts.ExperimentalSandbox, w.ExperimentalSandbox),
		toolApprovalSecret:  w.effectiveToolApprovalSecret(opts, WorkflowGenerateOptions{}),
		onToolStart:         mergeToolStart(w.OnToolExecutionStart, opts.OnToolExecutionStart),
		onToolEnd:           mergeToolEnd(w.OnToolExecutionEnd, opts.OnToolExecutionEnd),
		telemetrySettings:   telemetrySettings,
		toolCallConcurrency: toolCallConcurrency,
	})
	if err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		return nil, err
	}
	// TS stream() awaits convertToLanguageModelPrompt before returning, so a
	// history with unresolved tool calls rejects stream() itself.
	if err := validateWorkflowToolResults(opts.Messages, effectiveWorkflowTools(w, opts, WorkflowGenerateOptions{})); err != nil {
		telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		return nil, err
	}
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if ctx != nil && ctx.Err() != nil {
		telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings})
		if onAbort != nil {
			onAbort(ctx, nil)
		}
		return nil, ctx.Err()
	}
	a := w.makeAgent(opts, WorkflowGenerateOptions{}, true)
	system := opts.System
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
		}
	}
	call := agent.AgentStreamOptions{
		AgentGenerateOptions:  agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: withFinishReasonStop(opts.StopWhen), Output: firstNonNil(opts.Output, w.Output), Telemetry: telemetrySettings, Internal: workflowInternalOptions},
		OnChunk:               opts.OnChunk,
		InitialStreamChunks:   prefixChunks,
		ExperimentalTransform: opts.ExperimentalTransform,
		// Mid-stream error chunks and aborts reach OnError/OnAbort too (TS
		// fires them from the stream pipeline, not only before it starts).
		OnError: mergeError(w.OnError, opts.OnError),
		OnAbort: onAbort,
	}
	stream, err := a.Stream(ctx, call)
	if err != nil {
		if isWorkflowAbort(ctx, err) {
			telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings})
			if onAbort != nil {
				onAbort(ctx, nil)
			}
		} else {
			telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
			if onError := mergeError(w.OnError, opts.OnError); onError != nil {
				onError(ctx, err)
			}
		}
		return nil, err
	}
	telemetrypkg.FireOnEnd(ctx, telemetrypkg.TelemetryFinishEvent{
		Settings:       telemetrySettings,
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, firstNonNil(opts.RuntimeContext, w.RuntimeContext)),
		ModelProvider:  modelProvider,
		ModelID:        modelID,
	})
	return &WorkflowStreamResult{StreamTextResult: stream}, nil
}

// isWorkflowAbort reports whether a failed run counts as an abort rather than
// an error: the caller's context ended, or the failure is a cancellation or a
// timeout such as the agent's own Timeout (TS classifies AbortError and
// TimeoutError as aborts, so onAbort fires and onError does not).
// workflowInternalOptions makes the loop continue after a provider-executed
// tool call with no result (TS WorkflowAgent warns and sends an empty result).
var workflowInternalOptions = &ai.InternalOptions{ContinueOnMissingProviderResult: true, StopOnStreamError: true}

func isWorkflowAbort(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// newWorkflowCallID returns the call ID shared by one WorkflowAgent run's
// telemetry events (TelemetryStartEvent.CallID).
func newWorkflowCallID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validateWorkflowToolResults(messages []types.Message, tools []types.Tool) error {
	var providerTools []string
	for _, tool := range tools {
		if tool.ProviderExecuted {
			providerTools = append(providerTools, tool.Name)
		}
	}
	return promptutils.ValidateToolResults(messages, providerTools...)
}
