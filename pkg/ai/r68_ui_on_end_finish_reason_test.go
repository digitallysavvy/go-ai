package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r68StepThenFail yields a text delta and a finish-step, then fails before
// the call-level finish part.
type r68StepThenFail struct{ n int }

func (s *r68StepThenFail) Next() (*provider.StreamChunk, error) {
	s.n++
	switch s.n {
	case 1:
		return &provider.StreamChunk{Type: provider.ChunkTypeStartStep}, nil
	case 2:
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"}, nil
	case 3:
		return &provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonToolCalls}, nil
	}
	return nil, errors.New("boom")
}
func (s *r68StepThenFail) Err() error   { return nil }
func (s *r68StepThenFail) Close() error { return nil }

// TS handleUIMessageStreamFinish reports onEnd's finishReason from the
// message state (state.finishReason), which only a `finish` UI chunk sets:
// none when sendFinish is false, or when the stream fails before its finish.
func TestR68ToUIMessageStreamOnEndFinishReasonFromMessageState(t *testing.T) {
	run := func(t *testing.T, stream provider.TextStream, sendFinish bool) interface{} {
		t.Helper()
		var got []map[string]interface{}
		chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
			SendFinish: &sendFinish,
			OnEnd:      func(e map[string]interface{}) { got = append(got, e) },
		})
		for range chunks { //nolint:revive // drain
		}
		for range errs { //nolint:revive // drain
		}
		if len(got) != 1 {
			t.Fatalf("%d OnEnd calls, want 1", len(got))
		}
		return got[0]["finishReason"]
	}
	finished := func() provider.TextStream {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "a"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
	}
	if got := run(t, finished(), true); got != types.FinishReasonStop {
		t.Fatalf("SendFinish true: finishReason = %#v, want stop", got)
	}
	if got := run(t, finished(), false); got != types.FinishReason("") {
		t.Fatalf("SendFinish false: finishReason = %#v, want none", got)
	}
	if got := run(t, &r68StepThenFail{}, true); got != types.FinishReason("") {
		t.Fatalf("failure before finish: finishReason = %#v, want none (not the step's reason)", got)
	}
}
