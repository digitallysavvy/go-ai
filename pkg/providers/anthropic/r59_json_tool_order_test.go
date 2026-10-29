package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r59Response(t *testing.T, raw string) anthropicResponse {
	t.Helper()
	var resp anthropicResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// r59: in jsonTool mode TS doGenerate still pushes a compaction block's text,
// and the json tool's text takes the tool_use block's place in the content
// order (after a server tool call and its result).
func TestR59JSONToolKeepsCompactionAndContentOrder(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "claude-sonnet-4-6", nil)

	t.Run("compaction", func(t *testing.T) {
		resp := r59Response(t, `{"id":"m","model":"claude-sonnet-4-6","type":"message","role":"assistant","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},"content":[
			{"type":"compaction","content":"summary","signature":"sig"},
			{"type":"text","text":"ignored"},
			{"type":"tool_use","id":"t1","name":"json","input":{"a":1}}]}`)
		result := model.convertResponse(resp, true)
		if len(result.Content) != 2 {
			t.Fatalf("content = %#v, want compaction text then json text", result.Content)
		}
		first, ok := result.Content[0].(types.TextContent)
		if !ok || first.Text != "summary" || string(first.ProviderMetadata) != `{"anthropic":{"signature":"sig","type":"compaction"}}` {
			t.Errorf("content[0] = %#v, want compaction text", result.Content[0])
		}
		if second, ok := result.Content[1].(types.TextContent); !ok || second.Text != `{"a":1}` {
			t.Errorf("content[1] = %#v, want json text", result.Content[1])
		}
		if result.Text != `summary{"a":1}` {
			t.Errorf("text = %q", result.Text)
		}
		if result.FinishReason != types.FinishReasonStop {
			t.Errorf("finish = %q, want stop", result.FinishReason)
		}
	})

	t.Run("after server tool", func(t *testing.T) {
		resp := r59Response(t, `{"id":"m","model":"claude-sonnet-4-6","type":"message","role":"assistant","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},"content":[
			{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"q"}},
			{"type":"web_search_tool_result","tool_use_id":"s1","content":[]},
			{"type":"tool_use","id":"t1","name":"json","input":{"a":1}}]}`)
		result := model.convertResponse(resp, true)
		if len(result.Content) != 3 {
			t.Fatalf("content = %#v, want tool call, tool result, json text", result.Content)
		}
		if _, ok := result.Content[0].(types.ToolCallContent); !ok {
			t.Errorf("content[0] = %#v, want tool call", result.Content[0])
		}
		if _, ok := result.Content[1].(types.ToolResultContent); !ok {
			t.Errorf("content[1] = %#v, want tool result", result.Content[1])
		}
		if text, ok := result.Content[2].(types.TextContent); !ok || text.Text != `{"a":1}` {
			t.Errorf("content[2] = %#v, want json text", result.Content[2])
		}
	})

	t.Run("json only stays on text", func(t *testing.T) {
		resp := r59Response(t, `{"id":"m","model":"claude-sonnet-4-6","type":"message","role":"assistant","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},"content":[
			{"type":"tool_use","id":"t1","name":"json","input":{"a":1}}]}`)
		result := model.convertResponse(resp, true)
		if result.Content != nil || result.Text != `{"a":1}` {
			t.Errorf("content = %#v text = %q, want text only", result.Content, result.Text)
		}
	})
}
