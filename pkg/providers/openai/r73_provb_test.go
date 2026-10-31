package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func r73provbAllTools() []types.Tool {
	maxResults := 4
	threshold := 0.5
	compression := 80
	external := false
	memory := responses.MemoryLimit4G
	skillRef := "skill_reference"
	skillID := "sk_1"
	syntax := "regex"
	definition := `\d+`
	return []types.Tool{
		openaitool.WebSearch(openaitool.WebSearchConfig{
			ExternalWebAccess: &external,
			Filters:           &openaitool.WebSearchFilters{AllowedDomains: []string{"a.com"}},
			SearchContextSize: "high",
			UserLocation:      &openaitool.WebSearchLocation{City: "Paris"},
		}),
		openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{SearchContextSize: "low"}), //nolint:staticcheck // the deprecated tool is still sent
		openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: openaitool.CodeInterpreterContainer{FileIDs: []string{"f1"}}}),
		openaitool.FileSearch(openaitool.FileSearchConfig{
			VectorStoreIDs: []string{"vs_1"},
			MaxNumResults:  &maxResults,
			Ranking:        &openaitool.FileSearchRanking{Ranker: "auto", ScoreThreshold: &threshold},
			Filters:        map[string]interface{}{"key": "k", "type": "eq", "value": "v"},
		}),
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{OutputFormat: "webp", OutputCompression: &compression}),
		openaitool.MCP(openaitool.MCPConfig{
			ServerLabel:     "docs",
			ServerURL:       "https://mcp.example.com",
			Authorization:   "Bearer x",
			AllowedTools:    openaitool.MCPAllowedTools{ToolNames: []string{"search"}},
			RequireApproval: openaitool.MCPRequireApproval{Never: &openaitool.MCPApprovalFilter{ToolNames: []string{"search"}}},
		}),
		openaitool.ToolSearch(openaitool.ToolSearchArgs{
			Execution:   "client",
			Description: "Find tools",
			Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}},
		}),
		openaitool.NewCustomTool(
			openaitool.WithDescription("digits"),
			openaitool.WithFormat(openaitool.CustomToolFormat{Type: "grammar", Syntax: &syntax, Definition: &definition}),
		).ToTool("digits"),
		responses.NewLocalShellTool(),
		responses.NewShellTool(responses.WithShellEnvironment(responses.ShellEnvironment{
			Type:        "container_auto",
			FileIDs:     []string{"f2"},
			MemoryLimit: &memory,
			Skills:      []responses.ShellSkill{{Type: &skillRef, SkillID: &skillID}},
		})),
		responses.NewApplyPatchTool(),
		responses.NewComputerTool(),
		responses.NewProgrammaticToolCallingTool(),
	}
}

// r73provbResponsesToolsWire is the Responses API `tools` array the openai
// constructors produced before they set Type/ProviderID/ProviderArgs (CR-3):
// the same test passes with the c689e39f constructors.
const r73provbResponsesToolsWire = `[{"external_web_access":false,"filters":{"allowed_domains":["a.com"]},"search_context_size":"high","type":"web_search","user_location":{"city":"Paris","type":"approximate"}},{"search_context_size":"low","type":"web_search_preview"},{"container":{"file_ids":["f1"],"type":"auto"},"type":"code_interpreter"},{"filters":{"key":"k","type":"eq","value":"v"},"max_num_results":4,"ranking_options":{"ranker":"auto","score_threshold":0.5},"type":"file_search","vector_store_ids":["vs_1"]},{"output_compression":80,"output_format":"webp","type":"image_generation"},{"allowed_tools":{"tool_names":["search"]},"authorization":"Bearer x","require_approval":{"never":{"tool_names":["search"]}},"server_label":"docs","server_url":"https://mcp.example.com","type":"mcp"},{"description":"Find tools","execution":"client","parameters":{"properties":{"query":{"type":"string"}},"type":"object"},"type":"tool_search"},{"description":"digits","format":{"definition":"\\d+","syntax":"regex","type":"grammar"},"name":"digits","type":"custom"},{"type":"local_shell"},{"environment":{"file_ids":["f2"],"memory_limit":"4g","skills":[{"skill_id":"sk_1","type":"skill_reference","version":"latest"}],"type":"container_auto"},"type":"shell"},{"type":"apply_patch"},{"type":"computer"},{"type":"programmatic_tool_calling"}]`

