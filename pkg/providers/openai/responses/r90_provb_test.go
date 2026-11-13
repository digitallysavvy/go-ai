package responses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r90provbParallelInput = `{"tool_uses":[{"recipient_name":"functions.weather","parameters":{"location":"San Francisco"}},{"recipient_name":"functions.cityAttractions","parameters":{"city":"Rome"}}]}`

// r90provbParallelPrompt ports createExpandedParallelToolCallPrompt from
// convert-to-openai-responses-input.test.ts (TS 22b8158276). With compact
// metadata only the first child carries the wrapper input.
func r90provbParallelPrompt(compact bool) types.Prompt {
	opts := func(index int) map[string]interface{} {
		meta := map[string]interface{}{
			"itemId": "fc_parallel", "toolCallId": "call_parallel", "toolName": "parallel",
			"index": index, "count": 2,
		}
		if !compact || index == 0 {
			meta["input"] = r90provbParallelInput
		}
		return map[string]interface{}{"openai": map[string]interface{}{"parallelToolCall": meta}}
	}
	return types.Prompt{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call_parallel_0", ToolName: "weather", Arguments: map[string]interface{}{"location": "San Francisco"}, ProviderOptions: opts(0)},
			types.ToolCallContent{ToolCallID: "call_parallel_1", ToolName: "cityAttractions", Arguments: map[string]interface{}{"city": "Rome"}, ProviderOptions: opts(1)},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "call_parallel_0", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": 72}}, ProviderOptions: opts(0)},
			types.ToolResultContent{ToolCallID: "call_parallel_1", ToolName: "cityAttractions", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Colosseum"}, ProviderOptions: opts(1)},
		}},
	}}
}

// r90provbRoundTrip serializes the prompt through JSON the way a persisted
// history comes back (TS JSON.parse(JSON.stringify(prompt))): the
// provider options become plain maps.
func r90provbRoundTripOptions(p types.Prompt) types.Prompt {
	for _, msg := range p.Messages {
		for i, part := range msg.Content {
			switch c := part.(type) {
			case types.ToolCallContent:
				c.ProviderOptions = r90provbJSONMap(c.ProviderOptions)
				msg.Content[i] = c
			case types.ToolResultContent:
				c.ProviderOptions = r90provbJSONMap(c.ProviderOptions)
				msg.Content[i] = c
			}
		}
	}
	return p
}

func r90provbJSONMap(m map[string]interface{}) map[string]interface{} {
	b, _ := json.Marshal(m)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func r90provbJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	var g interface{}
	_ = json.Unmarshal(b, &g)
	b, _ = json.Marshal(g)
	return string(b)
}

// TestR90provbCompactParallelMetadataRoundTrips ports "round-trips
// serialized metadata" (TS 22b8158276): a group whose siblings omit the
// wrapper input is regrouped, in either result order.
func TestR90provbCompactParallelMetadataRoundTrips(t *testing.T) {
	for _, tc := range []struct{ hasConversation, reverse bool }{{true, false}, {false, false}, {true, true}, {false, true}} {
		prompt := r90provbRoundTripOptions(r90provbParallelPrompt(true))
		if tc.reverse {
			c := prompt.Messages[1].Content
			c[0], c[1] = c[1], c[0]
		}
		got, warnings, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{
			Store: true, HasConversation: tc.hasConversation, HasPreviousResponseID: !tc.hasConversation,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []interface{}{}
		if !tc.hasConversation {
			want = append(want, map[string]interface{}{"type": "function_call", "call_id": "call_parallel", "name": "parallel", "arguments": r90provbParallelInput})
		}
		want = append(want, map[string]interface{}{"type": "function_call_output", "call_id": "call_parallel", "output": "{\"temperature\":72}\nColosseum"})
		if r90provbJSON(got) != r90provbJSON(want) {
			t.Fatalf("%+v: input =\n%s\nwant\n%s", tc, r90provbJSON(got), r90provbJSON(want))
		}
		if len(warnings) != 0 {
			t.Fatalf("%+v: warnings = %v", tc, warnings)
		}
	}
}

// TestR90provbParallelMissingOrConflictingInput ports "keeps child calls
// separate when wrapper input is missing / conflicting" (TS 22b8158276).
func TestR90provbParallelMissingOrConflictingInput(t *testing.T) {
	for _, kind := range []string{"missing", "conflicting"} {
		prompt := r90provbParallelPrompt(true)
		for _, msg := range prompt.Messages {
			for _, part := range msg.Content {
				var po map[string]interface{}
				switch c := part.(type) {
				case types.ToolCallContent:
					po = c.ProviderOptions
				case types.ToolResultContent:
					po = c.ProviderOptions
				}
				meta := po["openai"].(map[string]interface{})["parallelToolCall"].(map[string]interface{})
				if kind == "missing" {
					delete(meta, "input")
				} else if meta["index"] == 1 {
					meta["input"] = "{}"
				}
			}
		}
		got, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{Store: true, HasPreviousResponseID: true})
		if err != nil {
			t.Fatal(err)
		}
		want := []interface{}{
			map[string]interface{}{"type": "function_call", "call_id": "call_parallel_0", "name": "weather", "arguments": `{"location":"San Francisco"}`},
			map[string]interface{}{"type": "function_call", "call_id": "call_parallel_1", "name": "cityAttractions", "arguments": `{"city":"Rome"}`},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_parallel_0", "output": `{"temperature":72}`},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_parallel_1", "output": "Colosseum"},
		}
		if r90provbJSON(got) != r90provbJSON(want) {
			t.Fatalf("%s: input =\n%s\nwant\n%s", kind, r90provbJSON(got), r90provbJSON(want))
		}
	}
}

// TestR90provbExpandedChildKeepsInputOnce: only the first expanded child
// carries the wrapper input (TS expand-parallel-tool-call.ts 133,
// `...(index === 0 && { input })`).
func TestR90provbExpandedChildKeepsInputOnce(t *testing.T) {
	tools := []types.Tool{
		{Name: "weather", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "cityAttractions", Parameters: map[string]interface{}{"type": "object"}},
	}
	calls, ok := ExpandParallelToolCall("call_parallel", "parallel", r90provbParallelInput, "fc_parallel", tools, "openai")
	if !ok || len(calls) != 2 {
		t.Fatalf("expand = %v, %v", calls, ok)
	}
	want := []string{
		`{"openai":{"parallelToolCall":{"count":2,"index":0,"input":` + r90provbJSON(r90provbParallelInput) + `,"itemId":"fc_parallel","toolCallId":"call_parallel","toolName":"parallel"}}}`,
		`{"openai":{"parallelToolCall":{"count":2,"index":1,"itemId":"fc_parallel","toolCallId":"call_parallel","toolName":"parallel"}}}`,
	}
	for i, c := range calls {
		if got := r90provbJSON(c.ProviderMetadata); got != want[i] {
			t.Fatalf("child %d metadata = %s\nwant %s", i, got, want[i])
		}
	}
	// An explicit non-string input is still rejected; absent is accepted.
	if _, ok := GetParallelToolCallMetadata(map[string]interface{}{"openai": map[string]interface{}{"parallelToolCall": map[string]interface{}{
		"itemId": "i", "toolCallId": "c", "toolName": "parallel", "input": nil, "index": 0, "count": 1}}}, "openai"); ok {
		t.Fatal("null input must be rejected")
	}
}
