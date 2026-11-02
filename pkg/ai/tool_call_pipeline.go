package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// Error is TS `Error repairing tool call: ${getErrorMessage(cause)}`, where
// getErrorMessage is the cause's toString() ("Error: <message>", or the TS
// error name for SDK errors) and "unknown error" without a cause.
func (e *ToolCallRepairError) Error() string {
	return "Error repairing tool call: " + providerutils.ToolErrorText(e.Cause)
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

// parseAndRefineToolCall is ParseToolCall that also returns the call with
// the input the schema received, before validation and RefineToolInput (the
// approval inputSchemaInput, TS parseResult.rawValue).
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
	// refine returns the refined call and the call as the schema received
	// it (rawArgs, the JSON-parsed input before validation): TS
	// setToolCallInputSchemaInput(toolCall, parseResult.rawValue), which
	// feeds the approval request's inputSchemaInput. A nil rawArgs keeps the
	// parsed input (provider-executed dynamic calls have no schema input).
	refine := func(parsed types.ToolCall, rawArgs map[string]interface{}) (types.ToolCall, types.ToolCall, error) {
		refined, err := refineParsedToolCall(ctx, parsed, opts)
		pre := parsed
		if rawArgs != nil {
			pre.Arguments = rawArgs
		}
		return refined, pre, err
	}
	// TS `tools == null`: a nil slice means no tools at all, while an empty,
	// non-nil set (ActiveTools filtering every tool out) still goes through
	// doParseToolCall, so its NoSuchToolError lists the (empty) available
	// tools and RepairToolCall runs.
	if opts.Tools == nil {
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
			return refine(parsed, nil)
		}
		return types.ToolCall{}, types.ToolCall{}, &NoSuchToolError{ToolName: call.ToolName}
	}

	parsed, rawArgs, err := doParseToolCall(call, opts.Tools)
	if err == nil {
		// The refiner runs inside the same try block as the parse (TS), so
		// its NoSuchTool/InvalidToolInput errors are repaired too.
		var refined, pre types.ToolCall
		if refined, pre, err = refine(parsed, rawArgs); err == nil {
			return refined, pre, nil
		}
	}
	return repairParsedToolCall(ctx, opts, err, refine)
}

func repairParsedToolCall(ctx context.Context, opts ParseToolCallOptions, err error, refine func(types.ToolCall, map[string]interface{}) (types.ToolCall, types.ToolCall, error)) (types.ToolCall, types.ToolCall, error) {
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
	parsedRepaired, rawRepaired, err := doParseToolCall(*repaired, opts.Tools)
	if err != nil {
		return types.ToolCall{}, types.ToolCall{}, err
	}
	refinedRepaired, pre, err := refine(parsedRepaired, rawRepaired)
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
				// TS asSchema(getOwn(tools, toolName)?.inputSchema): an
				// unknown tool has the empty object schema, not an error.
				return emptyToolInputJSONSchema(), nil
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

// doParseToolCall also returns the JSON-parsed input before schema
// validation (TS parseResult.rawValue), nil for a provider-executed dynamic
// call.
func doParseToolCall(call types.ToolCall, tools []types.Tool) (types.ToolCall, map[string]interface{}, error) {
	tool := findToolForCall(call, tools)
	if tool == nil {
		// Provider-executed dynamic tools are not part of the tool list (TS:
		// `toolCall.providerExecuted && toolCall.dynamic`). A call that is
		// providerExecuted but not dynamic still raises NoSuchToolError when
		// unmatched, matching TS doParseToolCall exactly.
		if call.ProviderExecuted && call.Dynamic {
			parsed, err := parseProviderExecutedDynamicToolCall(call)
			return parsed, nil, err
		}
		return types.ToolCall{}, nil, &NoSuchToolError{ToolName: call.ToolName, AvailableTools: toolNames(tools)}
	}

	args, err := toolCallInputArguments(call)
	if err != nil {
		return types.ToolCall{}, nil, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: toolInputParseError(call, err)}
	}
	// The validator may fill or transform values in place: keep a copy.
	rawArgs := cloneSchemaMap(args)
	if tool.ValidateInput != nil {
		// TS jsonSchema(schema, {validate}): the tool's own validator replaces
		// the schema check, and its value is the input the call carries.
		validated, err := tool.ValidateInput(args)
		if err != nil {
			return types.ToolCall{}, nil, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: newToolInputTypeValidationError(call, args, err)}
		}
		if m, ok := validated.(map[string]interface{}); ok {
			args = m
		}
	} else if validator := toolCallInputValidator(tool); validator != nil {
		// TS's safeParseJSON({text, schema}) validates through zod, whose
		// .parse() fills any .default() values before the caller (and, in
		// turn, tool.execute) ever sees the input. schema.Validate alone
		// doesn't fill defaults, so apply them first -- matching Object.
		args = applyToolCallInputDefaults(args, validator)
		if err := validator.Validate(args); err != nil {
			// TS safeParseJSON({text, schema}) reports a schema failure as a
			// TypeValidationError of the parsed (pre-default) value.
			return types.ToolCall{}, nil, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: newToolInputTypeValidationError(call, rawArgs, err)}
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
	return parsed, rawArgs, nil
}

// toolInputTypeValidationError is the cause of an InvalidToolInputError for a
// schema failure (Tool.ValidateInput or the Parameters schema): TS
// safeValidateTypes / safeParseJSON wrap the validation error in a
// TypeValidationError, and InvalidToolInputError shows it with
// getErrorMessage (its toString, "AI_TypeValidationError: Type validation
// failed: Value: <input>.\nError message: Error: <message>"). The embedded
// *ValidationError holds the TS message text; errors.As reaches it.
type toolInputTypeValidationError struct {
	*providererrors.ValidationError
}

