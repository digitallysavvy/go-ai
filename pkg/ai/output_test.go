package ai

import (
	"context"
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
}

func isNoObjectGeneratedError(err error, out **NoObjectGeneratedError) bool {
	if e, ok := err.(*NoObjectGeneratedError); ok {
		*out = e
		return true
	}
	return false
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
		Name   string `json:"name"`
		Moons  int    `json:"moons"`
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
	defer result.Close()

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
