package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
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
	StepNumber int
	// Model is the language model for the call (TS prepareCall/prepareStep
	// `model`). A hook may set it to run the call (PrepareCall) or the step
	// (PrepareStep) on another model; nil keeps the current one.
	Model                 provider.LanguageModel
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

	// InstructionMessages are the instructions when they were given as system
	// messages (TS `instructions: SystemModelMessage | SystemModelMessage[]`),
	// keeping each message's ProviderOptions. A hook may return a different
	// list to replace them. When the hook changes System and leaves
	// InstructionMessages alone, System wins, as TS `instructions` does.
	InstructionMessages []types.Message
	// InitialInstructionMessages are the original instructions given as
	// system messages; read-only like InitialInstructions.
	InitialInstructionMessages []types.Message

	// ToolCallConcurrency and Telemetry mirror TS prepareCall's
	// `toolCallConcurrency` and `telemetry`. The hook sees the call's current
	// value (nil when none is set) and may return another; nil leaves the
	// call's value. As in TS, a per-call ToolCallConcurrency wins over a
	// PrepareCall override. PrepareStep does not apply either.
	ToolCallConcurrency *int
	Telemetry           *ai.TelemetrySettings
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

// workflowTelemetryToolsContext is the tools context a telemetry event
// carries: one entry per tool, holding only the keys IncludeToolsContext
// selects for it (TS filterToolsContext).
func workflowTelemetryToolsContext(settings *ai.TelemetrySettings, toolsContext map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if settings == nil || len(settings.IncludeToolsContext) == 0 {
		return out
	}
	for name, v := range toolsContext {
		out[name] = workflowTelemetryRuntimeContext(&ai.TelemetrySettings{IncludeRuntimeContext: settings.IncludeToolsContext[name]}, v)
	}
	return out
}

// workflowRootOperation is the root's operationId. TS WorkflowAgent.generate
// and stream share one execute path whose onStart always reports
// 'ai.workflowAgent.stream'.
const workflowRootOperation = "ai.workflowAgent.stream"

// workflowStartEvent is the WorkflowAgent root start event: the call's
// messages, request settings, retry count (default 2), headers (tagged
// ai-sdk-agent/workflow) and contexts after prepareCall, as TS
// telemetryDispatcher.onStart for the workflow operation.
func workflowStartEvent(operation, callID, modelProvider, modelID string, settings *ai.TelemetrySettings, early *earlyPrepare, prepared *LanguageModelCallOptions, messages []types.Message, prompt string, runtimeContext interface{}, toolsContext map[string]interface{}) telemetrypkg.TelemetryStartEvent {
	e := telemetrypkg.TelemetryStartEvent{
		CallID:         callID,
		OperationType:  operation,
		ModelProvider:  modelProvider,
		ModelID:        modelID,
		Settings:       settings,
		RuntimeContext: workflowTelemetryRuntimeContext(settings, runtimeContext),
		ToolsContext:   workflowTelemetryToolsContext(settings, toolsContext),
	}
	if prompt == "" && early != nil {
		prompt = early.cfg.Prompt
	}
	if len(messages) > 0 {
		e.Messages = messages
	} else if prompt != "" {
		e.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt}}}}
	}
	var maxRetries *int
	if prepared != nil {
		e.MaxOutputTokens, e.Temperature, e.TopP, e.TopK = prepared.MaxTokens, prepared.Temperature, prepared.TopP, prepared.TopK
		e.PresencePenalty, e.FrequencyPenalty, e.StopSequences, e.Seed = prepared.PresencePenalty, prepared.FrequencyPenalty, prepared.StopSequences, prepared.Seed
		e.Headers = version.WithUserAgentSuffix(prepared.Headers, "ai-sdk-agent/workflow")
		maxRetries = prepared.MaxRetries
	} else if early != nil {
		c := early.cfg
		e.MaxOutputTokens, e.Temperature, e.TopP, e.TopK = c.MaxTokens, c.Temperature, c.TopP, c.TopK
		e.PresencePenalty, e.FrequencyPenalty, e.StopSequences, e.Seed = c.PresencePenalty, c.FrequencyPenalty, c.StopSequences, c.Seed
		e.Headers = c.Headers
		maxRetries = c.MaxRetries
	}
	retries := 2
	if maxRetries != nil {
		retries = *maxRetries
	}
	e.MaxRetries = &retries
	return e
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

