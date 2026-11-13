package workflow

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// The UI message chunk's `type` is read by its exact name, as TS's chunk
// schema does (review round 95 repo sweep).
func TestR95integFinishEventTypeKeyIsExact(t *testing.T) {
	if containsFinishEvent([]*streaming.SSEEvent{{Data: `{"Type":"finish"}`}}) {
		t.Fatal("a Type key counted as the finish chunk")
	}
	if !containsFinishEvent([]*streaming.SSEEvent{{Data: `{"type":"finish"}`}}) {
		t.Fatal("finish chunk not recognized")
	}
}
