package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Output represents a specification for how to handle model output
// It defines both the expected response format and how to parse the output
type Output[OUTPUT any, PARTIAL any] interface {
	// ResponseFormat returns the response format to use for the model
	ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error)

	// ParseCompleteOutput parses the complete output from the model
	ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) (OUTPUT, error)

	// ParsePartialOutput parses partial output from streaming responses
	ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[PARTIAL], error)
}

// ParseCompleteOutputOptions contains options for parsing complete output
type ParseCompleteOutputOptions struct {
	Text         string
	Response     *types.ResponseMetadata
	Usage        *types.Usage
	FinishReason types.FinishReason
}

// ParsePartialOutputOptions contains options for parsing partial output
type ParsePartialOutputOptions struct {
	Text string
}

// PartialOutput represents a partial output result
type PartialOutput[T any] struct {
	Partial T
}

// NoObjectGeneratedError is returned when object generation fails
type NoObjectGeneratedError struct {
	Message      string
	Cause        error
	Text         string
	Response     *types.ResponseMetadata
	Usage        *types.Usage
	FinishReason types.FinishReason
}

func (e *NoObjectGeneratedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *NoObjectGeneratedError) Unwrap() error {
	return e.Cause
}

// =============================================================================
// Text Output
// =============================================================================

// textOutput is the implementation of Output for plain text generation
type textOutput struct{}

// TextOutput creates an output specification for generating plain text
// This is the default output mode.
func TextOutput() Output[string, string] {
	return &textOutput{}
}

func (o *textOutput) ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error) {
	return &provider.ResponseFormat{
		Type: "text",
	}, nil
}

func (o *textOutput) ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) (string, error) {
	return options.Text, nil
}

func (o *textOutput) ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[string], error) {
	return &PartialOutput[string]{
		Partial: options.Text,
	}, nil
}

// =============================================================================
// Object Output
// =============================================================================

// ObjectOutputOptions contains options for object output generation
type ObjectOutputOptions struct {
	// Schema defines the structure of the object to generate
	Schema schema.Schema

	// Name is an optional name of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema name)
	Name string

	// Description is an optional description of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema description)
	Description string
}

// objectOutput is the implementation of Output for typed object generation
type objectOutput[T any] struct {
	schema      schema.Schema
	name        string
	description string
}

// ObjectOutput creates an output specification for generating typed objects using schemas
// When the model generates a text response, it will return an object that matches the schema.
//
// Example:
//
//	type Person struct {
//	    Name string `json:"name"`
//	    Age  int    `json:"age"`
//	}
//
//	output := ObjectOutput[Person](ObjectOutputOptions{
//	    Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
//	        "type": "object",
//	        "properties": map[string]interface{}{
//	            "name": map[string]interface{}{"type": "string"},
//	            "age":  map[string]interface{}{"type": "integer"},
//	        },
//	        "required": []string{"name", "age"},
//	    }),
//	    Name:        "person",
//	    Description: "Information about a person",
//	})
func ObjectOutput[T any](opts ObjectOutputOptions) Output[T, T] {
	return &objectOutput[T]{
		schema:      opts.Schema,
		name:        opts.Name,
		description: opts.Description,
	}
}

func (o *objectOutput[T]) ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error) {
	jsonSchema := o.schema.Validator().JSONSchema()

	format := &provider.ResponseFormat{
		Type:   "json",
		Schema: jsonSchema,
	}

	if o.name != "" {
		format.Name = o.name
	}
	if o.description != "" {
		format.Description = o.description
	}

	return format, nil
}

