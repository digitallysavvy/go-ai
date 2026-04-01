package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// objectCallCtx carries call-scoped metadata through the internal mode functions
// so structured callback events can be correlated across OnStepStart/OnStepFinish/OnFinish.
type objectCallCtx struct {
	callID   string
	funcID   string
	metadata map[string]interface{}
}

// extractObjectReasoning pulls the first ReasoningContent text from a
// GenerateResult's Content slice. Returns empty string when none is present.
// Mirrors the TS SDK's extractReasoningContent helper used in generateObject.
func extractObjectReasoning(result *types.GenerateResult) string {
	if result == nil {
		return ""
	}
	for _, part := range result.Content {
		if rc, ok := part.(types.ReasoningContent); ok {
			return rc.Text
		}
	}
	return ""
}

// schemaToMap extracts the JSON Schema representation from a schema.Schema.
// Returns nil if the schema is nil.
func schemaToMap(s schema.Schema) map[string]interface{} {
	if s == nil {
		return nil
	}
	type jsonSchemaProvider interface {
		JSONSchema() map[string]interface{}
	}
	if p, ok := s.(jsonSchemaProvider); ok {
		return p.JSONSchema()
	}
	return nil
}

// ObjectOutputMode defines the output mode for structured generation
type ObjectOutputMode string

const (
	// ObjectModeObject returns a single object (default)
	ObjectModeObject ObjectOutputMode = "object"

	// ObjectModeArray returns an array of objects (streaming)
	ObjectModeArray ObjectOutputMode = "array"

	// ObjectModeEnum forces selection from enum values
	ObjectModeEnum ObjectOutputMode = "enum"

	// ObjectModeNoSchema returns raw JSON without validation
	ObjectModeNoSchema ObjectOutputMode = "no-schema"
)

// GenerateObjectOptions contains options for structured object generation
type GenerateObjectOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt string
	Messages []types.Message
	System string

	// Schema for the output object (not required for no-schema mode)
	Schema schema.Schema

	// Output mode - object, array, enum, or no-schema
	OutputMode ObjectOutputMode

	// Enum values (required for enum mode)
	EnumValues []string

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Seed             *int
	MaxRetries       int

	// Additional HTTP headers sent with the request.
	Headers map[string]string

	// Additional provider-specific options.
	ProviderOptions map[string]interface{}

	// SchemaName is an optional name for the output schema.
	SchemaName string

	// SchemaDescription is an optional description for the output schema.
	SchemaDescription string

	// Telemetry configuration for observability
	ExperimentalTelemetry *TelemetrySettings

	// ========================================================================
	// Structured Event Callbacks (P1-7)
	// These callbacks receive typed event structs and are panic-safe.
	// They fire in addition to (not instead of) the legacy OnFinish callback.
	// ========================================================================

	// ExperimentalOnStart is called once before any LLM call is made.
	ExperimentalOnStart func(ctx context.Context, e ObjectOnStartEvent)

	// ExperimentalOnStepStart is called just before the provider is called.
	ExperimentalOnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// OnStepFinish is called after the provider returns, before JSON parsing.
	OnStepFinish func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnFinishEvent is called when the operation completes with a typed event.
	// For GenerateObject, the event Error field is always nil.
	OnFinishEvent func(ctx context.Context, e ObjectOnFinishEvent)

	// Legacy callback — kept for backward compatibility.
	// Prefer OnFinishEvent for structured access.
	OnFinish func(ctx context.Context, result *GenerateObjectResult, userContext interface{})

	// ExperimentalContext allows passing custom context through generation lifecycle
	ExperimentalContext interface{}
}

// GenerateObjectResult contains the result of object generation
type GenerateObjectResult struct {
	// The generated object (unmarshaled JSON)
	Object interface{}

	// For array mode: array of objects
	Array []interface{}

	// For enum mode: selected enum value
	EnumValue string

	// Raw JSON text
	Text string

	// Finish reason
	FinishReason types.FinishReason

	// Token usage information
	Usage types.Usage

	// Warnings from the provider
	Warnings []types.Warning

	// Reasoning text generated by the model (if any).
	// Mirrors GenerateObjectResult.reasoning in the TS SDK.
	Reasoning string

	// Request holds raw request metadata (e.g. body sent to the provider).
	// Mirrors GenerateObjectResult.request in the TS SDK.
	Request map[string]interface{}

	// Response holds raw response metadata (e.g. body received from the provider).
	// Mirrors GenerateObjectResult.response in the TS SDK.
	Response map[string]interface{}

	// ProviderMetadata holds provider-specific metadata.
	// Mirrors GenerateObjectResult.providerMetadata in the TS SDK.
	ProviderMetadata map[string]interface{}
}

