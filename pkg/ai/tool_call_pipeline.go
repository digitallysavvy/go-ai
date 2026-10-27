package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// NoSuchToolError is returned (as the Error of an invalid tool call) when the
// model calls a tool that is not available in the current step. Mirrors TS
// NoSuchToolError.
type NoSuchToolError struct {
	ToolName string
	// AvailableTools lists the tool names that were available. Nil means that
	// no tools were provided at all.
	AvailableTools []string
}

func (e *NoSuchToolError) Error() string {
	if e.AvailableTools == nil {
		return fmt.Sprintf("Model tried to call unavailable tool '%s'. No tools are available.", e.ToolName)
	}
	return fmt.Sprintf("Model tried to call unavailable tool '%s'. Available tools: %s.", e.ToolName, strings.Join(e.AvailableTools, ", "))
}

// IsNoSuchToolError reports whether err is a NoSuchToolError.
func IsNoSuchToolError(err error) bool {
	var target *NoSuchToolError
	return errors.As(err, &target)
}

// ToolCallRepairError is returned when the configured RepairToolCall function
// fails. Mirrors TS ToolCallRepairError.
type ToolCallRepairError struct {
	// Cause is the error returned by the repair function.
	Cause error
	// OriginalError is the NoSuchToolError or InvalidToolInputError that
	// triggered the repair.
	OriginalError error
}

func (e *ToolCallRepairError) Error() string {
	msg := "unknown error"
	if e.Cause != nil {
		msg = e.Cause.Error()
	}
	return "Error repairing tool call: " + msg
}

func (e *ToolCallRepairError) Unwrap() error { return e.Cause }

// IsToolCallRepairError reports whether err is a ToolCallRepairError.
func IsToolCallRepairError(err error) bool {
	var target *ToolCallRepairError
	return errors.As(err, &target)
}

// ToolCallRepairOptions is passed to a ToolCallRepairFunction. Mirrors the
// options object of the TS ToolCallRepairFunction. Cancellation is signalled
// through the ctx argument (TS abortSignal).
type ToolCallRepairOptions struct {
	// ToolCall is the tool call that failed to parse.
	ToolCall types.ToolCall

	// Tools are the tools available in the step.
	Tools []types.Tool

	// InputSchema returns the JSON schema of a tool's input.
	InputSchema func(toolName string) (map[string]interface{}, error)

	// Instructions is the text instructions (system prompt) of the step.
	Instructions string

	// InstructionMessages are the system-message instructions of the step,
	// when instructions were given as system messages.
	InstructionMessages []types.Message

	// System is the step's system prompt.
	//
	// Deprecated: use Instructions.
	System string

	// Messages are the messages of the current generation step.
	Messages []types.Message

	// Error is the *NoSuchToolError or *InvalidToolInputError that occurred.
	Error error
}

// ToolCallRepairFunction attempts to repair a tool call that failed to parse.
// Return (nil, nil) when the call cannot be repaired. The returned call may
// carry the repaired input as RawArguments (JSON text) or Arguments. Mirrors
// TS ToolCallRepairFunction.
type ToolCallRepairFunction func(ctx context.Context, options ToolCallRepairOptions) (*types.ToolCall, error)

func effectiveRepairToolCall(stable, experimental ToolCallRepairFunction) ToolCallRepairFunction {
	if stable != nil {
		return stable
	}
	return experimental
}

// ParseToolCallOptions configures ParseToolCall.
type ParseToolCallOptions struct {
	ToolCall            types.ToolCall
	Tools               []types.Tool
	RepairToolCall      ToolCallRepairFunction
	Instructions        string
	InstructionMessages []types.Message
	Messages            []types.Message

	// RefineToolInput refines the parsed input by tool name inside the parse
	// step (TS refineToolInput), so an InvalidToolInputError (or a
	// NoSuchToolError) returned by a refiner triggers RepairToolCall like a
	// schema failure does.
	RefineToolInput map[string]ToolInputRefiner
	RuntimeContext  interface{}
	ToolsContext    map[string]interface{}
}