func (o *objectOutput[T]) ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) (T, error) {
	var zero T

	// Parse JSON
	var result T
	if err := json.Unmarshal([]byte(options.Text), &result); err != nil {
		return zero, &NoObjectGeneratedError{
			Message:      "No object generated: could not parse the response",
			Cause:        err,
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	// Validate against schema
	if err := o.schema.Validator().Validate(result); err != nil {
		return zero, &NoObjectGeneratedError{
			Message:      "No object generated: response did not match schema",
			Cause:        err,
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	return result, nil
}

func (o *objectOutput[T]) ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[T], error) {
	// Try to parse as partial JSON
	parsed, err := jsonutil.ParsePartialJSON(options.Text)
	if err != nil {
		return nil, nil // No partial result available yet
	}

	if parsed == nil {
		return nil, nil
	}

	// Convert to expected type
	// Note: This does not validate partial results, matching TypeScript behavior
	jsonBytes, err := json.Marshal(parsed)
	if err != nil {
		return nil, nil
	}

	var partial T
	if err := json.Unmarshal(jsonBytes, &partial); err != nil {
		return nil, nil
	}

	return &PartialOutput[T]{
		Partial: partial,
	}, nil
}

// =============================================================================
// Array Output
// =============================================================================

// ArrayOutputOptions contains options for array output generation
type ArrayOutputOptions[ELEMENT any] struct {
	// ElementSchema defines the structure of each array element
	ElementSchema schema.Schema

	// Name is an optional name of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema name)
	Name string

	// Description is an optional description of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema description)
	Description string
}

// arrayOutput is the implementation of Output for array generation
type arrayOutput[ELEMENT any] struct {
	elementSchema schema.Schema
	name          string
	description   string
}

// ArrayOutput creates an output specification for generating arrays of elements
// When the model generates a text response, it will return an array of elements.
//
// Example:
//
//	output := ArrayOutput[Person](ArrayOutputOptions[Person]{
//	    ElementSchema: schema.NewSimpleJSONSchema(personSchema),
//	    Name:          "people",
//	    Description:   "List of people",
//	})
func ArrayOutput[ELEMENT any](opts ArrayOutputOptions[ELEMENT]) Output[[]ELEMENT, []ELEMENT] {
	return &arrayOutput[ELEMENT]{
		elementSchema: opts.ElementSchema,
		name:          opts.Name,
		description:   opts.Description,
	}
}

func (o *arrayOutput[ELEMENT]) ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error) {
	elementJSONSchema := o.elementSchema.Validator().JSONSchema()

	// Remove $schema from element schema if present
	delete(elementJSONSchema, "$schema")

	// Create array wrapper schema
	arraySchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"elements": map[string]interface{}{
				"type":  "array",
				"items": elementJSONSchema,
			},
		},
		"required":             []string{"elements"},
		"additionalProperties": false,
	}

	format := &provider.ResponseFormat{
		Type:   "json",
		Schema: arraySchema,
	}

	if o.name != "" {
		format.Name = o.name
	}
	if o.description != "" {
		format.Description = o.description
	}

	return format, nil
}

