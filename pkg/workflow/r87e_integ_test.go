package workflow

import (
	"context"
	"testing"
)

// TS WorkflowAgent doStreamStep calls model.doStream with the configured
// maxOutputTokens and no prepareLanguageModelCallOptions, so 0 reaches the
// provider; doGenerateStep runs prepareLanguageModelCallOptions and rejects
// it (cr-r87e-gen-to-carry CR 6).
func TestR87eintegWorkflowStreamPassesZeroMaxOutputTokens(t *testing.T) {
	zero := 0
	m := &wfMockModel{}
	w, err := NewWorkflowAgent(WorkflowAgent{Model: m, MaxTokens: &zero})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Stream err = %v", err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatalf("ReadAll err = %v", err)
	}
	if len(m.opts) != 1 || m.opts[0].MaxTokens == nil || *m.opts[0].MaxTokens != 0 {
		t.Fatalf("model options = %+v", m.opts)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err == nil {
		t.Fatal("Generate accepted maxOutputTokens 0")
	}
}
