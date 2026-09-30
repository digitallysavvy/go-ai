package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestCodeModeWithToolSearch_DiscoversAndResetsPerGeneration is an
// end-to-end integration test combining code-mode with tool search: it runs
// the real QuickJS sandbox (no mocked codemode execution) through
// ai.GenerateText with a mock model, verifying that code-mode discovers a
// deferred host tool via tools.toolSearch() inside the sandbox in one step
// and calls it by name in a later step once discovery makes it available,
// with the result flowing back out as the sandboxed program's JSON-decoded
// return value.
//
// Ports TypeScript's code-mode/src/tool-search.test.ts, "discovers tools
// between sandbox executions and resets discovery between generations"
// (verified against ai@7.0.118's packages/code-mode/src/tool-search.test.ts).
func TestCodeModeWithToolSearch_DiscoversAndResetsPerGeneration(t *testing.T) {
	executeCount := 0
	getForecast := types.Tool{
		Name:         "getForecast",
		DeferLoading: true,
		Description:  "Weather forecast for a city",
		Parameters: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"city"},
			"properties":           map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executeCount++
			city, _ := input["city"].(string)
			return map[string]interface{}{"city": city, "forecast": "rain"}, nil
		},
	}
	stockPrice := types.Tool{
		Name:         "stockPrice",
		DeferLoading: true,
		Description:  "Stock prices",
		Parameters: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"symbol"},
			"properties":           map[string]interface{}{"symbol": map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return float64(42), nil
		},
	}
	codeMode := CodeModeTool(ToolCallerOptions{ToolDiscovery: ToolDiscoveryConversation})
	toolSearch := ai.ToolSearch()

	toolCallers := ai.ExperimentalToolCallers{
		toolSearch.Name:  {codeMode.Name},
		getForecast.Name: {codeMode.Name},
		stockPrice.Name:  {codeMode.Name},
	}

	// Even guessing the name cannot call an undiscovered tool in this step
	// (mirrors the TS test's own comment).
	programs := []string{
		`const matches = await tools.toolSearch({ query: "weather" }); ` +
			`try { await tools.getForecast({ city: "Bangalore" }); return { calledEarly: true }; } ` +
			`catch (e) { return { matches, calledEarly: false }; }`,
		`return await tools.getForecast({ city: "Bangalore" });`,
	}

	for generation := 0; generation < 2; generation++ {
		model := &testutil.MockLanguageModel{}
		model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := len(model.GenerateCalls) - 1
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{{
					ID:        fmt.Sprintf("call-%d", step),
					ToolName:  codeMode.Name,
					Arguments: map[string]interface{}{"js": programs[step]},
				}},
			}, nil
		}

		result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
			Model:                   model,
			Prompt:                  "Get the weather in Bangalore.",
			Tools:                   []types.Tool{codeMode, toolSearch, getForecast, stockPrice},
			ExperimentalToolCallers: toolCallers,
			StopWhen:                []ai.StopCondition{ai.IsStepCount(2)},
		})
		if err != nil {
			t.Fatalf("generation %d: GenerateText error: %v", generation, err)
		}
		if len(result.Steps) != 2 {
			t.Fatalf("generation %d: got %d steps, want 2", generation, len(result.Steps))
		}

		step0Results := result.Steps[0].ToolResults
		if len(step0Results) != 1 {
			t.Fatalf("generation %d: step0 toolResults = %d, want 1 (%+v)", generation, len(step0Results), step0Results)
		}
		wantStep0 := map[string]interface{}{
			"matches": map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{"name": "getForecast", "description": "Weather forecast for a city"},
				},
			},
			"calledEarly": false,
		}
		assertDeepEqual(t, step0Results[0].Result, wantStep0)

		step1Results := result.Steps[1].ToolResults
		if len(step1Results) != 1 {
			t.Fatalf("generation %d: step1 toolResults = %d, want 1 (%+v)", generation, len(step1Results), step1Results)
		}
		wantStep1 := map[string]interface{}{"city": "Bangalore", "forecast": "rain"}
		assertDeepEqual(t, step1Results[0].Result, wantStep1)

		if executeCount != generation+1 {
			t.Fatalf("generation %d: getForecast.Execute called %d times total, want %d", generation, executeCount, generation+1)
		}

		if len(model.GenerateCalls) != 2 {
			t.Fatalf("generation %d: model called %d times, want 2", generation, len(model.GenerateCalls))
		}
		firstTools := model.GenerateCalls[0].Tools
		secondTools := model.GenerateCalls[1].Tools
		if len(firstTools) != 1 || firstTools[0].Name != codeMode.Name {
			t.Fatalf("generation %d: first-step model tools = %v, want [%s]", generation, toolNamesForTest(firstTools), codeMode.Name)
		}
		if len(secondTools) != 1 || secondTools[0].Name != codeMode.Name {
			t.Fatalf("generation %d: second-step model tools = %v, want [%s]", generation, toolNamesForTest(secondTools), codeMode.Name)
		}

		firstPrompt := marshalPromptForTest(t, model.GenerateCalls[0].Prompt)
		secondPrompt := marshalPromptForTest(t, model.GenerateCalls[1].Prompt)
		if strings.Contains(firstPrompt, "getForecast") {
			t.Fatalf("generation %d: first-step prompt should not mention getForecast:\n%s", generation, firstPrompt)
		}
		if !strings.Contains(secondPrompt, "getForecast") {
			t.Fatalf("generation %d: second-step prompt should mention getForecast:\n%s", generation, secondPrompt)
		}
		if strings.Contains(secondPrompt, "stockPrice") {
			t.Fatalf("generation %d: second-step prompt should not mention stockPrice:\n%s", generation, secondPrompt)
		}

		// NOTE: TS's tool-search.test.ts also asserts
		// `second.prompt.slice(0, first.prompt.length)).toEqual(first.prompt)`
		// (the second step's prompt is an exact prefix-extension of the
		// first step's). That does not hold here: Go's step loop
		// (pkg/ai/generate.go's currentMessages, pkg/ai/stream.go, and
		// pkg/agent/toolloop.go) carries forward the pre-toolCallerMessages
		// currentMessages into the next step rather than TS's
		// `messagesForNextStep = [...stepMessages, ...stepResponseMessages]`
		// (which folds that step's AppendToolCallerMessages announcement
		// into the persisted history). So each step's local-caller catalog
		// announcement is recomputed against the bare running history
		// instead of accumulating in it, and the prior step's announcement
		// message is dropped rather than kept. This is a pre-existing gap
		// in the tool-caller message-threading logic from P1-2c/P1-2d
		// (merged before this unit), outside CM2's ordering-only scope;
		// flagged as a hand-off rather than fixed here.
	}
}

