package responses

import (
	"regexp"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// textFormatNamePattern is the json_schema format name rule of TS
// openaiResponsesTextFormatSchema (openai-responses-text-format.ts).
var textFormatNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ParseTextFormat validates a textFormat (request level) or textFormatUpdate
// (system message) option like TS openaiResponsesTextFormatSchema, a
// discriminated union of {type: 'text'} and {type: 'json_schema', name
// (1-64 chars, [a-zA-Z0-9_-]), schema (object), description?, strict?}.
// Like zod's object output, unknown keys are dropped and the keys follow the
// schema order. field names the option in the error (for example
// "providerOptions.openai.textFormat").
func ParseTextFormat(field string, value interface{}) (*jsonorder.Object, error) {
	invalid := func(sub, message string) error {
		f := field
		if sub != "" {
			f += "." + sub
		}
		return &providererrors.InvalidArgumentError{Field: f, Message: message}
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, invalid("", "must be an object")
	}
	out := jsonorder.New()
	switch m["type"] {
	case "text":
		out.Set("type", "text")
		return out, nil
	case "json_schema":
	default:
		return nil, invalid("type", `must be one of "text", "json_schema"`)
	}
	name, ok := m["name"].(string)
	if !ok || len(name) < 1 || len(name) > 64 || !textFormatNamePattern.MatchString(name) {
		return nil, invalid("name", "must be a string of 1 to 64 characters matching /^[a-zA-Z0-9_-]+$/")
	}
	schema, ok := m["schema"].(map[string]interface{})
	if !ok {
		return nil, invalid("schema", "must be an object")
	}
	out.Set("type", "json_schema")
	out.Set("name", name)
	out.Set("schema", schema)
	if v, present := m["description"]; present {
		s, isString := v.(string)
		if !isString {
			return nil, invalid("description", "must be a string")
		}
		out.Set("description", s)
	}
	if v, present := m["strict"]; present {
		b, isBool := v.(bool)
		if !isBool {
			return nil, invalid("strict", "must be a boolean")
		}
		out.Set("strict", b)
	}
	return out, nil
}

// NormalizeTextFormat is TS normalizeOpenAIResponsesTextFormat: a json_schema
// format's schema goes through normalize (normalizeOpenAIJsonSchema), its
// warnings appended; a text format is returned as is.
func NormalizeTextFormat(format *jsonorder.Object, normalize func(schema interface{}) (interface{}, []types.Warning, error), warnings *[]types.Warning) (*jsonorder.Object, error) {
	if format == nil || normalize == nil {
		return format, nil
	}
	if t, _ := format.Get("type"); t != "json_schema" {
		return format, nil
	}
	schema, _ := format.Get("schema")
	normalized, schemaWarnings, err := normalize(schema)
	if err != nil {
		return nil, err
	}
	if warnings != nil {
		*warnings = append(*warnings, schemaWarnings...)
	}
	out := jsonorder.New()
	for _, k := range format.Keys() {
		v, _ := format.Get(k)
		if k == "schema" {
			v = normalized
		}
		out.Set(k, v)
	}
	return out, nil
}
