package workflow

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type sliceWriter struct{ chunks []ai.UIMessageChunk }

func (w *sliceWriter) Write(c ai.UIMessageChunk) error { w.chunks = append(w.chunks, c); return nil }
func (w *sliceWriter) Close() error                    { return nil }

// Open parts are closed in the order they were opened (a JS Set), not in Go's
// random map order.
func TestCloseOpenExecutionPartsKeepsOpenOrder(t *testing.T) {
	ids := []string{"t-9", "t-1", "t-5", "t-3", "t-7", "t-2"}
	sc := newMutableStreamContext(nil)
	ps := newExecutionPartState()
	for _, id := range ids {
		recordWorkflowChunk(ai.UIMessageChunk{"type": "text-start", "id": id}, sc, ps)
	}
	for i := 0; i < 20; i++ { // map iteration order would vary between runs
		w := &sliceWriter{}
		psCopy := newExecutionPartState()
		for _, id := range ids {
			psCopy.openedTextParts.add(id)
		}
		if err := closeOpenExecutionParts(w, sc, psCopy); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range w.chunks {
			got = append(got, c["id"].(string))
		}
		for j := range ids {
			if got[j] != ids[j] {
				t.Fatalf("close order = %v, want %v", got, ids)
			}
		}
	}
}

func TestFinishReasonStringDefaultsToStop(t *testing.T) {
	if finishReasonString("") != "stop" || finishReasonString(types.FinishReasonToolCalls) != "tool-calls" {
		t.Fatal("finishReasonString")
	}
}
