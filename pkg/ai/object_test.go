package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateObject_ObjectMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name": "John", "age": 30}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "number"},
		},
	})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a person",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Error("expected non-nil object")
	}

	obj := result.Object.(map[string]interface{})
	if obj["name"] != "John" {
		t.Errorf("unexpected name: %v", obj["name"])
	}
	if obj["age"] != 30.0 {
		t.Errorf("unexpected age: %v", obj["age"])
	}
}

func TestGenerateObject_ArrayMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Array mode wraps in { "elements": [...] } matching TS SDK's arrayOutputStrategy.
			return &types.GenerateResult{
				Text:         `{"elements": [{"name": "John"}, {"name": "Jane"}]}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate people",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Array) != 2 {
		t.Errorf("expected 2 items, got %d", len(result.Array))
	}
}

func TestGenerateObject_ArrayModeReturnsDefaultedElements(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"elements":[{"name":"John"},{"name":"Jane","region":"eu"}]}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate people",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Array) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Array))
	}
	first, ok := result.Array[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first element = %T, want map", result.Array[0])
	}
	if first["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", first["region"])
	}
	second, ok := result.Array[1].(map[string]interface{})
	if !ok {
		t.Fatalf("second element = %T, want map", result.Array[1])
	}
	if second["region"] != "eu" {
		t.Fatalf("explicit region = %v, want eu", second["region"])
	}
}

func TestGenerateObject_ArrayModeResponseFormatDoesNotMutateElementSchema(t *testing.T) {
	t.Parallel()

	schemaMap := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}
	testSchema := schema.NewSimpleJSONSchema(schemaMap)

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"elements":[{"name":"John"}]}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "array",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := schemaMap["$schema"]; !ok {
		t.Fatal("GenerateObject removed $schema from caller schema")
	}
	if _, ok := testSchema.Validator().JSONSchema()["$schema"]; !ok {
		t.Fatal("GenerateObject removed $schema from schema validator")
	}
}

func TestGenerateObject_ObjectModeReturnsDefaultedObject(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a person",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	obj, ok := result.Object.(map[string]interface{})
	if !ok {
		t.Fatalf("object = %T, want map", result.Object)
	}
	if obj["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", obj["region"])
	}
}

func TestGenerateObject_EnumMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Enum mode wraps in { "result": "value" } matching TS SDK's enumOutputStrategy.
			return &types.GenerateResult{
				Text:         `{"result": "happy"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "What's the mood?",
		OutputMode: ObjectModeEnum,
		EnumValues: []string{"happy", "sad", "neutral"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EnumValue != "happy" {
		t.Errorf("expected 'happy', got %s", result.EnumValue)
	}
}

func TestGenerateObject_NoSchemaMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"arbitrary": "data", "count": 42}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate anything",
		OutputMode: ObjectModeNoSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Error("expected non-nil object")
	}
}

func TestGenerateObject_NilModel(t *testing.T) {
	t.Parallel()

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  nil,
		Prompt: "Generate",
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateObject_SchemaRequired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}

	// Object mode requires schema
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate",
		OutputMode: ObjectModeObject,
		Schema:     nil,
	})

	if err == nil {
		t.Fatal("expected error for missing schema")
	}

	// Array mode requires schema
	_, err = GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate",
		OutputMode: ObjectModeArray,
		Schema:     nil,
	})

	if err == nil {
		t.Fatal("expected error for missing schema in array mode")
	}
}

func TestGenerateObject_EnumValuesRequired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate",
		OutputMode: ObjectModeEnum,
		EnumValues: nil,
	})

	if err == nil {
		t.Fatal("expected error for missing enum values")
	}
}

func TestGenerateObject_InvalidOutputMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate",
		OutputMode: "invalid-mode",
	})

	if err == nil {
		t.Fatal("expected error for invalid output mode")
	}
}

