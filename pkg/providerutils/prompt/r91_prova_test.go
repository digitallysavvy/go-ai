package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// core2-08: TS extractErrorValue / the code execution errorInfo parse a
// string value with secureJsonParse (convert-to-anthropic-prompt.ts:68-85,
// 1240-1250): a value with __proto__ throws, so its errorCode is not read.
func TestR91provaAnthropicErrorValueSecureParse(t *testing.T) {
	webSearch := func(value string) []types.Message {
		return []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "s", ToolName: "web_search", ProviderExecuted: true, Arguments: map[string]interface{}{"query": "q"}},
			types.ToolResultContent{ToolCallID: "s", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: value}},
		}}}
	}
	p, err := ConvertToAnthropicPrompt(webSearch(`{"__proto__":{},"errorCode":"max_uses_exceeded"}`), AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", p.Messages, `[{"role":"assistant","content":[
		{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"q"}},
		{"type":"web_search_tool_result","tool_use_id":"s","content":{"type":"web_search_tool_result_error","error_code":"unavailable"}}
	]}]`)

	// The plain value keeps its errorCode.
	p, err = ConvertToAnthropicPrompt(webSearch(`{"errorCode":"max_uses_exceeded"}`), AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", p.Messages, `[{"role":"assistant","content":[
		{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"q"}},
		{"type":"web_search_tool_result","tool_use_id":"s","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}
	]}]`)

	if got := parseErrorValue(`{"constructor":{"prototype":{}},"type":"code_execution_tool_result_error","errorCode":"x"}`); len(got) != 0 {
		t.Fatalf("parseErrorValue = %v, want {}", got)
	}
}

// core2-09: TS getCacheControl `if (!cacheControlValue) return undefined`
// (get-cache-control.ts:50-52): a JS-falsy cacheControl is ignored and uses
// no breakpoint.
func TestR91provaAnthropicFalsyCacheControl(t *testing.T) {
	for _, falsy := range []interface{}{false, 0, 0.0, "", int64(0)} {
		v := NewAnthropicCacheControlValidator()
		msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "a", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": falsy}}},
		}}}
		p, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{CacheControlValidator: v})
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, "messages", p.Messages, `[{"role":"user","content":[{"type":"text","text":"a"}]}]`)
		if v.breakpointCount != 0 {
			t.Fatalf("%#v: breakpointCount = %d, want 0", falsy, v.breakpointCount)
		}
	}
	// A truthy value is still sent.
	p, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "a", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
	}}}, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", p.Messages, `[{"role":"user","content":[{"type":"text","text":"a","cache_control":{"type":"ephemeral"}}]}]`)
}

// Typed-option ruling: message and part provider options given as Go-typed
// literals (map[string]string, map[string]bool, typed structs) are read
// through their JSON form, like the map[string]interface{} form.
func TestR91provaAnthropicTypedPartOptions(t *testing.T) {
	type cacheControl struct {
		Type string `json:"type"`
		TTL  string `json:"ttl,omitempty"`
	}
	for name, po := range map[string]map[string]interface{}{
		"map[string]string inner": {"anthropic": map[string]interface{}{"cacheControl": map[string]string{"type": "ephemeral", "ttl": "1h"}}},
		"map[string]map outer":    {"anthropic": map[string]map[string]string{"cacheControl": {"type": "ephemeral", "ttl": "1h"}}},
		"typed struct":            {"anthropic": map[string]interface{}{"cacheControl": cacheControl{Type: "ephemeral", TTL: "1h"}}},
	} {
		p, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "a", ProviderOptions: po},
		}}}, AnthropicPromptOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertJSONEqual(t, name, p.Messages, `[{"role":"user","content":[{"type":"text","text":"a","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]`)
	}

	// File part citations and title as typed literals.
	p, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "text/plain", Data: []byte("hello"), ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"citations": map[string]bool{"enabled": true}, "title": "T"},
		}},
	}}}, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content := p.Messages[0]["content"].([]map[string]interface{})
	if content[0]["citations"] == nil || content[0]["title"] != "T" {
		t.Fatalf("document block = %v, want citations and title", content[0])
	}
}
