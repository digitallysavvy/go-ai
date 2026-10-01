package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	StopWhen              []ai.StopCondition
	Output                interface{}
	Telemetry             *ai.TelemetrySettings
	ID                    string
	Prompt                string

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
	// ExperimentalDownload customizes remote file URL downloads before model
	// calls, forwarded to agent.AgentConfig.ExperimentalDownload.
	ExperimentalDownload ai.DownloadFunction
}

// WorkflowGenerateOptions configures a single generate invocation.
type WorkflowGenerateOptions struct {
	Prompt                      string
	Messages                    []types.Message
	System                      string
	Instructions                interface{}
	AllowSystemInMessages       bool
	Tools                       []types.Tool
	ToolSet                     map[string]types.Tool
	StopWhen                    []ai.StopCondition
	Telemetry                   *ai.TelemetrySettings
	RuntimeContext              interface{}
	ToolsContext                map[string]interface{}
	Include                     *ai.IncludeOptions
	ExperimentalSandbox         interface{}
	ExperimentalRefineToolInput map[string]ai.ToolInputRefiner
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
	// ExperimentalDownload overrides the agent's ExperimentalDownload for this call.
	ExperimentalDownload ai.DownloadFunction

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
type WorkflowResult struct{ *agent.AgentResult }

// IsLoopFinished reports whether the workflow ended naturally.
func (r *WorkflowResult) IsLoopFinished() bool {
	if r == nil || r.AgentResult == nil {
		return false
	}
	return r.StopReason == ""
}

// WorkflowStreamResult wraps the streaming result.
type WorkflowStreamResult struct{ *ai.StreamTextResult }

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
	out := make([]types.Tool, 0, len(toolSet))
	for name, tool := range toolSet {
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

	for _, approval := range collected.ApprovedToolApprovals {
		call := approval.ToolCall
		if call.ProviderExecuted {
			continue
		}
		tool := workflowToolByName(opts.tools, call.ToolName)
		if tool == nil || tool.Execute == nil {
			continue
		}
		if !workflowToolNeedsApproval(ctx, tool, call, messages, opts.toolsContext) {
			localResults = append(localResults, errorTextResult(call, fmt.Sprintf("Tool %q does not require approval", call.ToolName)))
			continue
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
			localResults = append(localResults, errorTextResult(call, revalidationReason))
			continue
		}

		result, execErr := executeWorkflowApprovedTool(ctx, tool, call, opts)
		if execErr != nil {
			errText := execErr.Error()
			localResults = append(localResults, errorTextResult(call, errText))
			// Failed executions stream as tool errors, not tool results.
			prefixChunks = append(prefixChunks, provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Error:      execErr,
					Dynamic:    call.Dynamic,
				},
			})
			continue
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
				return nil, nil, err
			}
			part.Output = output
			part.Result = nil
		}
		localResults = append(localResults, part)
		prefixChunks = append(prefixChunks, provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: call.ID,
				ToolName:   call.ToolName,
				Input:      call.Arguments,
				Result:     result,
				Dynamic:    call.Dynamic,
			},
		})
	}

	for _, approval := range collected.DeniedToolApprovals {
		call := approval.ToolCall
		if call.ProviderExecuted || approval.ExistingToolResult != nil {
			continue
		}
		localResults = append(localResults, types.ToolResultContent{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Input:      call.Arguments,
			Output:     &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: approval.ApprovalResponse.Reason},
		})
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
		cleaned = append(cleaned, types.Message{Role: types.RoleTool, Content: localResults})
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
}

func (w *WorkflowAgent) makePrepareCall(pctx prepareCallContext) func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
	if w.PrepareCall == nil && w.PrepareStep == nil && w.FilterActiveTools == nil && pctx.activeTools == nil && w.ActiveTools == nil {
		return nil
	}
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
		if w.PrepareStep != nil {
			if mutated, err := w.PrepareStep(ctx, opts); err == nil {
				opts = mutated
			}
		}
		if w.PrepareCall != nil {
			if mutated, err := w.PrepareCall(ctx, opts); err == nil {
				opts = mutated
			}
		}
		c.System, c.AllowSystemInMessages, c.Messages, c.Tools, c.ToolChoice, c.CallOptions = opts.System, opts.AllowSystemInMessages, opts.Messages, opts.Tools, opts.ToolChoice, opts.CallOptions
		c.Temperature, c.MaxTokens, c.TopP, c.TopK = opts.Temperature, opts.MaxTokens, opts.TopP, opts.TopK
		c.FrequencyPenalty, c.PresencePenalty, c.StopSequences, c.Seed = opts.FrequencyPenalty, opts.PresencePenalty, opts.StopSequences, opts.Seed
		c.Headers, c.Reasoning, c.SendReasoning, c.ProviderOptions = opts.Headers, opts.Reasoning, opts.SendReasoning, opts.ProviderOptions
		c.RuntimeContext, c.ToolsContext, c.ExperimentalSandbox, c.CustomData = opts.RuntimeContext, opts.ToolsContext, opts.ExperimentalSandbox, opts.CustomData
		c.StopWhen, c.ExperimentalDownload, c.MaxRetries, c.Timeout = opts.StopWhen, opts.ExperimentalDownload, opts.MaxRetries, opts.Timeout
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