// ParseToolCall resolves a provider tool call against the available tools,
// parses and validates its input, and runs RepairToolCall for NoSuchTool and
// InvalidToolInput errors. Calls that cannot be parsed or repaired are
// returned with Invalid=true, Dynamic=true and Error set. The only returned
// error is a context error when ctx is cancelled (pending repairs are
// abandoned). Mirrors TS parseToolCall.
func ParseToolCall(ctx context.Context, opts ParseToolCallOptions) (types.ToolCall, error) {
	refined, _, err := parseAndRefineToolCall(ctx, opts)
	return refined, err
}

// parseAndRefineToolCall is ParseToolCall that also returns the call as it
// was parsed, before RefineToolInput ran (the approval inputSchemaInput).
func parseAndRefineToolCall(ctx context.Context, opts ParseToolCallOptions) (types.ToolCall, types.ToolCall, error) {
	call := opts.ToolCall
	parsed, pre, err := parseToolCallWithRepair(ctx, opts)
	if err == nil {
		return parsed, pre, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return types.ToolCall{}, types.ToolCall{}, ctxErr
	}
	tool := findToolForCall(call, opts.Tools)
	invalid := call
	if args, parseErr := toolCallInputArguments(call); parseErr == nil {
		invalid.Arguments = args
	}
	invalid.Dynamic = true
	invalid.Invalid = true
	invalid.Error = err
	if tool != nil {
		invalid.Title = tool.Title
		if tool.Metadata != nil {
			invalid.ToolMetadata = cloneStringAnyMap(tool.Metadata)
		}
	}
	return invalid, invalid, nil
}

// refineParsedToolCall runs the refiner registered for the call's tool; the
// refiner's error is returned (TS refineParsedToolCallInput).
func refineParsedToolCall(ctx context.Context, parsed types.ToolCall, opts ParseToolCallOptions) (types.ToolCall, error) {
	refiner := opts.RefineToolInput[parsed.ToolName]
	if refiner == nil {
		return parsed, nil
	}
	var tool *types.Tool
	for i := range opts.Tools {
		if opts.Tools[i].Name == parsed.ToolName {
			tool = &opts.Tools[i]
			break
		}
	}
	input, err := refiner(ctx, ToolInputRefinementOptions{
		ToolCall:       parsed,
		Tool:           tool,
		RuntimeContext: opts.RuntimeContext,
		ToolsContext:   opts.ToolsContext,
	})
	if err != nil {
		return types.ToolCall{}, err
	}
	if input != nil {
		parsed.Arguments = input
	}
	return parsed, nil
}

func parseToolCallWithRepair(ctx context.Context, opts ParseToolCallOptions) (types.ToolCall, types.ToolCall, error) {
	call := opts.ToolCall
	refine := func(parsed types.ToolCall) (types.ToolCall, types.ToolCall, error) {
		refined, err := refineParsedToolCall(ctx, parsed, opts)
		return refined, parsed, err
	}
	if len(opts.Tools) == 0 {
		// TS parseToolCall: `toolCall.providerExecuted && toolCall.dynamic`.
		// A provider-executed call that isn't marked dynamic (e.g. Anthropic
		// web_search/web_fetch, which rely on a matching registered tool)
		// must still raise NoSuchToolError when no tools are registered at
		// all — it is not implicitly treated as a dynamic provider tool.
		if call.ProviderExecuted && call.Dynamic {
			parsed, err := parseProviderExecutedDynamicToolCall(call)
			if err != nil {
				return types.ToolCall{}, types.ToolCall{}, err
			}
			return refine(parsed)
		}
		return types.ToolCall{}, types.ToolCall{}, &NoSuchToolError{ToolName: call.ToolName}
	}

	parsed, err := doParseToolCall(call, opts.Tools)
	if err == nil {
		// The refiner runs inside the same try block as the parse (TS), so
		// its NoSuchTool/InvalidToolInput errors are repaired too.
		var refined, pre types.ToolCall
		if refined, pre, err = refine(parsed); err == nil {
			return refined, pre, nil
		}
	}
	return repairParsedToolCall(ctx, opts, err, refine)
}

