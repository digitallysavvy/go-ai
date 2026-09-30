package providerutils

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ChatResponseFormatOptions configures ChatResponseFormat.
type ChatResponseFormatOptions struct {
	// StructuredOutputs enables json_schema output when a schema is present.
	// When false, JSON mode falls back to {"type":"json_object"}.
	StructuredOutputs bool

	// StrictJSONSchema is sent as json_schema.strict.
	StrictJSONSchema bool

	// WarnWhenSchemaUnsupported emits the TS "JSON response format schema is
	// only supported with structuredOutputs" warning when a schema is dropped.
	WarnWhenSchemaUnsupported bool
}

// IsJSONResponseFormat reports whether the response format requests JSON
// output (TS responseFormat.type === 'json'). The Go SDK also uses
// "json_schema" and "json_object" for the same intent.
func IsJSONResponseFormat(rf *provider.ResponseFormat) bool {
	if rf == nil {
		return false
	}
	switch rf.Type {
	case "json", "json_schema", "json_object":
		return true
	}
	return false
}

// ChatResponseFormat builds the Chat Completions `response_format` value used
// by OpenAI-compatible providers. It mirrors the TS mapping shared by
// openai-chat, groq, mistral and openai-compatible chat models:
//
//	json + schema + structured outputs → {type: json_schema, json_schema: {schema, strict, name ?? 'response', description}}
//	json otherwise                     → {type: json_object}
//	text / nil                         → nil (omit)
func ChatResponseFormat(rf *provider.ResponseFormat, opts ChatResponseFormatOptions) (map[string]interface{}, []types.Warning) {
	if !IsJSONResponseFormat(rf) {
		return nil, nil
	}
	schema := ResponseFormatJSONSchema(rf.Schema)
	if rf.Type == "json_object" {
		// Explicit JSON-object mode never sends a schema.
		schema = nil
	}

	var warnings []types.Warning
	if schema != nil && !opts.StructuredOutputs && opts.WarnWhenSchemaUnsupported {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "responseFormat",
			Details: "JSON response format schema is only supported with structuredOutputs",
		})
	}

	if schema == nil || !opts.StructuredOutputs {
		return map[string]interface{}{"type": "json_object"}, warnings
	}

	name := rf.Name
	if name == "" {
		name = "response"
	}
	jsonSchema := map[string]interface{}{
		"schema": schema,
		"strict": opts.StrictJSONSchema,
		"name":   name,
	}
	if rf.Description != "" {
		jsonSchema["description"] = rf.Description
	}
	return map[string]interface{}{
		"type":        "json_schema",
		"json_schema": jsonSchema,
	}, warnings
}

// ResponseFormatJSONSchema resolves a ResponseFormat.Schema value (a JSON
// Schema map or a schema.Schema implementation) to a JSON-serializable JSON
// Schema. It returns nil when no schema is set.
func ResponseFormatJSONSchema(value interface{}) interface{} {
	type jsonSchemaProvider interface {
		JSONSchema() map[string]interface{}
	}
	switch s := value.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return s
	case jsonSchemaProvider:
		return s.JSONSchema()
	case schema.Schema:
		// schema.Schema implementations expose the JSON Schema via their validator.
		if p, ok := s.Validator().(jsonSchemaProvider); ok {
			return p.JSONSchema()
		}
	}
	return value
}

// BoolOption reads a boolean provider option, returning def when absent.
func BoolOption(options map[string]interface{}, key string, def bool) bool {
	if v, ok := options[key].(bool); ok {
		return v
	}
	return def
}