// TestGenerateObject_CrossModeValidation tests the cross-mode validation rules
// that mirror TS SDK's validateObjectGenerationInput().
func TestGenerateObject_CrossModeValidation(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	cases := []struct {
		name string
		opts GenerateObjectOptions
	}{
		{
			name: "object mode with enum values",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", Schema: testSchema, OutputMode: ObjectModeObject, EnumValues: []string{"a"}},
		},
		{
			name: "array mode with enum values",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", Schema: testSchema, OutputMode: ObjectModeArray, EnumValues: []string{"a"}},
		},
		{
			name: "enum mode with schema",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", Schema: testSchema, OutputMode: ObjectModeEnum, EnumValues: []string{"a"}},
		},
		{
			name: "enum mode with schema description",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", OutputMode: ObjectModeEnum, EnumValues: []string{"a"}, SchemaDescription: "desc"},
		},
		{
			name: "enum mode with schema name",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", OutputMode: ObjectModeEnum, EnumValues: []string{"a"}, SchemaName: "Name"},
		},
		{
			name: "no-schema mode with schema",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", Schema: testSchema, OutputMode: ObjectModeNoSchema},
		},
		{
			name: "no-schema mode with schema description",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", OutputMode: ObjectModeNoSchema, SchemaDescription: "desc"},
		},
		{
			name: "no-schema mode with schema name",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", OutputMode: ObjectModeNoSchema, SchemaName: "Name"},
		},
		{
			name: "no-schema mode with enum values",
			opts: GenerateObjectOptions{Model: model, Prompt: "x", OutputMode: ObjectModeNoSchema, EnumValues: []string{"a"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := GenerateObject(context.Background(), tc.opts)
			if err == nil {
				t.Fatalf("expected error for %q", tc.name)
			}
		})
	}
}

func TestGenerateObject_StructuredOutputUnsupported(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: false}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err == nil {
		t.Fatal("expected error for unsupported structured output")
	}
	if err.Error() != "model does not support structured output" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateObject_JSONParseError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "not valid json",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
}

func TestGenerateObject_ArrayParseError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"not": "array"}`, // Object instead of array
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Generate",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})

	if err == nil {
		t.Fatal("expected error for invalid array JSON")
	}
}

func TestGenerateObject_InvalidEnumValue(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			// Returns a valid wrapper object but with a value not in the enum list.
			return &types.GenerateResult{
				Text:         `{"result": "angry"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "Pick mood",
		OutputMode: ObjectModeEnum,
		EnumValues: []string{"happy", "sad", "neutral"},
	})

	if err == nil {
		t.Fatal("expected error for invalid enum value")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
}

func TestGenerateObject_NoObjectGeneratedErrorIncludesResponseMetadata(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1710000000, 0).UTC()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"`,
				FinishReason: types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{
					ID:        "resp_123",
					Timestamp: ts,
					ModelID:   "mock-model",
					Headers:   map[string]string{"x-test": "1"},
				},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
	if noObjErr.Response == nil {
		t.Fatal("expected response metadata")
	}
	if noObjErr.Response.ID != "resp_123" {
		t.Fatalf("id = %q", noObjErr.Response.ID)
	}
	if !noObjErr.Response.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v", noObjErr.Response.Timestamp)
	}
	if noObjErr.Response.ModelID != "mock-model" {
		t.Fatalf("modelId = %q", noObjErr.Response.ModelID)
	}
	if noObjErr.Response.Headers["x-test"] != "1" {
		t.Fatalf("headers = %#v", noObjErr.Response.Headers)
	}
}

func TestGenerateObject_NoTextReturnsNoObjectGeneratedError(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1710000100, 0).UTC()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "",
				FinishReason: types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{
					ID:        "resp_empty",
					Timestamp: ts,
					ModelID:   "mock-model",
					Headers:   map[string]string{"x-empty": "1"},
				},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
	if noObjErr.Message != "No object generated: could not parse the response." {
		t.Fatalf("message = %q", noObjErr.Message)
	}
	if noObjErr.Response == nil || noObjErr.Response.ID != "resp_empty" {
		t.Fatalf("response = %#v", noObjErr.Response)
	}
}