func repairParsedToolCall(ctx context.Context, opts ParseToolCallOptions, err error, refine func(types.ToolCall) (types.ToolCall, types.ToolCall, error)) (types.ToolCall, types.ToolCall, error) {
	if opts.RepairToolCall == nil || (!IsNoSuchToolError(err) && !IsInvalidToolInputError(err)) {
		return types.ToolCall{}, types.ToolCall{}, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return types.ToolCall{}, types.ToolCall{}, ctxErr
	}

	repaired, repairErr := waitForRepair(ctx, opts, err)
	if repairErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return types.ToolCall{}, types.ToolCall{}, ctxErr
		}
		return types.ToolCall{}, types.ToolCall{}, &ToolCallRepairError{Cause: repairErr, OriginalError: err}
	}
	if repaired == nil {
		return types.ToolCall{}, types.ToolCall{}, err
	}
	parsedRepaired, err := doParseToolCall(*repaired, opts.Tools)
	if err != nil {
		return types.ToolCall{}, types.ToolCall{}, err
	}
	refinedRepaired, pre, err := refine(parsedRepaired)
	if err != nil {
		return types.ToolCall{}, types.ToolCall{}, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return types.ToolCall{}, types.ToolCall{}, ctxErr
	}
	return refinedRepaired, pre, nil
}

// waitForRepair runs the repair function and returns as soon as ctx is
// cancelled, even when the repair function ignores ctx (TS
// waitForPromiseWithAbortSignal).
func waitForRepair(ctx context.Context, opts ParseToolCallOptions, parseErr error) (*types.ToolCall, error) {
	tools := opts.Tools
	repairOpts := ToolCallRepairOptions{
		ToolCall: opts.ToolCall,
		Tools:    tools,
		InputSchema: func(toolName string) (map[string]interface{}, error) {
			tool := findToolForCall(types.ToolCall{ToolName: toolName}, tools)
			if tool == nil {
				return nil, &NoSuchToolError{ToolName: toolName, AvailableTools: toolNames(tools)}
			}
			return toolInputJSONSchema(tool), nil
		},
		Instructions:        opts.Instructions,
		InstructionMessages: opts.InstructionMessages,
		System:              opts.Instructions,
		Messages:            opts.Messages,
		Error:               parseErr,
	}
	if ctx.Done() == nil {
		return opts.RepairToolCall(ctx, repairOpts)
	}
	type repairResult struct {
		call *types.ToolCall
		err  error
	}
	done := make(chan repairResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- repairResult{err: fmt.Errorf("repair function panicked: %v", r)}
			}
		}()
		repairedCall, err := opts.RepairToolCall(ctx, repairOpts)
		done <- repairResult{call: repairedCall, err: err}
	}()
	select {
	case result := <-done:
		return result.call, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func doParseToolCall(call types.ToolCall, tools []types.Tool) (types.ToolCall, error) {
	tool := findToolForCall(call, tools)
	if tool == nil {
		// Provider-executed dynamic tools are not part of the tool list (TS:
		// `toolCall.providerExecuted && toolCall.dynamic`). A call that is
		// providerExecuted but not dynamic still raises NoSuchToolError when
		// unmatched, matching TS doParseToolCall exactly.
		if call.ProviderExecuted && call.Dynamic {
			return parseProviderExecutedDynamicToolCall(call)
		}
		return types.ToolCall{}, &NoSuchToolError{ToolName: call.ToolName, AvailableTools: toolNames(tools)}
	}

	args, err := toolCallInputArguments(call)
	if err != nil {
		return types.ToolCall{}, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: err}
	}
	if validator := toolCallInputValidator(tool); validator != nil {
		// TS's safeParseJSON({text, schema}) validates through zod, whose
		// .parse() fills any .default() values before the caller (and, in
		// turn, tool.execute) ever sees the input. schema.Validate alone
		// doesn't fill defaults, so apply them first -- matching Object.
		args = applyToolCallInputDefaults(args, validator)
		if err := validator.Validate(args); err != nil {
			return types.ToolCall{}, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: err}
		}
	}

	parsed := call
	parsed.Arguments = args
	parsed.Invalid = false
	parsed.Error = nil
	parsed.Title = tool.Title
	if tool.Metadata != nil {
		parsed.ToolMetadata = cloneStringAnyMap(tool.Metadata)
	}
	parsed.Dynamic = tool.Type == "dynamic"
	return parsed, nil
}

