package mistral

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72provTSSchemas: TS zod schemas of mistral.web_search, mistral.web_search_premium at e64ed82f27.
const r72provTSSchemas = `{"mistral.web_search":{"input":{"type":"object","properties":{"arguments":{"type":"string"}},"required":["arguments"]},"output":{"type":"object","properties":{"info":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}},"definitions":{"__schema0":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"},{"type":"array","items":{"$ref":"#/definitions/__schema0"}},{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}]}}}},"mistral.web_search_premium":{"input":{"type":"object","properties":{"arguments":{"type":"string"}},"required":["arguments"]},"output":{"type":"object","properties":{"info":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}},"definitions":{"__schema0":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"},{"type":"array","items":{"$ref":"#/definitions/__schema0"}},{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}]}}}}}`

// r72provNormalize round-trips v through JSON, drops "description" (the
// provider tool descriptions are not part of validation) and sorts
// "required" lists so Go literals and zod output compare equal.
func r72provNormalize(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	var walk func(interface{})
	walk = func(n interface{}) {
		switch x := n.(type) {
		case map[string]interface{}:
			delete(x, "description")
			for k, child := range x {
				if k == "required" {
					if list, ok := child.([]interface{}); ok {
						sort.Slice(list, func(i, j int) bool { return list[i].(string) < list[j].(string) })
					}
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(out)
	return out
}

// r72provCheckTS compares each tool's Parameters and OutputSchema with the
// zod toJSONSchema output (draft-07, io "input", "$schema" removed) of the
// TS tool at e64ed82f27. Provider tool calls are validated against
// Parameters and ValidateUIMessages checks outputs against OutputSchema.
func r72provCheckTS(t *testing.T, tsJSON string, tools map[string]types.Tool) {
	t.Helper()
	var want map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(tsJSON), &want); err != nil {
		t.Fatal(err)
	}
	for id, tool := range tools {
		w := want[id]
		for _, c := range []struct {
			field    string
			got, exp interface{}
		}{{"Parameters", tool.Parameters, w["input"]}, {"OutputSchema", tool.OutputSchema, w["output"]}} {
			g, e := r72provNormalize(t, c.got), r72provNormalize(t, c.exp)
			if !reflect.DeepEqual(g, e) {
				gb, _ := json.Marshal(g)
				eb, _ := json.Marshal(e)
				t.Errorf("%s %s:\n got %s\nwant %s", id, c.field, gb, eb)
			}
		}
	}
}

// r72provValidate reports whether value (JSON text) passes the schema.
func r72provValidate(t *testing.T, sch interface{}, value string) error {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		t.Fatal(err)
	}
	return schema.NewSimpleJSONSchema(sch.(map[string]interface{})).Validator().Validate(v)
}

func TestR72ProvWebSearchSchemasEqualTS(t *testing.T) {
	r72provCheckTS(t, r72provTSSchemas, map[string]types.Tool{"mistral.web_search": WebSearch(), "mistral.web_search_premium": WebSearchPremium()})
}

// The output schema accepts the info record of JSON values the provider
// returns (and no info), and rejects a non-record info, like TS zod.
func TestR72ProvWebSearchOutputSchema(t *testing.T) {
	out := WebSearch().OutputSchema
	for _, ok := range []string{`{}`, `{"info":{"sources":[{"url":"u","n":1,"x":null,"nested":{"a":[true]}}]}}`} {
		if err := r72provValidate(t, out, ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{`{"info":"text"}`, `{"info":null}`, `[]`} {
		if err := r72provValidate(t, out, bad); err == nil {
			t.Errorf("%s accepted, want invalid", bad)
		}
	}
}

// The web search tools are provider tools (TS type 'provider'): the chat
// model drops them with TS's unsupported warning instead of sending a
// function tool named mistral.web_search (tools: [] like TS), and the conversation model still
// maps them to its built-in tools.
func TestR72ProvWebSearchIsProviderTool(t *testing.T) {
	if WebSearch().Type != types.ToolTypeProviderDefined || WebSearchPremium().Type != types.ToolTypeProviderDefined {
		t.Fatalf("Type = %q / %q, want %q", WebSearch().Type, WebSearchPremium().Type, types.ToolTypeProviderDefined)
	}
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"mistral-small-latest","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model, err := p.LanguageModel("mistral-small-latest")
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}}},
		Tools:  []types.Tool{WebSearch()},
	})
	if err != nil {
		t.Fatal(err)
	}
	// TS prepareTools returns the (empty) mistralTools list: tools: [].
	if tools, ok := body["tools"].([]interface{}); !ok || len(tools) != 0 {
		t.Errorf("chat request tools = %v, want [] (no function tool)", body["tools"])
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && w.Feature == "provider-defined tool mistral.web_search" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want the unsupported provider tool warning", result.Warnings)
	}
	wire, _, warnings := prepareConversationTools([]types.Tool{WebSearch(), WebSearchPremium()}, types.ToolChoice{})
	if len(warnings) != 0 || len(wire) != 2 || wire[0]["type"] != "web_search" || wire[1]["type"] != "web_search_premium" {
		t.Errorf("conversation tools = %v, warnings %v", wire, warnings)
	}
}
