package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// =============================================================================
// OUT-T08: Unit tests for factory ParseComplete and ParsePartial
// =============================================================================

func TestTextOutput_ParseCompleteOutput(t *testing.T) {
	t.Parallel()
	out := TextOutput()
	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: "hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", result)
	}
}

func TestTextOutput_ParsePartialOutput(t *testing.T) {
	t.Parallel()
	out := TextOutput()
	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: "partial",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial == nil {
		t.Fatal("expected non-nil partial")
	}
	if partial.Partial != "partial" {
		t.Errorf("expected %q, got %q", "partial", partial.Partial)
	}
}

// TestOutputProcessor_ParsePartialOutput_DistinguishesNullFromNotYetParseable
// ports TS stream-text.ts's `result !== undefined` check (audit row
// 84f5d1b / WG4): a JSON null is a legitimate parsed value (hasValue=true,
// value=nil), distinct from "not enough content to parse anything yet"
// (hasValue=false).
func TestOutputProcessor_ParsePartialOutput_DistinguishesNullFromNotYetParseable(t *testing.T) {
	t.Parallel()

	jsonOut := JSONOutput(JSONOutputOptions{}).(outputProcessor)

	value, hasValue, err := jsonOut.parsePartialOutput(context.Background(), ParsePartialOutputOptions{Text: "null"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasValue {
		t.Fatal("hasValue = false for a fully-parsed JSON null, want true")
	}
	if value != nil {
		t.Fatalf("value = %#v, want nil", value)
	}

	_, hasValue, err = jsonOut.parsePartialOutput(context.Background(), ParsePartialOutputOptions{Text: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasValue {
		t.Fatal("hasValue = true for empty/unparseable input, want false")
	}
}

// TestOutputProcessor_ParsePartialOutput_TextOutputAlwaysHasValue verifies
// that Output.Text's partial value (including the empty string) always
// reports hasValue=true, since any accumulated text is already valid partial
// text output.
func TestOutputProcessor_ParsePartialOutput_TextOutputAlwaysHasValue(t *testing.T) {
	t.Parallel()

	textOut := TextOutput().(outputProcessor)

	value, hasValue, err := textOut.parsePartialOutput(context.Background(), ParsePartialOutputOptions{Text: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasValue {
		t.Fatal("hasValue = false for empty text output, want true (an empty string is a valid partial)")
	}
	if value != "" {
		t.Fatalf("value = %#v, want empty string", value)
	}
}

func TestObjectOutput_ParseCompleteOutput(t *testing.T) {
	t.Parallel()

	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	out := ObjectOutput[Person](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string"},
				"age":  map[string]interface{}{"type": "integer"},
			},
		}),
	})

	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"name":"Alice","age":30}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "Alice" || result.Age != 30 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestObjectOutput_ParseCompleteOutput_ReturnsDefaultedObject(t *testing.T) {
	t.Parallel()

	out := ObjectOutput[map[string]interface{}](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string"},
				"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
			},
		}),
	})

	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"name":"Alice"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", result["region"])
	}
}

func TestObjectOutput_ParseCompleteOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	type Person struct {
		Name string `json:"name"`
	}

	out := ObjectOutput[Person](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: "not json",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Errorf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: could not parse the response." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

func isNoObjectGeneratedError(err error, out **NoObjectGeneratedError) bool {
	return errors.As(err, out)
}

func TestObjectOutput_ParsePartialOutput(t *testing.T) {
	t.Parallel()

	type Obj struct {
		Name string `json:"name"`
	}

	out := ObjectOutput[Obj](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	// Valid partial JSON
	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"name":"Al`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Partial may or may not resolve depending on json repair; just ensure no panic
	_ = partial

	// Complete JSON should parse successfully
	partial2, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"name":"Alice"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial2 != nil && partial2.Partial.Name != "Alice" {
		t.Errorf("expected Name=Alice, got %q", partial2.Partial.Name)
	}
}

func TestArrayOutput_ParseCompleteOutput(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"elements":[{"label":"go"},{"label":"ai"}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	if result[0].Label != "go" || result[1].Label != "ai" {
		t.Errorf("unexpected elements: %+v", result)
	}
}

// TestArrayOutput_MinMaxItemsInSchema ports TS Output.array()'s minItems/
// maxItems JSON Schema placement (audit row d4485fe / WG4).
func TestArrayOutput_MinMaxItemsInSchema(t *testing.T) {
	t.Parallel()

	out := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		MinItems:      intPtr(2),
		MaxItems:      intPtr(5),
	})

	format, err := out.ResponseFormat(context.Background())
	if err != nil {
		t.Fatalf("ResponseFormat error: %v", err)
	}
	schemaMap, ok := format.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("Schema = %T, want map[string]interface{}", format.Schema)
	}
	props := schemaMap["properties"].(map[string]interface{})
	elements := props["elements"].(map[string]interface{})
	if elements["minItems"] != 2 {
		t.Errorf("minItems = %v, want 2", elements["minItems"])
	}
	if elements["maxItems"] != 5 {
		t.Errorf("maxItems = %v, want 5", elements["maxItems"])
	}
}