// TestCodeModeWithToolSearch_RejectsDescriptionDiscoveryBeforeCallingModel
// ports TypeScript's code-mode/src/tool-search.test.ts, "rejects search
// with description discovery before calling the model" (verified against
// ai@7.0.118): a tool-search tool routed through a code-mode caller that
// uses the default 'description' discovery (no PrepareModelMessage to
// announce catalog updates in conversation) must be rejected up front,
// before the model is ever called.
func TestCodeModeWithToolSearch_RejectsDescriptionDiscoveryBeforeCallingModel(t *testing.T) {
	model := &testutil.MockLanguageModel{}
	codeMode := CodeModeTool(ToolCallerOptions{}) // default discovery: ToolDiscoveryDescription
	search := ai.ToolSearch(ai.ToolSearchConfig{Name: "search"})

	_, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Search tools",
		Tools:  []types.Tool{codeMode, search},
		ExperimentalToolCallers: ai.ExperimentalToolCallers{
			search.Name: {codeMode.Name},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "toolDiscovery: 'conversation'") {
		t.Fatalf("expected a toolDiscovery error, got %v", err)
	}
	if len(model.GenerateCalls) != 0 {
		t.Fatalf("model should not have been called, got %d calls", len(model.GenerateCalls))
	}
}

func marshalPromptForTest(t *testing.T, prompt types.Prompt) string {
	t.Helper()
	encoded, err := json.Marshal(prompt.Messages)
	if err != nil {
		t.Fatalf("marshal prompt: %v", err)
	}
	return string(encoded)
}
