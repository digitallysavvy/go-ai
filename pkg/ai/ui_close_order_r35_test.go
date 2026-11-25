package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Blocks left open when a step finishes are closed in the order they were
// opened, not in random map order.
func TestToUIMessageStream_ClosesOpenBlocksInOpenOrder(t *testing.T) {
	for i := 0; i < 30; i++ {
		stream := testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "t-c"},
			{Type: provider.ChunkTypeTextStart, ID: "t-a"},
			{Type: provider.ChunkTypeTextStart, ID: "t-b"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		out, errCh := ToUIMessageStream(context.Background(), stream)
		var ended []string
		for c := range out {
			if c["type"] == "text-end" {
				ended = append(ended, c["id"].(string))
			}
		}
		<-errCh
		if want := []string{"t-c", "t-a", "t-b"}; !reflect.DeepEqual(ended, want) {
			t.Fatalf("iteration %d: ended %v, want %v", i, ended, want)
		}
	}
}
