package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A harness turn whose open step was discarded (a suspended turn) ends with a
// bare finish. TS HarnessStreamTextResult.finish never calls startStep, so
// the full stream has the discarded step's start-step only, not a second one.
func TestR50HarnessFinishAfterDiscardOpensNoStep(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "partial"},
		{Type: provider.ChunkTypeDiscardStep},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonOther},
	})
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{TerminalFinishClosesTurn: true})
	var got []provider.ChunkType
	stream := result.FullStream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		got = append(got, c.Type)
	}
	want := []provider.ChunkType{provider.ChunkTypeStart, provider.ChunkTypeStartStep, provider.ChunkTypeText, provider.ChunkTypeFinish}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("full stream = %v, want %v", got, want)
	}

	// A harness turn with no step at all ends with the finish part only.
	src = testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonOther},
	})
	result = NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{TerminalFinishClosesTurn: true})
	got = nil
	stream = result.FullStream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		got = append(got, c.Type)
	}
	if want := []provider.ChunkType{provider.ChunkTypeStart, provider.ChunkTypeFinish}; !reflect.DeepEqual(got, want) {
		t.Fatalf("empty turn full stream = %v, want %v", got, want)
	}
}

// A general external stream whose only step ends directly in a finish records
// that step, so its UI stream closes it with a finish-step (and OnStepEnd)
// before the call-level finish.
func TestR50ExternalLoneFinishStepHasUIFinishStep(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})
	var mu sync.Mutex
	stepEnds := 0
	ui, errs := result.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{OnStepEnd: func(map[string]interface{}) {
		mu.Lock()
		stepEnds++
		mu.Unlock()
	}})
	var got []string
	for c := range ui {
		got = append(got, c["type"].(string))
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"start", "start-step", "finish-step", "finish"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ui = %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if stepEnds != 1 || len(result.Steps()) != 1 {
		t.Fatalf("OnStepEnd calls = %d, steps = %d, want 1 and 1", stepEnds, len(result.Steps()))
	}
}