func parseProviderExecutedDynamicToolCall(call types.ToolCall) (types.ToolCall, error) {
	args, err := toolCallInputArguments(call)
	if err != nil {
		return types.ToolCall{}, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: err}
	}
	parsed := call
	parsed.Arguments = args
	parsed.ProviderExecuted = true
	parsed.ProviderExecutedSet = true
	parsed.Dynamic = true
	return parsed, nil
}

// toolCallInputArguments returns the tool input as an object. Raw streamed JSON
// input takes precedence; an empty input is treated as an empty object (many
// models emit empty strings for tools without arguments).
func toolCallInputArguments(call types.ToolCall) (map[string]interface{}, error) {
	raw := call.RawArguments
	// TS: toolCall.input.trim() === '' (JavaScript whitespace).
	if providerutils.JSTrim(raw) == "" {
		if call.Arguments == nil {
			return map[string]interface{}{}, nil
		}
		return call.Arguments, nil
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case map[string]interface{}:
		return v, nil
	case nil:
		return map[string]interface{}{}, nil
	default:
		return nil, fmt.Errorf("tool input must be a JSON object, got %T", value)
	}
}

func toolCallInputText(call types.ToolCall) string {
	if call.RawArguments != "" {
		return call.RawArguments
	}
	if call.Arguments == nil {
		return ""
	}
	data, err := jsonstr.Marshal(call.Arguments)
	if err != nil {
		return ""
	}
	return string(data)
}

// toolCallInputValidator returns a JSON-schema validator for the tool input,
// or nil when the tool has no JSON schema that can validate a decoded map.
// Provider-executed tools are validated by the provider.
func toolCallInputValidator(tool *types.Tool) schema.Validator {
	if tool == nil || tool.ProviderExecuted || tool.Type == "provider" {
		return nil
	}
	if isHintedParameters(tool.Parameters) {
		if m, ok := schema.ParametersMap(tool.Parameters); ok && len(m) > 0 {
			return schema.NewJSONSchema(m)
		}
		return nil
	}
	switch s := tool.Parameters.(type) {
	case map[string]interface{}:
		if len(s) == 0 {
			return nil
		}
		return schema.NewJSONSchema(s)
	case schema.Schema:
		if s == nil {
			return nil
		}
		if v, ok := s.Validator().(*schema.JSONSchemaValidator); ok {
			return v
		}
	}
	return nil
}

// applyToolCallInputDefaults fills any JSON-Schema "default" values missing
// from args, mirroring zod's .default() handling during TS's
// safeParseJSON/safeValidateTypes (see parse-tool-call.ts's doParseToolCall).
// It never mutates args; on the rare failure to represent the defaulted
// result as an object (e.g. a non-object root schema), it returns args
// unchanged rather than losing data.
func applyToolCallInputDefaults(args map[string]interface{}, validator schema.Validator) map[string]interface{} {
	defaulted := schema.ApplyDefaults(args, schema.NewSimpleJSONSchema(validator.JSONSchema()))
	if obj, ok := defaulted.(map[string]interface{}); ok {
		return obj
	}
	return args
}

func toolInputJSONSchema(tool *types.Tool) map[string]interface{} {
	if isHintedParameters(tool.Parameters) {
		if m, ok := schema.ParametersMap(tool.Parameters); ok {
			return m
		}
	}
	switch s := tool.Parameters.(type) {
	case map[string]interface{}:
		return s
	case schema.Schema:
		if s != nil {
			return s.Validator().JSONSchema()
		}
	}
	return map[string]interface{}{"type": "object"}
}

