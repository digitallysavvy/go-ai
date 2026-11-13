package streaming

import (
	"encoding/json"
	"testing"
)

// Shared OpenAI-compatible response decodes read keys by their exact names,
// as the TS zod schemas do (review round 95, repo sweep after the guard
// extension).
func TestR95integCompatDecodesKeysMatchExactly(t *testing.T) {
	text, parts, err := compatContentParts([]byte(`[{"type":"text","Text":"no","text":"yes"},{"type":"thinking","thinking":[{"type":"text","Text":"x","text":"r"}]}]`))
	if err != nil || text != "yes" || len(parts) != 2 || parts[1].Text != "r" {
		t.Errorf("text = %q parts = %+v err = %v", text, parts, err)
	}
	text, parts, err = compatContentParts([]byte(`[{"Type":"text","text":"a"}]`))
	if err != nil || text != "" || len(parts) != 0 {
		t.Errorf("Type key: text = %q parts = %+v err = %v", text, parts, err)
	}
	if got := rawStreamErrorText(json.RawMessage(`{"Message":"m"}`)); got != `{"Message":"m"}` {
		t.Errorf("rawStreamErrorText = %q, want the raw object (no exact message key)", got)
	}
	var u openAICompatStreamUsage
	if err := json.Unmarshal([]byte(`{"Prompt_Tokens":5,"completion_tokens":2}`), &u); err != nil || u.PromptTokens != 0 || u.CompletionTokens != 2 {
		t.Errorf("usage = %+v err = %v", u, err)
	}
}