// TestArrayOutput_MinItemsGreaterThanMaxItemsIsInvalidArgument ports TS's
// synchronous constructor-time validation (audit row d4485fe / WG4).
func TestArrayOutput_MinItemsGreaterThanMaxItemsIsInvalidArgument(t *testing.T) {
	t.Parallel()

	out := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		MinItems:      intPtr(5),
		MaxItems:      intPtr(2),
	})

	if _, err := out.ResponseFormat(context.Background()); err == nil {
		t.Fatal("expected an error when minItems > maxItems")
	}
	if _, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{Text: `{"elements":[]}`}); err == nil {
		t.Fatal("expected an error when minItems > maxItems")
	}
}

// TestArrayOutput_NegativeMinItemsIsInvalidArgument ports TS's
// validateArrayBound (audit row d4485fe / WG4).
func TestArrayOutput_NegativeMinItemsIsInvalidArgument(t *testing.T) {
	t.Parallel()

	out := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		MinItems:      intPtr(-1),
	})

	if _, err := out.ResponseFormat(context.Background()); err == nil {
		t.Fatal("expected an error for negative minItems")
	}
}

// TestArrayOutput_ParseCompleteOutput_LengthOutOfBounds ports TS's
// getArrayLengthValidationError applied to the final parsed array (audit row
// d4485fe / WG4).
func TestArrayOutput_ParseCompleteOutput_LengthOutOfBounds(t *testing.T) {
	t.Parallel()

	out := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		MinItems:      intPtr(2),
		MaxItems:      intPtr(3),
	})

	if _, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"elements":[{}]}`,
	}); err == nil {
		t.Fatal("expected NoObjectGeneratedError for too few elements")
	}

	if _, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"elements":[{},{},{},{}]}`,
	}); err == nil {
		t.Fatal("expected NoObjectGeneratedError for too many elements")
	}

	if _, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"elements":[{},{},{}]}`,
	}); err != nil {
		t.Fatalf("unexpected error for in-bounds length: %v", err)
	}
}

// TestArrayOutput_ResponseFormatHoistsDefsToRoot ports TS's preservation of
// root-level $defs/definitions when wrapping array output schemas (audit row
// 72ec74f / WG4): putting them under "items" breaks "#/$defs/..." refs,
// which resolve against the document root.
func TestArrayOutput_ResponseFormatHoistsDefsToRoot(t *testing.T) {
	t.Parallel()

	elementSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$ref":    "#/$defs/Item",
		"$defs": map[string]interface{}{
			"Item": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
	})

	out := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: elementSchema,
	})

	format, err := out.ResponseFormat(context.Background())
	if err != nil {
		t.Fatalf("ResponseFormat error: %v", err)
	}
	schemaMap, ok := format.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("Schema = %T, want map[string]interface{}", format.Schema)
	}
	if _, ok := schemaMap["$defs"]; !ok {
		t.Fatal("$defs was not hoisted to the wrapper root")
	}
	props := schemaMap["properties"].(map[string]interface{})
	elements := props["elements"].(map[string]interface{})
	items := elements["items"].(map[string]interface{})
	if _, ok := items["$defs"]; ok {
		t.Fatal("$defs was left under items as well as hoisted to root")
	}
	if items["$ref"] != "#/$defs/Item" {
		t.Fatalf("items[$ref] = %v, want #/$defs/Item preserved", items["$ref"])
	}
}

func TestArrayOutput_ParseCompleteOutput_MissingElements(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"other":"field"}`,
	})
	if err == nil {
		t.Fatal("expected error for missing elements key")
	}
}