func (w *WorkflowAgent) makeAgent(ovr WorkflowStreamOptions, govr WorkflowGenerateOptions) *agent.ToolLoopAgent {
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
	pctx := prepareCallContext{
		activeTools:         ovr.ActiveTools,
		stopWhen:            stopWhen,
		download:            download,
		maxRetries:          maxRetries,
		timeout:             timeout,
		initialInstructions: system,
		initialMessages:     initialMessages,
	}
	return agent.NewToolLoopAgent(agent.AgentConfig{
		ID: w.ID, Model: w.Model, System: system, Prompt: w.Prompt, Tools: tools, StopWhen: stopWhen,
		AllowSystemInMessages: allowSystemInMessages,
		CallOptionsSchema:     w.CallOptionsSchema, CallOptions: w.CallOptions, PrepareCall: w.makePrepareCall(pctx),
		Temperature: w.Temperature, MaxTokens: w.MaxTokens, TopP: w.TopP, TopK: w.TopK, FrequencyPenalty: w.FrequencyPenalty,
		PresencePenalty: w.PresencePenalty, StopSequences: w.StopSequences, Seed: w.Seed, Headers: version.WithUserAgentSuffix(w.Headers, "ai-sdk-agent/workflow"), Reasoning: w.Reasoning,
		SendReasoning: w.SendReasoning, ProviderOptions: w.ProviderOptions, RuntimeContext: runtimeContext, ToolsContext: toolsContext,
		ToolChoice:                     w.ToolChoice,
		Output:                         w.Output,
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
		OnEndEvent:                     mergeFinish(resolveEnd(w.OnEnd, w.OnFinish), mergeFinish(resolveEnd(govr.OnEnd, govr.OnFinish), resolveEnd(ovr.OnEnd, ovr.OnFinish))),
	})
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
	ctx = telemetrypkg.FireOnStart(ctx, telemetrypkg.TelemetryStartEvent{
		OperationType: "ai.workflowAgent.generate",
		ModelProvider: modelProvider,
		ModelID:       modelID,
		Settings:      telemetrySettings,
	})
	var err error
	opts.Messages, _, err = processWorkflowApprovalResume(ctx, workflowApprovalResumeOptions{
		messages:            opts.Messages,
		tools:               effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts),
		runtimeContext:      firstNonNil(opts.RuntimeContext, w.RuntimeContext),
		toolsContext:        firstNonNilMap(opts.ToolsContext, w.ToolsContext),
		experimentalSandbox: firstNonNil(opts.ExperimentalSandbox, w.ExperimentalSandbox),
		toolApprovalSecret:  w.effectiveToolApprovalSecret(WorkflowStreamOptions{}, opts),
		onToolStart:         mergeToolStart(w.OnToolExecutionStart, opts.OnToolExecutionStart),
		onToolEnd:           mergeToolEnd(w.OnToolExecutionEnd, opts.OnToolExecutionEnd),
		telemetrySettings:   telemetrySettings,
	})
	if err != nil {
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
	a := w.makeAgent(WorkflowStreamOptions{}, opts)
	system := opts.System
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
		}
	}
	call := agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: opts.StopWhen, Output: w.Output, Telemetry: telemetrySettings}
	result, err := a.GenerateAgent(ctx, call)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings})
			if onAbort != nil {
				onAbort(ctx, nil)
			}
		} else {
			telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		}
		if onError := mergeError(w.OnError, opts.OnError); onError != nil {
			onError(ctx, err)
		}
		return nil, err
	}
	telemetrypkg.FireOnEnd(ctx, telemetrypkg.TelemetryFinishEvent{
		Settings:      telemetrySettings,
		FinishReason:  string(result.FinishReason),
		ModelProvider: modelProvider,
		ModelID:       modelID,
		Text:          result.Text,
		Usage: telemetrypkg.TelemetryUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
	})
	return &WorkflowResult{AgentResult: result}, nil
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
		OperationType: "ai.workflowAgent.stream",
		ModelProvider: modelProvider,
		ModelID:       modelID,
		Settings:      telemetrySettings,
	})
	var err error
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
	})
	if err != nil {
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
	a := w.makeAgent(opts, WorkflowGenerateOptions{})
	system := opts.System
	if opts.Instructions != nil {
		if normalized, err := normalizeInstructions(opts.Instructions); err == nil {
			system = normalized
		}
	}
	call := agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: opts.StopWhen, Output: w.Output, Telemetry: telemetrySettings},
		OnChunk:              opts.OnChunk,
		InitialStreamChunks:  prefixChunks,
	}
	stream, err := a.Stream(ctx, call)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			telemetrypkg.FireOnAbort(ctx, telemetrypkg.TelemetryAbortEvent{Settings: telemetrySettings})
			if onAbort != nil {
				onAbort(ctx, nil)
			}
		} else {
			telemetrypkg.FireOnError(ctx, telemetrypkg.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		}
		if onError := mergeError(w.OnError, opts.OnError); onError != nil {
			onError(ctx, err)
		}
		return nil, err
	}
	telemetrypkg.FireOnEnd(ctx, telemetrypkg.TelemetryFinishEvent{
		Settings:      telemetrySettings,
		ModelProvider: modelProvider,
		ModelID:       modelID,
	})
	return &WorkflowStreamResult{StreamTextResult: stream}, nil
}
