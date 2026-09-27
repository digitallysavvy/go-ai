package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

const defaultObjectMaxRetries = 2

type RepairTextFunc func(ctx context.Context, text string, parseErr error) (*string, error)

// effectiveRepairText resolves the stable RepairText field over the
// deprecated ExperimentalRepairText alias (audit row 09a52cb).
func effectiveRepairText(stable, experimental RepairTextFunc) RepairTextFunc {
	if stable != nil {
		return stable
	}
	return experimental
}

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
	var parts []string
	for _, part := range result.Content {
		if rc, ok := part.(types.ReasoningContent); ok {
			parts = append(parts, rc.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "\n" + parts[i]
	}
	return out
}

// schemaToMap extracts the JSON Schema representation from a schema.Schema.
// Returns nil if the schema is nil.
// schemaToMap extracts a schema.Schema's JSON Schema representation via its
// Validator(), which the schema.Validator interface always guarantees
// (JSONSchema() map[string]interface{}). Go through the validator rather
// than asserting an ad hoc "JSONSchema() ..." method directly on s: some
// Schema implementations (e.g. *schema.SimpleJSONSchema) only expose it on
// the value Validator() returns, so asserting it on s itself silently
// dropped the whole schema (empty {} item schemas for array/enum output
// wrapping) for those implementations.
func schemaToMap(s schema.Schema) map[string]interface{} {
	if s == nil {
		return nil
	}
	v := s.Validator()
	if v == nil {
		return nil
	}
	return v.JSONSchema()
}

// hoistSchemaDefs removes "definitions"/"$defs" from itemSchema (mutating
// it) and returns them as entries to merge into the wrapper root schema, so
// "#/$defs/..."/"#/definitions/..." refs (which resolve against the document
// root) keep working once itemSchema is nested under "items" (audit row
// 72ec74f / WG4: root-level JSON Schema definitions must survive wrapping an
// element schema for array output).
func hoistSchemaDefs(itemSchema map[string]interface{}) map[string]interface{} {
	root := map[string]interface{}{}
	if definitions, ok := itemSchema["definitions"]; ok {
		root["definitions"] = definitions
		delete(itemSchema, "definitions")
	}
	if defs, ok := itemSchema["$defs"]; ok {
		root["$defs"] = defs
		delete(itemSchema, "$defs")
	}
	return root
}

func cloneSchemaMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneSchemaValue(value)
	}
	return out
}

func cloneSchemaValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return cloneSchemaMap(v)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = cloneSchemaValue(item)
		}
		return out
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	default:
		return v
	}
}

func doGenerateWithRetry(ctx context.Context, model provider.LanguageModel, opts *provider.GenerateOptions, maxRetries int) (*types.GenerateResult, error) {
	if maxRetries <= 0 {
		return model.DoGenerate(ctx, opts)
	}

	var result *types.GenerateResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   maxRetries,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  retryutil.IsRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		result, err = model.DoGenerate(retryCtx, opts)
		return err
	})
	return result, err
}

func doStreamWithRetry(ctx context.Context, model provider.LanguageModel, opts *provider.GenerateOptions, maxRetries int) (provider.TextStream, error) {
	if maxRetries <= 0 {
		return model.DoStream(ctx, opts)
	}

	var stream provider.TextStream
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   maxRetries,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  retryutil.IsRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		stream, err = model.DoStream(retryCtx, opts)
		return err
	})
	return stream, err
}

func normalizeObjectMaxRetries(maxRetries int) (int, error) {
	if maxRetries < 0 {
		return 0, fmt.Errorf("maxRetries must be >= 0")
	}
	if maxRetries == 0 {
		return defaultObjectMaxRetries, nil
	}
	return maxRetries, nil
}

func responseMetadataFromGenerateResult(model provider.LanguageModel, result *types.GenerateResult) *types.ResponseMetadata {
	return responseMetadataFromGenerateResultWithID(model, result, newCallID)
}

func responseMetadataFromGenerateResultWithID(model provider.LanguageModel, result *types.GenerateResult, generateID IDGenerator) *types.ResponseMetadata {
	if result == nil {
		return nil
	}
	if result.ResponseMetadata != nil {
		return result.ResponseMetadata
	}
	if generateID == nil {
		generateID = newCallID
	}
	result.ResponseMetadata = &types.ResponseMetadata{
		ID:        generateID(),
		Timestamp: time.Now(),
		ModelID:   model.ModelID(),
		Headers:   result.ResponseHeaders,
	}
	return result.ResponseMetadata
}

func generateStepResponseFromGenerateResult(model provider.LanguageModel, result *types.GenerateResult) GenerateStepResponse {
	return generateStepResponseFromGenerateResultWithID(model, result, newCallID)
}

func generateStepResponseFromGenerateResultWithID(model provider.LanguageModel, result *types.GenerateResult, generateID IDGenerator) GenerateStepResponse {
	meta := responseMetadataFromGenerateResultWithID(model, result, generateID)
	if meta == nil {
		return GenerateStepResponse{Body: result.RawResponse}
	}
	return GenerateStepResponse{
		ID:        meta.ID,
		Timestamp: meta.Timestamp,
		ModelID:   meta.ModelID,
		Headers:   meta.Headers,
		Body:      result.RawResponse,
	}
}

func newNoObjectGeneratedError(message string, cause error, text string, model provider.LanguageModel, result *types.GenerateResult) error {
	if result == nil {
		return &NoObjectGeneratedError{
			Message: message,
			Cause:   cause,
			Text:    text,
		}
	}
	usage := result.Usage
	return &NoObjectGeneratedError{
		Message:      message,
		Cause:        cause,
		Text:         text,
		Response:     responseMetadataFromGenerateResult(model, result),
		Usage:        &usage,
		FinishReason: result.FinishReason,
	}
}

