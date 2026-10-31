package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR69WorkflowStreamOutputUnsetWithoutSpec: core StreamText's Output() is
// the text with no Output option (TS `output ?? text()`), but TS
// WorkflowAgent's stream mode parses only an effective output specification,
// so WorkflowStreamResult.Output() stays nil without one. With a spec the
// text is parsed.
func TestR69WorkflowStreamOutputUnsetWithoutSpec(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &fnModel{chunks: chunks}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := st.Output(); got != nil {
		t.Errorf("no spec: Output() = %#v, want nil", got)
	}
	if got := st.StreamTextResult.Output(); got != "hello" {
		t.Errorf("core StreamText Output() = %#v, want the text", got)
	}

	st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", Output: ai.TextOutput()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := st.Output(); got != "hello" {
		t.Errorf("text spec: Output() = %#v, want %q", got, "hello")
	}
}