func TestGenerateObject_ResultResponseIncludesProviderMetadataFields(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1710000200, 0).UTC()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"}`,
				FinishReason: types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{
					ID:        "resp_success",
					Timestamp: ts,
					ModelID:   "mock-model",
					Headers:   map[string]string{"x-test": "1"},
				},
				RawResponse: map[string]interface{}{"ok": true},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.ID != "resp_success" {
		t.Fatalf("response id = %q", result.Response.ID)
	}
	if !result.Response.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v", result.Response.Timestamp)
	}
	if result.Response.ModelID != "mock-model" {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["x-test"] != "1" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}

func TestGenerateObject_DefaultRetriesMatchesTS(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("retry me")
			}
			return &types.GenerateResult{
				Text:         `{"ok":true}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestGenerateObject_ExperimentalRepairText(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []string{"name"},
	})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
		ExperimentalRepairText: func(ctx context.Context, text string, parseErr error) (*string, error) {
			repaired := text + "}"
			return &repaired, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Fatal("expected repaired object")
	}
}

func TestGenerateObject_ExperimentalRepairTextReceivesUnderlyingCauseAndCanDecline(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []string{"name"},
	})

	var repairErr error
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
		ExperimentalRepairText: func(ctx context.Context, text string, parseErr error) (*string, error) {
			repairErr = parseErr
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
	if repairErr == nil {
		t.Fatal("expected repair function to receive parse error")
	}
	if repairErr == noObjErr {
		t.Fatal("expected repair function to receive underlying cause, not wrapped error")
	}
	if noObjErr.Cause == nil || repairErr.Error() != noObjErr.Cause.Error() {
		t.Fatalf("repair error = %v, cause = %v", repairErr, noObjErr.Cause)
	}
}

func TestGenerateObject_OnFinishCallback(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"test": "value"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	finishCalled := false
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
		OnFinish: func(ctx context.Context, result *GenerateObjectResult, userContext interface{}) {
			finishCalled = true
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finishCalled {
		t.Error("expected OnFinish callback to be called")
	}
}

func TestGenerateObject_GenerationError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("generation failed")
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, expectedErr
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestGenerateObjectInto_Unmarshal(t *testing.T) {
	t.Parallel()

	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name": "Alice", "age": 25}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "integer"},
		},
	})

	var person Person
	err := GenerateObjectInto(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate person",
		Schema: testSchema,
	}, &person)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if person.Name != "Alice" {
		t.Errorf("unexpected name: %s", person.Name)
	}
	if person.Age != 25 {
		t.Errorf("unexpected age: %d", person.Age)
	}
}

func TestStreamObject_Basic(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"result"`},
				{Type: provider.ChunkTypeText, Text: `: "streamed"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Stream object",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Error("expected non-nil object")
	}
}

func TestStreamObject_DefaultRetriesMatchesTS(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("retry me")
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"ok":true}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestStreamObject_ExperimentalRepairText(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"Jane"`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []string{"name"},
	})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
		ExperimentalRepairText: func(ctx context.Context, text string, parseErr error) (*string, error) {
			repaired := text + "}"
			return &repaired, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Fatal("expected repaired object")
	}
}

func TestStreamObject_ExperimentalRepairTextReceivesUnderlyingCauseAndCanDecline(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"Jane"`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []string{"name"},
	})

	var repairErr error
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
		ExperimentalRepairText: func(ctx context.Context, text string, parseErr error) (*string, error) {
			repairErr = parseErr
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
	if repairErr == nil {
		t.Fatal("expected repair function to receive parse error")
	}
	if repairErr == noObjErr {
		t.Fatal("expected repair function to receive underlying cause, not wrapped error")
	}
	if noObjErr.Cause == nil || repairErr.Error() != noObjErr.Cause.Error() {
		t.Fatalf("repair error = %v, cause = %v", repairErr, noObjErr.Cause)
	}
}

func TestStreamObject_NilModel(t *testing.T) {
	t.Parallel()

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: nil,
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
}

func TestStreamObject_NilSchema(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{StructuredSupport: true}

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: nil,
	})

	if err == nil {
		t.Fatal("expected error for nil schema")
	}
}

func TestStreamObject_ReasoningAccumulated(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoning, Reasoning: "I think "},
				{Type: provider.ChunkTypeReasoning, Reasoning: "carefully."},
				{Type: provider.ChunkTypeText, Text: `{"result": "ok"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Stream with reasoning",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reasoning != "I think carefully." {
		t.Errorf("expected accumulated reasoning, got %q", result.Reasoning)
	}
}

