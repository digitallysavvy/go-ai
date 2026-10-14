package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func TestSerializableSchemaRoundTrip(t *testing.T) {
	in := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"city":  map[string]interface{}{"type": "string", "description": "City name"},
			"count": map[string]interface{}{"type": "integer", "enum": []interface{}{1, 2, 3}},
		},
		"required": []interface{}{"city"},
	}
	m, err := MarshalSchema(in)
	if err != nil {
		t.Fatalf("MarshalSchema() error = %v", err)
	}
	out, err := UnmarshalSchema(m)
	if err != nil {
		t.Fatalf("UnmarshalSchema() error = %v", err)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	if obj["type"] != "object" {
		t.Fatalf("unexpected type: %v", obj["type"])
	}
}

func TestSerializeToolSetRoundTripOmitsFunctions(t *testing.T) {
	tools := []types.Tool{{
		Name:        "weather",
		Description: "Get weather",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"city"},
		},
		Strict:           types.BoolPtr(true),
		ProviderExecuted: true,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "hidden", nil
		},
	}}
	serialized, err := SerializeToolSet(tools)
	if err != nil {
		t.Fatalf("SerializeToolSet() error = %v", err)
	}
	def, ok := serialized["weather"]
	if !ok {
		t.Fatal("missing serialized tool")
	}
	if def.Parameters["type"] != "object" || def.Strict == nil || !*def.Strict || !def.ProviderExecuted {
		t.Fatalf("unexpected serialized def: %+v", def)
	}
	resolved := ResolveSerializableTools(serialized)
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved tool, got %d", len(resolved))
	}
	if resolved[0].Execute != nil {
		t.Fatal("serialized tool should not restore function fields")
	}
	if resolved[0].Name != "weather" || resolved[0].Strict == nil || !*resolved[0].Strict || !resolved[0].ProviderExecuted {
		t.Fatalf("unexpected resolved tool: %+v", resolved[0])
	}
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"city": "Tokyo"}); err != nil {
		t.Fatalf("expected valid input: %v", err)
	}
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"city": 123}); err == nil {
		t.Fatal("expected validation error")
	}
}

// TestValidateSerializableToolInputAppliesDefaultsBeforeValidating covers
// SC2 item 2: a required field declared with a JSON-Schema default must not
// fail validation just because the caller omitted it, matching
// pkg/ai/tool_call_pipeline.go's applyToolCallInputDefaults and the rest of
// the codebase's apply-defaults-before-validate convention.
func TestValidateSerializableToolInputAppliesDefaultsBeforeValidating(t *testing.T) {
	def := SerializableToolDef{
		Name: "weather",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string"},
				"unit": map[string]interface{}{"type": "string", "default": "celsius"},
			},
			"required": []interface{}{"city", "unit"},
		},
	}
	defaulted, err := ValidateSerializableToolInput(def, map[string]interface{}{"city": "Tokyo"})
	if err != nil {
		t.Fatalf("expected the missing defaulted field to pass validation, got %v", err)
	}
	got, ok := defaulted.(map[string]interface{})
	if !ok || got["unit"] != "celsius" {
		t.Fatalf("expected the returned value to carry the filled-in default, got %#v", defaulted)
	}
	// A field with no default is still required.
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"unit": "celsius"}); err == nil {
		t.Fatal("expected validation error for a missing field with no schema default")
	}
}

func objSchema(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		r := make([]interface{}, len(required))
		for i, v := range required {
			r[i] = v
		}
		s["required"] = r
	}
	return s
}

