package ai_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// textModel returns a mock model that answers every request with text.
// In a real program you would use a provider, for example
// openai.New(...).LanguageModel(...).
func textModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			words := strings.SplitAfter(text, " ")
			chunks := make([]provider.StreamChunk, 0, len(words)+1)
			for _, w := range words {
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, Text: w})
			}
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop})
			return testutil.NewMockTextStream(chunks), nil
		},
	}
}

// GenerateText sends one prompt and returns the full response.
func ExampleGenerateText() {
	ctx := context.Background()
	model := textModel("An agent is a model that calls tools in a loop.")

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is an agent?",
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.Text)
	fmt.Println(result.FinishReason)
	// Output:
	// An agent is a model that calls tools in a loop.
	// stop
}

// StreamText returns a result whose Chunks channel delivers text as it arrives.
func ExampleStreamText() {
	ctx := context.Background()
	model := textModel("Go makes concurrency simple.")

	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: "Say something about Go.",
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = stream.Close() }()

	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	fmt.Println()
	// Output:
	// Go makes concurrency simple.
}

// GenerateObjectInto asks the model for JSON that matches a schema and
// decodes it into a Go value.
func ExampleGenerateObjectInto() {
	ctx := context.Background()
	model := textModel(`{"name":"Lasagna","steps":["Layer","Bake"]}`)
	model.StructuredSupport = true

	recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":  map[string]interface{}{"type": "string"},
			"steps": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"name", "steps"},
	})

	var recipe struct {
		Name  string   `json:"name"`
		Steps []string `json:"steps"`
	}
	err := ai.GenerateObjectInto(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a lasagna recipe.",
		Schema: recipeSchema,
	}, &recipe)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(recipe.Name, len(recipe.Steps))
	// Output:
	// Lasagna 2
}

// A tool is a types.Tool value: a name, a JSON schema for the input, and an
// Execute function. GenerateText runs the tool when the model calls it and
// sends the result back to the model.
func Example_defineTool() {
	ctx := context.Background()

	weather := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a city",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string", "description": "City name"},
			},
			"required": []string{"city"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			city, _ := params["city"].(string)
			return map[string]interface{}{"city": city, "temperature": 72, "condition": "sunny"}, nil
		},
	}

	// The mock model asks for the tool on the first step and answers on the second.
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{{
						ID:        "call_1",
						ToolName:  "get_weather",
						Arguments: map[string]interface{}{"city": "Lisbon"},
					}},
				}, nil
			}
			return &types.GenerateResult{Text: "It is 72 degrees and sunny in Lisbon.", FinishReason: types.FinishReasonStop}, nil
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What is the weather in Lisbon?",
		Tools:    []types.Tool{weather},
		StopWhen: []ai.StopCondition{ai.StepCountIs(3)},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.Text)
	fmt.Println("steps:", len(result.Steps))
	// Output:
	// It is 72 degrees and sunny in Lisbon.
	// steps: 2
}

// PipeUIMessageStreamToResponse writes a StreamText result to an HTTP
// response in the UI message stream format that the AI SDK useChat hook reads.
func ExamplePipeUIMessageStreamToResponse() {
	ctx := context.Background()
	model := textModel("Hello from Go.")

	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{Model: model, Prompt: "Hi"})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = stream.Close() }()

	// In a handler, pass the http.ResponseWriter instead of a recorder.
	rec := httptest.NewRecorder()
	if err := ai.PipeUIMessageStreamToResponse(ctx, stream, rec); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Content-Type"))
	fmt.Println(rec.Header().Get("x-vercel-ai-ui-message-stream"))
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// text/event-stream
	// v1
	// data: {"type":"start"}
	//
	// data: {"type":"start-step"}
	//
	// data: {"id":"text-1","type":"text-start"}
	//
	// data: {"delta":"Hello ","id":"text-1","type":"text-delta"}
	//
	// data: {"delta":"from ","id":"text-1","type":"text-delta"}
	//
	// data: {"delta":"Go.","id":"text-1","type":"text-delta"}
	//
	// data: {"id":"text-1","type":"text-end"}
	//
	// data: {"type":"finish-step"}
	//
	// data: {"finishReason":"stop","type":"finish"}
	//
	// data: [DONE]
}