// GenerateObject performs structured object generation.
// The output is a JSON object that conforms to the provided schema.
// Supports multiple output modes: object, array, enum, no-schema.
//
// Deprecated: Use GenerateText with ObjectOutput[T](), ArrayOutput[T](),
// ChoiceOutput(), or JSONOutput() instead. The Output option provides
// type-safe structured generation without a separate function call.
func GenerateObject(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.IsEnabled {
		tracer := telemetry.GetTracer(opts.ExperimentalTelemetry)

		// Create top-level ai.generateObject span
		spanName := "ai.generateObject"
		if opts.ExperimentalTelemetry.FunctionID != "" {
			spanName = spanName + "." + opts.ExperimentalTelemetry.FunctionID
		}

		ctx, span = tracer.Start(ctx, spanName)
		defer span.End()

		// Add base telemetry attributes
		span.SetAttributes(
			attribute.String("ai.operationId", "ai.generateObject"),
			attribute.String("ai.model.provider", opts.Model.Provider()),
			attribute.String("ai.model.id", opts.Model.ModelID()),
			attribute.String("ai.settings.output", string(opts.OutputMode)),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
		for key, value := range opts.ExperimentalTelemetry.Metadata {
			span.SetAttributes(attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}

		// Record prompt if enabled
		if opts.ExperimentalTelemetry.RecordInputs && opts.Prompt != "" {
			span.SetAttributes(attribute.String("ai.prompt", opts.Prompt))
		}
	}

	// Set default output mode
	if opts.OutputMode == "" {
		opts.OutputMode = ObjectModeObject
	}

	// Validate mode-specific requirements.
	// Mirrors TS SDK's validateObjectGenerationInput() in validate-object-generation-input.ts.
	switch opts.OutputMode {
	case ObjectModeObject:
		if opts.Schema == nil {
			return nil, fmt.Errorf("schema is required for object output")
		}
		if len(opts.EnumValues) != 0 {
			return nil, fmt.Errorf("enum values are not supported for object output")
		}
	case ObjectModeArray:
		if opts.Schema == nil {
			return nil, fmt.Errorf("element schema is required for array output")
		}
		if len(opts.EnumValues) != 0 {
			return nil, fmt.Errorf("enum values are not supported for array output")
		}
	case ObjectModeEnum:
		if opts.Schema != nil {
			return nil, fmt.Errorf("schema is not supported for enum output")
		}
		if opts.SchemaDescription != "" {
			return nil, fmt.Errorf("schema description is not supported for enum output")
		}
		if opts.SchemaName != "" {
			return nil, fmt.Errorf("schema name is not supported for enum output")
		}
		if len(opts.EnumValues) == 0 {
			return nil, fmt.Errorf("enum values are required for enum output")
		}
	case ObjectModeNoSchema:
		if opts.Schema != nil {
			return nil, fmt.Errorf("schema is not supported for no-schema output")
		}
		if opts.SchemaDescription != "" {
			return nil, fmt.Errorf("schema description is not supported for no-schema output")
		}
		if opts.SchemaName != "" {
			return nil, fmt.Errorf("schema name is not supported for no-schema output")
		}
		if len(opts.EnumValues) != 0 {
			return nil, fmt.Errorf("enum values are not supported for no-schema output")
		}
	default:
		return nil, fmt.Errorf("invalid output mode: %s", opts.OutputMode)
	}

	// Generate a call ID for correlating all callback events for this call.
	callID := newCallID()

	// Extract telemetry info for callback events.
	cbFuncID, cbMeta := telemetryCallbackInfo(opts.ExperimentalTelemetry)

	// Build telemetry bool helpers
	var isEnabled, recordInputs, recordOutputs *bool
	if opts.ExperimentalTelemetry != nil {
		b := opts.ExperimentalTelemetry.IsEnabled
		isEnabled = &b
		ri := opts.ExperimentalTelemetry.RecordInputs
		recordInputs = &ri
		ro := opts.ExperimentalTelemetry.RecordOutputs
		recordOutputs = &ro
	}

	// Fire ExperimentalOnStart before any LLM call.
	Notify(ctx, ObjectOnStartEvent{
		CallID:            callID,
		OperationID:       "ai.generateObject",
		Provider:          opts.Model.Provider(),
		ModelID:           opts.Model.ModelID(),
		System:            opts.System,
		Prompt:            opts.Prompt,
		Messages:          opts.Messages,
		MaxOutputTokens:   opts.MaxTokens,
		Temperature:       opts.Temperature,
		TopP:              opts.TopP,
		TopK:              opts.TopK,
		FrequencyPenalty:  opts.FrequencyPenalty,
		PresencePenalty:   opts.PresencePenalty,
		Seed:              opts.Seed,
		MaxRetries:        opts.MaxRetries,
		Headers:           opts.Headers,
		ProviderOptions:   opts.ProviderOptions,
		Output:            opts.OutputMode,
		Schema:            schemaToMap(opts.Schema),
		SchemaName:        opts.SchemaName,
		SchemaDescription: opts.SchemaDescription,
		IsEnabled:         isEnabled,
		RecordInputs:      recordInputs,
		RecordOutputs:     recordOutputs,
		FunctionID:        cbFuncID,
		Metadata:          cbMeta,
	}, opts.ExperimentalOnStart)

	callCtx := objectCallCtx{
		callID:   callID,
		funcID:   cbFuncID,
		metadata: cbMeta,
	}

	// Handle different modes
	var result *GenerateObjectResult
	var err error
	switch opts.OutputMode {
	case ObjectModeObject:
		result, err = generateObjectMode(ctx, opts, callCtx)
	case ObjectModeArray:
		result, err = generateArrayMode(ctx, opts, callCtx)
	case ObjectModeEnum:
		result, err = generateEnumMode(ctx, opts, callCtx)
	case ObjectModeNoSchema:
		result, err = generateNoSchemaMode(ctx, opts, callCtx)
	default:
		return nil, fmt.Errorf("unsupported output mode: %s", opts.OutputMode)
	}

	// Record telemetry output attributes
	if span != nil && result != nil {
		// Record output if enabled
		if opts.ExperimentalTelemetry.RecordOutputs {
			span.SetAttributes(attribute.String("ai.response.text", result.Text))
		}

		// Record finish reason
		span.SetAttributes(attribute.String("ai.response.finishReason", string(result.FinishReason)))

		// Record usage information
		if result.Usage.InputTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.promptTokens", *result.Usage.InputTokens))
		}
		if result.Usage.OutputTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.completionTokens", *result.Usage.OutputTokens))
		}
		if result.Usage.TotalTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.totalTokens", *result.Usage.TotalTokens))
		}
	}

	return result, err
}

