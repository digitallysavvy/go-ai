package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
	if result.Request == nil {
		t.Error("expected non-nil Request in fallback result")
	}
	if result.Response == nil {
		t.Error("expected non-nil Response in fallback result")
	}
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
