package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS handleUIMessageStreamFinish: an error thrown by onStepEnd goes to the
// stream's onError and the stream carries on; an error thrown by onEnd fails
// the stream.
func TestCreateUIMessageStream_CallbackErrors(t *testing.T) {
	t.Run("onStepEnd error is reported to onError", func(t *testing.T) {
		var reported []error
		stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				w.Write(UIMessageChunk{"type": "finish-step"})
				w.Write(UIMessageChunk{"type": "finish"})
			},
			OnError:   func(err error) string { reported = append(reported, err); return "x" },
			OnStepEnd: func(map[string]interface{}) { panic(errors.New("step boom")) },
		})
		var n int
		for range stream {
			n++
		}
		if err := <-errCh; err != nil {
			t.Fatalf("stream failed: %v", err)
		}
		if n != 2 {
			t.Fatalf("chunks = %d, want 2", n)
		}
		if len(reported) != 1 || reported[0].Error() != "step boom" {
			t.Fatalf("onError got %v", reported)
		}
	})
	t.Run("onEnd error fails the stream", func(t *testing.T) {
		stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) { w.Write(UIMessageChunk{"type": "finish"}) },
			OnEnd:   func(map[string]interface{}) { panic(errors.New("end boom")) },
		})
		for range stream {
		}
		if err := <-errCh; err == nil || err.Error() != "end boom" {
			t.Fatalf("stream error = %v, want end boom", err)
		}
	})
}

func TestToUIMessageStream_CallbackErrors(t *testing.T) {
	chunks := func() provider.TextStream {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
	}
	t.Run("onStepEnd", func(t *testing.T) {
		var reported []error
		out, errCh := ToUIMessageStream(context.Background(), chunks(), UIMessageStreamResultOptions{
			OnError:   func(err error) string { reported = append(reported, err); return "x" },
			OnStepEnd: func(map[string]interface{}) { panic("step boom") },
		})
		for range out {
		}
		if err := <-errCh; err != nil {
			t.Fatalf("stream failed: %v", err)
		}
		if len(reported) != 1 || reported[0].Error() != "step boom" {
			t.Fatalf("onError got %v", reported)
		}
	})
	t.Run("onEnd", func(t *testing.T) {
		out, errCh := ToUIMessageStream(context.Background(), chunks(), UIMessageStreamResultOptions{
			OnEnd: func(map[string]interface{}) { panic("end boom") },
		})
		for range out {
		}
		if err := <-errCh; err == nil || err.Error() != "end boom" {
			t.Fatalf("stream error = %v, want end boom", err)
		}
	})
}
