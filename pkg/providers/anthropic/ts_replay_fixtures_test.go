package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// TS "should preserve the complete code execution transcript when replaying
// streamed output": the streamed provider-executed calls/results, replayed as an
// assistant message, must reproduce the exact server_tool_use / result blocks.
func TestFixtureCodeExecutionReplayPreservesTranscript(t *testing.T) {
	ce := []types.Tool{tools.CodeExecution20260120()}
	live, closeLive := modelWithID(t, "claude-sonnet-5", serveChunksFixture("anthropic-code-execution-20260120-prompt-cache.1", t), nil)
	defer closeLive()
	s, err := live.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: ce})
	if err != nil {
		t.Fatal(err)
	}
	var calls []*types.ToolCall
	var results []*types.ToolResult
	var text strings.Builder
	for _, c := range drain(t, s) {
		switch c.Type {
		case provider.ChunkTypeToolCall:
			calls = append(calls, c.ToolCall)
		case provider.ChunkTypeToolResult:
			results = append(results, c.ToolResult)
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		}
	}
	if len(calls) != 2 || len(results) != 2 {
		t.Fatalf("calls=%d results=%d", len(calls), len(results))
	}

	var body map[string]interface{}
	replay, closeReplay := modelWithID(t, "claude-sonnet-5", serveChunksFixture("anthropic-refusal", t), &body)
	defer closeReplay()
	assistant := []types.ContentPart{}
	for i := range calls {
		assistant = append(assistant,
			types.ToolCallContent{ToolCallID: calls[i].ID, ToolName: calls[i].ToolName, Arguments: calls[i].Arguments, ProviderExecuted: true},
			types.ToolResultContent{ToolCallID: results[i].ToolCallID, ToolName: results[i].ToolName, ProviderExecuted: true, Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: results[i].Result}},
		)
	}
	assistant = append(assistant, types.TextContent{Text: text.String()})
	rs, err := replay.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			{Role: types.RoleAssistant, Content: assistant},
		}},
		Tools: ce,
	})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, rs)

	msgs := body["messages"].([]interface{})
	got := msgs[1].(map[string]interface{})["content"]
	gotJSON, _ := json.Marshal(got)
	want := `[{"type":"server_tool_use","id":"srvtoolu_011fxGj786xCAh2kPk9GMxQw","name":"bash_code_execution","input":{"command":"for n in $(seq 1 12); do echo \"$n: $((n*n))\"; done"}},` +
		`{"type":"bash_code_execution_tool_result","tool_use_id":"srvtoolu_011fxGj786xCAh2kPk9GMxQw","content":{"type":"bash_code_execution_result","stdout":"1: 1\n2: 4\n3: 9\n4: 16\n5: 25\n6: 36\n7: 49\n8: 64\n9: 81\n10: 100\n11: 121\n12: 144\n","stderr":"","return_code":0,"content":[]}},` +
		`{"type":"server_tool_use","id":"srvtoolu_013eUksWZnfcjFk1iarJsYgM","name":"bash_code_execution","input":{"command":"sum=0; for n in $(seq 1 12); do sum=$((sum + n*n)); done; echo \"Sum: $sum\""}},` +
		`{"type":"bash_code_execution_tool_result","tool_use_id":"srvtoolu_013eUksWZnfcjFk1iarJsYgM","content":{"type":"bash_code_execution_result","stdout":"Sum: 650\n","stderr":"","return_code":0,"content":[]}},` +
		`{"type":"text","text":"The sum of the squares of the numbers 1 through 12 is **650**."}]`
	var a, b interface{}
	_ = json.Unmarshal(gotJSON, &a)
	_ = json.Unmarshal([]byte(want), &b)
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(ab) != string(bb) {
		t.Fatalf("replayed transcript\n got %s\nwant %s", gotJSON, want)
	}
}

// TS "deferred result": a tool_search_tool_result that arrives without its
// server_tool_use maps to the registered tool-search tool.
func TestFixtureToolSearchDeferredResult(t *testing.T) {
	deferred := func(name string) types.Tool {
		return types.Tool{Name: name, Description: "d", Parameters: map[string]interface{}{"type": "object"}, ProviderOptions: ToolOptions{DeferLoading: ptrBool(true)}}
	}
	cases := []struct {
		fixture string
		search  types.Tool
		fn      string
	}{
		{"anthropic-tool-search-deferred-bm25", tools.ToolSearchBm2520251119(), "get_weather"},
		{"anthropic-tool-search-deferred-regex", tools.ToolSearchRegex20251119(), "get_temp_data"},
	}
	for _, tc := range cases {
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "q"}, Tools: []types.Tool{tc.search, deferred(tc.fn)}}
		// stream
		m, closeFn := modelWithID(t, "claude-sonnet-4-5", serveChunksFixture(tc.fixture, t), nil)
		s, err := m.DoStream(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		var got *types.ToolResult
		for _, c := range drain(t, s) {
			if c.Type == provider.ChunkTypeToolResult && got == nil {
				got = c.ToolResult
			}
		}
		closeFn()
		if got == nil || got.ToolName != tc.search.Name {
			t.Fatalf("%s stream: result = %+v, want tool %s", tc.fixture, got, tc.search.Name)
		}
		// generate (fixture .2)
		m, closeFn = modelWithID(t, "claude-sonnet-4-5", serveJSONFixture(tc.fixture+".2", t), nil)
		res, err := m.DoGenerate(context.Background(), opts)
		closeFn()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range res.Content {
			if r, ok := c.(types.ToolResultContent); ok {
				found = true
				if r.ToolName != tc.search.Name {
					t.Fatalf("%s generate: tool = %s, want %s", tc.fixture, r.ToolName, tc.search.Name)
				}
			}
		}
		if !found {
			t.Fatalf("%s generate: no tool result", tc.fixture)
		}
	}
}

func ptrBool(b bool) *bool { return &b }