// TS #22080: formats, annotations, union types and JSON Schema 2020-12 are
// accepted; formats are annotations and not assertions.
func TestValidateSerializableToolInputAcceptsFormatsAnnotationsUnionsAnd2020(t *testing.T) {
	formatted := SerializableToolDef{Name: "formatted", InputSchema: objSchema(map[string]interface{}{
		"email": map[string]interface{}{"type": "string", "format": "email"},
		"id":    map[string]interface{}{"type": "string", "format": "uuid"},
		"x":     map[string]interface{}{"type": "string", "format": "made-up"},
	})}
	if _, err := ValidateSerializableToolInput(formatted, map[string]interface{}{"email": "nope", "id": "nope", "x": "y"}); err != nil {
		t.Fatalf("formats must not be asserted: %v", err)
	}
	annotated := SerializableToolDef{Name: "annotated", InputSchema: objSchema(map[string]interface{}{
		"value": map[string]interface{}{"type": []interface{}{"string", "number"}, "example": "example", "x-order": 1},
	})}
	if _, err := ValidateSerializableToolInput(annotated, map[string]interface{}{"value": 1.0}); err != nil {
		t.Fatalf("annotations and union types must be accepted: %v", err)
	}
	modern := objSchema(map[string]interface{}{
		"creditCard":     map[string]interface{}{"type": "number"},
		"billingAddress": map[string]interface{}{"type": "string"},
	})
	modern["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	modern["dependentRequired"] = map[string]interface{}{"creditCard": []interface{}{"billingAddress"}}
	def := SerializableToolDef{Name: "modern", InputSchema: modern}
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"creditCard": 1234.0}); err == nil {
		t.Fatal("expected 2020-12 dependentRequired violation")
	}
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"creditCard": 1234.0, "billingAddress": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TS #22392: validation errors name allowed enum values, unexpected property
// names, and every invalid array location.
func TestValidateSerializableToolInputReportsActionableDetails(t *testing.T) {
	interaction := SerializableToolDef{Name: "interaction", InputSchema: objSchema(map[string]interface{}{
		"action": map[string]interface{}{"enum": []interface{}{"click", "type", "scroll"}},
	}, "action")}
	_, err := ValidateSerializableToolInput(interaction, map[string]interface{}{"action": "press"})
	if err == nil || !strings.Contains(err.Error(), `data/action must be one of "click", "type", "scroll"`) {
		t.Fatalf("enum error = %v", err)
	}

	edit := SerializableToolDef{Name: "edit", InputSchema: objSchema(map[string]interface{}{
		"edits": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"oldText": map[string]interface{}{"type": "string"},
					"newText": map[string]interface{}{"type": "string"},
				},
				"required": []string{"oldText", "newText"},
			},
		},
	}, "edits")}
	_, err = ValidateSerializableToolInput(edit, map[string]interface{}{"edits": []interface{}{
		map[string]interface{}{"oldText": "a", "newText": "b", "path": "a.ts"},
		map[string]interface{}{"oldText": "c", "newText": "d", "path": "b.ts"},
	}})
	want := `data/edits/0 has unexpected property "path", data/edits/1 has unexpected property "path"`
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("additionalProperties error = %v, want suffix %q", err, want)
	}

	req := SerializableToolDef{Name: "req", InputSchema: objSchema(map[string]interface{}{
		"mode": map[string]interface{}{"const": "fast"},
	}, "mode", "name")}
	_, err = ValidateSerializableToolInput(req, map[string]interface{}{"mode": "slow"})
	if err == nil || !strings.Contains(err.Error(), `data is missing required property "name"`) || !strings.Contains(err.Error(), `data/mode must equal "fast"`) {
		t.Fatalf("required/const error = %v", err)
	}
}

type ownValidatorSchema struct{}

func (ownValidatorSchema) Validator() schema.Validator { return ownValidator{} }

type ownValidator struct{}

func (ownValidator) Validate(interface{}) error { return errors.New("own validator rejects") }
func (ownValidator) JSONSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "number"}}}
}

// TS #22397: tools whose schema has its own validator are flagged and not
// re-validated against the lossy JSON Schema copy.
func TestSerializeToolSetRecordsOwnValidator(t *testing.T) {
	serialized, err := SerializeToolSet([]types.Tool{
		{Name: "validated", Parameters: ownValidatorSchema{}},
		{Name: "jsonOnly", Parameters: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})},
		{Name: "plain", Parameters: map[string]interface{}{"type": "object"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !serialized["validated"].HasOwnValidator {
		t.Fatal("validated: HasOwnValidator = false, want true")
	}
	if serialized["validated"].InputSchema["type"] != "object" {
		t.Fatalf("validated: schema = %v", serialized["validated"].InputSchema)
	}
	if serialized["jsonOnly"].HasOwnValidator || serialized["plain"].HasOwnValidator {
		t.Fatal("JSON-only schemas must not set HasOwnValidator")
	}
	if serialized["jsonOnly"].InputSchema["type"] != "object" {
		t.Fatalf("jsonOnly schema lost: %v", serialized["jsonOnly"].InputSchema)
	}
	in := map[string]interface{}{"value": "42", "extra": true}
	got, err := ValidateSerializableToolInput(serialized["validated"], in)
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Fatalf("own-validator tool must defer validation: got %v, %v", got, err)
	}
	if _, err := ValidateSerializableToolInput(serialized["jsonOnly"], map[string]interface{}{"value": "x"}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