// generateObjectMode handles standard object generation
func generateObjectMode(ctx context.Context, opts GenerateObjectOptions, cc objectCallCtx) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json_schema",
			Schema:      opts.Schema,
			Name:        opts.SchemaName,
			Description: opts.SchemaDescription,
		},
		Telemetry: opts.ExperimentalTelemetry,
	}

	// Fire ExperimentalOnStepStart before calling the provider.
	Notify(ctx, ObjectOnStepStartEvent{
		CallID:          cc.callID,
		StepNumber:      0,
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
		PromptMessages:  &genOpts.Prompt,
		FunctionID:      cc.funcID,
		Metadata:        cc.metadata,
	}, opts.ExperimentalOnStepStart)

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	reasoning := extractObjectReasoning(genResult)

	// Build response metadata with generated fallbacks, mirroring TS SDK's responseData:
	// { id: result.response?.id ?? generateId(), timestamp: ..., modelId: ..., body: ... }
	reqMeta := map[string]interface{}{"body": genResult.RawRequest}
	resMeta := map[string]interface{}{
		"id":        newCallID(),
		"timestamp": time.Now(),
		"modelId":   opts.Model.ModelID(),
		"body":      genResult.RawResponse,
	}

	// Fire OnStepFinish after provider returns, BEFORE JSON parsing.
	Notify(ctx, ObjectOnStepFinishEvent{
		CallID:           cc.callID,
		StepNumber:       0,
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		ObjectText:       genResult.Text,
		Reasoning:        reasoning,
		Warnings:         genResult.Warnings,
		Request:          reqMeta,
		Response:         resMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnStepFinish)

	var obj interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w", err)
	}

	if err := opts.Schema.Validator().Validate(obj); err != nil {
		return nil, fmt.Errorf("output validation failed: %w", err)
	}

	result := &GenerateObjectResult{
		Object:           obj,
		Text:             genResult.Text,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Reasoning:        reasoning,
		Request:          reqMeta,
		Response:         resMeta,
		ProviderMetadata: genResult.ProviderMetadata,
	}

	// Fire OnFinishEvent after successful parse.
	Notify(ctx, ObjectOnFinishEvent{
		CallID:           cc.callID,
		Object:           obj,
		Error:            nil,
		Reasoning:        reasoning,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Request:          reqMeta,
		Response:         resMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnFinishEvent)

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// generateArrayMode handles array generation
func generateArrayMode(ctx context.Context, opts GenerateObjectOptions, cc objectCallCtx) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Build the wrapped array schema matching TS SDK's arrayOutputStrategy.jsonSchema():
	// { type: 'object', properties: { elements: { type: 'array', items: itemSchema } }, required: ['elements'] }
	// This wrapper is required because most LLMs cannot generate a top-level JSON array directly.
	itemSchemaMap := schemaToMap(opts.Schema)
	if itemSchemaMap == nil {
		itemSchemaMap = map[string]interface{}{}
	}
	// Remove $schema from item schema (mirrors TS: const { $schema, ...itemSchema } = ...)
	delete(itemSchemaMap, "$schema")
	wrappedArraySchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"elements": map[string]interface{}{
				"type":  "array",
				"items": itemSchemaMap,
			},
		},
		"required":             []string{"elements"},
		"additionalProperties": false,
	}

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json_schema",
			Schema:      enumSchemaWrapper{wrappedArraySchema},
			Name:        opts.SchemaName,
			Description: opts.SchemaDescription,
		},
		Telemetry: opts.ExperimentalTelemetry,
	}

	Notify(ctx, ObjectOnStepStartEvent{
		CallID:          cc.callID,
		StepNumber:      0,
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
		PromptMessages:  &genOpts.Prompt,
		FunctionID:      cc.funcID,
		Metadata:        cc.metadata,
	}, opts.ExperimentalOnStepStart)

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	arrayReasoning := extractObjectReasoning(genResult)

	arrReqMeta := map[string]interface{}{"body": genResult.RawRequest}
	arrResMeta := map[string]interface{}{
		"id":        newCallID(),
		"timestamp": time.Now(),
		"modelId":   opts.Model.ModelID(),
		"body":      genResult.RawResponse,
	}

	Notify(ctx, ObjectOnStepFinishEvent{
		CallID:           cc.callID,
		StepNumber:       0,
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		ObjectText:       genResult.Text,
		Reasoning:        arrayReasoning,
		Warnings:         genResult.Warnings,
		Request:          arrReqMeta,
		Response:         arrResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnStepFinish)

	// Parse the wrapper object and extract the 'elements' array.
	// Model output is { "elements": [...] } matching the wrapper schema sent above.
	var wrapper map[string]interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse JSON array: %w", err)
	}
	rawElements, ok := wrapper["elements"]
	if !ok {
		return nil, fmt.Errorf("failed to parse JSON array: missing 'elements' field")
	}
	arr, ok := rawElements.([]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to parse JSON array: 'elements' is not an array")
	}

	// Validate each element against the original item schema.
	for i, element := range arr {
		if err := opts.Schema.Validator().Validate(element); err != nil {
			return nil, fmt.Errorf("validation failed for element %d: %w", i, err)
		}
	}

	result := &GenerateObjectResult{
		Array:            arr,
		Text:             genResult.Text,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Reasoning:        arrayReasoning,
		Request:          arrReqMeta,
		Response:         arrResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
	}

	Notify(ctx, ObjectOnFinishEvent{
		CallID:           cc.callID,
		Object:           arr,
		Error:            nil,
		Reasoning:        arrayReasoning,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Request:          arrReqMeta,
		Response:         arrResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnFinishEvent)

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// generateEnumMode handles enum selection.
// Mirrors the TS SDK's enumOutputStrategy.jsonSchema(): wraps enum values in an object
// { type: 'object', properties: { result: { type: 'string', enum: [...] } }, required: ['result'] }
// because most LLMs cannot generate a top-level enum value directly.
// The model outputs { "result": "value" } and we extract value.result.
func generateEnumMode(ctx context.Context, opts GenerateObjectOptions, cc objectCallCtx) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Build the wrapped enum schema matching TS SDK's enumOutputStrategy.jsonSchema().
	enumVals := make([]interface{}, len(opts.EnumValues))
	for i, v := range opts.EnumValues {
		enumVals[i] = v
	}
	wrappedEnumSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"result": map[string]interface{}{
				"type": "string",
				"enum": enumVals,
			},
		},
		"required":             []string{"result"},
		"additionalProperties": false,
	}

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: enumSchemaWrapper{wrappedEnumSchema},
		},
		Telemetry: opts.ExperimentalTelemetry,
	}

	Notify(ctx, ObjectOnStepStartEvent{
		CallID:          cc.callID,
		StepNumber:      0,
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
		PromptMessages:  &genOpts.Prompt,
		FunctionID:      cc.funcID,
		Metadata:        cc.metadata,
	}, opts.ExperimentalOnStepStart)

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	enumReasoning := extractObjectReasoning(genResult)

	enumReqMeta := map[string]interface{}{"body": genResult.RawRequest}
	enumResMeta := map[string]interface{}{
		"id":        newCallID(),
		"timestamp": time.Now(),
		"modelId":   opts.Model.ModelID(),
		"body":      genResult.RawResponse,
	}

	Notify(ctx, ObjectOnStepFinishEvent{
		CallID:           cc.callID,
		StepNumber:       0,
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		ObjectText:       genResult.Text,
		Reasoning:        enumReasoning,
		Warnings:         genResult.Warnings,
		Request:          enumReqMeta,
		Response:         enumResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnStepFinish)

	// Parse the wrapper object and extract the 'result' string.
	// Model output is { "result": "value" } matching the wrapper schema sent above.
	var wrapper map[string]interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse enum JSON output: %w", err)
	}
	resultVal, ok := wrapper["result"]
	if !ok {
		return nil, fmt.Errorf("enum output missing 'result' field")
	}
	selectedValue, ok := resultVal.(string)
	if !ok {
		return nil, fmt.Errorf("enum output 'result' is not a string: %T", resultVal)
	}

	// Validate selected value against allowed enum values.
	valid := false
	for _, enumVal := range opts.EnumValues {
		if selectedValue == enumVal {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid enum value: %q (expected one of %v)", selectedValue, opts.EnumValues)
	}

	result := &GenerateObjectResult{
		EnumValue:        selectedValue,
		Text:             genResult.Text,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Reasoning:        enumReasoning,
		Request:          enumReqMeta,
		Response:         enumResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
	}

	Notify(ctx, ObjectOnFinishEvent{
		CallID:           cc.callID,
		Object:           selectedValue,
		Error:            nil,
		Reasoning:        enumReasoning,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Request:          enumReqMeta,
		Response:         enumResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnFinishEvent)

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// enumSchemaWrapper wraps a plain map[string]interface{} so it satisfies
// the schema.Schema interface required by provider.ResponseFormat.Schema.
// Used by generateEnumMode to pass the { "enum": [...] } JSON schema.
type enumSchemaWrapper struct {
	m map[string]interface{}
}

func (e enumSchemaWrapper) JSONSchema() map[string]interface{} { return e.m }
func (e enumSchemaWrapper) Validator() schema.Validator        { return noopValidator{jsonSchema: e.m} }

type noopValidator struct {
	jsonSchema map[string]interface{}
}

func (n noopValidator) Validate(_ interface{}) error          { return nil }
func (n noopValidator) JSONSchema() map[string]interface{}    { return n.jsonSchema }

// generateNoSchemaMode handles raw JSON generation without validation
func generateNoSchemaMode(ctx context.Context, opts GenerateObjectOptions, cc objectCallCtx) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json_object",
		},
		Telemetry: opts.ExperimentalTelemetry,
	}

	Notify(ctx, ObjectOnStepStartEvent{
		CallID:          cc.callID,
		StepNumber:      0,
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
		PromptMessages:  &genOpts.Prompt,
		FunctionID:      cc.funcID,
		Metadata:        cc.metadata,
	}, opts.ExperimentalOnStepStart)

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	noSchemaReasoning := extractObjectReasoning(genResult)

	nsReqMeta := map[string]interface{}{"body": genResult.RawRequest}
	nsResMeta := map[string]interface{}{
		"id":        newCallID(),
		"timestamp": time.Now(),
		"modelId":   opts.Model.ModelID(),
		"body":      genResult.RawResponse,
	}

	Notify(ctx, ObjectOnStepFinishEvent{
		CallID:           cc.callID,
		StepNumber:       0,
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		ObjectText:       genResult.Text,
		Reasoning:        noSchemaReasoning,
		Warnings:         genResult.Warnings,
		Request:          nsReqMeta,
		Response:         nsResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnStepFinish)

	var obj interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w", err)
	}

	result := &GenerateObjectResult{
		Object:           obj,
		Text:             genResult.Text,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Reasoning:        noSchemaReasoning,
		Request:          nsReqMeta,
		Response:         nsResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
	}

	Notify(ctx, ObjectOnFinishEvent{
		CallID:           cc.callID,
		Object:           obj,
		Error:            nil,
		Reasoning:        noSchemaReasoning,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		Request:          nsReqMeta,
		Response:         nsResMeta,
		ProviderMetadata: genResult.ProviderMetadata,
		FunctionID:       cc.funcID,
		Metadata:         cc.metadata,
	}, opts.OnFinishEvent)

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// GenerateObjectInto is a convenience function that unmarshals the result into a provided struct
func GenerateObjectInto(ctx context.Context, opts GenerateObjectOptions, target interface{}) error {
	result, err := GenerateObject(ctx, opts)
	if err != nil {
		return err
	}

	// Unmarshal into target
	jsonBytes, err := json.Marshal(result.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("failed to unmarshal into target: %w", err)
	}

	return nil
}