func r73provbJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestR73ProvbResponsesToolsWireUnchanged: the direct OpenAI Responses
// request for every openai tool constructor is unchanged by CR-3.
func TestR73ProvbResponsesToolsWireUnchanged(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5")
	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  r73provbAllTools(),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := r73provbJSON(t, body["tools"]); got != r73provbResponsesToolsWire {
		if os.Getenv("R73PROVB_DUMP") != "" {
			t.Logf("DUMP %s", got)
		}
		t.Fatalf("tools =\n%s\nwant\n%s", got, r73provbResponsesToolsWire)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}

// TestR73ProvbChatDropsProviderTools: Chat Completions takes function tools
// only; TS prepareChatTools drops every provider tool with an
// "unsupported" `tool type: provider` warning. With Type "provider" the
// openai constructors now get the same treatment (v0.5 sent them as
// function tools named "openai.web_search" etc.).
func TestR73ProvbChatDropsProviderTools(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	fn := types.Tool{Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	tools := append([]types.Tool{fn}, r73provbAllTools()...)
	body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: tools}, false)
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := json.Marshal(body["tools"])
	if strings.Contains(string(sent), "openai.") || !strings.Contains(string(sent), `"lookup"`) {
		t.Fatalf("chat tools = %s, want only the function tool", sent)
	}
	unsupported := 0
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "tool type: provider" {
			unsupported++
		}
	}
	if unsupported != len(tools)-1 {
		t.Fatalf("%d `tool type: provider` warnings, want %d: %#v", unsupported, len(tools)-1, warnings)
	}
}

func r73provbServer(t *testing.T, stream bool, items ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, `data: {"type":"response.created","response":{"id":"r","created_at":1,"model":"gpt-5"}}`+"\n\n")
			for i, item := range items {
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
			}
			_, _ = fmt.Fprint(w, `data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"resp","object":"response","created_at":1,"status":"completed","model":"gpt-5","output":[%s],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, strings.Join(items, ","))
	}))
}

type r73provbPart struct {
	call             bool
	toolName         string
	providerExecuted bool
}

func r73provbCollect(t *testing.T, stream bool, tools []types.Tool, items ...string) []r73provbPart {
	t.Helper()
	srv := r73provbServer(t, stream, items...)
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-5")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: tools}
	var parts []r73provbPart
	if !stream {
		res, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.ToolCalls {
			parts = append(parts, r73provbPart{true, c.ToolName, c.ProviderExecuted})
		}
		for _, p := range res.Content {
			if r, ok := p.(types.ToolResultContent); ok {
				parts = append(parts, r73provbPart{false, r.ToolName, true})
			}
		}
		return parts
	}
	s, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolCall && c.ToolCall != nil {
			parts = append(parts, r73provbPart{true, c.ToolCall.ToolName, c.ToolCall.ProviderExecuted})
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			parts = append(parts, r73provbPart{false, c.ToolResult.ToolName, true})
		}
	}
	return parts
}

// TestR73ProvbHostedToolNamesUnchanged: hosted tool calls of the openai
// constructors (Name == ProviderID) keep their bare names after CR-3.
func TestR73ProvbHostedToolNamesUnchanged(t *testing.T) {
	tools := []types.Tool{
		openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{}),
		openaitool.FileSearch(openaitool.FileSearchConfig{VectorStoreIDs: []string{"vs"}}),
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{}),
		openaitool.WebSearch(openaitool.WebSearchConfig{}),
	}
	items := []string{
		`{"type":"code_interpreter_call","id":"ci_1","container_id":"c","code":"1","status":"completed","outputs":[]}`,
		`{"type":"file_search_call","id":"fs_1","status":"completed","queries":["q"],"results":null}`,
		`{"type":"image_generation_call","id":"ig_1","status":"completed","result":"aGk="}`,
		`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"q"}}`,
	}
	for _, stream := range []bool{false, true} {
		names := map[string]bool{}
		for _, p := range r73provbCollect(t, stream, tools, items...) {
			names[p.toolName] = true
		}
		for _, want := range []string{"code_interpreter", "file_search", "image_generation", "web_search"} {
			if !names[want] {
				t.Errorf("stream=%v: no %q part in %v", stream, want, names)
			}
		}
		for name := range names {
			if strings.HasPrefix(name, "openai.") {
				t.Errorf("stream=%v: hosted part named %q", stream, name)
			}
		}
	}
	// A custom SDK name still surfaces (TS toCustomToolName).
	named := []types.Tool{{Name: "generateImage", Type: types.ToolTypeProviderDefined, ProviderID: "openai.image_generation", ProviderArgs: map[string]interface{}{}}}
	for _, stream := range []bool{false, true} {
		parts := r73provbCollect(t, stream, named, items[2])
		if len(parts) == 0 || parts[0].toolName != "generateImage" {
			t.Errorf("stream=%v custom-named image_generation parts = %#v", stream, parts)
		}
	}
}

// TestR73ProvbToolSearchCallsNameTheTool: tool_search calls and outputs are
// named after the declared tool (TS toolNameMapping.toCustomToolName), in
// generate and stream alike. v0.5 named them "tool_search", which matched
// no tool in the request: a client-mode call could not run and history
// replay did not recognize the call.
func TestR73ProvbToolSearchCallsNameTheTool(t *testing.T) {
	tools := []types.Tool{openaitool.ToolSearch(openaitool.ToolSearchArgs{})}
	items := []string{
		`{"type":"tool_search_call","id":"tsc_1","execution":"server","call_id":null,"status":"completed","arguments":{"paths":["a"]}}`,
		`{"type":"tool_search_output","id":"tso_1","execution":"server","call_id":null,"status":"completed","tools":[]}`,
	}
	for _, stream := range []bool{false, true} {
		parts := r73provbCollect(t, stream, tools, items...)
		if len(parts) != 2 {
			t.Fatalf("stream=%v parts = %#v", stream, parts)
		}
		for _, p := range parts {
			if p.toolName != "openai.tool_search" || !p.providerExecuted {
				t.Errorf("stream=%v part = %#v, want openai.tool_search (provider-executed)", stream, p)
			}
		}
	}
}

// TestR73ProvbClientToolSearchRoundTrip: a client-mode tool_search call
// runs the tool's Execute and is replayed as tool_search_call +
// tool_search_output on the next step.
func TestR73ProvbClientToolSearchRoundTrip(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		step := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		output := `{"type":"tool_search_call","id":"tsc_1","execution":"client","call_id":"call_ts","status":"completed","arguments":{"query":"weather"}}`
		if step > 1 {
			output = `{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}`
		}
		_, _ = fmt.Fprintf(w, `{"id":"resp_%d","object":"response","created_at":1,"status":"completed","model":"gpt-5","output":[%s],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, step, output)
	}))
	defer srv.Close()
	executed := false
	search := openaitool.ToolSearch(openaitool.ToolSearchArgs{
		Execution:  "client",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "function", "name": "get_weather", "parameters": map[string]interface{}{"type": "object"}}}}, nil
		},
	})
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-5")
	res, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "weather?",
		Tools:    []types.Tool{search},
		StopWhen: []ai.StopCondition{ai.IsStepCount(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Fatalf("client tool_search Execute not called; steps = %d", len(res.Steps))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(bodies))
	}
	// store defaults to true, so the call is an item reference (TS).
	if !strings.Contains(bodies[1], `{"id":"tsc_1","type":"item_reference"}`) || !strings.Contains(bodies[1], `"type":"tool_search_output"`) || !strings.Contains(bodies[1], `"call_id":"call_ts"`) {
		t.Fatalf("step 2 input lacks the tool_search call/output: %s", bodies[1])
	}
}

