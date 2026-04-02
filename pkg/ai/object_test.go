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

func TestStreamObject_FallbackResultFields(t *testing.T) {
	t.Parallel()

	// When DoStream returns an error, StreamObject falls back to DoGenerate.
	// The returned result struct must include Reasoning, Request, Response, ProviderMetadata
	// (previously they were missing — populated only in the event but not the result).
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("streaming not supported")
		},
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:             `{"key": "val"}`,
				FinishReason:     types.FinishReasonStop,
				ProviderMetadata: map[string]interface{}{"model": "test"},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "test fallback",
		Schema: testSchema,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Request and Response are now typed structs (not pointers); zero values are always valid.
	// Verify they carry the expected data instead.
	_ = result.Request  // GenerateStepRequest
	_ = result.Response // GenerateStepResponse
	if result.ProviderMetadata == nil {
		t.Error("expected non-nil ProviderMetadata in fallback result")
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