func parseObjectResult(result *types.GenerateResult, mode ObjectOutputMode, s schema.Schema, enumValues []string, model provider.LanguageModel) (interface{}, []interface{}, string, error) {
	if result == nil {
		return nil, nil, "", newNoObjectGeneratedError("No object generated.", nil, "", model, nil)
	}
	if result.Text == "" {
		return nil, nil, "", newNoObjectGeneratedError("No object generated: could not parse the response.", io.ErrUnexpectedEOF, "", model, result)
	}
	obj, arr, enumValue, err := parseStreamFinal(mode, s, enumValues, result.Text)
	if err != nil {
		message := "No object generated: response did not match schema."
		var syntaxErr *json.SyntaxError
		var unmarshalTypeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr), errors.As(err, &unmarshalTypeErr), errors.Is(err, io.ErrUnexpectedEOF):
			message = "No object generated: could not parse the response."
		}
		return nil, nil, "", newNoObjectGeneratedError(message, err, result.Text, model, result)
	}
	return obj, arr, enumValue, nil
}

func attemptRepair(
	ctx context.Context,
	repair RepairTextFunc,
	text string,
	parseErr error,
) (*string, error) {
	if repair == nil {
		return nil, nil
	}
	cause := parseErr
	var noObjErr *NoObjectGeneratedError
	if errors.As(parseErr, &noObjErr) && noObjErr.Cause != nil {
		cause = noObjErr.Cause
	}
	return repair(ctx, text, cause)
}

func buildStreamObjectResponseFormat(opts StreamObjectOptions) (*provider.ResponseFormat, error) {
	switch opts.OutputMode {
	case ObjectModeObject:
		return &provider.ResponseFormat{
			Type:        "json_schema",
			Schema:      opts.Schema,
			Name:        opts.SchemaName,
			Description: opts.SchemaDescription,
		}, nil
	case ObjectModeArray:
		itemSchemaMap := cloneSchemaMap(schemaToMap(opts.Schema))
		if itemSchemaMap == nil {
			itemSchemaMap = map[string]interface{}{}
		}
		delete(itemSchemaMap, "$schema")
		rootDefs := hoistSchemaDefs(itemSchemaMap)
		wrapped := map[string]interface{}{
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
		for k, v := range rootDefs {
			wrapped[k] = v
		}
		return &provider.ResponseFormat{
			Type:        "json_schema",
			Schema:      enumSchemaWrapper{wrapped},
			Name:        opts.SchemaName,
			Description: opts.SchemaDescription,
		}, nil
	case ObjectModeEnum:
		enumVals := make([]interface{}, len(opts.EnumValues))
		for i, v := range opts.EnumValues {
			enumVals[i] = v
		}
		return &provider.ResponseFormat{
			Type: "json_schema",
			Schema: enumSchemaWrapper{map[string]interface{}{
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
			}},
		}, nil
	case ObjectModeNoSchema:
		return &provider.ResponseFormat{Type: "json_object"}, nil
	default:
		return nil, fmt.Errorf("invalid output mode: %s", opts.OutputMode)
	}
}

func parseStreamPartial(mode ObjectOutputMode, s schema.Schema, enumValues []string, parseResult jsonparser.ParseResult) (interface{}, bool) {
	if parseResult.Value == nil {
		return nil, false
	}

	switch mode {
	case ObjectModeObject, ObjectModeNoSchema:
		return parseResult.Value, true
	case ObjectModeArray:
		wrapper, ok := parseResult.Value.(map[string]interface{})
		if !ok {
			return nil, false
		}
		rawElements, ok := wrapper["elements"]
		if !ok {
			return nil, false
		}
		arr, ok := rawElements.([]interface{})
		if !ok {
			return nil, false
		}
		limit := len(arr)
		if parseResult.State == jsonparser.ParseStateRepaired && limit > 0 {
			limit--
		}
		resultArray := make([]interface{}, 0, limit)
		if s == nil {
			resultArray = append(resultArray, arr[:limit]...)
			return resultArray, true
		}
		for _, element := range arr[:limit] {
			element = schema.ApplyDefaults(element, s)
			if err := s.Validator().Validate(element); err != nil {
				return nil, false
			}
			resultArray = append(resultArray, element)
		}
		return resultArray, true
	case ObjectModeEnum:
		wrapper, ok := parseResult.Value.(map[string]interface{})
		if !ok {
			return nil, false
		}
		rawResult, ok := wrapper["result"]
		if !ok {
			return nil, false
		}
		selected, ok := rawResult.(string)
		if !ok {
			return nil, false
		}
		if selected == "" {
			return nil, false
		}
		var matches []string
		for _, enumVal := range enumValues {
			if strings.HasPrefix(enumVal, selected) {
				matches = append(matches, enumVal)
			}
		}
		if len(matches) == 0 {
			return nil, false
		}
		if len(matches) == 1 {
			return matches[0], true
		}
		return selected, true
	default:
		return nil, false
	}
}

func parseStreamFinal(mode ObjectOutputMode, s schema.Schema, enumValues []string, text string) (interface{}, []interface{}, string, error) {
	if text == "" {
		return nil, nil, "", nil
	}

	switch mode {
	case ObjectModeObject:
		var obj interface{}
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, nil, "", err
		}
		obj = schema.ApplyDefaults(obj, s)
		if err := s.Validator().Validate(obj); err != nil {
			return nil, nil, "", err
		}
		return obj, nil, "", nil
	case ObjectModeArray:
		var wrapper map[string]interface{}
		if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
			return nil, nil, "", err
		}
		rawElements, ok := wrapper["elements"]
		if !ok {
			return nil, nil, "", fmt.Errorf("missing 'elements' field")
		}
		arr, ok := rawElements.([]interface{})
		if !ok {
			return nil, nil, "", fmt.Errorf("'elements' is not an array")
		}
		resultArray := make([]interface{}, len(arr))
		for i, element := range arr {
			element = schema.ApplyDefaults(element, s)
			if err := s.Validator().Validate(element); err != nil {
				return nil, nil, "", fmt.Errorf("validation failed for element %d: %w", i, err)
			}
			resultArray[i] = element
		}
		return resultArray, resultArray, "", nil
	case ObjectModeEnum:
		var wrapper map[string]interface{}
		if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
			return nil, nil, "", err
		}
		rawResult, ok := wrapper["result"]
		if !ok {
			return nil, nil, "", fmt.Errorf("enum output missing 'result' field")
		}
		selected, ok := rawResult.(string)
		if !ok {
			return nil, nil, "", fmt.Errorf("enum output 'result' is not a string: %T", rawResult)
		}
		for _, enumVal := range enumValues {
			if selected == enumVal {
				return selected, nil, selected, nil
			}
		}
		return nil, nil, "", fmt.Errorf("invalid enum value: %q (expected one of %v)", selected, enumValues)
	case ObjectModeNoSchema:
		var obj interface{}
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, nil, "", err
		}
		return obj, nil, "", nil
	default:
		return nil, nil, "", fmt.Errorf("invalid output mode: %s", mode)
	}
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
	Prompt   string
	Messages []types.Message
	System   string

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

	// RepairText repairs invalid JSON or schema-invalid object output.
	// Return nil when the output cannot be repaired. Takes precedence over
	// ExperimentalRepairText when both are set (audit row 09a52cb).
	RepairText RepairTextFunc

	// ExperimentalRepairText repairs invalid JSON or schema-invalid object output.
	// Return nil when the output cannot be repaired.
	//
	// Deprecated: use RepairText.
	ExperimentalRepairText RepairTextFunc

	// Additional HTTP headers sent with the request.
	Headers map[string]string

	// Additional provider-specific options.
	ProviderOptions map[string]interface{}

	// SchemaName is an optional name for the output schema.
	SchemaName string

	// SchemaDescription is an optional description for the output schema.
	SchemaDescription string

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// ========================================================================
	// Structured Event Callbacks.
	// These callbacks receive typed event structs and are panic-safe.
	// They fire in addition to (not instead of) the legacy OnFinish callback.
	// ========================================================================

	// OnStart is called once before any LLM call is made.
	OnStart func(ctx context.Context, e ObjectOnStartEvent)

	// ExperimentalOnStart is a deprecated alias for OnStart.
	//
	// Deprecated: use OnStart.
	ExperimentalOnStart func(ctx context.Context, e ObjectOnStartEvent)

	// OnStepStart is called just before the provider is called.
	OnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// ExperimentalOnStepStart is a deprecated alias for OnStepStart.
	//
	// Deprecated: use OnStepStart.
	ExperimentalOnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// OnStepEnd is called after the provider returns, before JSON parsing.
	OnStepEnd func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnStepFinish is called after the provider returns, before JSON parsing.
	//
	// Deprecated: use OnStepEnd.
	OnStepFinish func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnEnd is called when the operation completes with a typed event.
	// For GenerateObject, the event Error field is always nil.
	OnEnd func(ctx context.Context, e ObjectOnFinishEvent)

	// OnFinishEvent is a deprecated alias for OnEnd.
	//
	// Deprecated: use OnEnd.
	OnFinishEvent func(ctx context.Context, e ObjectOnFinishEvent)

	// Legacy callback — kept for backward compatibility.
	// Prefer OnEnd for structured access.
	OnFinish func(ctx context.Context, result *GenerateObjectResult, userContext interface{})

	// ExperimentalContext allows passing custom context through generation lifecycle
	ExperimentalContext interface{}
}