func toolNames(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// parseToolCalls parses every tool call of a step (TS Promise.all over
// parseToolCall).
func parseToolCalls(ctx context.Context, calls []types.ToolCall, opts ParseToolCallOptions) (refined, preRefinement []types.ToolCall, err error) {
	if len(calls) == 0 {
		return calls, calls, nil
	}
	refined = make([]types.ToolCall, len(calls))
	preRefinement = make([]types.ToolCall, len(calls))
	for i, call := range calls {
		opts.ToolCall = call
		result, pre, err := parseAndRefineToolCall(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		refined[i] = result
		preRefinement[i] = pre
	}
	return refined, preRefinement, nil
}

// invokeToolInputCallbacks calls OnInputStart and then OnInputAvailable for
// every valid tool call of a non-streaming step, in order (TS generateText
// "notify the tools that the tool calls are available").
func invokeToolInputCallbacks(ctx context.Context, calls []types.ToolCall, tools []types.Tool, messages []types.Message, toolsContext map[string]interface{}) error {
	for _, call := range calls {
		if call.Invalid {
			continue
		}
		tool := findToolForCall(call, tools)
		if tool == nil || (tool.OnInputStart == nil && tool.OnInputAvailable == nil) {
			continue
		}
		toolContext, err := validateToolContextFor(tool, call.ToolName, toolsContext[call.ToolName])
		if err != nil {
			return err
		}
		if tool.OnInputStart != nil {
			if err := tool.OnInputStart(ctx, types.OnInputStartOptions{
				ToolCallID: call.ID,
				Messages:   messages,
				Context:    toolContext,
			}); err != nil {
				return err
			}
		}
		if tool.OnInputAvailable != nil {
			if err := tool.OnInputAvailable(ctx, types.OnInputAvailableOptions{
				Input:      call.Arguments,
				Value:      call.Arguments, //nolint:staticcheck // deprecated alias of Input, kept for callbacks still reading it
				ToolCallID: call.ID,
				Messages:   messages,
				Context:    toolContext,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// InvokeToolInputCallbacks is the exported form of the non-streaming tool
// input notification for the legacy agent loop: OnInputStart and then
// OnInputAvailable for every valid call, in order.
func InvokeToolInputCallbacks(ctx context.Context, calls []types.ToolCall, tools []types.Tool, messages []types.Message, toolsContext map[string]interface{}) error {
	return invokeToolInputCallbacks(ctx, calls, tools, messages, toolsContext)
}

type validatedToolContext struct {
	value interface{}
	err   error
}

type ongoingToolInput struct {
	toolName string
	contexts map[string]validatedToolContext
}

// streamToolInputCallbacks invokes tool input lifecycle callbacks from a
// model stream: OnInputStart on tool-input-start, OnInputDelta on
// tool-input-delta and OnInputAvailable on (valid) tool-call chunks. The tool
// for OnInputAvailable is resolved from the tool-call chunk so repaired calls
// reach the repaired tool. Mirrors TS invokeToolCallbacksFromStream.
type streamToolInputCallbacks struct {
	tools        []types.Tool
	messages     []types.Message
	toolsContext map[string]interface{}
	ongoing      map[string]*ongoingToolInput
}

func newStreamToolInputCallbacks(tools []types.Tool, messages []types.Message, toolsContext map[string]interface{}) *streamToolInputCallbacks {
	return &streamToolInputCallbacks{
		tools:        tools,
		messages:     messages,
		toolsContext: toolsContext,
		ongoing:      map[string]*ongoingToolInput{},
	}
}

func (s *streamToolInputCallbacks) validatedContext(toolCallID, toolName string) (interface{}, error) {
	ongoing := s.ongoing[toolCallID]
	if ongoing != nil {
		if cached, ok := ongoing.contexts[toolName]; ok {
			return cached.value, cached.err
		}
	}
	tool := findToolForCall(types.ToolCall{ToolName: toolName}, s.tools)
	value, err := validateToolContextFor(tool, toolName, s.toolsContext[toolName])
	if ongoing != nil {
		ongoing.contexts[toolName] = validatedToolContext{value: value, err: err}
	}
	return value, err
}

func streamChunkToolCallID(chunk provider.StreamChunk) string {
	if chunk.ID != "" {
		return chunk.ID
	}
	if chunk.ToolCall != nil {
		return chunk.ToolCall.ID
	}
	return ""
}

func (s *streamToolInputCallbacks) handle(ctx context.Context, chunk provider.StreamChunk) error {
	if s == nil || len(s.tools) == 0 {
		return nil
	}
	switch chunk.Type {
	case provider.ChunkTypeToolInputStart:
		id := streamChunkToolCallID(chunk)
		toolName := ""
		if chunk.ToolCall != nil {
			toolName = chunk.ToolCall.ToolName
		}
		s.ongoing[id] = &ongoingToolInput{toolName: toolName, contexts: map[string]validatedToolContext{}}
		tool := findToolForCall(types.ToolCall{ToolName: toolName}, s.tools)
		if tool != nil && tool.OnInputStart != nil {
			toolContext, err := s.validatedContext(id, toolName)
			if err != nil {
				return err
			}
			return tool.OnInputStart(ctx, types.OnInputStartOptions{ToolCallID: id, Messages: s.messages, Context: toolContext})
		}
	case provider.ChunkTypeToolInputDelta:
		id := streamChunkToolCallID(chunk)
		ongoing := s.ongoing[id]
		if ongoing == nil {
			return nil
		}
		tool := findToolForCall(types.ToolCall{ToolName: ongoing.toolName}, s.tools)
		if tool != nil && tool.OnInputDelta != nil {
			toolContext, err := s.validatedContext(id, ongoing.toolName)
			if err != nil {
				return err
			}
			return tool.OnInputDelta(ctx, types.OnInputDeltaOptions{
				InputTextDelta: chunk.Text,
				Delta:          chunk.Text, //nolint:staticcheck // deprecated alias of InputTextDelta, kept for callbacks still reading it
				ToolCallID:     id,
				Messages:       s.messages,
				Context:        toolContext,
			})
		}
	case provider.ChunkTypeToolCall:
		if chunk.ToolCall == nil {
			return nil
		}
		call := *chunk.ToolCall
		tool := findToolForCall(call, s.tools)
		if !call.Invalid && tool != nil && tool.OnInputAvailable != nil {
			toolContext, err := s.validatedContext(call.ID, call.ToolName)
			delete(s.ongoing, call.ID)
			if err != nil {
				return err
			}
			return tool.OnInputAvailable(ctx, types.OnInputAvailableOptions{
				Input:      call.Arguments,
				Value:      call.Arguments, //nolint:staticcheck // deprecated alias of Input, kept for callbacks still reading it
				ToolCallID: call.ID,
				Messages:   s.messages,
				Context:    toolContext,
			})
		}
		delete(s.ongoing, call.ID)
	}
	return nil
}

// invalidToolCallError returns the error tool result synthesized for an
// invalid client tool call (TS "insert error tool outputs for invalid tool
// calls"). Invalid provider-executed calls never get a client tool error.
func invalidToolCallResult(call types.ToolCall) types.ToolResult {
	err := call.Error
	if err == nil {
		err = fmt.Errorf("invalid tool call")
	}
	return types.ToolResult{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.Arguments,
		Error:            err,
		Dynamic:          true,
		ProviderMetadata: call.ProviderMetadata,
		ToolMetadata:     call.ToolMetadata,
	}
}

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *NoSuchToolError) ErrorName() string { return "AI_NoSuchToolError" }

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *ToolCallRepairError) ErrorName() string { return "AI_ToolCallRepairError" }

// isHintedParameters reports whether a tool's Parameters is a
// jsonorder.Hinted, in its value or pointer form.
func isHintedParameters(p interface{}) bool {
	switch p.(type) {
	case jsonorder.Hinted, *jsonorder.Hinted:
		return true
	}
	return false
}
