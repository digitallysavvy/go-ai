package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports ai/src/tool-search/prepare-tool-search.test.ts and (partially)
// ai/src/tool-search/tool-search.test.ts.

func searchTestCaller() types.Tool {
	msg := "catalog"
	return types.Tool{
		Name:       "code",
		Parameters: map[string]interface{}{"type": "object"},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeLocal,
			Bind: func(tools map[string]types.Tool) types.Tool {
				return types.Tool{Name: "code", Parameters: map[string]interface{}{"type": "object"}}
			},
			PrepareModelMessage: func(tools map[string]types.Tool) *string { return &msg },
		},
	}
}

func searchTestWeatherTool() types.Tool {
	return types.Tool{
		Name:         "getWeather",
		DeferLoading: true,
		Description:  "Weather forecast.",
		Parameters:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"secretSchemaField": map[string]interface{}{"type": "string"}}},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
}

func searchTestTools() []types.Tool {
	return []types.Tool{searchTestCaller(), ToolSearch("search"), searchTestWeatherTool()}
}

var searchTestCallers = ResolvedToolCallers{
	"search":     {"code"},
	"getWeather": {"code"},
}

func runSearch(t *testing.T, tools []types.Tool, query string) map[string]interface{} {
	t.Helper()
	var search *types.Tool
	for i := range tools {
		if tools[i].Name == "search" {
			search = &tools[i]
		}
	}
	if search == nil {
		t.Fatalf("search tool not found in %v", toolNames(tools))
	}
	out, err := search.Execute(context.Background(), map[string]interface{}{"query": query}, types.ToolExecutionOptions{ToolCallID: "search"})
	if err != nil {
		t.Fatalf("search.Execute() error = %v", err)
	}
	result, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("search.Execute() output = %#v, want map[string]interface{}", out)
	}
	return result
}

func searchResultNames(t *testing.T, result map[string]interface{}) []string {
	t.Helper()
	list, ok := result["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("result[tools] = %#v", result["tools"])
	}
	names := make([]string, len(list))
	for i, m := range list {
		names[i], _ = m["name"].(string)
	}
	return names
}

func hasTool(tools []types.Tool, name string) bool {
	for _, tl := range tools {
		if tl.Name == name {
			return true
		}
	}
	return false
}

func TestToolSearchState_DiscoversWithoutExposingSchemas(t *testing.T) {
	t.Parallel()
	state, err := NewToolSearchState(searchTestTools(), searchTestCallers)
	if err != nil {
		t.Fatalf("NewToolSearchState() error = %v", err)
	}
	first := state.Apply(searchTestTools(), nil, nil)
	if got, want := toolNames(first), []string{"code", "search"}; !equalStrings(got, want) {
		t.Fatalf("first = %v, want %v", got, want)
	}

	result := runSearch(t, first, "WEATHER")
	if got, want := searchResultNames(t, result), []string{"getWeather"}; !equalStrings(got, want) {
		t.Fatalf("search result = %v, want %v", got, want)
	}
	if desc := result["tools"].([]map[string]interface{})[0]["description"]; desc != "Weather forecast." {
		t.Fatalf("description = %v", desc)
	}

	// Discovery is visible on the next Apply call, not on `first` itself.
	if hasTool(first, "getWeather") {
		t.Fatalf("first should not gain getWeather in place")
	}
	second := state.Apply(searchTestTools(), nil, nil)
	if !hasTool(second, "getWeather") {
		t.Fatalf("second should include discovered getWeather")
	}
}

func TestToolSearchState_KeepsStateIsolatedAcrossGenerations(t *testing.T) {
	t.Parallel()
	first, err := NewToolSearchState(searchTestTools(), searchTestCallers)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToolSearchState(searchTestTools(), searchTestCallers)
	if err != nil {
		t.Fatal(err)
	}
	runSearch(t, first.Apply(searchTestTools(), nil, nil), "weather")
	if !hasTool(first.Apply(searchTestTools(), nil, nil), "getWeather") {
		t.Fatalf("first state should have discovered getWeather")
	}
	if hasTool(second.Apply(searchTestTools(), nil, nil), "getWeather") {
		t.Fatalf("second state should not see first state's discovery")
	}
}