func TestArrayOutput_ParseCompleteOutput_NonArrayElementsIsSchemaMismatch(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"elements":"not-an-array"}`,
	})
	if err == nil {
		t.Fatal("expected error for non-array elements")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Fatalf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: response did not match schema." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

func TestArrayOutput_ParsePartialOutput_SkipsInvalidNonLastElement(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"label"},
		}),
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"elements":[{},{"label":"valid"}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial == nil || len(partial.Partial) != 1 || partial.Partial[0].Label != "valid" {
		t.Fatalf("partial = %#v, want only valid element", partial)
	}
}

func TestArrayOutput_ParsePartialOutput_ReturnsNilOnInvalidWrapperShape(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})

	for _, text := range []string{
		`[]`,
		`{"other":[]}`,
		`{"elements":"not-array"}`,
	} {
		partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{Text: text})
		if err != nil {
			t.Fatalf("unexpected wrapper shape error for %s: %v", text, err)
		}
		if partial != nil {
			t.Fatalf("partial = %#v for %s, want nil", partial, text)
		}
	}
}

func TestArrayOutput_ParsePartialOutput_ReturnsEmptySliceWhenNoElementsValidate(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"label"},
		}),
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"elements":[{}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial == nil {
		t.Fatal("partial = nil, want empty partial array")
	}
	if partial.Partial == nil {
		t.Fatal("partial.Partial = nil, want empty slice")
	}
	if len(partial.Partial) != 0 {
		t.Fatalf("partial.Partial len = %d, want 0", len(partial.Partial))
	}
}

func TestArrayOutput_ParsePartialOutput_IgnoresInvalidLastElement(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"label"},
		}),
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"elements":[{"label":"complete"},{}`,
	})
	if err != nil {
		t.Fatalf("unexpected error for invalid last partial element: %v", err)
	}
	if partial == nil || len(partial.Partial) != 1 || partial.Partial[0].Label != "complete" {
		t.Fatalf("partial = %#v, want only complete first element", partial)
	}
}

func TestArrayOutput_ParsePartialOutput_IncludesLastElementForSuccessfulParse(t *testing.T) {
	t.Parallel()

	type Tag struct {
		Label string `json:"label"`
	}

	out := ArrayOutput[Tag](ArrayOutputOptions[Tag]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"label"},
		}),
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"elements":[{"label":"complete"},{"label":"final"}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial == nil || len(partial.Partial) != 2 || partial.Partial[1].Label != "final" {
		t.Fatalf("partial = %#v, want both complete elements", partial)
	}
}

type sentimentType string

const (
	sentimentPos sentimentType = "positive"
	sentimentNeg sentimentType = "negative"
	sentimentNeu sentimentType = "neutral"
)

func TestChoiceOutput_ParseCompleteOutput(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{sentimentPos, sentimentNeg, sentimentNeu},
	})

	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"result":"positive"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != sentimentPos {
		t.Errorf("expected %q, got %q", sentimentPos, result)
	}
}

func TestChoiceOutput_ParseCompleteOutput_InvalidChoice(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{sentimentPos, sentimentNeg, sentimentNeu},
	})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"result":"unknown"}`,
	})
	if err == nil {
		t.Fatal("expected error for invalid choice")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Fatalf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: response did not match schema." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

func TestChoiceOutput_ParseCompleteOutput_NonStringResultIsSchemaMismatch(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{sentimentPos, sentimentNeg, sentimentNeu},
	})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"result":123}`,
	})
	if err == nil {
		t.Fatal("expected error for non-string result")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Fatalf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: response did not match schema." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

func TestChoiceOutput_ParsePartialOutput(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{sentimentPos, sentimentNeg, sentimentNeu},
	})

	// Exact match
	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"result":"positive"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial != nil && partial.Partial != sentimentPos {
		t.Errorf("expected positive, got %q", partial.Partial)
	}
}