func (o *arrayOutput[ELEMENT]) ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) ([]ELEMENT, error) {
	// Parse JSON
	var wrapper struct {
		Elements []interface{} `json:"elements"`
	}

	if err := json.Unmarshal([]byte(options.Text), &wrapper); err != nil {
		return nil, &NoObjectGeneratedError{
			Message:      "No object generated: could not parse the response",
			Cause:        err,
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	if wrapper.Elements == nil {
		return nil, &NoObjectGeneratedError{
			Message:      "No object generated: response did not match schema",
			Cause:        fmt.Errorf("response must be an object with an elements array"),
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	// Validate and convert each element
	elements := make([]ELEMENT, len(wrapper.Elements))
	for i, elem := range wrapper.Elements {
		// Validate element against schema
		if err := o.elementSchema.Validator().Validate(elem); err != nil {
			return nil, &NoObjectGeneratedError{
				Message:      "No object generated: response did not match schema",
				Cause:        err,
				Text:         options.Text,
				Response:     options.Response,
				Usage:        options.Usage,
				FinishReason: options.FinishReason,
			}
		}

		// Convert to typed element
		jsonBytes, err := json.Marshal(elem)
		if err != nil {
			return nil, &NoObjectGeneratedError{
				Message:      "No object generated: could not convert element",
				Cause:        err,
				Text:         options.Text,
				Response:     options.Response,
				Usage:        options.Usage,
				FinishReason: options.FinishReason,
			}
		}

		var typedElem ELEMENT
		if err := json.Unmarshal(jsonBytes, &typedElem); err != nil {
			return nil, &NoObjectGeneratedError{
				Message:      "No object generated: could not convert element",
				Cause:        err,
				Text:         options.Text,
				Response:     options.Response,
				Usage:        options.Usage,
				FinishReason: options.FinishReason,
			}
		}

		elements[i] = typedElem
	}

	return elements, nil
}

func (o *arrayOutput[ELEMENT]) ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[[]ELEMENT], error) {
	// Try to parse as partial JSON
	parsed, err := jsonutil.ParsePartialJSON(options.Text)
	if err != nil {
		return nil, nil
	}

	if parsed == nil {
		return nil, nil
	}

	// Check if it has an elements array
	parsedMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	elementsRaw, ok := parsedMap["elements"]
	if !ok {
		return nil, nil
	}

	elementsArray, ok := elementsRaw.([]interface{})
	if !ok {
		return nil, nil
	}

	// Parse each element that validates
	var elements []ELEMENT
	for i, elemRaw := range elementsArray {
		// Skip last element if JSON was repaired (might be incomplete)
		// This matches TypeScript behavior
		if i == len(elementsArray)-1 {
			// Check if this looks like incomplete JSON
			elemStr := fmt.Sprintf("%v", elemRaw)
			if !jsonutil.IsPartiallyValid(elemStr) {
				break
			}
		}

		// Validate element
		if err := o.elementSchema.Validator().Validate(elemRaw); err != nil {
			continue // Skip invalid elements
		}

		// Convert to typed element
		jsonBytes, err := json.Marshal(elemRaw)
		if err != nil {
			continue
		}

		var typedElem ELEMENT
		if err := json.Unmarshal(jsonBytes, &typedElem); err != nil {
			continue
		}

		elements = append(elements, typedElem)
	}

	return &PartialOutput[[]ELEMENT]{
		Partial: elements,
	}, nil
}

// =============================================================================
// Choice Output
// =============================================================================

// ChoiceOutputOptions contains options for choice output generation
type ChoiceOutputOptions[CHOICE ~string] struct {
	// Options is the list of available choices
	Options []CHOICE

	// Name is an optional name of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema name)
	Name string

	// Description is an optional description of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema description)
	Description string
}

// choiceOutput is the implementation of Output for choice generation
type choiceOutput[CHOICE ~string] struct {
	options     []CHOICE
	name        string
	description string
}

// ChoiceOutput creates an output specification for generating a choice from a list of options
// When the model generates a text response, it will return one of the choice options.
//
// Example:
//
//	type Sentiment string
//	const (
//	    Positive Sentiment = "positive"
//	    Negative Sentiment = "negative"
//	    Neutral  Sentiment = "neutral"
//	)
//
//	output := ChoiceOutput[Sentiment](ChoiceOutputOptions[Sentiment]{
//	    Options:     []Sentiment{Positive, Negative, Neutral},
//	    Name:        "sentiment",
//	    Description: "The sentiment of the text",
//	})
func ChoiceOutput[CHOICE ~string](opts ChoiceOutputOptions[CHOICE]) Output[CHOICE, CHOICE] {
	return &choiceOutput[CHOICE]{
		options:     opts.Options,
		name:        opts.Name,
		description: opts.Description,
	}
}

func (o *choiceOutput[CHOICE]) ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error) {
	// Convert options to []string for schema
	enumValues := make([]string, len(o.options))
	for i, opt := range o.options {
		enumValues[i] = string(opt)
	}

	choiceSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"result": map[string]interface{}{
				"type": "string",
				"enum": enumValues,
			},
		},
		"required":             []string{"result"},
		"additionalProperties": false,
	}

	format := &provider.ResponseFormat{
		Type:   "json",
		Schema: choiceSchema,
	}

	if o.name != "" {
		format.Name = o.name
	}
	if o.description != "" {
		format.Description = o.description
	}

	return format, nil
}