// StreamObjectOptions contains options for streaming object generation
type StreamObjectOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt string
	Messages []types.Message
	System string

	// Schema for the output object
	Schema schema.Schema

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Seed             *int
	MaxRetries       int

	// Additional HTTP headers sent with the request.
	Headers map[string]string

	// Additional provider-specific options.
	ProviderOptions map[string]interface{}

	// SchemaName is an optional name for the output schema.
	SchemaName string

	// SchemaDescription is an optional description for the output schema.
	SchemaDescription string

	// Telemetry configuration for observability
	ExperimentalTelemetry *TelemetrySettings

	// ========================================================================
	// Structured Event Callbacks (P1-7)
	// These callbacks receive typed event structs and are panic-safe.
	// ========================================================================

	// ExperimentalOnStart is called once before any LLM call is made.
	ExperimentalOnStart func(ctx context.Context, e ObjectOnStartEvent)

	// ExperimentalOnStepStart is called just before the provider is called.
	ExperimentalOnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// OnStepFinish is called after the provider returns, before JSON parsing.
	OnStepFinish func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnFinishEvent is called when the operation completes.
	// For StreamObject, the event Error field may be set if parsing failed.
	OnFinishEvent func(ctx context.Context, e ObjectOnFinishEvent)

	// OnError is called when the stream itself encounters an error.
	// This is separate from parse/validation errors reported via OnFinishEvent.
	OnError func(ctx context.Context, err error)

	// Legacy callbacks — kept for backward compatibility.
	OnChunk  func(partialObject interface{})
	OnFinish func(ctx context.Context, result *GenerateObjectResult, userContext interface{})

	// ExperimentalContext allows passing custom context through generation lifecycle
	ExperimentalContext interface{}
}