// resolveObjectOnStart returns onStart if set, else its deprecated alias
// experimentalOnStart.
func resolveObjectOnStart(onStart, experimentalOnStart func(context.Context, ObjectOnStartEvent)) func(context.Context, ObjectOnStartEvent) {
	if onStart != nil {
		return onStart
	}
	return experimentalOnStart
}

// resolveObjectOnStepStart returns onStepStart if set, else its deprecated
// alias experimentalOnStepStart.
func resolveObjectOnStepStart(onStepStart, experimentalOnStepStart func(context.Context, ObjectOnStepStartEvent)) func(context.Context, ObjectOnStepStartEvent) {
	if onStepStart != nil {
		return onStepStart
	}
	return experimentalOnStepStart
}

// resolveObjectOnEnd returns onEnd if set, else its deprecated alias
// onFinishEvent.
func resolveObjectOnEnd(onEnd, onFinishEvent func(context.Context, ObjectOnFinishEvent)) func(context.Context, ObjectOnFinishEvent) {
	if onEnd != nil {
		return onEnd
	}
	return onFinishEvent
}

func resolveObjectOnStepEnd(onStepEnd, onStepFinish func(context.Context, ObjectOnStepFinishEvent)) func(context.Context, ObjectOnStepFinishEvent) {
	if onStepEnd != nil {
		return onStepEnd
	}
	return onStepFinish
}

