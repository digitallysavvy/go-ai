package codex

import (
	"context"
	"testing"
)

// TS `pickOpenAI` uses `processEnv.OPENAI_API_KEY ?? processEnv.CODEX_API_KEY`:
// a present-but-empty OPENAI_API_KEY wins over CODEX_API_KEY.
func TestPickOpenAINullishCoalescing(t *testing.T) {
	got, _, err := pickOpenAI(context.Background(), map[string]string{"OPENAI_API_KEY": "", "CODEX_API_KEY": "codex"}, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["CODEX_API_KEY"]; ok {
		t.Errorf("empty OPENAI_API_KEY must not fall back to CODEX_API_KEY: %v", got)
	}
	got, _, err = pickOpenAI(context.Background(), map[string]string{"CODEX_API_KEY": "codex"}, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got["CODEX_API_KEY"] != "codex" {
		t.Errorf("absent OPENAI_API_KEY must fall back to CODEX_API_KEY: %v", got)
	}
}
