package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// StepEndCallback is called after each completed step.
type StepEndCallback func(ctx context.Context, e ai.OnStepFinishEvent)

// StepFinishCallback is called after each completed step.
//
// Deprecated: use StepEndCallback.
type StepFinishCallback func(ctx context.Context, e ai.OnStepFinishEvent)

// FinishCallback is called once after workflow completion.
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
	OnFinish     FinishCallback
	OnError      ErrorCallback
	OnAbort      AbortCallback

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

	OnStart              StartCallback
	OnStepStart          StepStartCallback
	OnToolExecutionStart ToolExecutionStartCallback
	OnToolExecutionEnd   ToolExecutionEndCallback
	OnStepEnd            StepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish StepFinishCallback
	OnFinish     FinishCallback
	OnError      ErrorCallback
	OnAbort      AbortCallback
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

	OnChunk              func(chunk provider.StreamChunk)
	OnStart              StartCallback
	OnStepStart          StepStartCallback
	OnToolExecutionStart ToolExecutionStartCallback
	OnToolExecutionEnd   ToolExecutionEndCallback
	OnStepEnd            StepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish StepFinishCallback
	OnFinish     FinishCallback
	OnError      ErrorCallback
	OnAbort      AbortCallback
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

func workflowToolCallFromContent(part types.ToolCallContent) types.ToolCall {
	return types.ToolCall{
		ID:               part.ToolCallID,
		ToolName:         part.ToolName,
		Title:            part.Title,
		Arguments:        part.Arguments,
		RawArguments:     part.Input,
		ProviderExecuted: part.ProviderExecuted,
		ToolMetadata:     part.ToolMetadata,
		ThoughtSignature: part.ThoughtSignature,
		Dynamic:          part.Dynamic,
		Invalid:          part.Invalid,
	}
}

func workflowToolCallIsZero(call types.ToolCall) bool {
	return call.ID == "" && call.ToolName == "" && len(call.Arguments) == 0 && call.RawArguments == "" && !call.ProviderExecuted
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
	case types.NeedsApprovalFunc:
		return v(ctx, call.Arguments)
	default:
		return false
	}
}

func validateWorkflowToolInput(tool *types.Tool, input map[string]interface{}) error {
	if tool == nil || tool.Parameters == nil {
		return nil
	}
	switch s := tool.Parameters.(type) {
	case schema.Schema:
		return s.Validator().Validate(input)
	case map[string]interface{}:
		return schema.NewSimpleJSONSchema(s).Validator().Validate(input)
	default:
		return nil
	}
}