func (o *choiceOutput[CHOICE]) ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) (CHOICE, error) {
	var zero CHOICE

	// Parse JSON
	var wrapper struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal([]byte(options.Text), &wrapper); err != nil {
		return zero, &NoObjectGeneratedError{
			Message:      "No object generated: could not parse the response",
			Cause:        err,
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	// Validate that result is one of the options
	var found bool
	for _, opt := range o.options {
		if wrapper.Result == string(opt) {
			found = true
			break
		}
	}

	if !found {
		return zero, &NoObjectGeneratedError{
			Message:      "No object generated: response did not match schema",
			Cause:        fmt.Errorf("response must be an object that contains a choice value"),
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	return CHOICE(wrapper.Result), nil
}

func (o *choiceOutput[CHOICE]) ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[CHOICE], error) {
	var zero CHOICE

	// Try to parse as partial JSON
	parsed, err := jsonutil.ParsePartialJSON(options.Text)
	if err != nil {
		return nil, nil
	}

	if parsed == nil {
		return nil, nil
	}

	// Check if it has a result field
	parsedMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	resultRaw, ok := parsedMap["result"]
	if !ok {
		return nil, nil
	}

	resultStr, ok := resultRaw.(string)
	if !ok {
		return nil, nil
	}

	// Find potential matches (for partial strings)
	var potentialMatches []CHOICE
	for _, opt := range o.options {
		optStr := string(opt)
		if len(resultStr) <= len(optStr) && optStr[:len(resultStr)] == resultStr {
			potentialMatches = append(potentialMatches, opt)
		}
	}

	// If no matches, return nil
	if len(potentialMatches) == 0 {
		return nil, nil
	}

	// For exact matches, return immediately
	for _, opt := range o.options {
		if resultStr == string(opt) {
			return &PartialOutput[CHOICE]{
				Partial: opt,
			}, nil
		}
	}

	// For partial matches, only return if unambiguous
	if len(potentialMatches) == 1 {
		return &PartialOutput[CHOICE]{
			Partial: potentialMatches[0],
		}, nil
	}

	return &PartialOutput[CHOICE]{
		Partial: zero,
	}, nil
}

// =============================================================================
// JSON Output (Untyped)
// =============================================================================

// JSONOutputOptions contains options for JSON output generation
type JSONOutputOptions struct {
	// Name is an optional name of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema name)
	Name string

	// Description is an optional description of the output
	// Used by some providers for additional LLM guidance (e.g., via tool or schema description)
	Description string
}

// jsonOutput is the implementation of Output for unstructured JSON generation
type jsonOutput struct {
	name        string
	description string
}

// JSONOutput creates an output specification for generating unstructured JSON
// When the model generates a text response, it will return a JSON value (object, array, etc.)
//
// Example:
//
//	output := JSONOutput(JSONOutputOptions{
//	    Name:        "data",
//	    Description: "Arbitrary JSON data",
//	})
func JSONOutput(opts JSONOutputOptions) Output[interface{}, interface{}] {
	return &jsonOutput{
		name:        opts.Name,
		description: opts.Description,
	}
}

func (o *jsonOutput) ResponseFormat(ctx context.Context) (*provider.ResponseFormat, error) {
	format := &provider.ResponseFormat{
		Type: "json",
	}

	if o.name != "" {
		format.Name = o.name
	}
	if o.description != "" {
		format.Description = o.description
	}

	return format, nil
}

func (o *jsonOutput) ParseCompleteOutput(ctx context.Context, options ParseCompleteOutputOptions) (interface{}, error) {
	// Parse JSON
	var result interface{}
	if err := json.Unmarshal([]byte(options.Text), &result); err != nil {
		return nil, &NoObjectGeneratedError{
			Message:      "No object generated: could not parse the response",
			Cause:        err,
			Text:         options.Text,
			Response:     options.Response,
			Usage:        options.Usage,
			FinishReason: options.FinishReason,
		}
	}

	return result, nil
}

func (o *jsonOutput) ParsePartialOutput(ctx context.Context, options ParsePartialOutputOptions) (*PartialOutput[interface{}], error) {
	// Try to parse as partial JSON
	parsed, err := jsonutil.ParsePartialJSON(options.Text)
	if err != nil {
		return nil, nil
	}

	if parsed == nil {
		return nil, nil
	}

	return &PartialOutput[interface{}]{
		Partial: parsed,
	}, nil
}