// StreamObject performs streaming object generation.
// As JSON is streamed, partial objects are parsed and validated.
//
// Deprecated: Use StreamText with ObjectOutput[T]() or ArrayOutput[T]()
// instead. The Output option provides type-safe structured streaming with
// PartialOutput() for incremental results.
func StreamObject(ctx context.Context, opts StreamObjectOptions) (*GenerateObjectResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Schema == nil {
		return nil, fmt.Errorf("schema is required")
	}

	// Generate call ID and extract telemetry info for callback events.
	callID := newCallID()
	cbFuncID, cbMeta := telemetryCallbackInfo(opts.ExperimentalTelemetry)

	var isEnabled, recordInputs, recordOutputs *bool
	if opts.ExperimentalTelemetry != nil {
		b := opts.ExperimentalTelemetry.IsEnabled
		isEnabled = &b
		ri := opts.ExperimentalTelemetry.RecordInputs
		recordInputs = &ri
		ro := opts.ExperimentalTelemetry.RecordOutputs
		recordOutputs = &ro
	}

	// Fire ExperimentalOnStart before any LLM call.
	Notify(ctx, ObjectOnStartEvent{
		CallID:            callID,
		OperationID:       "ai.streamObject",
		Provider:          opts.Model.Provider(),
		ModelID:           opts.Model.ModelID(),
		System:            opts.System,
		Prompt:            opts.Prompt,
		Messages:          opts.Messages,
		MaxOutputTokens:   opts.MaxTokens,
		Temperature:       opts.Temperature,
		TopP:              opts.TopP,
		TopK:              opts.TopK,
		FrequencyPenalty:  opts.FrequencyPenalty,
		PresencePenalty:   opts.PresencePenalty,
		Seed:              opts.Seed,
		MaxRetries:        opts.MaxRetries,
		Headers:           opts.Headers,
		ProviderOptions:   opts.ProviderOptions,
		Output:            ObjectModeObject,
		Schema:            schemaToMap(opts.Schema),
		SchemaName:        opts.SchemaName,
		SchemaDescription: opts.SchemaDescription,
		IsEnabled:         isEnabled,
		RecordInputs:      recordInputs,
		RecordOutputs:     recordOutputs,
		FunctionID:        cbFuncID,
		Metadata:          cbMeta,
	}, opts.ExperimentalOnStart)

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Create streaming generation options
	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json_schema",
			Schema:      opts.Schema,
			Name:        opts.SchemaName,
			Description: opts.SchemaDescription,
		},
		Telemetry: opts.ExperimentalTelemetry,
	}

	// Fire ExperimentalOnStepStart before calling the provider.
	Notify(ctx, ObjectOnStepStartEvent{
		CallID:          callID,
		StepNumber:      0,
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
		PromptMessages:  &genOpts.Prompt,
		FunctionID:      cbFuncID,
		Metadata:        cbMeta,
	}, opts.ExperimentalOnStepStart)

	// Try to start streaming
	// If streaming is not supported or fails, fall back to non-streaming
	stream, err := opts.Model.DoStream(ctx, genOpts)
	if err != nil || stream == nil {
		// Fallback to non-streaming generation
		result, err := opts.Model.DoGenerate(ctx, genOpts)
		if err != nil {
			if opts.OnError != nil {
				opts.OnError(ctx, err)
			}
			return nil, fmt.Errorf("generation failed: %w", err)
		}

		fallbackReasoning := extractObjectReasoning(result)

		fbReqMeta := map[string]interface{}{"body": result.RawRequest}
		fbResMeta := map[string]interface{}{
			"id":        newCallID(),
			"timestamp": time.Now(),
			"modelId":   opts.Model.ModelID(),
			"body":      result.RawResponse,
		}

		// Fire OnStepFinish before parsing.
		Notify(ctx, ObjectOnStepFinishEvent{
			CallID:           callID,
			StepNumber:       0,
			Provider:         opts.Model.Provider(),
			ModelID:          opts.Model.ModelID(),
			FinishReason:     result.FinishReason,
			Usage:            result.Usage,
			ObjectText:       result.Text,
			Reasoning:        fallbackReasoning,
			Warnings:         result.Warnings,
			Request:          fbReqMeta,
			Response:         fbResMeta,
			ProviderMetadata: result.ProviderMetadata,
			FunctionID:       cbFuncID,
			Metadata:         cbMeta,
		}, opts.OnStepFinish)

		// Parse final JSON
		var finalObject interface{}
		if parseErr := json.Unmarshal([]byte(result.Text), &finalObject); parseErr != nil {
			// Parsing failed — fire OnFinishEvent with error and nil object.
			Notify(ctx, ObjectOnFinishEvent{
				CallID:           callID,
				Object:           nil,
				Error:            parseErr,
				Reasoning:        fallbackReasoning,
				FinishReason:     result.FinishReason,
				Usage:            result.Usage,
				Warnings:         result.Warnings,
				Request:          fbReqMeta,
				Response:         fbResMeta,
				ProviderMetadata: result.ProviderMetadata,
				FunctionID:       cbFuncID,
				Metadata:         cbMeta,
			}, opts.OnFinishEvent)
			return nil, fmt.Errorf("failed to parse JSON: %w", parseErr)
		}

		// Validate
		if valErr := opts.Schema.Validator().Validate(finalObject); valErr != nil {
			Notify(ctx, ObjectOnFinishEvent{
				CallID:           callID,
				Object:           nil,
				Error:            valErr,
				Reasoning:        fallbackReasoning,
				FinishReason:     result.FinishReason,
				Usage:            result.Usage,
				Warnings:         result.Warnings,
				Request:          fbReqMeta,
				Response:         fbResMeta,
				ProviderMetadata: result.ProviderMetadata,
				FunctionID:       cbFuncID,
				Metadata:         cbMeta,
			}, opts.OnFinishEvent)
			return nil, fmt.Errorf("validation failed: %w", valErr)
		}

		finalResult := &GenerateObjectResult{
			Object:           finalObject,
			Text:             result.Text,
			FinishReason:     result.FinishReason,
			Usage:            result.Usage,
			Warnings:         result.Warnings,
			Reasoning:        fallbackReasoning,
			Request:          fbReqMeta,
			Response:         fbResMeta,
			ProviderMetadata: result.ProviderMetadata,
		}

		Notify(ctx, ObjectOnFinishEvent{
			CallID:           callID,
			Object:           finalObject,
			Error:            nil,
			Reasoning:        fallbackReasoning,
			FinishReason:     result.FinishReason,
			Usage:            result.Usage,
			Warnings:         result.Warnings,
			Request:          fbReqMeta,
			Response:         fbResMeta,
			ProviderMetadata: result.ProviderMetadata,
			FunctionID:       cbFuncID,
			Metadata:         cbMeta,
		}, opts.OnFinishEvent)

		if opts.OnFinish != nil {
			opts.OnFinish(ctx, finalResult, opts.ExperimentalContext)
		}

		return finalResult, nil
	}
	defer stream.Close() //nolint:errcheck

	// Accumulate text and track partial objects
	var accumulatedText string
	var accumulatedReasoning string
	var lastObject interface{}
	var usage types.Usage
	var finishReason types.FinishReason
	var streamWarnings []types.Warning
	var streamProviderMetadata map[string]interface{}
	var streamErr error // captures a non-EOF stream error for deferred callback firing

	// Process stream chunks. On a non-EOF error we record it and break so that
	// OnStepFinish / OnFinishEvent still fire with whatever was accumulated —
	// matching the TS SDK's TransformStream flush behaviour where the flush
	// handler always executes even after a transform error.
	for {
		chunk, chunkErr := stream.Next()
		if chunkErr != nil {
			if chunkErr == io.EOF || errors.Is(chunkErr, io.ErrClosedPipe) {
				break
			}
			if opts.OnError != nil {
				opts.OnError(ctx, chunkErr)
			}
			streamErr = chunkErr
			break
		}

		// Collect any warnings from any chunk.
		if len(chunk.Warnings) > 0 {
			streamWarnings = append(streamWarnings, chunk.Warnings...)
		}

		// Handle different chunk types
		switch chunk.Type {
		case provider.ChunkTypeText:
			// Accumulate text
			accumulatedText += chunk.Text

			// Try to parse partial JSON
			parseResult := parsePartialJSON(accumulatedText)

			// If we successfully parsed something and it's different from last
			if parseResult.Value != nil && !deepEqual(parseResult.Value, lastObject) {
				// Validate against schema
				if err := opts.Schema.Validator().Validate(parseResult.Value); err == nil {
					// Valid partial object - emit it
					lastObject = parseResult.Value

					// Call OnChunk callback if provided
					if opts.OnChunk != nil {
						opts.OnChunk(lastObject)
					}
				}
			}

		case provider.ChunkTypeReasoning:
			// Accumulate reasoning/thinking content.
			// Mirrors TS SDK's extractReasoningContent used in generateObject.
			accumulatedReasoning += chunk.Reasoning

		case provider.ChunkTypeUsage:
			if chunk.Usage != nil {
				usage = usage.Add(*chunk.Usage)
			}

		case provider.ChunkTypeFinish:
			finishReason = chunk.FinishReason
			if chunk.Usage != nil {
				usage = usage.Add(*chunk.Usage)
			}
		}

		// Accumulate provider metadata from any chunk that carries it
		// (Gemini emits it on ChunkTypeFinish; mirrors stream.go behaviour).
		if len(chunk.ProviderMetadata) > 0 {
			var pm map[string]interface{}
			if jsonErr := json.Unmarshal(chunk.ProviderMetadata, &pm); jsonErr == nil {
				streamProviderMetadata = pm
			}
		}
	}

	// Apply default finish reason, mirroring TS SDK's `finishReason ?? 'other'` in the flush handler.
	// If the stream ended without a finish chunk (e.g. stream error, provider omitted it),
	// default to 'other' rather than leaving the zero-value empty string.
	if finishReason == "" {
		finishReason = types.FinishReasonOther
	}

	// DoStream returns no request/response metadata (unlike DoGenerate), so we
	// generate fallback values matching the TS SDK's `request ?? {}` / `fullResponse` pattern.
	// TS initializes fullResponse = { id: generateId(), timestamp: currentDate(), modelId: model.modelId }
	// and resolves _request with `request ?? {}`.
	streamReqMeta := map[string]interface{}{}
	streamResMeta := map[string]interface{}{
		"id":        newCallID(),
		"timestamp": time.Now(),
		"modelId":   opts.Model.ModelID(),
	}

	// If the stream itself errored, fire OnStepFinish + OnFinishEvent with the
	// error (matching TS flush handler) then return.
	if streamErr != nil {
		Notify(ctx, ObjectOnStepFinishEvent{
			CallID:           callID,
			StepNumber:       0,
			Provider:         opts.Model.Provider(),
			ModelID:          opts.Model.ModelID(),
			FinishReason:     finishReason,
			Usage:            usage,
			ObjectText:       accumulatedText,
			Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
			Warnings:         streamWarnings,
			Request:          streamReqMeta,
			Response:         streamResMeta,
			ProviderMetadata: streamProviderMetadata,
			FunctionID:       cbFuncID,
			Metadata:         cbMeta,
		}, opts.OnStepFinish)
		Notify(ctx, ObjectOnFinishEvent{
			CallID:           callID,
			Object:           nil,
			Error:            streamErr,
			Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
			FinishReason:     finishReason,
			Usage:            usage,
			Warnings:         streamWarnings,
			Request:          streamReqMeta,
			Response:         streamResMeta,
			ProviderMetadata: streamProviderMetadata,
			FunctionID:       cbFuncID,
			Metadata:         cbMeta,
		}, opts.OnFinishEvent)
		return nil, fmt.Errorf("stream error: %w", streamErr)
	}

	// Fire OnStepFinish after stream ends, BEFORE JSON parsing.
	// TS SDK passes reasoning: undefined here (streaming object path does not extract reasoning).
	Notify(ctx, ObjectOnStepFinishEvent{
		CallID:           callID,
		StepNumber:       0,
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     finishReason,
		Usage:            usage,
		ObjectText:       accumulatedText,
		Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
		Warnings:         streamWarnings,
		Request:          streamReqMeta,
		Response:         streamResMeta,
		ProviderMetadata: streamProviderMetadata,
		FunctionID:       cbFuncID,
		Metadata:         cbMeta,
	}, opts.OnStepFinish)

	// Parse final JSON
	var finalObject interface{}
	if accumulatedText != "" {
		if parseErr := json.Unmarshal([]byte(accumulatedText), &finalObject); parseErr != nil {
			// Parse failure: fire OnFinishEvent with error and nil object.
			Notify(ctx, ObjectOnFinishEvent{
				CallID:           callID,
				Object:           nil,
				Error:            parseErr,
				Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
				FinishReason:     finishReason,
				Usage:            usage,
				Warnings:         streamWarnings,
				Request:          streamReqMeta,
				Response:         streamResMeta,
				ProviderMetadata: streamProviderMetadata,
				FunctionID:       cbFuncID,
				Metadata:         cbMeta,
			}, opts.OnFinishEvent)
			return nil, fmt.Errorf("failed to parse final JSON: %w", parseErr)
		}

		// Validate final object
		if valErr := opts.Schema.Validator().Validate(finalObject); valErr != nil {
			Notify(ctx, ObjectOnFinishEvent{
				CallID:           callID,
				Object:           nil,
				Error:            valErr,
				Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
				FinishReason:     finishReason,
				Usage:            usage,
				Warnings:         streamWarnings,
				Request:          streamReqMeta,
				Response:         streamResMeta,
				ProviderMetadata: streamProviderMetadata,
				FunctionID:       cbFuncID,
				Metadata:         cbMeta,
			}, opts.OnFinishEvent)
			return nil, fmt.Errorf("final object validation failed: %w", valErr)
		}
	}

	// Build result. Reasoning is accumulated for convenience but note that TS SDK does
	// not expose reasoning in the streaming object path (onStepFinish/onFinish receive undefined).
	result := &GenerateObjectResult{
		Object:           finalObject,
		Text:             accumulatedText,
		Reasoning:        accumulatedReasoning,
		FinishReason:     finishReason,
		Usage:            usage,
		Warnings:         streamWarnings,
		Request:          streamReqMeta,
		Response:         streamResMeta,
		ProviderMetadata: streamProviderMetadata,
	}

	// Fire OnFinishEvent after successful parse.
	Notify(ctx, ObjectOnFinishEvent{
		CallID:           callID,
		Object:           finalObject,
		Error:            nil,
		Reasoning:        "", // TS SDK always passes reasoning: undefined in streaming callbacks
		FinishReason:     finishReason,
		Usage:            usage,
		Warnings:         streamWarnings,
		Request:          streamReqMeta,
		Response:         streamResMeta,
		ProviderMetadata: streamProviderMetadata,
		FunctionID:       cbFuncID,
		Metadata:         cbMeta,
	}, opts.OnFinishEvent)

	// Call legacy OnFinish if provided
	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// parsePartialJSON wraps the jsonparser.ParsePartialJSON function
func parsePartialJSON(text string) jsonparser.ParseResult {
	return jsonparser.ParsePartialJSON(text)
}

// deepEqual performs a deep equality check on two JSON values
// This is used to prevent emitting duplicate partial objects during streaming
func deepEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	return reflect.DeepEqual(a, b)
}
