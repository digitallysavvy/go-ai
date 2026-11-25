package anthropic

import "testing"

// prova CR (carry CR §2, round 96): an mcp_tool_result text item's
// citations are z.array(z.json()): a non-finite number (JSON.parse of
// 1e400) fails the frame / response, a finite value passes.
func TestR96integMCPResultCitationsJSONValue(t *testing.T) {
	frame := func(citation string) string {
		return `{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_result","tool_use_id":"t","is_error":false,` +
			`"content":[{"type":"text","text":"a","citations":[` + citation + `]}]}}`
	}
	if msg := anthropicChunkValidationMessage(frame(`{"n":1,"s":"x"}`)); msg != "" {
		t.Fatalf("finite citation: %s", msg)
	}
	if msg := anthropicChunkValidationMessage(frame(`{"n":1e400}`)); msg == "" {
		t.Fatal("1e400 citation: frame passed, want the z.json() failure")
	}
	response := func(citation string) []byte {
		return []byte(`{"type":"message","content":[{"type":"mcp_tool_result","tool_use_id":"t","is_error":false,` +
			`"content":[{"type":"text","text":"a","citations":[` + citation + `]}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}
	validate := anthropicResponseValidator("anthropic.messages")
	if err := validate(200, nil, response(`[1,2]`)); err != nil {
		t.Fatalf("finite response citation: %v", err)
	}
	if err := validate(200, nil, response(`[-1e400]`)); err == nil {
		t.Fatal("1e400 response citation: err = nil")
	}
}