func (e *toolInputTypeValidationError) Unwrap() error { return e.ValidationError }

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *toolInputTypeValidationError) ErrorName() string { return "AI_TypeValidationError" }

// toolInputJSONParseError is the cause of an InvalidToolInputError for tool
// input that is not JSON (TS safeParseJSON: a JSONParseError, shown as
// "AI_JSONParseError: JSON parsing failed: Text: <input>.\nError message:
// <parser error>"). errors.As reaches the embedded *JSONParseError.
type toolInputJSONParseError struct {
	*providererrors.JSONParseError
}

func (e *toolInputJSONParseError) Unwrap() error { return e.JSONParseError }

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *toolInputJSONParseError) ErrorName() string { return "AI_JSONParseError" }

// toolInputParseError types a toolCallInputArguments failure: text that is
// not JSON is a JSONParseError; JSON that is not an object fails the input
// schema (a TypeValidationError of the parsed value).
func toolInputParseError(call types.ToolCall, err error) error {
	if errors.Is(err, errToolInputNotObject) {
		return newToolInputTypeValidationError(call, nil, err)
	}
	return &toolInputJSONParseError{providererrors.NewJSONParseError(call.RawArguments, err)}
}

// errToolInputNotObject marks tool input that is JSON but not an object.
var errToolInputNotObject = errors.New("tool input must be a JSON object")

func newToolInputTypeValidationError(call types.ToolCall, args map[string]interface{}, err error) error {
	// TS JSON.stringify(value) of the parsed input: the raw input's keys in
	// JS enumeration order (array-index keys first, a repeated key at its
	// first position with its last value), strings re-encoded.
	var value interface{} = args
	if raw := call.RawArguments; providerutils.JSTrim(raw) != "" {
		if v, ok := jsonorder.Parse([]byte(raw)); ok {
			value = jsonorder.Stringify(v)
		}
	}
	return &toolInputTypeValidationError{providererrors.NewValidationErrorWithContext(value, providerutils.ToolErrorText(err), err, &providererrors.ValidationContext{})}
}

func parseProviderExecutedDynamicToolCall(call types.ToolCall) (types.ToolCall, error) {
	args, err := toolCallInputArguments(call)
	if err != nil {
		return types.ToolCall{}, &InvalidToolInputError{ToolName: call.ToolName, ToolInput: toolCallInputText(call), Cause: toolInputParseError(call, err)}
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
	case string:
		// A custom (text) tool call: providers carry TS's
		// `input: JSON.stringify(text)` as RawArguments and the same text as
		// Arguments {"input": text}.
		if call.Arguments != nil {
			return call.Arguments, nil
		}
		return nil, fmt.Errorf("%w, got %T", errToolInputNotObject, value)
	default:
		return nil, fmt.Errorf("%w, got %T", errToolInputNotObject, value)
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

// toolCallInputValidator returns the validator for the tool input, or nil
// when the tool has no input schema. TS parseToolCall validates the input
// with the tool's inputSchema whatever kind of schema it is, so every
// schema.Schema / schema.Validator is used: JSON Schema, OrderedSchema,
// struct-tag schemas (which decode the input map into the struct and check
// its validate: tags) and custom validators. Provider tools (provider-executed
// or not) are validated too: an invalid provider-executed call becomes invalid
// and dynamic without a client tool error. Provider tools without Parameters
// are not validated.
func toolCallInputValidator(tool *types.Tool) schema.Validator {
	if tool == nil {
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
		if isNilInterfaceValue(s) {
			return nil
		}
		return toolSchemaValidator(s.Validator())
	case schema.Validator:
		return toolSchemaValidator(s)
	}
	return nil
}

// toolSchemaValidator returns v, or nil when v is nil or a typed nil pointer.
func toolSchemaValidator(v schema.Validator) schema.Validator {
	if isNilInterfaceValue(v) {
		return nil
	}
	return v
}

// isNilInterfaceValue reports whether v is nil or holds a nil pointer.
func isNilInterfaceValue(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
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
	case schema.Validator:
		if s != nil {
			return s.JSONSchema()
		}
	}
	return emptyToolInputJSONSchema()
}

// emptyToolInputJSONSchema is TS asSchema(undefined).jsonSchema, the schema
// of a tool without an input schema (or of an unknown tool).
func emptyToolInputJSONSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
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
		if tool == nil {
			continue
		}
		if err := invokeToolInputCallbacksFor(ctx, call, tool, messages, toolsContext); err != nil {
			return err
		}
	}
	return nil
}

// invokeToolInputCallbacksFor runs one valid call's OnInputStart and then
// OnInputAvailable, validating the tool context first when either is set.
func invokeToolInputCallbacksFor(ctx context.Context, call types.ToolCall, tool *types.Tool, messages []types.Message, toolsContext map[string]interface{}) error {
	if tool.OnInputStart == nil && tool.OnInputAvailable == nil {
		return nil
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
	return nil
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
// The tool error has no provider metadata: TS builds it without the call's
// providerMetadata (generateText and streamLanguageModelCall), so the tool
// message carries no providerOptions.
func invalidToolCallResult(call types.ToolCall) types.ToolResult {
	err := call.Error
	if err == nil {
		err = fmt.Errorf("invalid tool call")
	}
	return types.ToolResult{
		ToolCallID:   call.ID,
		ToolName:     call.ToolName,
		Title:        call.Title,
		Input:        call.Arguments,
		Error:        err,
		Dynamic:      true,
		ToolMetadata: call.ToolMetadata,
		// TS streamLanguageModelCall gives this tool-error the call's title.
		InvalidToolCall: true,
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
