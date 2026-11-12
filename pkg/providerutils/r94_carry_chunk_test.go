package providerutils

import "testing"

// Stream chunks decode with exact keys too (zod chunk schemas).
func TestR94carryUnmarshalChunkMatchesKeysExactly(t *testing.T) {
	var chunk struct {
		ID    string `json:"id"`
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := UnmarshalChunk([]byte(`{"Id":"x","usage":{"Prompt_Tokens":5,"prompt_tokens":2.0}}`), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.ID != "" || chunk.Usage == nil || chunk.Usage.PromptTokens != 2 {
		t.Errorf("chunk = %+v", chunk)
	}
}