func processWorkflowApprovalResume(ctx context.Context, messages []types.Message, tools []types.Tool, runtimeContext interface{}, toolsContext map[string]interface{}, experimentalSandbox interface{}) ([]types.Message, []provider.StreamChunk, error) {
	if len(messages) == 0 {
		return messages, nil, nil
	}
	toolCallsByID := map[string]types.ToolCall{}
	requestsByApprovalID := map[string]types.ToolCall{}
	responsesByApprovalID := map[string]types.ToolApprovalResponseContent{}
	var responseOrder []string
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleAssistant:
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.ToolCallContent:
					call := workflowToolCallFromContent(p)
					toolCallsByID[call.ID] = call
				case *types.ToolCallContent:
					if p != nil {
						call := workflowToolCallFromContent(*p)
						toolCallsByID[call.ID] = call
					}
				case types.ToolApprovalRequestContent:
					call := p.ToolCall
					if workflowToolCallIsZero(call) {
						call = toolCallsByID[p.ToolCallID]
					}
					if !workflowToolCallIsZero(call) {
						requestsByApprovalID[p.ApprovalID] = call
					}
				case *types.ToolApprovalRequestContent:
					if p != nil {
						call := p.ToolCall
						if workflowToolCallIsZero(call) {
							call = toolCallsByID[p.ToolCallID]
						}
						if !workflowToolCallIsZero(call) {
							requestsByApprovalID[p.ApprovalID] = call
						}
					}
				}
			}
		case types.RoleTool:
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.ToolApprovalResponseContent:
					if _, exists := responsesByApprovalID[p.ApprovalID]; !exists {
						responseOrder = append(responseOrder, p.ApprovalID)
					}
					responsesByApprovalID[p.ApprovalID] = p
				case *types.ToolApprovalResponseContent:
					if p != nil {
						if _, exists := responsesByApprovalID[p.ApprovalID]; !exists {
							responseOrder = append(responseOrder, p.ApprovalID)
						}
						responsesByApprovalID[p.ApprovalID] = *p
					}
				}
			}
		}
	}
	if len(responsesByApprovalID) == 0 {
		return messages, nil, nil
	}

	providerApprovalIDs := map[string]bool{}
	localResults := make([]types.ContentPart, 0)
	prefixChunks := make([]provider.StreamChunk, 0)
	approvedOrder := make([]string, 0, len(responseOrder))
	deniedOrder := make([]string, 0, len(responseOrder))
	for _, approvalID := range responseOrder {
		if responsesByApprovalID[approvalID].Approved {
			approvedOrder = append(approvedOrder, approvalID)
		} else {
			deniedOrder = append(deniedOrder, approvalID)
		}
	}
	for _, approvalID := range approvedOrder {
		response := responsesByApprovalID[approvalID]
		call, ok := requestsByApprovalID[approvalID]
		if !ok {
			continue
		}
		if call.ProviderExecuted {
			providerApprovalIDs[approvalID] = true
			continue
		}
		tool := workflowToolByName(tools, call.ToolName)
		if response.Approved {
			if tool == nil || tool.Execute == nil {
				continue
			}
			if !workflowToolNeedsApproval(ctx, tool, call, messages, toolsContext) {
				localResults = append(localResults, types.ToolResultContent{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: fmt.Sprintf("Tool %q does not require approval", call.ToolName)},
				})
				continue
			}
			if err := validateWorkflowToolInput(tool, call.Arguments); err != nil {
				errText := err.Error()
				localResults = append(localResults, types.ToolResultContent{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: errText},
				})
				continue
			}
			toolContext := interface{}(nil)
			if toolsContext != nil {
				toolContext = toolsContext[call.ToolName]
			}
			result, err := tool.Execute(ctx, call.Arguments, types.ToolExecutionOptions{
				ToolCallID:          call.ID,
				UserContext:         runtimeContext,
				RuntimeContext:      runtimeContext,
				ToolContext:         toolContext,
				Metadata:            map[string]interface{}{},
				ToolMetadata:        call.ToolMetadata,
				ExperimentalSandbox: experimentalSandbox,
			})
			if err != nil {
				errText := err.Error()
				localResults = append(localResults, types.ToolResultContent{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: errText},
				})
				prefixChunks = append(prefixChunks, provider.StreamChunk{
					Type: provider.ChunkTypeToolResult,
					ToolResult: &types.ToolResult{
						ToolCallID: call.ID,
						ToolName:   call.ToolName,
						Input:      call.Arguments,
						Result:     errText,
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
				output, err := tool.ToModelOutput(ctx, types.ToModelOutputOptions{
					ToolCallID: call.ID,
					Input:      call.Arguments,
					Output:     result,
					Result:     result,
					ToolCall:   &call,
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
				},
			})
			continue
		}
	}
	for _, approvalID := range deniedOrder {
		response := responsesByApprovalID[approvalID]
		call, ok := requestsByApprovalID[approvalID]
		if !ok {
			continue
		}
		if call.ProviderExecuted {
			providerApprovalIDs[approvalID] = true
			continue
		}
		localResults = append(localResults, types.ToolResultContent{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Input:      call.Arguments,
			Output:     &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: response.Reason},
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
	}
	if len(localResults) > 0 {
		prefixChunks = append(prefixChunks,
			provider.StreamChunk{Type: provider.ChunkTypeStreamFinish},
			provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
		)
	}
	return cleaned, prefixChunks, nil
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

func (w *WorkflowAgent) makePrepareCall(activeTools []string) func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
	if w.PrepareCall == nil && w.PrepareStep == nil && w.FilterActiveTools == nil && activeTools == nil && w.ActiveTools == nil {
		return nil
	}
	return func(ctx context.Context, c agent.PrepareCallConfig) agent.PrepareCallConfig {
		opts := LanguageModelCallOptions{StepNumber: c.StepNumber, System: c.System, AllowSystemInMessages: c.AllowSystemInMessages, Messages: c.Messages, Tools: c.Tools, ToolChoice: c.ToolChoice, CallOptions: c.CallOptions, Temperature: c.Temperature, MaxTokens: c.MaxTokens, TopP: c.TopP, TopK: c.TopK, FrequencyPenalty: c.FrequencyPenalty, PresencePenalty: c.PresencePenalty, StopSequences: c.StopSequences, Seed: c.Seed, Headers: c.Headers, Reasoning: c.Reasoning, SendReasoning: c.SendReasoning, ProviderOptions: c.ProviderOptions, RuntimeContext: c.RuntimeContext, ToolsContext: c.ToolsContext, ExperimentalSandbox: c.ExperimentalSandbox, PreviousSteps: c.PreviousSteps, AccumulatedUsage: c.AccumulatedUsage, CustomData: c.CustomData}
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
		if w.FilterActiveTools != nil {
			c.Tools = w.FilterActiveTools(ctx, c.StepNumber, c.Tools)
		}
		effectiveActive := activeTools
		if effectiveActive == nil {
			effectiveActive = w.ActiveTools
		}
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
	return agent.NewToolLoopAgent(agent.AgentConfig{
		ID: w.ID, Model: w.Model, System: system, Prompt: w.Prompt, Tools: tools, StopWhen: stopWhen,
		AllowSystemInMessages: allowSystemInMessages,
		CallOptionsSchema:     w.CallOptionsSchema, CallOptions: w.CallOptions, PrepareCall: w.makePrepareCall(ovr.ActiveTools),
		Temperature: w.Temperature, MaxTokens: w.MaxTokens, TopP: w.TopP, TopK: w.TopK, FrequencyPenalty: w.FrequencyPenalty,
		PresencePenalty: w.PresencePenalty, StopSequences: w.StopSequences, Seed: w.Seed, Headers: w.Headers, Reasoning: w.Reasoning,
		SendReasoning: w.SendReasoning, ProviderOptions: w.ProviderOptions, RuntimeContext: runtimeContext, ToolsContext: toolsContext,
		ToolChoice:                  w.ToolChoice,
		Output:                      w.Output,
		Telemetry:                   telemetry,
		Include:                     include,
		ExperimentalSandbox:         sandbox,
		ExperimentalRefineToolInput: refineToolInput,
		OnStart:                     mergeStart(w.OnStart, mergeStart(govr.OnStart, ovr.OnStart)),
		OnStepStartEvent:            mergeStepStart(w.OnStepStart, mergeStepStart(govr.OnStepStart, ovr.OnStepStart)),
		OnToolExecutionStart:        mergeToolStart(w.OnToolExecutionStart, mergeToolStart(govr.OnToolExecutionStart, ovr.OnToolExecutionStart)),
		OnToolExecutionEnd:          mergeToolEnd(w.OnToolExecutionEnd, mergeToolEnd(govr.OnToolExecutionEnd, ovr.OnToolExecutionEnd)),
		OnStepFinishEvent:           mergeStepFinish(resolveStepEnd(w.OnStepEnd, w.OnStepFinish), mergeStepFinish(resolveStepEnd(govr.OnStepEnd, govr.OnStepFinish), resolveStepEnd(ovr.OnStepEnd, ovr.OnStepFinish))),
		OnFinishEvent:               mergeFinish(w.OnFinish, mergeFinish(govr.OnFinish, ovr.OnFinish)),
	})
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
			legacy.OnToolExecutionStart = opts.OnToolCallStart
		}
		legacy.OnToolExecutionEnd = opts.OnToolExecutionEnd
		if legacy.OnToolExecutionEnd == nil {
			legacy.OnToolExecutionEnd = opts.OnToolCallFinish
		}
		legacy.OnStepEnd = opts.OnStepEnd
		legacy.OnStepFinish = opts.OnStepFinish
		legacy.OnFinish = opts.OnFinish
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
	var err error
	opts.Messages, _, err = processWorkflowApprovalResume(ctx, opts.Messages, effectiveWorkflowTools(w, WorkflowStreamOptions{}, opts), firstNonNil(w.RuntimeContext, opts.RuntimeContext), firstNonNilMap(w.ToolsContext, opts.ToolsContext), firstNonNil(w.ExperimentalSandbox, opts.ExperimentalSandbox))
	if err != nil {
		return nil, err
	}
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if ctx != nil && ctx.Err() != nil {
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
	telemetry := w.Telemetry
	if opts.Telemetry != nil {
		telemetry = opts.Telemetry
	}
	call := agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: opts.StopWhen, Output: w.Output, Telemetry: telemetry}
	result, err := a.GenerateAgent(ctx, call)
	if err != nil {
		if ctx != nil && ctx.Err() != nil && onAbort != nil {
			onAbort(ctx, nil)
		}
		if onError := mergeError(w.OnError, opts.OnError); onError != nil {
			onError(ctx, err)
		}
		return nil, err
	}
	return &WorkflowResult{AgentResult: result}, nil
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
			legacy.OnToolExecutionStart = opts.OnToolCallStart
		}
		legacy.OnToolExecutionEnd = opts.OnToolExecutionEnd
		if legacy.OnToolExecutionEnd == nil {
			legacy.OnToolExecutionEnd = opts.OnToolCallFinish
		}
		legacy.OnStepEnd = opts.OnStepEnd
		legacy.OnStepFinish = opts.OnStepFinish
		legacy.OnFinish = opts.OnFinish
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
	var err error
	var prefixChunks []provider.StreamChunk
	opts.Messages, prefixChunks, err = processWorkflowApprovalResume(ctx, opts.Messages, effectiveWorkflowTools(w, opts, WorkflowGenerateOptions{}), firstNonNil(w.RuntimeContext, opts.RuntimeContext), firstNonNilMap(w.ToolsContext, opts.ToolsContext), firstNonNil(w.ExperimentalSandbox, opts.ExperimentalSandbox))
	if err != nil {
		return nil, err
	}
	onAbort := mergeAbort(w.OnAbort, opts.OnAbort)
	if ctx != nil && ctx.Err() != nil {
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
	telemetry := w.Telemetry
	if opts.Telemetry != nil {
		telemetry = opts.Telemetry
	}
	call := agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: opts.Prompt, Messages: opts.Messages, System: system, AllowSystemInMessages: opts.AllowSystemInMessages || w.AllowSystemInMessages, StopWhen: opts.StopWhen, Output: w.Output, Telemetry: telemetry},
		OnChunk:              opts.OnChunk,
		InitialStreamChunks:  prefixChunks,
	}
	stream, err := a.Stream(ctx, call)
	if err != nil {
		if ctx != nil && ctx.Err() != nil && onAbort != nil {
			onAbort(ctx, nil)
		}
		if onError := mergeError(w.OnError, opts.OnError); onError != nil {
			onError(ctx, err)
		}
		return nil, err
	}
	return &WorkflowStreamResult{StreamTextResult: stream}, nil
}