// workflowToolDeclaresApproval is the TS `!tool.needsApproval` guard on resume:
// only whether the tool declares an approval setting matters, a function
// setting is truthy without being called (the policy itself is re-evaluated by
// ValidateApprovedToolApprovals).
func workflowToolDeclaresApproval(tool *types.Tool) bool {
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
	case types.ToolNeedsApprovalFunc, types.NeedsApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		return true
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
	// telemetrySettings, when set, dispatches each approved tool's execution
	// start and end to the telemetry integrations.
	telemetrySettings *telemetrypkg.Options
	// telemetryCallID is the call ID those tool events carry. The resume runs
	// before the workflow root opens (TS order), so no integration has state
	// for it yet and, as in TS, no tool span is recorded; an ID is still set
	// so the OTel integrations do not fall back to an ambient span in ctx.
	telemetryCallID string
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
		if !workflowToolDeclaresApproval(tool) {
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
		var ctxErr *workflowToolContextError
		if errors.As(execErr, &ctxErr) {
			return approvedOutcome{err: ctxErr.err}
		}
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
// workflowToolContextError marks a per-tool context that failed its schema,
// which fails the whole resume rather than yielding an error-text result.
type workflowToolContextError struct{ err error }

func (e *workflowToolContextError) Error() string { return e.err.Error() }
func (e *workflowToolContextError) Unwrap() error { return e.err }

// resolveWorkflowToolContext is TS resolveToolContext: the tool's entry in the
// tools context, normalized and validated by its context schema when it has one.
func resolveWorkflowToolContext(tool *types.Tool, toolName string, toolsContext map[string]interface{}) (interface{}, error) {
	var entry interface{}
	if toolsContext != nil {
		entry = toolsContext[toolName]
	}
	return ai.ValidateToolContext(tool, toolName, entry)
}

func executeWorkflowApprovedTool(ctx context.Context, tool *types.Tool, call types.ToolCall, opts workflowApprovalResumeOptions) (interface{}, error) {
	// TS resolveToolContext: the per-tool context is validated and normalized
	// by the tool's context schema before any callback or execute sees it. A
	// failure there is thrown outside executeTool's own handling, so it fails
	// the run instead of becoming a tool result.
	toolContext, ctxErr := resolveWorkflowToolContext(tool, call.ToolName, opts.toolsContext)
	if ctxErr != nil {
		return nil, &workflowToolContextError{err: ctxErr}
	}
	if opts.onToolStart != nil {
		opts.onToolStart(ctx, ai.OnToolCallStartEvent{
			ToolCallID:  call.ID,
			ToolName:    call.ToolName,
			ToolCall:    call,
			ToolContext: toolContext,
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
	toolCtx := telemetrypkg.FireOnToolCallStart(ctx, telemetrypkg.TelemetryToolCallStartEvent{
		Settings:   opts.telemetrySettings,
		CallID:     opts.telemetryCallID,
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
		CallID:     opts.telemetryCallID,
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
			ToolContext:     toolContext,
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

// mergeSafe is TS mergeCallbacks: every callback runs for the event and one
// that fails (here: panics) neither stops the others nor reaches the caller,
// as Promise.allSettled swallows a rejected callback. A single callback is
// guarded the same way. It returns nil when both are nil.
func mergeSafe[E any](a, b func(context.Context, E)) func(context.Context, E) {
	if a == nil && b == nil {
		return nil
	}
	return func(ctx context.Context, e E) {
		for _, fn := range []func(context.Context, E){a, b} {
			if fn == nil {
				continue
			}
			func() {
				defer func() { _ = recover() }()
				fn(ctx, e)
			}()
		}
	}
}

func mergeStart(a, b StartCallback) StartCallback { return mergeSafe(a, b) }

func mergeStepStart(a, b StepStartCallback) StepStartCallback { return mergeSafe(a, b) }

func mergeToolStart(a, b ToolExecutionStartCallback) ToolExecutionStartCallback {
	return mergeSafe(a, b)
}

func mergeToolEnd(a, b ToolExecutionEndCallback) ToolExecutionEndCallback { return mergeSafe(a, b) }

func mergeStepFinish(a, b StepFinishCallback) StepFinishCallback { return mergeSafe(a, b) }

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
func mergeFinish(a, b FinishCallback) FinishCallback { return mergeSafe(a, b) }

func mergeError(a, b ErrorCallback) ErrorCallback {
	if a == nil && b == nil {
		return nil
	}
	return func(ctx context.Context, err error) {
		for _, fn := range []ErrorCallback{a, b} {
			if fn == nil {
				continue
			}
			func() {
				defer func() { _ = recover() }()
				fn(ctx, err)
			}()
		}
	}
}

func mergeAbort(a, b AbortCallback) AbortCallback {
	if a == nil && b == nil {
		return nil
	}
	return func(ctx context.Context, steps []types.StepResult) {
		for _, fn := range []AbortCallback{a, b} {
			if fn == nil {
				continue
			}
			func() {
				defer func() { _ = recover() }()
				fn(ctx, steps)
			}()
		}
	}
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

	// instructionMessages/initialInstructionMessages carry instructions given
	// as system messages so PrepareCall/PrepareStep keep their ProviderOptions.
	instructionMessages []types.Message
	// toolCallConcurrency is the call's own limit (nil when unbounded);
	// callConcurrencySet records that a per-call option set it, which TS lets
	// win over a PrepareCall override.
	toolCallConcurrency *int
	callConcurrencySet  bool

	// baseHeaders are the call's headers before the WorkflowAgent
	// user-agent suffix: the hook sees and returns these, and the suffix is
	// added after it (TS appends it to the merged settings).
	baseHeaders map[string]string
	// reapplyCall puts the per-call generation settings back over a
	// PrepareCall result: TS merges `{...prepared, ...options}`, so a
	// setting given on the call wins over prepareCall's.
	reapplyCall func(*LanguageModelCallOptions)
}

// resolveInstructionChange decides which instructions a hook left behind. The
// hook got (inSystem, inMessages); out is what it returned. A changed message
// list wins; otherwise a changed System replaces the messages; otherwise
// nothing changed.
func resolveInstructionChange(inSystem string, inMessages []types.Message, outSystem string, outMessages []types.Message) (system string, messages []types.Message, changed bool) {
	switch {
	case !reflect.DeepEqual(inMessages, outMessages):
		if len(outMessages) > 0 {
			return outSystem, outMessages, true
		}
		return outSystem, nil, true
	case outSystem != inSystem:
		return outSystem, nil, true
	}
	return inSystem, inMessages, false
}

// instructionMessagesFrom returns the system messages of an Instructions value
// given as a message or message list, nil for any other type.
func instructionMessagesFrom(instructions interface{}) []types.Message {
	switch v := instructions.(type) {
	case types.Message:
		return []types.Message{v}
	case []types.Message:
		return v
	}
	return nil
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
	opts.Model = p.Model
	opts.System, opts.AllowSystemInMessages, opts.Tools, opts.ToolChoice, opts.CallOptions = p.System, p.AllowSystemInMessages, p.Tools, p.ToolChoice, p.CallOptions
	opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK = p.Temperature, p.MaxTokens, p.TopP, p.TopK
	opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed = p.FrequencyPenalty, p.PresencePenalty, p.StopSequences, p.Seed
	opts.Headers, opts.Reasoning, opts.SendReasoning, opts.ProviderOptions = p.Headers, p.Reasoning, p.SendReasoning, p.ProviderOptions
	opts.RuntimeContext, opts.ToolsContext, opts.ExperimentalSandbox = p.RuntimeContext, p.ToolsContext, p.ExperimentalSandbox
	opts.StopWhen, opts.ActiveTools, opts.ExperimentalDownload, opts.MaxRetries, opts.Timeout = p.StopWhen, p.ActiveTools, p.ExperimentalDownload, p.MaxRetries, p.Timeout
	opts.InstructionMessages, opts.ToolCallConcurrency, opts.Telemetry = p.InstructionMessages, p.ToolCallConcurrency, p.Telemetry
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

// prepareCallRunner holds the pieces of the agent-level PrepareCall closure
// that a run needs outside the agent loop: an approval resume runs after
// PrepareCall (TS prepareInvocation), so the hook is run once up front, before
// the loop, and the loop's first step then reuses its result.
type prepareCallRunner struct {
	w    *WorkflowAgent
	pctx prepareCallContext
	st   *prepareCallState
}

// callOptions builds the options a PrepareCall hook receives for a step.
// Headers, ProviderOptions and ToolsContext are copies, so a hook that edits
// them in place leaves the agent's maps alone (other runs read them
// concurrently).
func (r *prepareCallRunner) callOptions(c agent.PrepareCallConfig) LanguageModelCallOptions {
	pctx := r.pctx
	return LanguageModelCallOptions{
		StepNumber: c.StepNumber, Model: c.Model, System: c.System, AllowSystemInMessages: c.AllowSystemInMessages, Messages: c.Messages, Tools: c.Tools, ToolChoice: c.ToolChoice, CallOptions: c.CallOptions,
		Temperature: c.Temperature, MaxTokens: c.MaxTokens, TopP: c.TopP, TopK: c.TopK, FrequencyPenalty: c.FrequencyPenalty, PresencePenalty: c.PresencePenalty, StopSequences: c.StopSequences, Seed: c.Seed,
		Headers: maps.Clone(pctx.baseHeaders), Reasoning: c.Reasoning, SendReasoning: c.SendReasoning, ProviderOptions: maps.Clone(c.ProviderOptions),
		RuntimeContext: c.RuntimeContext, ToolsContext: maps.Clone(c.ToolsContext), ExperimentalSandbox: c.ExperimentalSandbox,
		PreviousSteps: c.PreviousSteps, AccumulatedUsage: c.AccumulatedUsage, CustomData: c.CustomData,
		StopWhen: pctx.stopWhen, ActiveTools: pctx.activeTools, ExperimentalDownload: pctx.download,
		MaxRetries: pctx.maxRetries, Timeout: pctx.timeout,
		InitialInstructions: pctx.initialInstructions, InitialMessages: pctx.initialMessages,
		InstructionMessages: c.InstructionMessages, InitialInstructionMessages: pctx.instructionMessages,
		ToolCallConcurrency: pctx.toolCallConcurrency, Telemetry: c.Telemetry,
	}
}

// runHook runs the PrepareCall hook for the first step and records its result
// for later steps. The caller holds st.mu.
func (r *prepareCallRunner) runHook(ctx context.Context, c agent.PrepareCallConfig, opts LanguageModelCallOptions) (LanguageModelCallOptions, error) {
	inSystem, inInstructionMessages := c.System, c.InstructionMessages
	// TS hands prepareCall the resolved messages: a plain prompt is its
	// single user message.
	if len(opts.Messages) == 0 && c.Prompt != "" {
		opts.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: c.Prompt}}}}
	}
	r.st.baseMessages = len(opts.Messages)
	mutated, err := r.w.PrepareCall(ctx, opts)
	if err != nil {
		return opts, err
	}
	opts = mutated
	if r.pctx.reapplyCall != nil {
		r.pctx.reapplyCall(&opts)
	}
	if !r.pctx.callConcurrencySet {
		if _, err := resolveToolCallConcurrency(opts.ToolCallConcurrency); err != nil {
			return opts, err
		}
	}
	opts.System, opts.InstructionMessages, _ = resolveInstructionChange(inSystem, inInstructionMessages, opts.System, opts.InstructionMessages)
	kept := opts
	r.st.prepared = &kept
	return opts, nil
}

// early runs PrepareCall before the agent loop, on the unresolved call
// messages, and marks it done so the loop's first step reuses the result. It
// returns the hook's effective options.
func (r *prepareCallRunner) early(ctx context.Context, c agent.PrepareCallConfig) (LanguageModelCallOptions, error) {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	r.st.started = true
	return r.runHook(ctx, c, r.callOptions(c))
}

// resolved records the messages the run starts the loop with once approvals
// were resolved from the hook's messages: they replace the hook's as the base
// the loop's own history is appended to.
func (r *prepareCallRunner) resolved(messages []types.Message) {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	if r.st.prepared != nil {
		r.st.prepared.Messages = messages
	}
	r.st.baseMessages = len(messages)
}

// makePrepareCall builds the agent-level PrepareCall closure. PrepareCall runs
// once, on the first step, before any PrepareStep hook; its results persist for
// the whole run. When stepsInline is set (the legacy generate loop calls this
// closure on every step) the PrepareStep hook also runs here; the streaming
// path runs it per step through makePrepareStep instead.
func (w *WorkflowAgent) makePrepareCall(pctx prepareCallContext, stepsInline bool, persist *stepPersistence) (func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig, *prepareCallRunner) {
	stepHook := w.stepHook(pctx)
	if w.PrepareCall == nil && (stepHook == nil || !stepsInline) && w.FilterActiveTools == nil && pctx.activeTools == nil && w.ActiveTools == nil {
		return nil, nil
	}
	r := &prepareCallRunner{w: w, pctx: pctx, st: &prepareCallState{}}
	st := r.st
	return func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
		opts := r.callOptions(c)
		inSystem, inInstructionMessages := c.System, c.InstructionMessages
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
				mutated, err := r.runHook(ctx, c, opts)
				if err != nil {
					st.mu.Unlock()
					return failStep(err)
				}
				opts = mutated
			}
		} else if st.prepared != nil {
			opts = st.overlay(opts)
		}
		st.mu.Unlock()
		// The call's active tools (after PrepareCall) filter the tool set
		// before a PrepareStep ActiveTools does, so a step can narrow but not
		// widen it (TS passes the filtered set to the iterator, which filters
		// it again by the step's active tools).
		callActive := opts.ActiveTools
		if callActive == nil {
			callActive = pctx.activeTools
		}
		headersOverridden := false
		if stepsInline && stepHook != nil {
			opts = persist.apply(opts)
			opts.Headers = maps.Clone(opts.Headers)
			inHeaders := maps.Clone(opts.Headers)
			mutated, err := stepHook(ctx, opts)
			if err != nil {
				return failStep(err)
			}
			headersOverridden = persist.record(opts, mutated, inHeaders)
			opts = mutated
		}
		if opts.Model != nil {
			c.Model = opts.Model
		}
		c.System, c.AllowSystemInMessages, c.Messages, c.Tools, c.ToolChoice, c.CallOptions = opts.System, opts.AllowSystemInMessages, opts.Messages, opts.Tools, opts.ToolChoice, opts.CallOptions
		c.System, c.InstructionMessages, _ = resolveInstructionChange(inSystem, inInstructionMessages, opts.System, opts.InstructionMessages)
		if opts.ToolCallConcurrency != nil && !pctx.callConcurrencySet {
			c.ToolCallConcurrency = opts.ToolCallConcurrency
		}
		if opts.Telemetry != nil {
			c.Telemetry = opts.Telemetry
		}
		c.Temperature, c.MaxTokens, c.TopP, c.TopK = opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK
		c.FrequencyPenalty, c.PresencePenalty, c.StopSequences, c.Seed = opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed
		c.Headers, c.Reasoning, c.SendReasoning, c.ProviderOptions = workflowStepHeaders(opts.Headers, headersOverridden), opts.Reasoning, opts.SendReasoning, opts.ProviderOptions
		c.RuntimeContext, c.ToolsContext, c.ExperimentalSandbox, c.CustomData = opts.RuntimeContext, opts.ToolsContext, opts.ExperimentalSandbox, opts.CustomData
		c.StopWhen, c.ExperimentalDownload, c.MaxRetries, c.Timeout = withFinishReasonStop(opts.StopWhen), opts.ExperimentalDownload, opts.MaxRetries, opts.Timeout
		if w.FilterActiveTools != nil {
			c.Tools = w.FilterActiveTools(ctx, c.StepNumber, c.Tools)
		}
		effectiveActive := opts.ActiveTools
		if effectiveActive == nil {
			effectiveActive = callActive
		}
		if callActive != nil {
			c.Tools = ai.FilterActiveTools(c.Tools, callActive)
		}
		c.ActiveTools = effectiveActive
		if effectiveActive != nil {
			c.Tools = ai.FilterActiveTools(c.Tools, effectiveActive)
		}
		return c
	}, r
}

// stepPersistence carries what a PrepareStep hook returned into the next
// step's hook input and settings. TS's model-call iterator keeps the current
// model, instructions (conversationPrompt[0]), runtime and tools contexts,
// active tools, tool choice and generation settings across steps, so an
// override applies until a later prepareStep changes it; only the sandbox is
// per step. Messages already persist through the loop's own history.
type stepPersistence struct {
	mu   sync.Mutex
	set  bool
	last LanguageModelCallOptions
	// headersOverridden records that a PrepareStep hook replaced the headers
	// (see record); it stays set for the rest of the run.
	headersOverridden bool
}

// workflowStepHeaders returns the headers for a step's model call: the
// call's headers tagged with the ai-sdk-agent/workflow user-agent suffix, or,
// once a PrepareStep hook replaced them, the hook's headers as they are. TS
// appends the suffix to the call's generation settings before the loop, and
// mergePrepareStepGenerationSettings then replaces the headers with the
// hook's, so the suffix is gone after an override.
func workflowStepHeaders(headers map[string]string, overridden bool) map[string]string {
	if overridden {
		return headers
	}
	return version.WithUserAgentSuffix(headers, "ai-sdk-agent/workflow")
}

// apply overlays the previous hook result onto a later step's options.
func (p *stepPersistence) apply(opts LanguageModelCallOptions) LanguageModelCallOptions {
	if p == nil {
		return opts
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.set {
		return opts
	}
	l := p.last
	if l.Model != nil {
		opts.Model = l.Model
	}
	opts.System, opts.InstructionMessages = l.System, l.InstructionMessages
	opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK = l.Temperature, l.MaxTokens, l.TopP, l.TopK
	opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed = l.FrequencyPenalty, l.PresencePenalty, l.StopSequences, l.Seed
	opts.MaxRetries, opts.Headers, opts.Reasoning, opts.ProviderOptions = l.MaxRetries, l.Headers, l.Reasoning, l.ProviderOptions
	opts.ToolChoice, opts.ActiveTools = l.ToolChoice, l.ActiveTools
	opts.RuntimeContext, opts.ToolsContext = l.RuntimeContext, l.ToolsContext
	return opts
}

// record keeps a hook result for later steps, with the instructions resolved
// against the hook's input (a changed System replaces instruction messages).
// inHeaders is a copy of the headers the hook was given: a hook that returns
// other headers replaced them (TS prepareStep `headers`, which drops the
// user-agent suffix). It returns whether the headers were replaced in this or
// an earlier step.
func (p *stepPersistence) record(in, out LanguageModelCallOptions, inHeaders map[string]string) bool {
	changed := !reflect.DeepEqual(inHeaders, out.Headers)
	if p == nil {
		return changed
	}
	out.System, out.InstructionMessages, _ = resolveInstructionChange(in.System, in.InstructionMessages, out.System, out.InstructionMessages)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.set, p.last = true, out
	if changed {
		p.headersOverridden = true
	}
	return p.headersOverridden
}

// stepCallSettings hands the headers and maxRetries a PrepareStep hook left
// for the step to the streaming loop's model call (ai.InternalOptions
// StepCallSettings): core PrepareStepOptions has no such fields, but TS
// WorkflowAgent's prepareStep overrides both (mergePrepareStepGenerationSettings).
type stepCallSettings struct {
	mu         sync.Mutex
	set        bool
	headers    map[string]string
	maxRetries *int
	// runner, when set, holds PrepareCall's result: its headers and
	// maxRetries are the base the first step's hook starts from.
	runner *prepareCallRunner
}

// base returns the headers (without the user-agent suffix) and maxRetries a
// step's hook starts from before any PrepareStep result persisted: the
// PrepareCall result's when it ran, else the call's.
func (s *stepCallSettings) base(pctx prepareCallContext) (map[string]string, *int) {
	if s.runner != nil {
		s.runner.st.mu.Lock()
		defer s.runner.st.mu.Unlock()
		if p := s.runner.st.prepared; p != nil {
			return p.Headers, p.MaxRetries
		}
	}
	// A copy: a hook that edits the map in place must not change the
	// agent's Headers (and race with its other runs).
	return maps.Clone(pctx.baseHeaders), pctx.maxRetries
}

func (s *stepCallSettings) put(headers map[string]string, maxRetries *int) {
	s.mu.Lock()
	s.set, s.headers, s.maxRetries = true, headers, maxRetries
	s.mu.Unlock()
}

func (s *stepCallSettings) get() (map[string]string, *int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers, s.maxRetries, s.set
}

// makePrepareStep adapts the WorkflowAgent PrepareStep hook to the streaming
// loop's per-step hook so it runs before every step, not only the first.
func (w *WorkflowAgent) makePrepareStep(pctx prepareCallContext, allTools []types.Tool, allowSystem bool, persist *stepPersistence, settings *stepCallSettings) func(context.Context, ai.PrepareStepOptions) ai.PrepareStepOptions {
	hook := w.stepHook(pctx)
	if hook == nil {
		return nil
	}
	return func(ctx context.Context, o ai.PrepareStepOptions) ai.PrepareStepOptions {
		activeTools := o.ActiveTools
		if activeTools == nil {
			activeTools = pctx.activeTools
		}
		baseHeaders, baseMaxRetries := settings.base(pctx)
		opts := LanguageModelCallOptions{
			StepNumber: o.StepNumber, Model: o.Model, System: o.System, AllowSystemInMessages: allowSystem, Messages: o.Messages, Tools: o.Tools, ToolChoice: o.ToolChoice,
			Temperature: o.Temperature, MaxTokens: o.MaxOutputTokens, TopP: o.TopP, TopK: o.TopK, FrequencyPenalty: o.FrequencyPenalty, PresencePenalty: o.PresencePenalty,
			StopSequences: o.StopSequences, Seed: o.Seed, Reasoning: o.Reasoning, ProviderOptions: maps.Clone(o.ProviderOptions), Headers: baseHeaders,
			RuntimeContext: o.RuntimeContext, ToolsContext: maps.Clone(o.ToolsContext), ExperimentalSandbox: o.ExperimentalSandbox,
			PreviousSteps: o.Steps, AccumulatedUsage: o.AccumulatedUsage,
			StopWhen: pctx.stopWhen, ActiveTools: activeTools, ExperimentalDownload: pctx.download,
			MaxRetries: baseMaxRetries, Timeout: pctx.timeout,
			InitialInstructions: pctx.initialInstructions, InitialMessages: pctx.initialMessages,
			InstructionMessages: o.InstructionMessages, InitialInstructionMessages: pctx.instructionMessages,
		}
		inSystem, inInstructionMessages := o.System, o.InstructionMessages
		opts = persist.apply(opts)
		opts.Headers = maps.Clone(opts.Headers)
		inHeaders := maps.Clone(opts.Headers)
		mutated, err := hook(ctx, opts)
		if err == nil && mutated.MaxRetries != nil && *mutated.MaxRetries < 0 {
			// TS prepareRetries rejects the step's maxRetries.
			err = &providererrors.InvalidArgumentError{Field: "maxRetries", Message: "maxRetries must be >= 0"}
		}
		if err != nil {
			base := o.Model
			if base == nil {
				base = w.Model
			}
			o.Model = &hookFailedModel{LanguageModel: base, err: err}
			return o
		}
		overridden := persist.record(opts, mutated, inHeaders)
		settings.put(workflowStepHeaders(mutated.Headers, overridden), mutated.MaxRetries)
		if mutated.Model != nil {
			o.Model = mutated.Model
		}
		// Only touch the instructions when the hook changed them: core drops
		// the step's system-message instructions (and their ProviderOptions)
		// as soon as System/Instructions is set.
		if system, messages, changed := resolveInstructionChange(inSystem, inInstructionMessages, mutated.System, mutated.InstructionMessages); changed {
			o.System = system
			o.Instructions = &system
			o.InstructionMessages = messages
		}
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

func (w *WorkflowAgent) makeAgent(ovr WorkflowStreamOptions, govr WorkflowGenerateOptions, streaming bool) (*agent.ToolLoopAgent, *earlyPrepare) {
	system := w.System
	var instructionMessages []types.Message
	// useInstructions applies an Instructions value over the current one; a
	// string System replaces any system-message instructions.
	useInstructions := func(v interface{}) {
		if normalized, err := normalizeInstructions(v); err == nil && normalized != "" {
			system = normalized
			instructionMessages = instructionMessagesFrom(v)
		}
	}
	useSystem := func(v string) {
		if v != "" {
			system = v
			instructionMessages = nil
		}
	}
	if w.Instructions != nil {
		useInstructions(w.Instructions)
	}
	useSystem(govr.System)
	if govr.Instructions != nil {
		useInstructions(govr.Instructions)
	}
	useSystem(ovr.System)
	if ovr.Instructions != nil {
		useInstructions(ovr.Instructions)
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
	callConcurrencySet := false
	if govr.ToolCallConcurrency != nil {
		toolCallConcurrency = govr.ToolCallConcurrency
		callConcurrencySet = true
	}
	if ovr.ToolCallConcurrency != nil {
		toolCallConcurrency = ovr.ToolCallConcurrency
		callConcurrencySet = true
	}
	hookToolCallConcurrency := toolCallConcurrency
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
	// TS options.activeTools ?? this.activeTools: the call's active tools,
	// which PrepareCall sees and may replace.
	activeTools := w.ActiveTools
	if govr.ActiveTools != nil {
		activeTools = govr.ActiveTools
	}
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
	basePrompt := govr.Prompt
	if ovr.Prompt != "" {
		basePrompt = ovr.Prompt
	}
	if basePrompt == "" {
		basePrompt = w.Prompt
	}
	// endBase is the conversation the run starts from; GenerateWithOptions and
	// StreamWithOptions replace it once PrepareCall and the approval resume
	// have settled it (see earlyPrepare.setConversation).
	endBase := &[]types.Message{}
	*endBase = workflowConversationBase(system, instructionMessages, initialMessages, basePrompt)
	onEnd := mergeFinish(resolveEnd(w.OnEnd, w.OnFinish), mergeFinish(resolveEnd(govr.OnEnd, govr.OnFinish), resolveEnd(ovr.OnEnd, ovr.OnFinish)))
	onStepStart := mergeStepStart(w.OnStepStart, mergeStepStart(govr.OnStepStart, ovr.OnStepStart))
	if onEnd != nil {
		inner := onEnd
		// lastStep is the prompt of the latest step: TS's iterator keeps one
		// conversationPrompt that a prepareStep messages/system override
		// replaces for the rest of the run, so the final conversation is that
		// step's prompt plus the messages of the steps from there on.
		var lastStep struct {
			sync.Mutex
			set                 bool
			number              int
			system              string
			instructionMessages []types.Message
			messages            []types.Message
		}
		userStepStart := onStepStart
		onStepStart = func(ctx context.Context, e ai.OnStepStartEvent) {
			lastStep.Lock()
			lastStep.set, lastStep.number = true, e.StepNumber
			lastStep.system = e.Instructions
			lastStep.instructionMessages = append([]types.Message(nil), e.InstructionMessages...)
			lastStep.messages = append([]types.Message(nil), e.Messages...)
			lastStep.Unlock()
			if userStepStart != nil {
				userStepStart(ctx, e)
			}
		}
		// TS WorkflowAgent's onEnd event carries `messages`: the final
		// conversation (the converted prompt the loop started from, then every
		// step's response messages).
		onEnd = func(ctx context.Context, e ai.OnFinishEvent) {
			if e.Messages != nil {
				// A run that failed by a thrown error: core already set
				// TS's `prompt.messages` fallback (InternalOptions
				// FailedEndMessages).
				inner(ctx, e)
				return
			}
			messages := append([]types.Message(nil), *endBase...)
			from := 0
			lastStep.Lock()
			if lastStep.set && lastStep.number <= len(e.Steps) {
				messages = workflowConversationBase(lastStep.system, lastStep.instructionMessages, lastStep.messages, "")
				from = lastStep.number
			}
			lastStep.Unlock()
			for _, step := range e.Steps[from:] {
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
		instructionMessages: instructionMessages,
		toolCallConcurrency: hookToolCallConcurrency,
		callConcurrencySet:  callConcurrencySet,
		baseHeaders:         callMap(ovr.Headers, govr.Headers, w.Headers),
		reapplyCall: func(o *LanguageModelCallOptions) {
			if v := callSetting(ovr.Temperature, govr.Temperature); v != nil {
				o.Temperature = v
			}
			if v := callSetting(ovr.MaxTokens, govr.MaxTokens); v != nil {
				o.MaxTokens = v
			}
			if v := callSetting(ovr.TopP, govr.TopP); v != nil {
				o.TopP = v
			}
			if v := callSetting(ovr.TopK, govr.TopK); v != nil {
				o.TopK = v
			}
			if v := callSetting(ovr.FrequencyPenalty, govr.FrequencyPenalty); v != nil {
				o.FrequencyPenalty = v
			}
			if v := callSetting(ovr.PresencePenalty, govr.PresencePenalty); v != nil {
				o.PresencePenalty = v
			}
			if v := callSlice(ovr.StopSequences, govr.StopSequences); v != nil {
				o.StopSequences = v
			}
			if v := callSetting(ovr.Seed, govr.Seed); v != nil {
				o.Seed = v
			}
			if v := callSetting(ovr.MaxRetries, govr.MaxRetries); v != nil {
				o.MaxRetries = v
			}
			if v := callSetting(ovr.Timeout, govr.Timeout); v != nil {
				o.Timeout = v
			}
			if v := callSetting(ovr.Reasoning, govr.Reasoning); v != nil {
				o.Reasoning = v
			}
			if v := callMap(ovr.Headers, govr.Headers); v != nil {
				o.Headers = v
			}
			if v := callMap(ovr.ProviderOptions, govr.ProviderOptions); v != nil {
				o.ProviderOptions = v
			}
		},
	}
	var stepAdapter func(context.Context, ai.PrepareStepOptions) ai.PrepareStepOptions
	persist := &stepPersistence{}
	var settings *stepCallSettings
	if streaming {
		settings = &stepCallSettings{}
		stepAdapter = w.makePrepareStep(pctx, tools, allowSystemInMessages, persist, settings)
	}
	prepareCall, runner := w.makePrepareCall(pctx, !streaming, persist)
	if settings != nil {
		settings.runner = runner
	}
	cfg := agent.AgentConfig{
		ID: w.ID, Model: w.Model, System: system, InstructionMessages: instructionMessages, Prompt: w.Prompt, Tools: tools, StopWhen: withFinishReasonStop(stopWhen),
		AllowSystemInMessages: allowSystemInMessages,
		CallOptionsSchema:     w.CallOptionsSchema, CallOptions: w.CallOptions, PrepareCall: prepareCall, PrepareStep: stepAdapter,
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
		OnStepStartEvent:               onStepStart,
		OnToolExecutionStart:           mergeToolStart(w.OnToolExecutionStart, mergeToolStart(govr.OnToolExecutionStart, ovr.OnToolExecutionStart)),
		OnToolExecutionEnd:             mergeToolEnd(w.OnToolExecutionEnd, mergeToolEnd(govr.OnToolExecutionEnd, ovr.OnToolExecutionEnd)),
		OnStepEndEvent:                 mergeStepFinish(resolveStepEnd(w.OnStepEnd, w.OnStepFinish), mergeStepFinish(resolveStepEnd(govr.OnStepEnd, govr.OnStepFinish), resolveStepEnd(ovr.OnStepEnd, ovr.OnStepFinish))),
		OnEndEvent:                     onEnd,
	}
	return agent.NewToolLoopAgent(cfg), &earlyPrepare{cfg: cfg, runner: runner, callConcurrencySet: callConcurrencySet, endBase: endBase, stepSettings: settings}
}

// workflowConversationBase is the conversation TS WorkflowAgent's loop starts
// from (convertToLanguageModelPrompt of the standardized prompt): the
// instructions as system messages, then the messages, or the prompt as a user
// message, with consecutive tool messages combined into one.
func workflowConversationBase(system string, instructionMessages, messages []types.Message, prompt string) []types.Message {
	var out []types.Message
	switch {
	case len(instructionMessages) > 0:
		out = append(out, instructionMessages...)
	case system != "":
		out = append(out, types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: system}}})
	}
	if len(messages) == 0 && prompt != "" {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt}}}}
	}
	for _, msg := range messages {
		if n := len(out); n > 0 && msg.Role == types.RoleTool && out[n-1].Role == types.RoleTool {
			merged := out[n-1]
			merged.Content = append(append([]types.ContentPart(nil), merged.Content...), msg.Content...)
			out[n-1] = merged
			continue
		}
		out = append(out, msg)
	}
	return out
}

// earlyPrepare runs the PrepareCall hook ahead of the agent loop (see
// prepareCallRunner.early). runner is nil when the agent has no PrepareCall.
type earlyPrepare struct {
	cfg                agent.AgentConfig
	runner             *prepareCallRunner
	callConcurrencySet bool
	// endBase is the start of the conversation the onEnd event reports.
	endBase *[]types.Message
	// stepSettings carries the stream PrepareStep's headers and maxRetries
	// to the step's model call; nil outside the stream path.
	stepSettings *stepCallSettings
}

// internalOptions returns the core internal options for the run: the shared
// workflow options plus, in the stream path, the PrepareStep call settings
// and the end event messages of a run that fails by a thrown error: TS's
// standardized prompt messages (`prompt.messages`, after the approval
// resume), without the instructions and with a plain prompt as one user
// message.
func (e *earlyPrepare) internalOptions(messages []types.Message, prompt string) *ai.InternalOptions {
	if e == nil || e.stepSettings == nil {
		return workflowInternalOptions
	}
	internal := *workflowInternalOptions
	internal.StepCallSettings = e.stepSettings.get
	if len(messages) == 0 && prompt != "" {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt}}}}
	}
	internal.FailedEndMessages = append([]types.Message{}, messages...)
	return &internal
}

