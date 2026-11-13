package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS HarnessStreamTextResult.appendToCurrentStepContent: a text-delta or
// reasoning-delta whose id has no part yet opens one, an empty delta
// included, so the step content holds that (empty) part and a terminal
// finish without finish-step fails the turn.
func TestR99streamExternalEmptyDeltaOpensPart(t *testing.T) {
	for _, kind := range []provider.ChunkType{provider.ChunkTypeText, provider.ChunkTypeReasoning} {
		t.Run(string(kind), func(t *testing.T) {
			r := NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: kind, ID: "b1"},
				{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: r71streamUsage},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage},
			}), ExternalStreamOptions{TerminalFinishClosesTurn: true})
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
			steps := r.Steps()
			if len(steps) != 1 || len(steps[0].Content) != 1 {
				t.Fatalf("steps = %+v", steps)
			}
			switch p := steps[0].Content[0].(type) {
			case types.TextContent:
				if kind != provider.ChunkTypeText || p.Text != "" {
					t.Fatalf("part = %#v", p)
				}
			case types.ReasoningContent:
				if kind != provider.ChunkTypeReasoning || p.Text != "" {
					t.Fatalf("part = %#v", p)
				}
			default:
				t.Fatalf("part = %#v", p)
			}

			r = NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: kind, ID: "b1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage},
			}), ExternalStreamOptions{TerminalFinishClosesTurn: true})
			if err := r.Err(); err == nil || err.Error() != errExternalUnclosedFinish {
				t.Fatalf("err = %v, want the unclosed step content error", err)
			}
		})
	}
}

// TS toUIMessageChunk spreads toolMetadata when it is not null, so a tool
// whose metadata is an empty object gives `toolMetadata: {}` on its UI tool
// chunks.
func TestR99streamUIEmptyToolMetadata(t *testing.T) {
	tool := types.Tool{Name: "weather", Metadata: map[string]interface{}{}, Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: "c1", Text: "{}"},
			{Type: provider.ChunkTypeToolInputEnd, ID: "c1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r71streamUsage},
		}, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		}),
		Prompt: "x",
		Tools:  []types.Tool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := ToUIMessageStream(context.Background(), r.NewStream(), UIMessageStreamResultOptions{Tools: []types.Tool{tool}})
	seen := map[string]bool{}
	for c := range chunks {
		switch c["type"] {
		case "tool-input-start", "tool-input-available", "tool-output-available":
			meta, ok := c["toolMetadata"].(map[string]interface{})
			if !ok || len(meta) != 0 {
				t.Errorf("%v toolMetadata = %#v, want {}", c["type"], c["toolMetadata"])
			}
			b, err := marshalUIChunkInTSOrder(c)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"toolMetadata":{}`) {
				t.Errorf("%v wire = %s", c["type"], b)
			}
			seen[c["type"].(string)] = true
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %v", seen)
	}
}

// TS handleUIMessageStreamFinish callOnStepFinish reports an onStepEnd error
// through onError inside its own transform; an onError that throws there
// rejects that transform, so the stream errors before the finish-step chunk:
// nothing more is written and onEnd is not called (no flush, no cancel).
func TestR99streamUIStepEndThrowingOnErrorStopsStream(t *testing.T) {
	onErrorFailed := errors.New("onError failed")
	check := func(t *testing.T, out <-chan UIMessageChunk, errCh <-chan error, ended func() bool) {
		t.Helper()
		var written []string
		for c := range out {
			written = append(written, c["type"].(string))
		}
		for _, typ := range written {
			if typ == "finish-step" || typ == "finish" {
				t.Fatalf("chunks = %v: nothing at or after the failed finish-step is written", written)
			}
		}
		if err := <-errCh; !errors.Is(err, onErrorFailed) {
			t.Fatalf("err = %v, want %v", err, onErrorFailed)
		}
		if ended() {
			t.Fatal("onEnd was called")
		}
	}
	newEnd := func() (UIMessageStreamOnEndCallback, func() bool) {
		var mu sync.Mutex
		called := false
		return func(map[string]interface{}) {
				mu.Lock()
				called = true
				mu.Unlock()
			}, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return called
			}
	}
	t.Run("toUIMessageStream", func(t *testing.T) {
		onEnd, ended := newEnd()
		out, errCh := ToUIMessageStream(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeStart},
			{Type: provider.ChunkTypeStartStep},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), UIMessageStreamResultOptions{
			OnError:   func(error) string { panic(onErrorFailed) },
			OnStepEnd: func(map[string]interface{}) { panic("step boom") },
			OnEnd:     onEnd,
		})
		check(t, out, errCh, ended)
	})
	t.Run("createUIMessageStream", func(t *testing.T) {
		onEnd, ended := newEnd()
		out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				w.Write(UIMessageChunk{"type": "start"})
				w.Write(UIMessageChunk{"type": "start-step"})
				w.Write(UIMessageChunk{"type": "finish-step"})
				w.Write(UIMessageChunk{"type": "finish"})
			},
			OnError:   func(error) string { panic(onErrorFailed) },
			OnStepEnd: func(map[string]interface{}) { panic("step boom") },
			OnEnd:     onEnd,
		})
		check(t, out, errCh, ended)
	})
}

// TS createUIMessageStream handleError: an onError that throws errors the
// source stream (safeError), which ends delivery: a chunk a still-open
// merged stream sends afterwards is not written.
func TestR99streamCreateUIThrowingOnErrorEndsDelivery(t *testing.T) {
	onErrorFailed := errors.New("onError failed")
	merged := make(chan UIMessageChunk)
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Merge(merged)
			panic("execute failed")
		},
		OnError: func(error) string { panic(onErrorFailed) },
	})
	var got []UIMessageChunk
	done := make(chan struct{})
	go func() {
		defer close(done)
		for c := range out {
			got = append(got, c)
		}
	}()
	if err := <-errCh; !errors.Is(err, onErrorFailed) {
		t.Fatalf("err = %v, want %v", err, onErrorFailed)
	}
	merged <- UIMessageChunk{"type": "data-late", "data": 1}
	close(merged)
	<-done
	if len(got) != 0 {
		t.Fatalf("chunks after the failed onError = %v", got)
	}
}