func TestGenerateObject_RetriesDoGenerate(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("transient")
			}
			return &types.GenerateResult{
				Text:         `{"x":1}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:      model,
		Prompt:     "retry",
		Schema:     testSchema,
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Fatal("expected object")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestGenerateObject_ReasoningConcatenatesParts(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text: `{"x":1}`,
				Content: []types.ContentPart{
					types.ReasoningContent{Text: "first"},
					types.ReasoningContent{Text: "second"},
				},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "reason",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reasoning != "first\nsecond" {
		t.Fatalf("reasoning = %q, want %q", result.Reasoning, "first\nsecond")
	}
}

func TestStreamObject_ArrayMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"elements":[{"name":"John"},{"name":"Jane"}]}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "array",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Array) != 2 {
		t.Fatalf("array length = %d, want 2", len(result.Array))
	}
}

func TestStreamObject_ArrayModeResponseFormatDoesNotMutateElementSchema(t *testing.T) {
	t.Parallel()

	schemaMap := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}
	testSchema := schema.NewSimpleJSONSchema(schemaMap)

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"elements":[{"name":"John"}]}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "array",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := schemaMap["$schema"]; !ok {
		t.Fatal("StreamObject removed $schema from caller schema")
	}
	if _, ok := testSchema.Validator().JSONSchema()["$schema"]; !ok {
		t.Fatal("StreamObject removed $schema from schema validator")
	}
}

func TestStreamObject_ArrayModeReturnsDefaultedElements(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"elements":[{"name":"John"}]}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "array",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Array) != 1 {
		t.Fatalf("array length = %d, want 1", len(result.Array))
	}
	first, ok := result.Array[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first element = %T, want map", result.Array[0])
	}
	if first["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", first["region"])
	}
}

func TestStreamObject_ArrayModePartialSkipsIncompleteLastElement(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"elements":[{"name":"John"},`},
				{Type: provider.ChunkTypeText, Text: `{"name":`},
				{Type: provider.ChunkTypeText, Text: `"Jane"}]}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
		"required": []string{"name"},
	})

	var partials []interface{}
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "array partial",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
		OnChunk: func(partialObject interface{}) {
			partials = append(partials, partialObject)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Array) != 2 {
		t.Fatalf("array length = %d, want 2", len(result.Array))
	}

	foundCompleteFirst := false
	for _, partial := range partials {
		arr, ok := partial.([]interface{})
		if !ok || len(arr) != 1 {
			continue
		}
		first, ok := arr[0].(map[string]interface{})
		if ok && first["name"] == "John" {
			foundCompleteFirst = true
		}
	}
	if !foundCompleteFirst {
		t.Fatalf("partials = %#v, want a callback with only the complete first element", partials)
	}
}

func TestStreamObject_ArrayModePartialAppliesDefaults(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"elements":[{"name":"John"},`},
				{Type: provider.ChunkTypeText, Text: `{"name":"Jane"}]}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
		"required": []string{"name"},
	})

	var partials []interface{}
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "array partial defaults",
		Schema:     testSchema,
		OutputMode: ObjectModeArray,
		OnChunk: func(partialObject interface{}) {
			partials = append(partials, partialObject)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, partial := range partials {
		arr, ok := partial.([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}
		first, ok := arr[0].(map[string]interface{})
		if ok && first["name"] == "John" && first["region"] == "us-east-1" {
			return
		}
	}
	t.Fatalf("partials = %#v, want defaulted first element", partials)
}

func TestStreamObject_ObjectModeReturnsDefaultedObject(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"John"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":   map[string]interface{}{"type": "string"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "object",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	obj, ok := result.Object.(map[string]interface{})
	if !ok {
		t.Fatalf("object = %T, want map", result.Object)
	}
	if obj["region"] != "us-east-1" {
		t.Fatalf("defaulted region = %v, want us-east-1", obj["region"])
	}
}

func TestStreamObject_EnumMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"result":"happy"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "enum",
		OutputMode: ObjectModeEnum,
		EnumValues: []string{"happy", "sad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EnumValue != "happy" {
		t.Fatalf("enum value = %q, want happy", result.EnumValue)
	}
}

func TestStreamObject_EnumModePartialAmbiguousPrefix(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"result":"foo`},
				{Type: provider.ChunkTypeText, Text: `bar"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var partials []interface{}
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "enum partial",
		OutputMode: ObjectModeEnum,
		EnumValues: []string{"foobar", "foobar2"},
		OnChunk: func(partialObject interface{}) {
			partials = append(partials, partialObject)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EnumValue != "foobar" {
		t.Fatalf("enum value = %q, want foobar", result.EnumValue)
	}
	if len(partials) < 2 || partials[0] != "foo" || partials[len(partials)-1] != "foobar" {
		t.Fatalf("partials = %#v, want ambiguous prefix then final enum value", partials)
	}
}

func TestStreamObject_EnumModePartialCompletesUnambiguousPrefix(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"result":"foo`},
				{Type: provider.ChunkTypeText, Text: `bar"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var partials []interface{}
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "enum partial",
		OutputMode: ObjectModeEnum,
		EnumValues: []string{"foobar", "barfoo"},
		OnChunk: func(partialObject interface{}) {
			partials = append(partials, partialObject)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EnumValue != "foobar" {
		t.Fatalf("enum value = %q, want foobar", result.EnumValue)
	}
	if len(partials) != 1 || partials[0] != "foobar" {
		t.Fatalf("partials = %#v, want completed unambiguous enum value", partials)
	}
}

func TestStreamObject_NoSchemaMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"freeform":true}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "noschema",
		OutputMode: ObjectModeNoSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Fatal("expected object")
	}
}

func TestStreamObject_OnChunkAllowsPartialInvalidObject(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"Jo"`},
				{Type: provider.ChunkTypeText, Text: `,"age":30}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "number"},
		},
		"required": []string{"name", "age"},
	})

	var partials []interface{}
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "partial",
		Schema: testSchema,
		OnChunk: func(partialObject interface{}) {
			partials = append(partials, partialObject)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(partials) == 0 {
		t.Fatal("expected partial callback before final object was valid")
	}
}

func TestStreamObject_ResponseMetadataIncludesFallbacksAndChunkMetadata(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1700000000, 0)
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{
					Type: provider.ChunkTypeResponseMetadata,
					ResponseMetadata: &provider.ResponseMetadata{
						ID:        "resp_123",
						ModelID:   "stream-model",
						Timestamp: ts,
						Headers:   map[string]string{"x-test": "1"},
					},
				},
				{Type: provider.ChunkTypeText, Text: `{"x":1}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
		ModelName: "requested-model",
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "meta",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.ID != "resp_123" {
		t.Fatalf("response id = %q, want resp_123", result.Response.ID)
	}
	if result.Response.ModelID != "stream-model" {
		t.Fatalf("model id = %q, want stream-model", result.Response.ModelID)
	}
	if !result.Response.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v, want %v", result.Response.Timestamp, ts)
	}
	if result.Response.Headers["x-test"] != "1" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}