// GenerateObjectResult contains the result of object generation
type GenerateObjectResult struct {
	// The generated object (unmarshaled JSON)
	Object interface{} `json:"object,omitempty"`

	// For array mode: array of objects
	Array []interface{} `json:"array,omitempty"`

	// For enum mode: selected enum value
	EnumValue string `json:"enumValue,omitempty"`

	// Raw JSON text
	Text string `json:"text,omitempty"`

	// Finish reason
	FinishReason types.FinishReason `json:"finishReason"`

	// Token usage information
	Usage types.Usage `json:"usage"`

	// Warnings from the provider
	Warnings []types.Warning `json:"warnings,omitempty"`

	// Reasoning text generated by the model (if any).
	// Mirrors GenerateObjectResult.reasoning in the TS SDK.
	Reasoning string `json:"reasoning,omitempty"`

	// Request holds raw request metadata (e.g. body sent to the provider).
	// Mirrors GenerateObjectResult.request in the TS SDK.
	Request GenerateStepRequest `json:"request"`

	// Response holds raw response metadata (e.g. body received from the provider).
	// Mirrors GenerateObjectResult.response in the TS SDK.
	Response GenerateStepResponse `json:"response"`

	// ProviderMetadata holds provider-specific metadata.
	// Mirrors GenerateObjectResult.providerMetadata in the TS SDK.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
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
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	// Set default output mode
	if opts.OutputMode == "" {
		opts.OutputMode = ObjectModeObject
	}
	maxRetries, maxRetryErr := normalizeObjectMaxRetries(opts.MaxRetries)
	if maxRetryErr != nil {
		return nil, maxRetryErr
	}
	opts.MaxRetries = maxRetries

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

	if opts.OutputMode != ObjectModeNoSchema && !opts.Model.SupportsStructuredOutput() {
		return nil, fmt.Errorf("model does not support structured output")
	}

	// Generate a call ID for correlating all callback events for this call.
	callID := newCallID()

	// Extract telemetry info for callback events.
	cbFuncID, cbMeta := telemetryCallbackInfo(opts.ExperimentalTelemetry)

	// Build telemetry bool helpers
	var isEnabled, recordInputs, recordOutputs *bool
	if opts.ExperimentalTelemetry != nil {
		isEnabled = opts.ExperimentalTelemetry.IsEnabled
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
	}, resolveObjectOnStart(opts.OnStart, opts.ExperimentalOnStart))

	// Route telemetry through the shared Fire* dispatch instead of creating
	// an OTel span directly in core (G4/5d0f18e): with no integration
	// registered this is a no-op, and with one registered it creates exactly
	// one "ai.generateObject" span instead of a duplicate.
	telObjectRecordInputs := opts.ExperimentalTelemetry == nil || opts.ExperimentalTelemetry.RecordInputs
	objectMaxRetries := opts.MaxRetries
	startEvent := telemetry.TelemetryStartEvent{
		OperationType:    "ai.generateObject",
		ModelProvider:    opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Settings:         opts.ExperimentalTelemetry,
		Prompt:           telemetryInputValue(opts.ExperimentalTelemetry, opts.Prompt),
		Headers:          opts.Headers,
		MaxOutputTokens:  opts.MaxTokens,
		Temperature:      opts.Temperature,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		PresencePenalty:  opts.PresencePenalty,
		FrequencyPenalty: opts.FrequencyPenalty,
		Seed:             opts.Seed,
		MaxRetries:       &objectMaxRetries,
		SettingsOutput:   string(opts.OutputMode),
	}
	if telObjectRecordInputs {
		startEvent.System = opts.System
		startEvent.Messages = opts.Messages
		startEvent.Schema = schemaToMap(opts.Schema)
		startEvent.SchemaName = opts.SchemaName
		startEvent.SchemaDescription = opts.SchemaDescription
	}
	ctx = telemetry.FireOnStart(ctx, startEvent)

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

	if err != nil {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: err})
		return result, err
	}
	if result != nil {
		telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
			OperationType:    "ai.generateObject",
			Settings:         opts.ExperimentalTelemetry,
			ModelProvider:    opts.Model.Provider(),
			ModelID:          opts.Model.ModelID(),
			FinishReason:     string(result.FinishReason),
			Text:             result.Text,
			Object:           objectResultValue(opts.OutputMode, result),
			ProviderMetadata: result.ProviderMetadata,
			Usage: telemetry.TelemetryUsage{
				InputTokens:  result.Usage.InputTokens,
				OutputTokens: result.Usage.OutputTokens,
				TotalTokens:  result.Usage.TotalTokens,
			},
		})
	}

	return result, err
}

