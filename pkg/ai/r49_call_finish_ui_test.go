package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A harness turn suspended mid-step discards the open step and ends with a
// bare call-level finish (TS HarnessStreamTextResult.finish(undefined)). TS
// toUIMessageStream maps that 'finish' part to the UI "finish" part only: no
// "finish-step" and no text-end for the block the next slice continues.
func TestR49ToUIMessageStream_CallLevelFinishAfterDiscardedStep(t *testing.T) {
	t.Parallel()
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "partial"},
		{Type: provider.ChunkTypeDiscardStep},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonOther},
	})
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{Provider: "harness", ModelID: "m", TerminalFinishClosesTurn: true})
	uiChunks, errs := result.ToUIMessageStream(context.Background())
	var got []string
	var finishReason interface{}
	for c := range uiChunks {
		ty, _ := c["type"].(string)
		got = append(got, ty)
		if ty == "finish" {
			finishReason = c["finishReason"]
		}
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("ToUIMessageStream error = %v", err)
	}
	want := "start,start-step,text-start,text-delta,finish"
	if strings.Join(got, ",") != want {
		t.Fatalf("UI chunk types = %v, want %s", got, want)
	}
	if finishReason != "other" {
		t.Fatalf("finishReason = %v, want other", finishReason)
	}
}