func TestToolSearchState_DoesNotDiscoverExcludedOrOtherCallerTools(t *testing.T) {
	t.Parallel()
	msg := "other-catalog"
	otherCode := types.Tool{
		Name:       "otherCode",
		Parameters: map[string]interface{}{"type": "object"},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type:                types.ToolCallerTypeLocal,
			Bind:                func(tools map[string]types.Tool) types.Tool { return types.Tool{Name: "otherCode"} },
			PrepareModelMessage: func(map[string]types.Tool) *string { return &msg },
		},
	}
	otherWeather := searchTestWeatherTool()
	otherWeather.Name = "otherWeather"

	registry := append(searchTestTools(), otherCode, otherWeather)
	callers := ResolvedToolCallers{
		"search":       {"code"},
		"getWeather":   {"code"},
		"otherWeather": {"otherCode"},
	}
	state, err := NewToolSearchState(registry, callers)
	if err != nil {
		t.Fatal(err)
	}

	// eligible = registry without getWeather.
	eligible := make([]types.Tool, 0, len(registry))
	for _, tl := range registry {
		if tl.Name != "getWeather" {
			eligible = append(eligible, tl)
		}
	}
	result := runSearch(t, state.Apply(eligible, nil, nil), "weather")
	if names := searchResultNames(t, result); len(names) != 0 {
		t.Fatalf("expected no matches, got %v", names)
	}

	state2, err := NewToolSearchState(registry, callers)
	if err != nil {
		t.Fatal(err)
	}
	runSearch(t, state2.Apply(registry, nil, nil), "weather")
	if hasTool(state2.Apply(eligible, nil, nil), "getWeather") {
		t.Fatalf("getWeather should stay hidden from the eligible (excluded) set")
	}
	if hasTool(state2.Apply(registry, nil, nil), "otherWeather") {
		t.Fatalf("otherWeather belongs to another caller and must not be discovered")
	}
}

func TestToolSearchState_RanksNamesAboveDescriptionsAndCapsAtFive(t *testing.T) {
	t.Parallel()
	registry := searchTestTools()
	callers := ResolvedToolCallers{"search": {"code"}, "getWeather": {"code"}}
	for i := 0; i < 8; i++ {
		cand := searchTestWeatherTool()
		cand.Name = "candidate" + string(rune('0'+i))
		registry = append(registry, cand)
		callers["candidate"+string(rune('0'+i))] = []string{"code"}
	}

	state, err := NewToolSearchState(registry, callers)
	if err != nil {
		t.Fatal(err)
	}
	result := runSearch(t, state.Apply(registry, nil, nil), "weather")
	want := []string{"getWeather", "candidate0", "candidate1", "candidate2", "candidate3"}
	if got := searchResultNames(t, result); !equalStrings(got, want) {
		t.Fatalf("result = %v, want %v", got, want)
	}
}

func TestToolSearchState_RejectsDescriptionDiscoveryCallers(t *testing.T) {
	t.Parallel()
	// Local caller without PrepareModelMessage.
	noMessage := types.Tool{
		Name:       "code",
		Parameters: map[string]interface{}{"type": "object"},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type: types.ToolCallerTypeLocal,
			Bind: func(tools map[string]types.Tool) types.Tool { return types.Tool{Name: "code"} },
		},
	}
	// Provider caller.
	providerCaller := types.Tool{
		Name:       "code",
		Parameters: map[string]interface{}{"type": "object"},
		ExperimentalToolCaller: &types.ToolCallerDefinition{
			Type:                   types.ToolCallerTypeProvider,
			PrepareProviderOptions: func(m map[string]interface{}) map[string]interface{} { return m },
		},
	}

	for _, code := range []types.Tool{noMessage, providerCaller} {
		tools := []types.Tool{code, ToolSearch("search"), searchTestWeatherTool()}
		_, err := NewToolSearchState(tools, searchTestCallers)
		if err == nil || !strings.Contains(err.Error(), "toolDiscovery: 'conversation'") {
			t.Fatalf("expected toolDiscovery error, got %v", err)
		}
	}
}

func TestToolSearchState_RejectsDeferringSearchToolItself(t *testing.T) {
	t.Parallel()
	tools := searchTestTools()
	for i := range tools {
		if tools[i].Name == "search" {
			tools[i].DeferLoading = true
		}
	}
	_, err := NewToolSearchState(tools, searchTestCallers)
	if err == nil || !strings.Contains(err.Error(), "search tool itself must not defer loading") {
		t.Fatalf("expected defer-loading error, got %v", err)
	}
}