// objectResultValue returns the TS-equivalent "event.object" value for
// onObjectOperationEnd (legacy-open-telemetry.ts): the parsed object for
// object/no-schema mode, the array for array mode, or the enum string for
// enum mode.
func objectResultValue(mode ObjectOutputMode, r *GenerateObjectResult) interface{} {
	if r == nil {
		return nil
	}
	switch mode {
	case ObjectModeArray:
		return r.Array
	case ObjectModeEnum:
		return r.EnumValue
	default:
		return r.Object
	}
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
	}, resolveObjectOnStepStart(opts.OnStepStart, opts.ExperimentalOnStepStart))

	// Fire step-start/language-model-call-start telemetry (H3 item 1):
	// GenerateObject previously fired no step/model-call spans at all.
	telStep := fireObjectStepStart(ctx, "ai.generateObject", cc.callID, opts.Model, genOpts, opts.ExperimentalTelemetry)

	genResult, err := doGenerateWithRetry(telStep.modelCallCtx, opts.Model, genOpts, opts.MaxRetries)
	if err != nil {
		// Close the step span (and, for the GenAI integration, the nested
		// "chat" span) opened by fireObjectStepStart above — otherwise they
		// leak, since no fireObjectStepEnd/fireObjectLanguageModelCallEnd will
		// ever run for this step (H4 item 2).
		fireObjectStepError(telStep, opts.ExperimentalTelemetry, err)
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Log model warnings once per model call (TS generate-object.ts
	// logWarnings, called right after the model call and before the
	// step-finish event is built).
	logModelWarnings(genResult.Warnings, opts.Model.Provider(), opts.Model.ModelID())

	reasoning := extractObjectReasoning(genResult)

	reqMeta := GenerateStepRequest{Body: genResult.RawRequest}
	resMeta := generateStepResponseFromGenerateResult(opts.Model, genResult)

	fireObjectLanguageModelCallEnd(telStep, opts.Model, opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, generateResultContentParts(genResult), resMeta.ID, genResult.ProviderMetadata)
	fireObjectStepEnd(telStep, "ai.generateObject", opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, genResult.Text, resMeta.ID, resMeta.ModelID, resMeta.Timestamp, genResult.ProviderMetadata, time.Time{})

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
	}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))

	obj, _, _, err := parseObjectResult(genResult, ObjectModeObject, opts.Schema, nil, opts.Model)
	repairTextFn := effectiveRepairText(opts.RepairText, opts.ExperimentalRepairText)
	if err != nil && repairTextFn != nil {
		repairedText, repairErr := attemptRepair(ctx, repairTextFn, genResult.Text, err)
		if repairErr != nil {
			return nil, repairErr
		}
		if repairedText != nil && *repairedText != genResult.Text {
			repairedResult := *genResult
			repairedResult.Text = *repairedText
			obj, _, _, err = parseObjectResult(&repairedResult, ObjectModeObject, opts.Schema, nil, opts.Model)
		}
	}
	if err != nil {
		return nil, err
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
	}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))

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
	itemSchemaMap := cloneSchemaMap(schemaToMap(opts.Schema))
	if itemSchemaMap == nil {
		itemSchemaMap = map[string]interface{}{}
	}
	// Remove $schema from item schema (mirrors TS: const { $schema, ...itemSchema } = ...)
	delete(itemSchemaMap, "$schema")
	// Hoist definitions/$defs to the wrapper root: see hoistSchemaDefs (audit
	// row 72ec74f / WG4).
	rootDefs := hoistSchemaDefs(itemSchemaMap)
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
	for k, v := range rootDefs {
		wrappedArraySchema[k] = v
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
	}, resolveObjectOnStepStart(opts.OnStepStart, opts.ExperimentalOnStepStart))

	telStep := fireObjectStepStart(ctx, "ai.generateObject", cc.callID, opts.Model, genOpts, opts.ExperimentalTelemetry)

	genResult, err := doGenerateWithRetry(telStep.modelCallCtx, opts.Model, genOpts, opts.MaxRetries)
	if err != nil {
		// Close the step span (and, for the GenAI integration, the nested
		// "chat" span) opened by fireObjectStepStart above — otherwise they
		// leak, since no fireObjectStepEnd/fireObjectLanguageModelCallEnd will
		// ever run for this step (H4 item 2).
		fireObjectStepError(telStep, opts.ExperimentalTelemetry, err)
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Log model warnings once per model call (TS generate-object.ts
	// logWarnings, called right after the model call and before the
	// step-finish event is built).
	logModelWarnings(genResult.Warnings, opts.Model.Provider(), opts.Model.ModelID())

	arrayReasoning := extractObjectReasoning(genResult)

	arrReqMeta := GenerateStepRequest{Body: genResult.RawRequest}
	arrResMeta := generateStepResponseFromGenerateResult(opts.Model, genResult)

	fireObjectLanguageModelCallEnd(telStep, opts.Model, opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, generateResultContentParts(genResult), arrResMeta.ID, genResult.ProviderMetadata)
	fireObjectStepEnd(telStep, "ai.generateObject", opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, genResult.Text, arrResMeta.ID, arrResMeta.ModelID, arrResMeta.Timestamp, genResult.ProviderMetadata, time.Time{})

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
	}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))

	_, arr, _, err := parseObjectResult(genResult, ObjectModeArray, opts.Schema, nil, opts.Model)
	repairTextFn := effectiveRepairText(opts.RepairText, opts.ExperimentalRepairText)
	if err != nil && repairTextFn != nil {
		repairedText, repairErr := attemptRepair(ctx, repairTextFn, genResult.Text, err)
		if repairErr != nil {
			return nil, repairErr
		}
		if repairedText != nil && *repairedText != genResult.Text {
			repairedResult := *genResult
			repairedResult.Text = *repairedText
			_, arr, _, err = parseObjectResult(&repairedResult, ObjectModeArray, opts.Schema, nil, opts.Model)
		}
	}
	if err != nil {
		return nil, err
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
	}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))

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
	}, resolveObjectOnStepStart(opts.OnStepStart, opts.ExperimentalOnStepStart))

	telStep := fireObjectStepStart(ctx, "ai.generateObject", cc.callID, opts.Model, genOpts, opts.ExperimentalTelemetry)

	genResult, err := doGenerateWithRetry(telStep.modelCallCtx, opts.Model, genOpts, opts.MaxRetries)
	if err != nil {
		// Close the step span (and, for the GenAI integration, the nested
		// "chat" span) opened by fireObjectStepStart above — otherwise they
		// leak, since no fireObjectStepEnd/fireObjectLanguageModelCallEnd will
		// ever run for this step (H4 item 2).
		fireObjectStepError(telStep, opts.ExperimentalTelemetry, err)
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Log model warnings once per model call (TS generate-object.ts
	// logWarnings, called right after the model call and before the
	// step-finish event is built).
	logModelWarnings(genResult.Warnings, opts.Model.Provider(), opts.Model.ModelID())

	enumReasoning := extractObjectReasoning(genResult)

	enumReqMeta := GenerateStepRequest{Body: genResult.RawRequest}
	enumResMeta := generateStepResponseFromGenerateResult(opts.Model, genResult)

	fireObjectLanguageModelCallEnd(telStep, opts.Model, opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, generateResultContentParts(genResult), enumResMeta.ID, genResult.ProviderMetadata)
	fireObjectStepEnd(telStep, "ai.generateObject", opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, genResult.Text, enumResMeta.ID, enumResMeta.ModelID, enumResMeta.Timestamp, genResult.ProviderMetadata, time.Time{})

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
	}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))

	_, _, selectedValue, err := parseObjectResult(genResult, ObjectModeEnum, nil, opts.EnumValues, opts.Model)
	repairTextFn := effectiveRepairText(opts.RepairText, opts.ExperimentalRepairText)
	if err != nil && repairTextFn != nil {
		repairedText, repairErr := attemptRepair(ctx, repairTextFn, genResult.Text, err)
		if repairErr != nil {
			return nil, repairErr
		}
		if repairedText != nil && *repairedText != genResult.Text {
			repairedResult := *genResult
			repairedResult.Text = *repairedText
			_, _, selectedValue, err = parseObjectResult(&repairedResult, ObjectModeEnum, nil, opts.EnumValues, opts.Model)
		}
	}
	if err != nil {
		return nil, err
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
	}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))

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

