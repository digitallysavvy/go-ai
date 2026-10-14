package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func oct14OpenAIOpts(id string) map[string]interface{} {
	return map[string]interface{}{"openai": map[string]interface{}{"itemId": id}}
}

func oct14Convert(t *testing.T, msgs []types.Message, opts ConvertOptions) []interface{} {
	t.Helper()
	input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: msgs}, "system", opts)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	// JSON round-trip so typed items and maps compare uniformly.
	raw, _ := json.Marshal(input)
	var out []interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func asst(parts ...types.ContentPart) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: parts}
}

// TS #22214: text parts sharing one stored message id emit a single
// item_reference.
func TestConvert_DedupesStoredTextItemReferences(t *testing.T) {
	t.Parallel()
	out := oct14Convert(t, []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		asst(
			types.TextContent{Text: "First. ", ProviderOptions: oct14OpenAIOpts("msg_123")},
			types.TextContent{Text: "Second.", ProviderOptions: oct14OpenAIOpts("msg_123")},
		),
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Continue"}}},
	}, ConvertOptions{Store: true})
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(out), out)
	}
	ref, _ := out[1].(map[string]interface{})
	if ref["type"] != "item_reference" || ref["id"] != "msg_123" {
		t.Errorf("out[1] = %#v", out[1])
	}
}

// TS #23123: stored text of a reasoning model is only referenced when
// preceded by its reasoning part.
func TestConvert_ReasoningModelStoredTextRequiresPrecedingReasoning(t *testing.T) {
	t.Parallel()
	opts := ConvertOptions{Store: true, IsReasoningModel: true}

	out := oct14Convert(t, []types.Message{asst(
		types.ReasoningContent{Text: "Thinking...", ProviderOptions: oct14OpenAIOpts("rs_123")},
		types.TextContent{Text: "Hello", ProviderOptions: oct14OpenAIOpts("msg_123")},
	)}, opts)
	want := []interface{}{
		map[string]interface{}{"type": "item_reference", "id": "rs_123"},
		map[string]interface{}{"type": "item_reference", "id": "msg_123"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("with reasoning: %#v, want %#v", out, want)
	}

	out = oct14Convert(t, []types.Message{asst(
		types.TextContent{Text: "Hello", ProviderOptions: oct14OpenAIOpts("msg_123")},
	)}, opts)
	want = []interface{}{map[string]interface{}{"role": "assistant", "content": "Hello"}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("without reasoning: %#v, want %#v", out, want)
	}

	opts.HasConversation = true
	out = oct14Convert(t, []types.Message{asst(
		types.TextContent{Text: "Hello", ProviderOptions: oct14OpenAIOpts("msg_123")},
	)}, opts)
	if len(out) != 0 {
		t.Errorf("conversation: %#v, want empty", out)
	}

	// Non-reasoning models keep referencing without reasoning.
	out = oct14Convert(t, []types.Message{asst(
		types.TextContent{Text: "Hello", ProviderOptions: oct14OpenAIOpts("msg_123")},
	)}, ConvertOptions{Store: true})
	if len(out) != 1 || out[0].(map[string]interface{})["type"] != "item_reference" {
		t.Errorf("non-reasoning: %#v", out)
	}
}

func webSearchResult(id, name string, value map[string]interface{}) types.ToolResultContent {
	return types.ToolResultContent{
		ToolCallID: id, ToolName: name,
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: value},
	}
}

// TS #22346: stateless replay reconstructs web_search_call items.
func TestConvert_ReconstructsWebSearchCallsWhenNotStored(t *testing.T) {
	t.Parallel()
	out := oct14Convert(t, []types.Message{asst(
		webSearchResult("ws_preview", "web_search_preview", map[string]interface{}{
			"action":  map[string]interface{}{"type": "search", "query": "AI SDK"},
			"sources": []interface{}{map[string]interface{}{"type": "url", "url": "https://ai-sdk.dev"}},
		}),
		webSearchResult("ws_open_page", "web_search", map[string]interface{}{
			"action": map[string]interface{}{"type": "openPage", "url": "https://ai-sdk.dev/docs"},
		}),
		webSearchResult("ws_find", "web_search", map[string]interface{}{
			"action": map[string]interface{}{"type": "findInPage", "url": "https://ai-sdk.dev/docs", "pattern": "streamText"},
		}),
	)}, ConvertOptions{})
	want := []interface{}{
		map[string]interface{}{"type": "web_search_call", "id": "ws_preview", "status": "completed", "action": map[string]interface{}{
			"type": "search", "query": "AI SDK", "sources": []interface{}{map[string]interface{}{"type": "url", "url": "https://ai-sdk.dev"}},
		}},
		map[string]interface{}{"type": "web_search_call", "id": "ws_open_page", "status": "completed", "action": map[string]interface{}{
			"type": "open_page", "url": "https://ai-sdk.dev/docs",
		}},
		map[string]interface{}{"type": "web_search_call", "id": "ws_find", "status": "completed", "action": map[string]interface{}{
			"type": "find_in_page", "url": "https://ai-sdk.dev/docs", "pattern": "streamText",
		}},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %#v\nwant %#v", out, want)
	}
}

func TestConvert_WebSearchWithoutActionIsOmitted(t *testing.T) {
	t.Parallel()
	out := oct14Convert(t, []types.Message{asst(
		webSearchResult("ws_missing", "web_search", map[string]interface{}{}),
	)}, ConvertOptions{})
	if len(out) != 0 {
		t.Errorf("got %#v, want empty", out)
	}
}

func TestConvert_WebSearchStoredUsesItemReference(t *testing.T) {
	t.Parallel()
	out := oct14Convert(t, []types.Message{asst(
		webSearchResult("ws_1", "custom_search", map[string]interface{}{
			"action": map[string]interface{}{"type": "search", "query": "q"},
		}),
	)}, ConvertOptions{Store: true, WebSearchToolName: "custom_search"})
	want := []interface{}{map[string]interface{}{"type": "item_reference", "id": "ws_1"}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %#v want %#v", out, want)
	}
	// Custom-named tool, not stored: still reconstructed.
	out = oct14Convert(t, []types.Message{asst(
		webSearchResult("ws_1", "custom_search", map[string]interface{}{
			"action": map[string]interface{}{"type": "search", "query": "q"},
		}),
	)}, ConvertOptions{WebSearchToolName: "custom_search"})
	if len(out) != 1 || out[0].(map[string]interface{})["type"] != "web_search_call" {
		t.Errorf("got %#v", out)
	}
}