// setConversation records the conversation the run starts the loop with:
// after PrepareCall (its instructions and messages) and the approval resume
// (the cleaned history plus the resumed tool results), as TS onEnd's
// `messages` begins with the prompt the loop was given.
func (e *earlyPrepare) setConversation(prepared *LanguageModelCallOptions, messages []types.Message, prompt string) {
	if e == nil || e.endBase == nil {
		return
	}
	system, instructionMessages := e.cfg.System, e.cfg.InstructionMessages
	if prepared != nil {
		system, instructionMessages = prepared.System, prepared.InstructionMessages
	}
	*e.endBase = workflowConversationBase(system, instructionMessages, messages, prompt)
}

// run is the PrepareCall hook over the call's unresolved messages, with the
// first-step configuration the agent loop would hand it. It returns nil when
// there is no hook to run.
func (e *earlyPrepare) run(ctx context.Context, messages []types.Message, prompt string) (*LanguageModelCallOptions, error) {
	if e == nil || e.runner == nil || e.runner.w.PrepareCall == nil {
		return nil, nil
	}
	cfg := e.cfg
	runtimeContext := cfg.RuntimeContext
	if runtimeContext == nil {
		runtimeContext = cfg.ExperimentalContext
	}
	c := agent.PrepareCallConfig{
		Model: cfg.Model, System: cfg.System, AllowSystemInMessages: cfg.AllowSystemInMessages, Prompt: prompt, Messages: messages,
		Tools: cfg.Tools, ActiveTools: cfg.ActiveTools, ToolChoice: cfg.ToolChoice, ToolCallConcurrency: cfg.ToolCallConcurrency,
		Temperature: cfg.Temperature, MaxTokens: cfg.MaxTokens, TopP: cfg.TopP, TopK: cfg.TopK,
		FrequencyPenalty: cfg.FrequencyPenalty, PresencePenalty: cfg.PresencePenalty, StopSequences: cfg.StopSequences, Seed: cfg.Seed,
		Headers: cfg.Headers, Reasoning: cfg.Reasoning, SendReasoning: cfg.SendReasoning, ProviderOptions: cfg.ProviderOptions,
		StopWhen: cfg.StopWhen, Telemetry: cfg.Telemetry, ExperimentalSandbox: cfg.ExperimentalSandbox,
		InstructionMessages: cfg.InstructionMessages, RuntimeContext: runtimeContext, ToolsContext: cfg.ToolsContext,
		CallOptions: cfg.CallOptions, MaxRetries: cfg.MaxRetries, Timeout: cfg.Timeout,
	}
	opts, err := e.runner.early(ctx, c)
	if err != nil {
		return nil, err
	}
	return &opts, nil
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

// validateCallInstructions rejects an instructions value of an unsupported
// type instead of silently running without it.
func validateCallInstructions(values ...interface{}) error {
	for _, v := range values {
		if _, err := normalizeInstructions(v); err != nil {
			return err
		}
	}
	return nil
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
		// The positional prompt stays unless the options carry their own.
		if opts.Prompt != "" {
			legacy.Prompt = opts.Prompt
		}
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
	if err := validateCallInstructions(w.Instructions, opts.Instructions); err != nil {
		return nil, err
	}
	telemetrySettings := w.Telemetry
	if opts.Telemetry != nil {
		telemetrySettings = opts.Telemetry
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
	a, early := w.makeAgent(WorkflowStreamOptions{}, opts, false)
	// TS runs prepareCall before it opens the call's telemetry and resolves
	// tool approvals, so the hook sees the unresolved messages and its
	// overrides reach the root span and the resume.
	prepared, err := early.run(ctx, opts.Messages, opts.Prompt)
	if err != nil {
		return nil, err
	}
	runtimeContext := firstNonNil(opts.RuntimeContext, w.RuntimeContext)
	toolsContext := firstNonNilMap(opts.ToolsContext, w.ToolsContext)
	sandbox := firstNonNil(opts.ExperimentalSandbox, w.ExperimentalSandbox)
	model := w.Model
	toolCallConcurrencyOverride := (*int)(nil)
	if prepared != nil {
		opts.Messages = prepared.Messages
		if len(opts.Messages) > 0 {
			// Messages from prepareCall take precedence over the prompt (TS).
			opts.Prompt = ""
		}
		runtimeContext, toolsContext, sandbox = prepared.RuntimeContext, prepared.ToolsContext, prepared.ExperimentalSandbox
		if prepared.Model != nil {
			model = prepared.Model
		}
		if prepared.Telemetry != nil {
			telemetrySettings = prepared.Telemetry
		}
		if !early.callConcurrencySet {
			toolCallConcurrencyOverride = prepared.ToolCallConcurrency
		}
	}
	modelProvider, modelID := workflowModelInfo(model)
	toolCallConcurrency, err := resolveToolCallConcurrency(toolCallConcurrencyOverride, opts.ToolCallConcurrency, w.ToolCallConcurrency)
	if err != nil {
		return nil, err
	}
	callID := newWorkflowCallID()
	var initialResponseMessages []types.Message
	opts.Messages, _, err = processWorkflowApprovalResume(ctx, workflowApprovalResumeOptions{
		initialResponse:     &initialResponseMessages,
		messages:            opts.Messages,
		tools:               effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts),
		runtimeContext:      runtimeContext,
		toolsContext:        toolsContext,
		experimentalSandbox: sandbox,
		toolApprovalSecret:  w.effectiveToolApprovalSecret(WorkflowStreamOptions{}, opts),
		onToolStart:         mergeToolStart(w.OnToolExecutionStart, opts.OnToolExecutionStart),
		onToolEnd:           mergeToolEnd(w.OnToolExecutionEnd, opts.OnToolExecutionEnd),
		telemetrySettings:   telemetrySettings,
		telemetryCallID:     callID,
		toolCallConcurrency: toolCallConcurrency,
	})
	if err != nil {
		return nil, err
	}
	if prepared != nil {
		early.runner.resolved(opts.Messages)
	}
	// TS convertToLanguageModelPrompt rejects a history whose tool calls (or
	// unresolved approval requests) lack tool results.
	if err := validateWorkflowToolResults(opts.Messages, effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts)); err != nil {
		return nil, err
	}
	early.setConversation(prepared, opts.Messages, opts.Prompt)
	// TS opens the workflow root (telemetryDispatcher.onStart) only after the
	// approved history tools ran and the prompt was converted, so its messages
	// are the cleaned post-resolution history and a failure before this point
	// dispatches no telemetry. The resume's tool events therefore find no
	// root, as in TS, where the integration has no call state yet and records
	// no tool span. Both TS methods open the root as 'ai.workflowAgent.stream'.
	cancelled := ctx != nil && ctx.Err() != nil
	if cancelled {
		fireWorkflowStartBeforeAbort(ctx, mergeStart(w.OnStart, opts.OnStart), model, opts.Messages, opts.Prompt, runtimeContext, toolsContext)
	}
	ctx = telemetrypkg.FireOnStart(ctx, workflowStartEvent(workflowRootOperation, callID, modelProvider, modelID, telemetrySettings, early, prepared, opts.Messages, opts.Prompt, runtimeContext, toolsContext))
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if cancelled {
		telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID})
		if onAbort != nil {
			onAbort(ctx, nil)
		}
		return nil, ctx.Err()
	}
	system := opts.System
	var instructionMessages []types.Message
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
			if normalized != "" {
				instructionMessages = instructionMessagesFrom(opts.Instructions)
			}
		}
	}
	call := agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, InstructionMessages: instructionMessages, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: withFinishReasonStop(opts.StopWhen), Output: firstNonNil(opts.Output, w.Output), Telemetry: telemetrySettings, Internal: workflowInternalOptions}
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
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, runtimeContext),
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