func (n noopValidator) Validate(_ interface{}) error       { return nil }
func (n noopValidator) JSONSchema() map[string]interface{} { return n.jsonSchema }

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
	}, resolveObjectOnStepStart(opts.OnStepStart, opts.ExperimentalOnStepStart))

	telStep := fireObjectStepStart(ctx, "ai.generateObject", cc.callID, opts.Model, genOpts, opts.ExperimentalTelemetry)

	genResult, err := doGenerateWithRetry(telStep.modelCallCtx, opts.Model, genOpts, opts.MaxRetries)
	if err != nil {
		// Close the step span (and, for the GenAI integration, the nested
		// "chat" span) opened by fireObjectStepStart above — otherwise they
		// leak, since no fireObjectStepEnd/fireObjectLanguageModelCallEnd will
		// ever run for this step (H4 item 2).
		fireObjectStepError(telStep, opts.ExperimentalTelemetry, err)
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Log model warnings once per model call (TS generate-object.ts
	// logWarnings, called right after the model call and before the
	// step-finish event is built).
	logModelWarnings(genResult.Warnings, opts.Model.Provider(), opts.Model.ModelID())

	noSchemaReasoning := extractObjectReasoning(genResult)

	nsReqMeta := GenerateStepRequest{Body: genResult.RawRequest}
	nsResMeta := generateStepResponseFromGenerateResult(opts.Model, genResult)

	fireObjectLanguageModelCallEnd(telStep, opts.Model, opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, generateResultContentParts(genResult), nsResMeta.ID, genResult.ProviderMetadata)
	fireObjectStepEnd(telStep, "ai.generateObject", opts.ExperimentalTelemetry, genResult.FinishReason, genResult.Usage, genResult.Text, nsResMeta.ID, nsResMeta.ModelID, nsResMeta.Timestamp, genResult.ProviderMetadata, time.Time{})

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
	}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))

	obj, _, _, err := parseObjectResult(genResult, ObjectModeNoSchema, nil, nil, opts.Model)
	repairTextFn := effectiveRepairText(opts.RepairText, opts.ExperimentalRepairText)
	if err != nil && repairTextFn != nil {
		repairedText, repairErr := attemptRepair(ctx, repairTextFn, genResult.Text, err)
		if repairErr != nil {
			return nil, repairErr
		}
		if repairedText != nil && *repairedText != genResult.Text {
			repairedResult := *genResult
			repairedResult.Text = *repairedText
			obj, _, _, err = parseObjectResult(&repairedResult, ObjectModeNoSchema, nil, nil, opts.Model)
		}
	}
	if err != nil {
		return nil, err
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
	}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))

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
	Prompt   string
	Messages []types.Message
	System   string

	// Schema for the output object
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

	// RepairText repairs invalid JSON or schema-invalid object output.
	// Return nil when the output cannot be repaired. Takes precedence over
	// ExperimentalRepairText when both are set (audit row 09a52cb).
	RepairText RepairTextFunc

	// ExperimentalRepairText repairs invalid JSON or schema-invalid object output.
	// Return nil when the output cannot be repaired.
	//
	// Deprecated: use RepairText.
	ExperimentalRepairText RepairTextFunc

	// Additional HTTP headers sent with the request.
	Headers map[string]string

	// Additional provider-specific options.
	ProviderOptions map[string]interface{}

	// SchemaName is an optional name for the output schema.
	SchemaName string

	// SchemaDescription is an optional description for the output schema.
	SchemaDescription string

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// ========================================================================
	// Structured Event Callbacks.
	// These callbacks receive typed event structs and are panic-safe.
	// ========================================================================

	// OnStart is called once before any LLM call is made.
	OnStart func(ctx context.Context, e ObjectOnStartEvent)

	// ExperimentalOnStart is a deprecated alias for OnStart.
	//
	// Deprecated: use OnStart.
	ExperimentalOnStart func(ctx context.Context, e ObjectOnStartEvent)

	// OnStepStart is called just before the provider is called.
	OnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// ExperimentalOnStepStart is a deprecated alias for OnStepStart.
	//
	// Deprecated: use OnStepStart.
	ExperimentalOnStepStart func(ctx context.Context, e ObjectOnStepStartEvent)

	// OnStepEnd is called after the provider returns, before JSON parsing.
	OnStepEnd func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnStepFinish is called after the provider returns, before JSON parsing.
	//
	// Deprecated: use OnStepEnd.
	OnStepFinish func(ctx context.Context, e ObjectOnStepFinishEvent)

	// OnEnd is called when the operation completes.
	// For StreamObject, the event Error field may be set if parsing failed.
	OnEnd func(ctx context.Context, e ObjectOnFinishEvent)

	// OnFinishEvent is a deprecated alias for OnEnd.
	//
	// Deprecated: use OnEnd.
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
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	if opts.OutputMode == "" {
		opts.OutputMode = ObjectModeObject
	}
	maxRetries, maxRetryErr := normalizeObjectMaxRetries(opts.MaxRetries)
	if maxRetryErr != nil {
		return nil, maxRetryErr
	}
	opts.MaxRetries = maxRetries
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
	if opts.OutputMode != ObjectModeNoSchema && !opts.Model.SupportsStructuredOutput() {
		return nil, fmt.Errorf("model does not support structured output")
	}

	// Generate call ID and extract telemetry info for callback events.
	callID := newCallID()
	cbFuncID, cbMeta := telemetryCallbackInfo(opts.ExperimentalTelemetry)

	var isEnabled, recordInputs, recordOutputs *bool
	if opts.ExperimentalTelemetry != nil {
		isEnabled = opts.ExperimentalTelemetry.IsEnabled
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
		Output:            opts.OutputMode,
		Schema:            schemaToMap(opts.Schema),
		SchemaName:        opts.SchemaName,
		SchemaDescription: opts.SchemaDescription,
		IsEnabled:         isEnabled,
		RecordInputs:      recordInputs,
		RecordOutputs:     recordOutputs,
		FunctionID:        cbFuncID,
		Metadata:          cbMeta,
	}, resolveObjectOnStart(opts.OnStart, opts.ExperimentalOnStart))

	// Route telemetry through the shared Fire* dispatch (H3 item 1):
	// StreamObject previously fired no telemetry spans whatsoever, unlike
	// GenerateObject/GenerateText/StreamText.
	streamObjectRecordInputs := opts.ExperimentalTelemetry == nil || opts.ExperimentalTelemetry.RecordInputs
	streamObjectMaxRetries := opts.MaxRetries
	startEvent := telemetry.TelemetryStartEvent{
		OperationType:    "ai.streamObject",
		ModelProvider:    opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Settings:         opts.ExperimentalTelemetry,
		Prompt:           telemetryInputValue(opts.ExperimentalTelemetry, opts.Prompt),
		Headers:          opts.Headers,
		MaxOutputTokens:  opts.MaxTokens,
		Temperature:      opts.Temperature,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		PresencePenalty:  opts.PresencePenalty,
		FrequencyPenalty: opts.FrequencyPenalty,
		Seed:             opts.Seed,
		MaxRetries:       &streamObjectMaxRetries,
		SettingsOutput:   string(opts.OutputMode),
	}
	if streamObjectRecordInputs {
		startEvent.System = opts.System
		startEvent.Messages = opts.Messages
		startEvent.Schema = schemaToMap(opts.Schema)
		startEvent.SchemaName = opts.SchemaName
		startEvent.SchemaDescription = opts.SchemaDescription
	}
	ctx = telemetry.FireOnStart(ctx, startEvent)

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
		ResponseFormat:   nil,
		Telemetry:        opts.ExperimentalTelemetry,
	}
	responseFormat, err := buildStreamObjectResponseFormat(opts)
	if err != nil {
		return nil, err
	}
	genOpts.ResponseFormat = responseFormat

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
	}, resolveObjectOnStepStart(opts.OnStepStart, opts.ExperimentalOnStepStart))

	telStep := fireObjectStepStart(ctx, "ai.streamObject", callID, opts.Model, genOpts, opts.ExperimentalTelemetry)

	stream, err := doStreamWithRetry(telStep.modelCallCtx, opts.Model, genOpts, opts.MaxRetries)
	if err != nil || stream == nil {
		if err == nil {
			err = errors.New("stream is nil")
		}
		if opts.OnError != nil {
			safeInvoke(func() { opts.OnError(ctx, err) })
		}
		// Close the step span (and, for the GenAI integration, the nested
		// "chat" span) opened by fireObjectStepStart above before the root
		// FireOnError call below, which only closes the root span (H4 item 2).
		fireObjectStepError(telStep, opts.ExperimentalTelemetry, err)
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: err})
		return nil, fmt.Errorf("stream error: %w", err)
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

	// Response metadata fields updated by ChunkTypeResponseMetadata chunks.
	streamResMeta := GenerateStepResponse{
		ID:        newCallID(),
		Timestamp: time.Now(),
		ModelID:   opts.Model.ModelID(),
	}

	// firstChunkAt records when the first (non-stream-start) chunk arrived,
	// mirroring TS stream-object.ts's `isFirstChunk`/`msToFirstChunk`
	// tracking in the TransformStream transform() — used by
	// fireObjectStepEnd to populate Performance.TimeToFirstOutputMs for the
	// legacy onObjectStepEnd "ai.stream.firstChunk" span event.
	var firstChunkAt time.Time

	// Process stream chunks. On a non-EOF error we record it and break so that
	// OnStepFinish / OnFinishEvent still fire with whatever was accumulated —
	// matching the TS SDK's TransformStream flush behaviour where the flush
	// handler always executes even after a transform error.