func TestStreamObject_NoTextReturnsNoObjectGeneratedError(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1710000300, 0).UTC()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{
					Type: provider.ChunkTypeResponseMetadata,
					ResponseMetadata: &provider.ResponseMetadata{
						ID:        "resp_stream_empty",
						ModelID:   "stream-model",
						Timestamp: ts,
						Headers:   map[string]string{"x-empty": "1"},
					},
				},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
		ModelName: "requested-model",
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "empty stream",
		Schema: testSchema,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var noObjErr *NoObjectGeneratedError
	if !errors.As(err, &noObjErr) {
		t.Fatalf("expected NoObjectGeneratedError, got %T", err)
	}
	if noObjErr.Message != "No object generated: could not parse the response." {
		t.Fatalf("message = %q", noObjErr.Message)
	}
	if noObjErr.Response == nil || noObjErr.Response.ID != "resp_stream_empty" {
		t.Fatalf("response = %#v", noObjErr.Response)
	}
	if !noObjErr.Response.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v", noObjErr.Response.Timestamp)
	}
}

func TestStreamObject_ProviderMetadataFromStream(t *testing.T) {
	t.Parallel()

	// Providers like Gemini emit ProviderMetadata on the ChunkTypeFinish chunk.
	// StreamObject must accumulate it and surface it in both the result struct
	// and the OnStepFinish / OnFinishEvent callbacks.
	wantMeta := map[string]interface{}{"vendor": "gemini", "version": "1.0"}
	metaJSON, _ := json.Marshal(wantMeta)

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"key": "val"}`},
				{
					Type:             provider.ChunkTypeFinish,
					FinishReason:     types.FinishReasonStop,
					ProviderMetadata: metaJSON,
				},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var stepMeta, finishMeta map[string]interface{}
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "stream with provider metadata",
		Schema: testSchema,
		OnStepFinish: func(_ context.Context, e ObjectOnStepFinishEvent) {
			stepMeta = e.ProviderMetadata
		},
		OnFinishEvent: func(_ context.Context, e ObjectOnFinishEvent) {
			finishMeta = e.ProviderMetadata
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatal("expected non-nil ProviderMetadata in result")
	}
	if result.ProviderMetadata["vendor"] != "gemini" {
		t.Errorf("unexpected ProviderMetadata: %v", result.ProviderMetadata)
	}
	if stepMeta == nil || stepMeta["vendor"] != "gemini" {
		t.Errorf("OnStepFinish ProviderMetadata wrong: %v", stepMeta)
	}
	if finishMeta == nil || finishMeta["vendor"] != "gemini" {
		t.Errorf("OnFinishEvent ProviderMetadata wrong: %v", finishMeta)
	}
}

func TestStreamObject_DoStreamErrorCallsOnErrorWithoutGenerateFallback(t *testing.T) {
	t.Parallel()

	streamErr := errors.New("streaming not supported")
	generateCalled := false
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, streamErr
		},
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			generateCalled = true
			return nil, errors.New("unexpected generate fallback")
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	var onError error

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "test stream startup error",
		Schema: testSchema,
		OnError: func(_ context.Context, err error) {
			onError = err
		},
	})

	if err == nil {
		t.Fatal("expected stream error")
	}
	if !errors.Is(err, streamErr) {
		t.Fatalf("error = %v, want wrapped stream error", err)
	}
	if !errors.Is(onError, streamErr) {
		t.Fatalf("OnError = %v, want stream error", onError)
	}
	if generateCalled {
		t.Fatal("DoGenerate should not be called when StreamObject DoStream fails")
	}
}

func TestGenerateObject_DefaultMode(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"key": "value"}`,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	// Don't specify OutputMode - should default to object mode
	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Object == nil {
		t.Error("expected object result")
	}
}

func TestGenerateObject_UsageTracking(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			input, output, total := int64(10), int64(20), int64(30)
			return &types.GenerateResult{
				Text:         `{"data": "test"}`,
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %v", result.Usage.TotalTokens)
	}
}

func TestGenerateObject_RawText(t *testing.T) {
	t.Parallel()

	expectedJSON := `{"test": "data"}`

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         expectedJSON,
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the raw text is preserved (may have whitespace differences)
	var expected, actual interface{}
	_ = json.Unmarshal([]byte(expectedJSON), &expected)
	_ = json.Unmarshal([]byte(result.Text), &actual)

	if expected.(map[string]interface{})["test"] != actual.(map[string]interface{})["test"] {
		t.Error("text content mismatch")
	}
}
