package alibaba

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func alibabaCC(v interface{}) map[string]interface{} {
	return map[string]interface{}{"alibaba": map[string]interface{}{"cacheControl": v}}
}

// TS get-cache-control.ts: markers come from each message's / part's own
// providerOptions.alibaba.cacheControl and are passed through unchanged; the
// message-level marker lands on the last part when the part has none.
func TestPerPartCacheControl(t *testing.T) {
	ephemeral := map[string]interface{}{"type": "ephemeral"}
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "sys"}}, ProviderOptions: alibabaCC(ephemeral)},
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "a"},
			types.TextContent{Text: "b", ProviderOptions: alibabaCC(ephemeral)},
			types.TextContent{Text: "c"},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "reply"}}, ProviderOptions: alibabaCC(ephemeral)},
	}
	v := NewPartCacheControlValidator()
	out, err := ConvertToAlibabaChatMessagesWithError(msgs, v, false)
	if err != nil {
		t.Fatal(err)
	}
	sys := out[0]["content"].([]map[string]interface{})
	if !reflect.DeepEqual(sys[0]["cache_control"], ephemeral) {
		t.Fatalf("system = %#v", out[0])
	}
	parts := out[1]["content"].([]map[string]interface{})
	if _, ok := parts[0]["cache_control"]; ok {
		t.Fatalf("part a should carry no marker: %#v", parts[0])
	}
	if !reflect.DeepEqual(parts[1]["cache_control"], ephemeral) {
		t.Fatalf("part b = %#v", parts[1])
	}
	if _, ok := parts[2]["cache_control"]; ok {
		t.Fatalf("part c should carry no marker: %#v", parts[2])
	}
	asst := out[2]["content"].([]map[string]interface{})
	if !reflect.DeepEqual(asst[0]["cache_control"], ephemeral) {
		t.Fatalf("assistant = %#v", out[2])
	}
	if len(v.Warnings()) != 0 {
		t.Fatalf("warnings = %#v", v.Warnings())
	}

	// A falsy marker is no marker; every marker past the fourth warns.
	v = NewPartCacheControlValidator()
	for i := 0; i < 6; i++ {
		v.GetCacheControlFromOptions(alibabaCC(ephemeral))
	}
	if v.GetCacheControlFromOptions(alibabaCC(false)) != nil || v.GetCacheControlFromOptions(alibabaCC("")) != nil {
		t.Fatal("falsy cache control should be ignored")
	}
	if len(v.Warnings()) != 2 || v.Warnings()[0].Message != "Max breakpoint limit exceeded. Only the last 4 cache markers will take effect." {
		t.Fatalf("warnings = %#v", v.Warnings())
	}
}

// Without a call-level cacheControl the model uses the per-part path, and an
// unsupported file part fails like TS convertToAlibabaChatMessages.
func TestBuildRequestPerPartCacheAndUnsupportedFiles(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus")
	body, _, err := m.buildRequestBodyChecked(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role:            types.RoleUser,
		Content:         []types.ContentPart{types.TextContent{Text: "hi"}},
		ProviderOptions: alibabaCC(map[string]interface{}{"type": "ephemeral"}),
	}}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs := body["messages"].([]map[string]interface{})
	parts := msgs[0]["content"].([]map[string]interface{})
	if parts[0]["cache_control"] == nil {
		t.Fatalf("messages = %#v", msgs)
	}

	_, _, err = m.buildRequestBodyChecked(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{
			MediaType: "application/pdf",
			FileData:  types.FileData{Type: types.FileDataTypeData, Data: []byte("x"), MediaType: "application/pdf"},
		}},
	}}}}, false)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "Only image file parts are supported" {
		t.Fatalf("error = %v", err)
	}
}

// TS: frequencyPenalty is warned about and never sent; an explicit
// enableThinking / thinkingBudget wins over top-level reasoning on its own.
func TestFrequencyPenaltyAndThinkingResolution(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus")
	fp := 0.5
	high := types.ReasoningHigh
	body, warnings, err := m.buildRequestBodyChecked(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		FrequencyPenalty: &fp,
		Reasoning:        &high,
		ProviderOptions:  map[string]interface{}{"alibaba": map[string]interface{}{"enableThinking": false}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["frequency_penalty"]; ok {
		t.Fatalf("frequency_penalty must not be sent: %#v", body)
	}
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "frequencyPenalty" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["enable_thinking"] != false {
		t.Fatalf("enable_thinking = %#v", body["enable_thinking"])
	}
	if _, ok := body["thinking_budget"]; ok {
		t.Fatalf("reasoning must not add thinking_budget next to an explicit option: %#v", body)
	}

	body, _, _ = m.buildRequestBodyChecked(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       &high,
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"thinkingBudget": 2048}},
	}, false)
	if _, ok := body["enable_thinking"]; ok || body["thinking_budget"] != 2048 {
		t.Fatalf("body = %#v", body)
	}
}

// Every tool result of a tool message becomes its own wire message (parallel
// tool calls), and content outputs are JSON-stringified.
func TestToolMessageWithSeveralResults(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "a", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "one"}},
		types.ToolResultContent{ToolCallID: "b", Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "two"}}}},
	}}}
	for name, out := range map[string][]map[string]interface{}{
		"legacy": ConvertToAlibabaChatMessages(msgs, nil, false),
		"parts": func() []map[string]interface{} {
			r, _ := ConvertToAlibabaChatMessagesWithError(msgs, NewPartCacheControlValidator(), false)
			return r
		}(),
	} {
		if len(out) != 2 || out[0]["tool_call_id"] != "a" || out[0]["content"] != "one" || out[1]["tool_call_id"] != "b" {
			t.Fatalf("%s: out = %#v", name, out)
		}
		if out[1]["content"] != `[{"type":"text","text":"two"}]` {
			t.Fatalf("%s: content = %#v", name, out[1]["content"])
		}
	}
}