streamLoop:
	for {
		chunk, chunkErr := stream.Next()
		if chunkErr != nil {
			if chunkErr == io.EOF || errors.Is(chunkErr, io.ErrClosedPipe) {
				break
			}
			if opts.OnError != nil {
				safeInvoke(func() { opts.OnError(ctx, chunkErr) })
			}
			streamErr = chunkErr
			break
		}

		// Collect any warnings from any chunk.
		if len(chunk.Warnings) > 0 {
			streamWarnings = append(streamWarnings, chunk.Warnings...)
		}

		// Record the first chunk's arrival time, mirroring TS's isFirstChunk
		// flag (stream-object.ts): TS skips its synthetic 'stream-start'
		// chunk before setting msToFirstChunk, so ChunkTypeStreamStart (a
		// warnings-only marker, handled above) is excluded here too.
		if firstChunkAt.IsZero() && chunk.Type != provider.ChunkTypeStreamStart {
			firstChunkAt = time.Now()
		}

		// Handle different chunk types
		switch chunk.Type {
		case provider.ChunkTypeText:
			// Accumulate text
			accumulatedText += chunk.Text

			// Try to parse partial JSON
			parseResult := parsePartialJSON(accumulatedText)

			// If we successfully parsed something and it's different from last
			if partial, ok := parseStreamPartial(opts.OutputMode, opts.Schema, opts.EnumValues, parseResult); ok && !deepEqual(partial, lastObject) {
				lastObject = partial
				if opts.OnChunk != nil {
					safeInvoke(func() { opts.OnChunk(lastObject) })
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

		case provider.ChunkTypeError:
			// A provider error part is terminal: stop reading immediately
			// rather than continuing to consume chunks after it, and report
			// finishReason "error" instead of whatever finish reason (if
			// any) the model happened to send. Mirrors TS stream-object.ts's
			// TransformStream error handling (audit row b181020 / WG5).
			chunkErr := errors.New(chunk.Text)
			if opts.OnError != nil {
				safeInvoke(func() { opts.OnError(ctx, chunkErr) })
			}
			streamErr = chunkErr
			finishReason = types.FinishReasonError
			break streamLoop

		case provider.ChunkTypeResponseMetadata:
			if chunk.ResponseMetadata != nil {
				if chunk.ResponseMetadata.Headers != nil {
					streamResMeta.Headers = chunk.ResponseMetadata.Headers
				}
				if chunk.ResponseMetadata.ID != "" {
					streamResMeta.ID = chunk.ResponseMetadata.ID
				}
				if !chunk.ResponseMetadata.Timestamp.IsZero() {
					streamResMeta.Timestamp = chunk.ResponseMetadata.Timestamp
				}
				if chunk.ResponseMetadata.ModelID != "" {
					streamResMeta.ModelID = chunk.ResponseMetadata.ModelID
				}
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

	// Log model warnings once per model call (TS stream-object.ts
	// logWarnings, called once the stream's terminal chunk has been
	// processed, regardless of whether it ended in an error).
	logModelWarnings(streamWarnings, opts.Model.Provider(), opts.Model.ModelID())

	streamReqMeta := GenerateStepRequest{}

	// Fire language-model-call-end/step-end telemetry unconditionally, like
	// the callback events above — TS's TransformStream flush handler always
	// runs, whether the stream ended cleanly or with a content error (H3
	// item 1).
	//
	// Content is the accumulated text (+ reasoning, if any) rather than nil:
	// GenAI's OnLanguageModelCallEnd builds gen_ai.output.messages from it
	// (formatOutputMessages), matching TS's flush handler, which always
	// passes the accumulated text/reasoning to its language-model-call-end
	// event regardless of streaming.
	var streamCallEndContent []types.ContentPart
	if accumulatedText != "" {
		streamCallEndContent = append(streamCallEndContent, types.TextContent{Text: accumulatedText})
	}
	if accumulatedReasoning != "" {
		streamCallEndContent = append(streamCallEndContent, types.ReasoningContent{Text: accumulatedReasoning})
	}
	fireObjectLanguageModelCallEnd(telStep, opts.Model, opts.ExperimentalTelemetry, finishReason, usage, streamCallEndContent, streamResMeta.ID, streamProviderMetadata)
	fireObjectStepEnd(telStep, "ai.streamObject", opts.ExperimentalTelemetry, finishReason, usage, accumulatedText, streamResMeta.ID, streamResMeta.ModelID, streamResMeta.Timestamp, streamProviderMetadata, firstChunkAt)

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
		}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))
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
		}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: streamErr})
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
	}, resolveObjectOnStepEnd(opts.OnStepEnd, opts.OnStepFinish))

	// Parse final JSON
	var finalObject interface{}
	var finalArray []interface{}
	var finalEnum string
	streamResult := &types.GenerateResult{
		Text:         accumulatedText,
		FinishReason: finishReason,
		Usage:        usage,
		ResponseMetadata: &types.ResponseMetadata{
			ID:        streamResMeta.ID,
			Timestamp: streamResMeta.Timestamp,
			ModelID:   streamResMeta.ModelID,
			Headers:   streamResMeta.Headers,
		},
	}
	parsedObject, parsedArray, parsedEnum, parseErr := parseObjectResult(streamResult, opts.OutputMode, opts.Schema, opts.EnumValues, opts.Model)
	repairTextFn := effectiveRepairText(opts.RepairText, opts.ExperimentalRepairText)
	if parseErr != nil && repairTextFn != nil {
		repairedText, repairErr := attemptRepair(ctx, repairTextFn, accumulatedText, parseErr)
		if repairErr != nil {
			parseErr = repairErr
		} else if repairedText != nil && *repairedText != accumulatedText {
			repairedResult := *streamResult
			repairedResult.Text = *repairedText
			parsedObject, parsedArray, parsedEnum, parseErr = parseObjectResult(&repairedResult, opts.OutputMode, opts.Schema, opts.EnumValues, opts.Model)
		}
	}
	if parseErr != nil {
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
		}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: parseErr})
		return nil, parseErr
	}
	finalObject = parsedObject
	finalArray = parsedArray
	finalEnum = parsedEnum

	// Build result. Reasoning is accumulated for convenience but note that TS SDK does
	// not expose reasoning in the streaming object path (onStepFinish/onFinish receive undefined).
	result := &GenerateObjectResult{
		Object:           finalObject,
		Array:            finalArray,
		EnumValue:        finalEnum,
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
	}, resolveObjectOnEnd(opts.OnEnd, opts.OnFinishEvent))
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		OperationType:    "ai.streamObject",
		Settings:         opts.ExperimentalTelemetry,
		ModelProvider:    opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		FinishReason:     string(finishReason),
		Text:             accumulatedText,
		Object:           objectResultValue(opts.OutputMode, result),
		ProviderMetadata: streamProviderMetadata,
		Usage:            telemetryUsageFromUsage(usage),
	})

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