// TestR73ProvbShellContainerIsProviderExecuted: a shell tool with a hosted
// container environment runs on OpenAI (TS isShellProviderExecuted), so its
// shell_call is provider-executed. v0.5's NewShellTool had no ProviderID
// and the check never matched.
func TestR73ProvbShellContainerIsProviderExecuted(t *testing.T) {
	call := `{"id":"sh_1","type":"shell_call","status":"completed","call_id":"call_1","action":{"commands":["ls"]}}`
	containerTool := responses.NewShellTool(responses.WithShellEnvironment(responses.ShellEnvironment{Type: "container_auto"}))
	localTool := responses.NewShellTool()
	tsShaped := types.Tool{Name: "openai.shell", Type: types.ToolTypeProviderDefined, ProviderID: "openai.shell",
		ProviderArgs: map[string]interface{}{"environment": map[string]interface{}{"type": "containerReference", "containerId": "c"}}}
	for _, stream := range []bool{false, true} {
		for _, c := range []struct {
			tool types.Tool
			want bool
		}{{containerTool, true}, {localTool, false}, {tsShaped, true}} {
			parts := r73provbCollect(t, stream, []types.Tool{c.tool}, call)
			if len(parts) != 1 || !parts[0].call || parts[0].toolName != "openai.shell" || parts[0].providerExecuted != c.want {
				t.Errorf("stream=%v args=%v parts = %#v, want providerExecuted=%v", stream, c.tool.ProviderArgs, parts, c.want)
			}
		}
	}
}
