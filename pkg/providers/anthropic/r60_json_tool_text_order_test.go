package anthropic

import "testing"

// r60: TS step text is the content's text parts joined in order, so in
// jsonTool mode a json tool text that precedes a compaction block stays ahead
// of the compaction text.
func TestR60JSONToolTextFollowsContentOrder(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "claude-sonnet-4-6", nil)
	resp := r59Response(t, `{"id":"m","model":"claude-sonnet-4-6","type":"message","role":"assistant","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},"content":[
		{"type":"tool_use","id":"t1","name":"json","input":{"a":1}},
		{"type":"compaction","content":"summary","signature":"sig"}]}`)
	result := model.convertResponse(resp, true)
	if result.Text != `{"a":1}summary` {
		t.Errorf("text = %q, want the json text then the compaction text", result.Text)
	}
}
