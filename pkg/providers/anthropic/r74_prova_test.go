package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// r74provaTSShapedTool builds the TS LanguageModelV4ProviderTool as is: the
// tools record key as Name, the TS id and args, no typed options and no
// constructor.
func r74provaTSShapedTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	if args == nil {
		args = map[string]interface{}{}
	}
	return types.Tool{Name: name, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}, true
}

// Replays every TS anthropic-language-model.test.ts case with the provider
// tools exactly as TS passes them (custom names such as "research" or
// "web_search", id and args) and without the Go name aliases: requests,
// betas, warnings and the tool names of calls and results match TS.
func TestR74prova_TSCasesLanguageWithTSToolNames(t *testing.T) {
	tsfixture.ProviderToolHook = r74provaTSShapedTool
	tsfixture.DropResponseTimestamp = true
	defer func() {
		tsfixture.ProviderToolHook, tsfixture.DropResponseTimestamp = nil, false
	}()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "claude-3-haiku-20240307"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should send the model id and settings":                                                      "TS passes topK 0.1; Go TopK is an int",
		"should not strip temperature / topP / topK when thinking is disabled":                       "TS passes topK 0.1; Go TopK is an int",
		"should pass headers":                                                                        "the TS test sets provider-level headers on createAnthropic; the harness builds a default provider",
		"should merge custom anthropic-beta headers without legacy fine-grained-tool-streaming beta": "the TS test sets provider-level headers on createAnthropic; the harness builds a default provider",
		"should stream the text output":                                                              longSnapshot,
		"should stream code execution tool results":                                                  longSnapshot,
		"should stream code execution tool results without beta header":                              longSnapshot,
		"should include file id list in code execution tool call result.":                            longSnapshot,
		"should throw an api error when the first stream chunk is an overloaded error":               "the TS test expects DoStream to fail; covered by TestStreamOverloadedFirstEvent",
		"should process PDF citation responses":                                                      "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
		"should process PDF citation responses in streaming":                                         "the TS test's 'base64PDFdata' is not valid base64; Go decodes file data",
	})
}

// anthropic-batch.test.ts "rejects aliased provider tool names that cannot
// be restored later": a provider tool whose name differs from its wire name.
func TestR74prova_BatchRejectsRenamedProviderTool(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
	}))
	defer srv.Close()
	start := func(name string) error {
		req := textBatchRequest("request-1", ClaudeSonnet4_5, "Search")
		req.Text.Options.Tools = []types.Tool{{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305", Name: name, ProviderArgs: map[string]interface{}{}}}
		_, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
		return err
	}
	var uf *providererrors.UnsupportedFunctionalityError
	if err := start("custom_search"); !errors.As(err, &uf) || uf.Functionality != "aliased provider tool names in batches" {
		t.Fatalf("custom_search: err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("server called %d times for a rejected batch", calls)
	}
	// The wire name and the Go constructor's id-name stay accepted.
	for _, name := range []string{"web_search", "anthropic.web_search_20250305"} {
		if err := start(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// A renamed client provider tool (TS: tools record key "shell" for
// bash_20250124) is sent as the bash tool, its call comes back under the
// custom name. A client tool call in the history keeps its custom name, as TS
// convertToAnthropicPrompt sends name: part.toolName for tool_use blocks.
func TestR74prova_RenamedProviderToolRoundTrip(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","id":"msg_1","model":"claude-sonnet-4-5","role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"bash","input":{"command":"ls"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	shell := tools.Bash20250124()
	shell.Name = "shell"
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{
		Tools: []types.Tool{shell},
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "run"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "toolu_1", ToolName: "shell", Input: `{"command":"pwd"}`}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "toolu_1", ToolName: "shell", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "/"}}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r73provaJSON(t, body["tools"]); got != `[{"name":"bash","type":"bash_20250124"}]` {
		t.Errorf("tools = %s", got)
	}
	msgs := body["messages"].([]interface{})
	use := msgs[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
	if use["name"] != "shell" {
		t.Errorf("history tool_use name = %v, want shell", use["name"])
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "shell" {
		t.Fatalf("tool calls = %+v, want one call named shell", res.ToolCalls)
	}
}

// Replays anthropic-prepare-tools.test.ts with the provider tools exactly as
// TS passes them (custom names, id and args; no typed options).
func TestR74prova_TSCasesPrepareToolsWithTSShapedTools(t *testing.T) {
	tsfixture.ProviderToolHook = r74provaTSShapedTool
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		o := prepareToolsOptions{
			supportsStructuredOutput: in["supportsStructuredOutput"] == true,
			supportsStrictTools:      in["supportsStrictTools"] == true,
			rejectsForcedToolUse:     in["rejectsForcedToolUse"] == true,
		}
		if tools, ok := in["tools"]; ok {
			ts, why := tsfixture.BuildTools(tools)
			if why != "" {
				return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
			}
			o.tools = ts
		}
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice := tsfixture.BuildToolChoice(tc)
			o.toolChoice = &choice
		}
		if v, ok := in["disableParallelToolUse"].(bool); ok {
			o.disableParallelToolUse = &v
		}
		out := prepareTools(o)
		if out.err != nil {
			return nil, nil, out.err
		}
		betas := out.betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"tools":        out.tools,
			"tool_choice":  out.toolChoice,
			"toolChoice":   out.toolChoice,
			"toolWarnings": tsfixture.WarningsToTS(out.warnings),
			"betas":        map[string]interface{}{"__set": betas},
		}, nil, nil
	}, map[string]string{
		"should correctly prepare computer_20251124 tool with enableZoom false": "enableZoom is a Go bool: explicit false is omitted like unset (same API default false)",
	})
}

// advisor_20260301ArgsSchema requires model: a TS-shaped advisor tool
// without it fails like validateTypes instead of taking the constructor's
// default model.
func TestR74prova_TSShapedAdvisorRequiresModel(t *testing.T) {
	for _, args := range []map[string]interface{}{{}, {"model": 3}} {
		tl := types.Tool{Name: "advisor", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.advisor_20260301", ProviderArgs: args}
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		var tv *providererrors.TypeValidationError
		if !errors.As(got.err, &tv) {
			t.Errorf("args %v: err = %v, want a TypeValidationError", args, got.err)
		}
	}
	ok := types.Tool{Name: "advisor", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.advisor_20260301", ProviderArgs: map[string]interface{}{"model": "claude-opus-4-8"}}
	got := prepareTools(prepareToolsOptions{tools: []types.Tool{ok}})
	if got.err != nil || len(got.tools) != 1 || got.tools[0]["model"] != "claude-opus-4-8" {
		t.Fatalf("valid advisor: err %v tools %v", got.err, got.tools)
	}
}