func TestToolSearchState_RespectsActiveToolsBeforeAndAfterDiscovery(t *testing.T) {
	t.Parallel()
	state, err := NewToolSearchState(searchTestTools(), nil)
	if err != nil {
		t.Fatal(err)
	}
	eligible := make([]types.Tool, 0)
	for _, tl := range searchTestTools() {
		if tl.Name != "getWeather" {
			eligible = append(eligible, tl)
		}
	}
	result := runSearch(t, state.Apply(eligible, nil, nil), "weather")
	if names := searchResultNames(t, result); len(names) != 0 {
		t.Fatalf("expected no matches when getWeather is excluded from activeTools, got %v", names)
	}
	runSearch(t, state.Apply(searchTestTools(), nil, nil), "weather")
	if hasTool(state.Apply(eligible, nil, nil), "getWeather") {
		t.Fatalf("getWeather must stay hidden while excluded from activeTools even after discovery")
	}
}

func TestToolSearchState_ConcurrentSearchesAccumulate(t *testing.T) {
	t.Parallel()
	registry := append(searchTestTools(), types.Tool{
		Name:         "getStockPrice",
		DeferLoading: true,
		Description:  "Stock price lookup.",
		Parameters:   map[string]interface{}{"type": "object"},
	})
	callers := ResolvedToolCallers{"search": {"code"}, "getWeather": {"code"}, "getStockPrice": {"code"}}
	state, err := NewToolSearchState(registry, callers)
	if err != nil {
		t.Fatal(err)
	}
	first := state.Apply(registry, nil, nil)
	var search *types.Tool
	for i := range first {
		if first[i].Name == "search" {
			search = &first[i]
		}
	}
	if search == nil {
		t.Fatalf("search tool not found")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = search.Execute(context.Background(), map[string]interface{}{"query": "weather"}, types.ToolExecutionOptions{})
	}()
	go func() {
		defer wg.Done()
		_, _ = search.Execute(context.Background(), map[string]interface{}{"query": "stock price"}, types.ToolExecutionOptions{})
	}()
	wg.Wait()

	if got, want := toolNames(first), []string{"code", "search"}; !equalStrings(got, want) {
		t.Fatalf("first unchanged = %v, want %v", got, want)
	}
	next := state.Apply(registry, nil, nil)
	if got, want := toolNames(next), []string{"code", "search", "getWeather", "getStockPrice"}; !equalStrings(got, want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

// TestGenerateText_ToolSearch_DiscoversOnNextStep ports the core scenario
// from ai/src/tool-search/tool-search.test.ts for generateText.
func TestGenerateText_ToolSearch_DiscoversOnNextStep(t *testing.T) {
	t.Parallel()
	var calls []*provider.GenerateOptions
	executeWeather := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := len(calls)
			calls = append(calls, opts)
			switch step {
			case 0:
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "search", ToolName: "search", Arguments: map[string]interface{}{"query": "weather"}},
					},
				}, nil
			case 1:
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "weather", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "Bangalore"}},
					},
				}, nil
			default:
				return &types.GenerateResult{FinishReason: types.FinishReasonStop, Text: "sunny"}, nil
			}
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(3)},
		Prompt:   "Find the weather.",
		Tools: []types.Tool{
			ToolSearch("search"),
			{
				Name:         "getWeather",
				DeferLoading: true,
				Description:  "Weather forecast",
				Parameters:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executeWeather++
					return input["city"].(string) + ": sunny", nil
				},
			},
			{
				Name:         "unrelated",
				DeferLoading: true,
				Description:  "Send email",
				Parameters:   map[string]interface{}{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if result.Text != "sunny" {
		t.Fatalf("Text = %q, want sunny", result.Text)
	}
	if executeWeather != 1 {
		t.Fatalf("executeWeather called %d times, want 1", executeWeather)
	}
	if len(calls) != 3 {
		t.Fatalf("model called %d times, want 3", len(calls))
	}
	if got, want := toolNames(calls[0].Tools), []string{"search"}; !equalStrings(got, want) {
		t.Fatalf("step0 tools = %v, want %v", got, want)
	}
	if got, want := toolNames(calls[1].Tools), []string{"search", "getWeather"}; !equalStrings(got, want) {
		t.Fatalf("step1 tools = %v, want %v", got, want)
	}
	for _, c := range calls {
		for _, tl := range c.Tools {
			if tl.Name == "unrelated" {
				t.Fatalf("unrelated tool must stay hidden, got %v", toolNames(c.Tools))
			}
		}
	}
}