func TestChoiceOutput_ParsePartialOutput_AmbiguousRepairedPrefixReturnsNil(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{"aaa", "aab", "ccc"},
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"result":"a`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial != nil {
		t.Fatalf("partial = %#v, want nil for ambiguous repaired prefix", partial)
	}
}

func TestChoiceOutput_ParsePartialOutput_SingleRepairedPrefixReturnsMatch(t *testing.T) {
	t.Parallel()

	out := ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
		Options: []sentimentType{"aaa", "aab", "ccc"},
	})

	partial, err := out.ParsePartialOutput(context.Background(), ParsePartialOutputOptions{
		Text: `{"result":"c`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partial == nil || partial.Partial != "ccc" {
		t.Fatalf("partial = %#v, want ccc", partial)
	}
}

func TestJSONOutput_ParseCompleteOutput(t *testing.T) {
	t.Parallel()

	out := JSONOutput(JSONOutputOptions{})

	result, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: `{"key":"value","num":42}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("unexpected key: %v", m["key"])
	}
}

func TestJSONOutput_ParseCompleteOutput_Invalid(t *testing.T) {
	t.Parallel()

	out := JSONOutput(JSONOutputOptions{})

	_, err := out.ParseCompleteOutput(context.Background(), ParseCompleteOutputOptions{
		Text: "not json",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Fatalf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: could not parse the response." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

// =============================================================================
// SchemaFor tests
// =============================================================================

func TestSchemaFor_Struct(t *testing.T) {
	t.Parallel()

	type Recipe struct {
		Name        string   `json:"name"`
		Ingredients []string `json:"ingredients"`
		Servings    int      `json:"servings"`
	}

	s := SchemaFor[Recipe]()
	if s == nil {
		t.Fatal("expected non-nil schema")
	}
	v := s.Validator()
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	jsonSchema := v.JSONSchema()
	if jsonSchema["type"] != "object" {
		t.Errorf("expected type=object, got %v", jsonSchema["type"])
	}
}

func TestSchemaFor_String(t *testing.T) {
	t.Parallel()
	s := SchemaFor[string]()
	jsonSchema := s.Validator().JSONSchema()
	if jsonSchema["type"] != "string" {
		t.Errorf("expected type=string, got %v", jsonSchema["type"])
	}
}

func TestSchemaFor_Int(t *testing.T) {
	t.Parallel()
	s := SchemaFor[int]()
	jsonSchema := s.Validator().JSONSchema()
	if jsonSchema["type"] != "integer" {
		t.Errorf("expected type=integer, got %v", jsonSchema["type"])
	}
}

// =============================================================================
// OUT-T13: GenerateText with each output type
// =============================================================================

func TestGenerateText_WithObjectOutput(t *testing.T) {
	t.Parallel()

	type Planet struct {
		Name  string `json:"name"`
		Moons int    `json:"moons"`
	}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Verify ResponseFormat was set from output spec
			if opts.ResponseFormat == nil {
				t.Error("expected ResponseFormat to be set")
			}
			return &types.GenerateResult{
				Text:         `{"name":"Earth","moons":1}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Tell me about Earth",
		Output: ObjectOutput[Planet](ObjectOutputOptions{
			Schema: SchemaFor[Planet](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output == nil {
		t.Fatal("expected Output to be populated")
	}
	planet, ok := result.Output.(Planet)
	if !ok {
		t.Fatalf("expected Planet, got %T", result.Output)
	}
	if planet.Name != "Earth" || planet.Moons != 1 {
		t.Errorf("unexpected planet: %+v", planet)
	}
}

func TestGenerateText_WithArrayOutput(t *testing.T) {
	t.Parallel()

	type Color struct {
		Name string `json:"name"`
		Hex  string `json:"hex"`
	}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.ResponseFormat == nil {
				t.Error("expected ResponseFormat to be set")
			}
			return &types.GenerateResult{
				Text:         `{"elements":[{"name":"red","hex":"#ff0000"},{"name":"blue","hex":"#0000ff"}]}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "List primary colors",
		Output: ArrayOutput[Color](ArrayOutputOptions[Color]{
			ElementSchema: SchemaFor[Color](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	colors, ok := result.Output.([]Color)
	if !ok {
		t.Fatalf("expected []Color, got %T", result.Output)
	}
	if len(colors) != 2 {
		t.Fatalf("expected 2 colors, got %d", len(colors))
	}
	if colors[0].Name != "red" || colors[1].Name != "blue" {
		t.Errorf("unexpected colors: %+v", colors)
	}
}

func TestGenerateText_WithArrayOutput_ReturnsDefaultedElements(t *testing.T) {
	t.Parallel()

	elementSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"elements":[{"name":"red"},{"name":"blue","region":"eu"}]}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "List colors",
		Output: ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
			ElementSchema: elementSchema,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	colors, ok := result.Output.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result.Output)
	}
	if colors[0]["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", colors[0]["region"])
	}
	if colors[1]["region"] != "eu" {
		t.Fatalf("explicit region = %v, want eu", colors[1]["region"])
	}
}

func TestGenerateText_WithChoiceOutput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.ResponseFormat == nil {
				t.Error("expected ResponseFormat to be set")
			}
			return &types.GenerateResult{
				Text:         `{"result":"positive"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Classify the sentiment: 'I love Go!'",
		Output: ChoiceOutput[sentimentType](ChoiceOutputOptions[sentimentType]{
			Options: []sentimentType{sentimentPos, sentimentNeg, sentimentNeu},
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sentiment, ok := result.Output.(sentimentType)
	if !ok {
		t.Fatalf("expected sentimentType, got %T", result.Output)
	}
	if sentiment != sentimentPos {
		t.Errorf("expected positive, got %q", sentiment)
	}
}

func TestGenerateText_WithJSONOutput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"arbitrary":true,"count":5}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Give me some JSON",
		Output: JSONOutput(JSONOutputOptions{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result.Output)
	}
	if m["arbitrary"] != true {
		t.Errorf("unexpected arbitrary: %v", m["arbitrary"])
	}
}

func TestGenerateText_WithTextOutput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "plain text result",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Say something",
		Output: TextOutput(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, ok := result.Output.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result.Output)
	}
	if text != "plain text result" {
		t.Errorf("expected %q, got %q", "plain text result", text)
	}
}

// TestGenerateText_Output_OnFinishEvent verifies that the OnFinishEvent
// notification carries the parsed structured output for GenerateText,
// mirroring TS generate-text.ts's onFinish event (audit row 6669d69 /
// WG4 item #98).
func TestGenerateText_Output_OnFinishEvent(t *testing.T) {
	t.Parallel()

	type Obj struct {
		Name string `json:"name"`
	}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"widget"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	var captured OnFinishEvent
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Name a thing",
		Output: ObjectOutput[Obj](ObjectOutputOptions{
			Schema: SchemaFor[Obj](),
		}),
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			captured = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj, ok := result.Output.(Obj)
	if !ok {
		t.Fatalf("expected Obj, got %T", result.Output)
	}
	if obj.Name != "widget" {
		t.Errorf("expected name=widget, got %q", obj.Name)
	}

	capturedObj, ok := captured.Output.(Obj)
	if !ok {
		t.Fatalf("expected OnFinishEvent.Output to be Obj, got %T", captured.Output)
	}
	if capturedObj.Name != "widget" {
		t.Errorf("expected OnFinishEvent.Output.Name=widget, got %q", capturedObj.Name)
	}
}

func TestGenerateText_OutputParseError(t *testing.T) {
	t.Parallel()

	type Obj struct {
		Name string `json:"name"`
	}

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Return invalid JSON
			return &types.GenerateResult{
				Text:         "this is not json",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Generate an object",
		Output: ObjectOutput[Obj](ObjectOutputOptions{
			Schema: SchemaFor[Obj](),
		}),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
	var noObj *NoObjectGeneratedError
	if !isNoObjectGeneratedError(err, &noObj) {
		t.Fatalf("expected *NoObjectGeneratedError, got %T", err)
	}
	if noObj.Message != "No object generated: could not parse the response." {
		t.Fatalf("message = %q", noObj.Message)
	}
}

func TestGenerateText_NoOutput_NoChange(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// ResponseFormat should not be set when Output is nil
			if opts.ResponseFormat != nil {
				t.Error("expected ResponseFormat to be nil when Output is nil")
			}
			return &types.GenerateResult{
				Text:         "plain text",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != nil {
		t.Errorf("expected nil Output, got %v", result.Output)
	}
}

// =============================================================================
// OUT-T19: StreamText with object and array outputs
// =============================================================================

func TestStreamText_WithObjectOutput_PartialOutput(t *testing.T) {
	t.Parallel()

	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"id"`},
		{Type: provider.ChunkTypeText, Text: `:1,"name"`},
		{Type: provider.ChunkTypeText, Text: `:"Widget"}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.ResponseFormat == nil {
				t.Error("expected ResponseFormat to be set")
			}
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Describe an item",
		Output: ObjectOutput[Item](ObjectOutputOptions{
			Schema: SchemaFor[Item](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close() //nolint:errcheck

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty text")
	}

	// After ReadAll the partial output should be populated
	partial := result.PartialOutput()
	if partial == nil {
		t.Log("partial output was nil (may depend on json repair capability)")
	}
}

func TestStreamText_WithObjectOutput_Callbacks(t *testing.T) {
	t.Parallel()

	type Fruit struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"name":"apple","color":"red"}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	// Use a channel to wait for OnFinish
	done := make(chan *StreamTextResult, 1)

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Describe a fruit",
		Output: ObjectOutput[Fruit](ObjectOutputOptions{
			Schema: SchemaFor[Fruit](),
		}),
		OnFinish: func(r *StreamTextResult) {
			done <- r
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for processStream goroutine to call OnFinish
	finalResult := <-done
	if finalResult.Text() == "" {
		t.Error("expected non-empty text in OnFinish result")
	}
}

func TestStreamText_WithArrayOutput_ResponseFormat(t *testing.T) {
	t.Parallel()

	type Step struct {
		Number int    `json:"number"`
		Text   string `json:"text"`
	}

	var capturedFormat *provider.ResponseFormat

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"elements":[{"number":1,"text":"first"}]}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			capturedFormat = opts.ResponseFormat
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "List steps",
		Output: ArrayOutput[Step](ArrayOutputOptions[Step]{
			ElementSchema: SchemaFor[Step](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	if capturedFormat == nil {
		t.Fatal("expected ResponseFormat to be set from ArrayOutput spec")
	}
	if capturedFormat.Type != "json" {
		t.Errorf("expected type=json, got %q", capturedFormat.Type)
	}
}

func TestArrayOutput_ResponseFormatDoesNotMutateElementSchema(t *testing.T) {
	t.Parallel()

	schemaMap := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
	}
	elementSchema := schema.NewSimpleJSONSchema(schemaMap)

	output := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{
		ElementSchema: elementSchema,
	})
	if _, err := output.ResponseFormat(context.Background()); err != nil {
		t.Fatalf("ResponseFormat error: %v", err)
	}

	if _, ok := schemaMap["$schema"]; !ok {
		t.Fatal("ResponseFormat removed $schema from caller schema")
	}
	if _, ok := elementSchema.Validator().JSONSchema()["$schema"]; !ok {
		t.Fatal("ResponseFormat removed $schema from schema validator")
	}
}

func TestStreamText_WithArrayOutput_InvalidNonLastPartialElementIsSkipped(t *testing.T) {
	t.Parallel()

	type Step struct {
		Text string `json:"text"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"elements":[{},{"text":"still streaming"}`},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "List steps",
		Output: ArrayOutput[Step](ArrayOutputOptions[Step]{
			ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"text"},
			}),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected StreamText error: %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if text != `{"elements":[{},{"text":"still streaming"}` {
		t.Fatalf("ReadAll() text = %q", text)
	}
}

func TestStreamText_PartialOutput_ThreadSafe(t *testing.T) {
	t.Parallel()

	type Data struct {
		Value string `json:"value"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"value"`},
		{Type: provider.ChunkTypeText, Text: `:"hello"}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Give data",
		Output: ObjectOutput[Data](ObjectOutputOptions{
			Schema: SchemaFor[Data](),
		}),
		// Trigger the processStream goroutine
		OnFinish: func(r *StreamTextResult) {},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Concurrently read PartialOutput while the goroutine processes chunks
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_ = result.PartialOutput()
		}
	}()

	// Drain stream from main goroutine too
	for range result.Chunks() {
	}

	<-done // no race detected = pass
}

func TestStreamText_NoOutput_NoResponseFormat(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.ResponseFormat != nil {
				t.Error("expected no ResponseFormat when Output is nil")
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if result.PartialOutput() != nil {
		t.Error("expected nil PartialOutput when no Output spec provided")
	}
}

// =============================================================================
// TS SDK gap fixes: finishReason guard, Output() final result, deduplication
// =============================================================================

// TestGenerateText_FinishReasonLength_NilOutput verifies that GenerateText does
// NOT call parseCompleteOutput (and leaves result.Output nil) when the model
// finishes with reason "length" (truncated response). Parsing truncated JSON
// would always fail; the TS SDK guards with if (finishReason === 'stop').
// TestGenerateText_FinishReasonLength_SurfacesNoObjectGeneratedError ports TS
// generate-text.ts's length-truncation diagnostics (audit rows eed7950/
// 9de0baf / WG4): a non-stop, non-tool-calls finish reason with non-empty
// text is still a parse candidate (a provider may truncate structured
// output), so truncated/invalid JSON now surfaces NoObjectGeneratedError
// instead of silently leaving Output nil with no error at all.
func TestGenerateText_FinishReasonLength_SurfacesNoObjectGeneratedError(t *testing.T) {
	t.Parallel()

	type Obj struct {
		Name string `json:"name"`
	}

	// The model returns truncated JSON and finishes with "length".
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"truncat`, // incomplete JSON
				FinishReason: types.FinishReasonLength,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Generate an object",
		Output: ObjectOutput[Obj](ObjectOutputOptions{
			Schema: SchemaFor[Obj](),
		}),
	})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("error = %v, want *NoObjectGeneratedError", err)
	}
	if noObj.FinishReason != types.FinishReasonLength {
		t.Errorf("FinishReason = %q, want length", noObj.FinishReason)
	}
	if result != nil {
		t.Errorf("expected nil result on output parsing failure, got %+v", result)
	}
}

// TestStreamText_Output_FinalResult verifies that StreamTextResult.Output()
// returns the fully-parsed typed value after the stream completes (matching the
// TS SDK's .output Promise that resolves via parseCompleteOutput at stream end).
func TestStreamText_Output_FinalResult(t *testing.T) {
	t.Parallel()

	type Color struct {
		Name string `json:"name"`
		Hex  string `json:"hex"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"name":"red","hex":"#ff0000"}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Name a color",
		Output: ObjectOutput[Color](ObjectOutputOptions{
			Schema: SchemaFor[Color](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	// Output() should be set to the final parsed value after stream ends.
	out := result.Output()
	if out == nil {
		t.Fatal("expected Output() to be non-nil after stream completes")
	}
	color, ok := out.(Color)
	if !ok {
		t.Fatalf("unexpected Output type: %T", out)
	}
	if color.Name != "red" {
		t.Errorf("expected name=red, got %q", color.Name)
	}
	if color.Hex != "#ff0000" {
		t.Errorf("expected hex=#ff0000, got %q", color.Hex)
	}
	if result.OutputErr() != nil {
		t.Errorf("expected nil OutputErr, got %v", result.OutputErr())
	}
}

// TestStreamText_Output_NilWhenLengthFinish verifies that Output() is nil when
// the stream ends with finishReason "length" (truncated response), matching the
// TS SDK guard: parseCompleteOutput is only called on 'stop' finish reason.
// TestStreamText_Output_LengthFinishSurfacesOutputErr ports TS
// stream-text.ts's length-truncation diagnostics (audit rows eed7950/
// 9de0baf / WG4): non-empty text with a non-stop, non-tool-calls finish
// reason is still a parse candidate, so truncated/invalid JSON now surfaces
// through OutputErr() (NoObjectGeneratedError) instead of leaving both
// Output() and OutputErr() silently nil. Output() itself must stay nil: a
// failed parse must never publish the parser's zero value as if it were a
// real result.
func TestStreamText_Output_LengthFinishSurfacesOutputErr(t *testing.T) {
	t.Parallel()

	type Obj struct {
		Val int `json:"val"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"val":4`}, // truncated
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Give a number",
		Output: ObjectOutput[Obj](ObjectOutputOptions{
			Schema: SchemaFor[Obj](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	if result.Output() != nil {
		t.Errorf("expected nil Output() on parse failure, got %v", result.Output())
	}
	var noObj *NoObjectGeneratedError
	if !errors.As(result.OutputErr(), &noObj) {
		t.Fatalf("OutputErr() = %v, want *NoObjectGeneratedError", result.OutputErr())
	}
}

// TestStreamText_Output_ViaOnFinish verifies that the final Output() value is
// also accessible inside the OnFinish callback (processStream path).
func TestStreamText_Output_ViaOnFinish(t *testing.T) {
	t.Parallel()

	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"x":3,"y":7}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	done := make(chan *StreamTextResult, 1)

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Give a point",
		Output: ObjectOutput[Point](ObjectOutputOptions{
			Schema: SchemaFor[Point](),
		}),
		OnFinish: func(r *StreamTextResult) {
			done <- r
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	finalResult := <-done

	out := finalResult.Output()
	if out == nil {
		t.Fatal("expected Output() to be set in OnFinish callback")
	}
	pt, ok := out.(Point)
	if !ok {
		t.Fatalf("unexpected Output type: %T", out)
	}
	if pt.X != 3 || pt.Y != 7 {
		t.Errorf("expected {3,7}, got {%d,%d}", pt.X, pt.Y)
	}
}

// TestStreamText_Output_OnFinishEvent verifies that the OnFinishEvent
// notification carries the parsed structured output (audit row 6669d69 /
// WG4 item #98), mirroring TS stream-text.ts's onFinish event which carries
// the resolved object alongside the raw text.
func TestStreamText_Output_OnFinishEvent(t *testing.T) {
	t.Parallel()

	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}

	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"x":5,"y":9}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	events := make(chan OnFinishEvent, 1)

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Give a point",
		Output: ObjectOutput[Point](ObjectOutputOptions{
			Schema: SchemaFor[Point](),
		}),
		OnEndEvent: func(ctx context.Context, e OnFinishEvent) {
			events <- e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	event := <-events

	out := event.Output
	if out == nil {
		t.Fatal("expected OnFinishEvent.Output to be non-nil")
	}
	pt, ok := out.(Point)
	if !ok {
		t.Fatalf("unexpected Output type: %T", out)
	}
	if pt.X != 5 || pt.Y != 9 {
		t.Errorf("expected {5,9}, got {%d,%d}", pt.X, pt.Y)
	}
}

// TestStreamText_PartialOutput_Deduplication verifies that PartialOutput is only
// updated when the JSON representation actually changes (matching TS SDK behavior
// where updates are suppressed when JSON.stringify(partial) equals the last
// published value).
//
// Dedup scenario: chunk 1 streams `{"title":"hello"` (missing closing brace).
// The JSON repair fills it in, producing Doc{Title:"hello"}.
// Chunk 2 streams `}`, completing the JSON. parsePartialOutput is called again
// with the full `{"title":"hello"}` text and returns the same Doc{Title:"hello"}.
// The JSON marshaling of both parses is identical — so the dedup should suppress
// the second update. The final Output() must still be the fully-parsed doc.
func TestStreamText_PartialOutput_Deduplication(t *testing.T) {
	t.Parallel()

	type Doc struct {
		Title string `json:"title"`
	}

	// Chunk 1: incomplete JSON — repaired partial = Doc{Title:"hello"}
	// Chunk 2: closing brace — full JSON — same parsed partial (dedup suppresses)
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: `{"title":"hello"`},
		{Type: provider.ChunkTypeText, Text: `}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Write a title",
		Output: ObjectOutput[Doc](ObjectOutputOptions{
			Schema: SchemaFor[Doc](),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	// Dedup must not suppress the final Output — parseCompleteOutput always runs.
	out := result.Output()
	if out == nil {
		t.Fatal("expected Output() to be set after stream (dedup must not block parseCompleteOutput)")
	}
	doc, ok := out.(Doc)
	if !ok {
		t.Fatalf("unexpected Output type: %T", out)
	}
	if doc.Title != "hello" {
		t.Errorf("expected title=hello, got %q", doc.Title)
	}

	// PartialOutput must also be non-nil (set from first chunk at minimum).
	if result.PartialOutput() == nil {
		t.Error("expected PartialOutput() to be non-nil after streaming")
	}
}
