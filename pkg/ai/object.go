package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

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
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Seed             *int

	// Callbacks
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
}

// GenerateObject performs structured object generation
// The output is a JSON object that conforms to the provided schema
// Supports multiple output modes: object, array, enum, no-schema
func GenerateObject(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Set default output mode
	if opts.OutputMode == "" {
		opts.OutputMode = ObjectModeObject
	}

	// Validate mode-specific requirements
	switch opts.OutputMode {
	case ObjectModeObject, ObjectModeArray:
		if opts.Schema == nil {
			return nil, fmt.Errorf("schema is required for %s mode", opts.OutputMode)
		}
	case ObjectModeEnum:
		if len(opts.EnumValues) == 0 {
			return nil, fmt.Errorf("enum values are required for enum mode")
		}
	case ObjectModeNoSchema:
		// No schema required
	default:
		return nil, fmt.Errorf("invalid output mode: %s", opts.OutputMode)
	}

	// Check if model supports structured output (except for no-schema mode)
	if opts.OutputMode != ObjectModeNoSchema && !opts.Model.SupportsStructuredOutput() {
		return nil, fmt.Errorf("model does not support structured output")
	}

	// Handle different modes
	switch opts.OutputMode {
	case ObjectModeObject:
		return generateObjectMode(ctx, opts)
	case ObjectModeArray:
		return generateArrayMode(ctx, opts)
	case ObjectModeEnum:
		return generateEnumMode(ctx, opts)
	case ObjectModeNoSchema:
		return generateNoSchemaMode(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported output mode: %s", opts.OutputMode)
	}
}

// generateObjectMode handles standard object generation
func generateObjectMode(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: opts.Schema,
		},
	}

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	var obj interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w", err)
	}

	if err := opts.Schema.Validator().Validate(obj); err != nil {
		return nil, fmt.Errorf("output validation failed: %w", err)
	}

	result := &GenerateObjectResult{
		Object:       obj,
		Text:         genResult.Text,
		FinishReason: genResult.FinishReason,
		Usage:        genResult.Usage,
		Warnings:     genResult.Warnings,
	}

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// generateArrayMode handles array generation
func generateArrayMode(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: opts.Schema,
		},
	}

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	var arr []interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &arr); err != nil {
		return nil, fmt.Errorf("failed to parse JSON array: %w", err)
	}

	// Validate each element
	for i, element := range arr {
		if err := opts.Schema.Validator().Validate(element); err != nil {
			return nil, fmt.Errorf("validation failed for element %d: %w", i, err)
		}
	}

	result := &GenerateObjectResult{
		Array:        arr,
		Text:         genResult.Text,
		FinishReason: genResult.FinishReason,
		Usage:        genResult.Usage,
		Warnings:     genResult.Warnings,
	}

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// generateEnumMode handles enum selection
func generateEnumMode(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	// Build prompt that forces enum selection
	enumPrompt := fmt.Sprintf("%s\n\nYou must respond with exactly one of these values: %v",
		opts.Prompt, opts.EnumValues)

	prompt := buildPrompt(enumPrompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json_object",
		},
	}

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Parse and extract enum value
	selectedValue := genResult.Text

	// Validate enum value
	valid := false
	for _, enumVal := range opts.EnumValues {
		if selectedValue == enumVal {
			valid = true
			break
		}
	}

	if !valid {
		return nil, fmt.Errorf("invalid enum value: %s (expected one of %v)", selectedValue, opts.EnumValues)
	}

	result := &GenerateObjectResult{
		EnumValue:    selectedValue,
		Text:         genResult.Text,
		FinishReason: genResult.FinishReason,
		Usage:        genResult.Usage,
		Warnings:     genResult.Warnings,
	}

	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	return result, nil
}

// generateNoSchemaMode handles raw JSON generation without validation
func generateNoSchemaMode(ctx context.Context, opts GenerateObjectOptions) (*GenerateObjectResult, error) {
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json_object",
		},
	}

	genResult, err := opts.Model.DoGenerate(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	var obj interface{}
	if err := json.Unmarshal([]byte(genResult.Text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w", err)
	}

	result := &GenerateObjectResult{
		Object:       obj,
		Text:         genResult.Text,
		FinishReason: genResult.FinishReason,
		Usage:        genResult.Usage,
		Warnings:     genResult.Warnings,
	}

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
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Seed             *int

	// Callbacks
	OnChunk  func(partialObject interface{})
	OnFinish func(ctx context.Context, result *GenerateObjectResult, userContext interface{})

	// ExperimentalContext allows passing custom context through generation lifecycle
	ExperimentalContext interface{}
}

// StreamObject performs streaming object generation
// As JSON is streamed, partial objects are parsed and validated
func StreamObject(ctx context.Context, opts StreamObjectOptions) (*GenerateObjectResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Schema == nil {
		return nil, fmt.Errorf("schema is required")
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Create streaming generation options
	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Seed:             opts.Seed,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: opts.Schema,
		},
	}

	// Try to start streaming
	// If streaming is not supported or fails, fall back to non-streaming
	stream, err := opts.Model.DoStream(ctx, genOpts)
	if err != nil || stream == nil {
		// Fallback to non-streaming generation
		result, err := opts.Model.DoGenerate(ctx, genOpts)
		if err != nil {
			return nil, fmt.Errorf("generation failed: %w", err)
		}

		// Parse final JSON
		var finalObject interface{}
		if err := json.Unmarshal([]byte(result.Text), &finalObject); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}

		// Validate
		if err := opts.Schema.Validator().Validate(finalObject); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}

		finalResult := &GenerateObjectResult{
			Object:       finalObject,
			Text:         result.Text,
			FinishReason: result.FinishReason,
			Usage:        result.Usage,
			Warnings:     result.Warnings,
		}

		if opts.OnFinish != nil {
			opts.OnFinish(ctx, finalResult, opts.ExperimentalContext)
		}

		return finalResult, nil
	}
	defer stream.Close()

	// Accumulate text and track partial objects
	var accumulatedText string
	var lastObject interface{}
	var usage types.Usage
	var finishReason types.FinishReason

	// Process stream chunks
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err.Error() == "EOF" || err.Error() == "io: read/write on closed pipe" {
				break
			}
			return nil, fmt.Errorf("stream error: %w", err)
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
	}

	// Parse final JSON
	var finalObject interface{}
	if accumulatedText != "" {
		if err := json.Unmarshal([]byte(accumulatedText), &finalObject); err != nil {
			return nil, fmt.Errorf("failed to parse final JSON: %w", err)
		}

		// Validate final object
		if err := opts.Schema.Validator().Validate(finalObject); err != nil {
			return nil, fmt.Errorf("final object validation failed: %w", err)
		}
	}

	// Build result
	result := &GenerateObjectResult{
		Object:       finalObject,
		Text:         accumulatedText,
		FinishReason: finishReason,
		Usage:        usage,
	}

	// Call OnFinish if provided
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
