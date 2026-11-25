package anthropic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func r75provaJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// R75 item 3: forcing a Go provider tool by its id-Name sends the Anthropic
// wire name ("bash"), the name the tool is declared under; a tool named as in
// TS ("bash", or a custom name) is sent unchanged.
func TestR75prova_ForcedToolChoiceOfIDNamedToolUsesWireName(t *testing.T) {
	bash := tools.Bash20250124()
	fn := types.Tool{Name: "lookup", Parameters: map[string]interface{}{"type": "object"}}
	cases := []struct {
		name   string
		tools  []types.Tool
		choice string
		want   string
	}{
		{"id-named", []types.Tool{bash, fn}, "anthropic.bash_20250124", "bash"},
		{"function", []types.Tool{bash, fn}, "lookup", "lookup"},
		{"ts-named", []types.Tool{{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124", Name: "bash"}}, "bash", "bash"},
		{"unknown tool", []types.Tool{fn}, "anthropic.bash_20250124", "anthropic.bash_20250124"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := prepareTools(prepareToolsOptions{tools: c.tools, toolChoice: &types.ToolChoice{Type: types.ToolChoiceTool, ToolName: c.choice}})
			if out.err != nil {
				t.Fatal(out.err)
			}
			if got := out.toolChoice["name"]; got != c.want {
				t.Fatalf("tool_choice = %v, want name %q", out.toolChoice, c.want)
			}
		})
	}

	// A model that rejects forced tool use keeps only the forced tool: the
	// id-named tool is found under its wire name.
	out := prepareTools(prepareToolsOptions{
		tools:                []types.Tool{bash, fn},
		toolChoice:           &types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "anthropic.bash_20250124"},
		rejectsForcedToolUse: true,
	})
	if got := r75provaJSON(t, out.tools); got != `[{"name":"bash","type":"bash_20250124"}]` {
		t.Fatalf("tools = %s", got)
	}
	if len(out.warnings) != 1 || out.toolChoice["type"] != "auto" {
		t.Fatalf("warnings = %+v, tool_choice = %v", out.warnings, out.toolChoice)
	}
}

// The request body carries the mapped name.
func TestR75prova_ForcedToolChoiceRequestBody(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","id":"msg_1","model":"claude-sonnet-4-5","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"ls"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "run"},
		Tools:      []types.Tool{tools.Bash20250124()},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "anthropic.bash_20250124"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r75provaJSON(t, body["tool_choice"]); got != `{"name":"bash","type":"tool"}` {
		t.Fatalf("tool_choice = %s", got)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "anthropic.bash_20250124" {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
}

// R75 item 4: batch results of id-named provider tools come back under the
// tool's Name, as DoGenerate returns them; other requests of the batch and
// batches started elsewhere keep the wire name.
func TestR75prova_BatchResultsUseIDNamedToolName(t *testing.T) {
	message := func(name string) string {
		return `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"` + name + `","input":{"command":"ls"}}],"model":"claude-sonnet-4-5","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`
	}
	resultsBody := `{"custom_id":"with-tool","result":{"type":"succeeded","message":` + message("bash") + `}}` + "\n" +
		`{"custom_id":"plain","result":{"type":"succeeded","message":` + message("bash") + `}}` + "\n"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var sent map[string]interface{}
	mux.HandleFunc("/messages/batches", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":2,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
	})
	for _, id := range []string{"b1", "b2"} {
		id := id
		mux.HandleFunc("/messages/batches/"+id, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"` + id + `","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":2,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srv.URL + `/results"}`))
		})
	}
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(resultsBody))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	withTool := textBatchRequest("with-tool", ClaudeSonnet4_5, "run")
	withTool.Text.Options.Tools = []types.Tool{tools.Bash20250124()}
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{withTool, textBatchRequest("plain", ClaudeSonnet4_5, "hi")}}); err != nil {
		t.Fatal(err)
	}
	reqs := sent["requests"].([]interface{})
	params := reqs[0].(map[string]interface{})["params"].(map[string]interface{})
	if got := r75provaJSON(t, params["tools"]); got != `[{"name":"bash","type":"bash_20250124"}]` {
		t.Fatalf("sent tools = %s", got)
	}

	names := func(batchID string) map[string]string {
		stream, err := p.ExperimentalBatch().DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: batchID})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.Close() }()
		out := map[string]string{}
		for {
			item, err := stream.Next()
			if err != nil {
				break
			}
			if item.TextResult == nil || len(item.TextResult.ToolCalls) != 1 {
				t.Fatalf("item = %+v", item)
			}
			name := item.TextResult.ToolCalls[0].ToolName
			for _, part := range item.TextResult.Content {
				if tc, ok := part.(types.ToolCallContent); ok && tc.ToolName != name {
					t.Fatalf("content tool name %q != tool call name %q", tc.ToolName, name)
				}
			}
			out[item.ID] = name
		}
		return out
	}
	if got := names("b1"); got["with-tool"] != "anthropic.bash_20250124" || got["plain"] != "bash" {
		t.Fatalf("b1 names = %v", got)
	}
	// b2 was not started by this provider: no tool list, wire names.
	if got := names("b2"); got["with-tool"] != "bash" || got["plain"] != "bash" {
		t.Fatalf("b2 names = %v", got)
	}
}

func TestR75prova_BatchToolNameStoreBounded(t *testing.T) {
	var s batchToolNameStore
	entry := &batchToolNames{all: map[string]string{"bash": "x"}}
	s.put("", entry)
	s.put("empty", nil)
	if len(s.names) != 0 {
		t.Fatalf("stored %v", s.names)
	}
	for i := 0; i <= maxRememberedBatchToolNames; i++ {
		s.put(string(rune('a'+i%26))+string(rune(i)), entry)
	}
	if len(s.names) != maxRememberedBatchToolNames || len(s.order) != maxRememberedBatchToolNames {
		t.Fatalf("len = %d/%d", len(s.names), len(s.order))
	}
	if s.get("a"+string(rune(0))) != nil {
		t.Fatal("oldest batch not dropped")
	}
}