// fireWorkflowStartBeforeAbort fires the user OnStart for a run whose context
// was already cancelled before the loop. TS WorkflowAgent calls onStart (then
// the telemetry start) before its abort check, so a pre-cancelled run still
// reports its start, then onAbort. Normally core fires OnStart; this run never
// reaches it. The event carries what TS's does: the model, the standardized
// messages (a plain prompt is one user message) and the contexts.
func fireWorkflowStartBeforeAbort(ctx context.Context, onStart StartCallback, model provider.LanguageModel, messages []types.Message, prompt string, runtimeContext interface{}, toolsContext map[string]interface{}) {
	if onStart == nil {
		return
	}
	if len(messages) == 0 && prompt != "" {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt}}}}
	}
	providerName, modelID := workflowModelInfo(model)
	onStart(ctx, ai.OnStartEvent{
		Provider: providerName,
		// ModelProvider is the deprecated alias of Provider, set for
		// callbacks still reading it.
		ModelProvider:       providerName, //nolint:staticcheck
		ModelID:             modelID,
		Prompt:              prompt,
		Messages:            messages,
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	})
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
		// The positional prompt stays unless the options carry their own.
		if opts.Prompt != "" {
			legacy.Prompt = opts.Prompt
		}
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
	if err := validateCallInstructions(w.Instructions, opts.Instructions); err != nil {
		return nil, err
	}
	telemetrySettings := w.Telemetry
	if opts.Telemetry != nil {
		telemetrySettings = opts.Telemetry
	}
	a, early := w.makeAgent(opts, WorkflowGenerateOptions{}, true)
	// TS runs prepareCall before it opens the call's telemetry and resolves
	// tool approvals, so the hook sees the unresolved messages and its
	// overrides reach the root span and the resume.
	prepared, err := early.run(ctx, opts.Messages, opts.Prompt)
	if err != nil {
		return nil, err
	}
	runtimeContext := firstNonNil(opts.RuntimeContext, w.RuntimeContext)
	toolsContext := firstNonNilMap(opts.ToolsContext, w.ToolsContext)
	sandbox := firstNonNil(opts.ExperimentalSandbox, w.ExperimentalSandbox)
	model := w.Model
	toolCallConcurrencyOverride := (*int)(nil)
	if prepared != nil {
		opts.Messages = prepared.Messages
		if len(opts.Messages) > 0 {
			// Messages from prepareCall take precedence over the prompt (TS).
			opts.Prompt = ""
		}
		runtimeContext, toolsContext, sandbox = prepared.RuntimeContext, prepared.ToolsContext, prepared.ExperimentalSandbox
		if prepared.Model != nil {
			model = prepared.Model
		}
		if prepared.Telemetry != nil {
			telemetrySettings = prepared.Telemetry
		}
		if !early.callConcurrencySet {
			toolCallConcurrencyOverride = prepared.ToolCallConcurrency
		}
	}
	modelProvider, modelID := workflowModelInfo(model)
	toolCallConcurrency, err := resolveToolCallConcurrency(toolCallConcurrencyOverride, opts.ToolCallConcurrency, w.ToolCallConcurrency)
	if err != nil {
		return nil, err
	}
	var prefixChunks []provider.StreamChunk
	opts.Messages, prefixChunks, err = processWorkflowApprovalResume(ctx, workflowApprovalResumeOptions{
		messages:            opts.Messages,
		tools:               effectiveWorkflowTools(w, opts, WorkflowGenerateOptions{}),
		runtimeContext:      runtimeContext,
		toolsContext:        toolsContext,
		experimentalSandbox: sandbox,
		toolApprovalSecret:  w.effectiveToolApprovalSecret(opts, WorkflowGenerateOptions{}),
		onToolStart:         mergeToolStart(w.OnToolExecutionStart, opts.OnToolExecutionStart),
		onToolEnd:           mergeToolEnd(w.OnToolExecutionEnd, opts.OnToolExecutionEnd),
		telemetrySettings:   telemetrySettings,
		telemetryCallID:     newWorkflowCallID(),
		toolCallConcurrency: toolCallConcurrency,
	})
	if err != nil {
		return nil, err
	}
	if prepared != nil {
		early.runner.resolved(opts.Messages)
	}
	// TS stream() awaits convertToLanguageModelPrompt before returning, so a
	// history with unresolved tool calls rejects stream() itself.
	if err := validateWorkflowToolResults(opts.Messages, effectiveWorkflowTools(w, opts, WorkflowGenerateOptions{})); err != nil {
		return nil, err
	}
	early.setConversation(prepared, opts.Messages, opts.Prompt)
	// The root opens after the approved history tools ran, with the cleaned
	// messages, as in GenerateWithOptions (TS onStart order). The span ends
	// as soon as the underlying stream is obtained: the streaming happens as
	// the caller drains WorkflowStreamResult, under the child span the
	// delegated ai.StreamText call creates for itself.
	cancelled := ctx != nil && ctx.Err() != nil
	if cancelled {
		fireWorkflowStartBeforeAbort(ctx, mergeStart(w.OnStart, opts.OnStart), model, opts.Messages, opts.Prompt, runtimeContext, toolsContext)
	}
	ctx = telemetrypkg.FireOnStart(ctx, workflowStartEvent(workflowRootOperation, "", modelProvider, modelID, telemetrySettings, early, prepared, opts.Messages, opts.Prompt, runtimeContext, toolsContext))
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if cancelled {
		telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings})
		if onAbort != nil {
			onAbort(ctx, nil)
		}
		return nil, ctx.Err()
	}
	system := opts.System
	var instructionMessages []types.Message
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
			if normalized != "" {
				instructionMessages = instructionMessagesFrom(opts.Instructions)
			}
		}
	}
	call := agent.AgentStreamOptions{
		AgentGenerateOptions:  agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, InstructionMessages: instructionMessages, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: withFinishReasonStop(opts.StopWhen), Output: firstNonNil(opts.Output, w.Output), Telemetry: telemetrySettings, Internal: early.internalOptions(opts.Messages, opts.Prompt)},
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
		RuntimeContext: workflowTelemetryRuntimeContext(telemetrySettings, runtimeContext),
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
var workflowInternalOptions = &ai.InternalOptions{ContinueOnMissingProviderResult: true, StopOnStreamError: true, EndOnError: true, HeadersAsGiven: true, ApprovalID: workflowApprovalID, RequestMessagesAsPrompt: true, DeferApprovalRequests: true}

// workflowApprovalID is TS WorkflowAgent's approval ID for a tool call:
// `approval-${toolCallId}`, in generate and stream mode.
func workflowApprovalID(toolCallID string) string { return "approval-" + toolCallID }

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
