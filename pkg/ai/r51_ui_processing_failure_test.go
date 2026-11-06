package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS toUIMessageStream: a tool result for a call the UI message has no part
// for makes processUIMessageStream throw (when an end or step-end callback
// is set). The stream fails with the UIMessageStreamError, the parts after it
// are not delivered, and onEnd reports it as the failed outcome.
func TestR51ToUIMessageStreamProcessingFailureFailsStream(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "unknown", ToolName: "t", Result: "x"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	var end map[string]interface{}
	var onErrorCalls int
	out, errCh := ToUIMessageStream(context.Background(), src, UIMessageStreamResultOptions{
		OnEnd:   func(e map[string]interface{}) { end = e },
		OnError: func(err error) string { onErrorCalls++; return err.Error() },
	})
	var types_ []string
	for c := range out {
		types_ = append(types_, stringValue(c["type"]))
	}
	err := <-errCh
	var target *UIMessageStreamError
	if !errors.As(err, &target) || target.ChunkType != "tool-invocation" || target.ChunkID != "unknown" {
		t.Fatalf("stream err = %T %v", err, err)
	}
	for _, ty := range types_ {
		if ty == "tool-output-available" || ty == "finish" {
			t.Fatalf("delivered %q after the failure: %v", ty, types_)
		}
	}
	if onErrorCalls != 0 {
		t.Errorf("OnError calls = %d", onErrorCalls)
	}
	if outcome, _ := end["outcome"].(map[string]interface{}); outcome["status"] != "failed" || outcome["error"] != err {
		t.Errorf("outcome = %#v", end["outcome"])
	}
}

// TS reports an error chunk's text through onError a second time only while
// it processes the stream for an end or step-end callback; without one,
// onError runs once (to make the errorText).
func TestR51UIStreamErrorChunkOnErrorCalls(t *testing.T) {
	for _, withOnEnd := range []bool{false, true} {
		want := 1
		if withOnEnd {
			want = 2
		}
		calls := 0
		opts := UIMessageStreamOptions{
			Execute: func(UIMessageStreamWriter) { panic(errors.New("boom")) },
			OnError: func(err error) string { calls++; return "masked" },
		}
		if withOnEnd {
			opts.OnEnd = func(map[string]interface{}) {}
		}
		stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), opts)
		for range stream {
		}
		<-errCh
		if calls != want {
			t.Errorf("CreateUIMessageStreamWithOptions withOnEnd=%v: OnError calls = %d, want %d", withOnEnd, calls, want)
		}

		calls = 0
		resultOpts := UIMessageStreamResultOptions{OnError: func(error) string { calls++; return "masked" }}
		if withOnEnd {
			resultOpts.OnEnd = func(map[string]interface{}) {}
		}
		out, errCh2 := ToUIMessageStream(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeError, Err: errors.New("provider failed")},
		}), resultOpts)
		for range out {
		}
		<-errCh2
		if calls != want {
			t.Errorf("ToUIMessageStream withOnEnd=%v: OnError calls = %d, want %d", withOnEnd, calls, want)
		}
	}
}

// An onError that panics while the stream is processed for callbacks fails
// the stream like any processing error (TS: the throw errors the transform),
// instead of crashing the consumer goroutine.
func TestR51UIStreamOnErrorPanicWhileProcessing(t *testing.T) {
	calls := 0
	var end map[string]interface{}
	stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) { w.Write(UIMessageChunk{"type": "error", "errorText": "x"}) },
		OnError: func(error) string {
			calls++
			panic("onError failed")
		},
		OnEnd: func(e map[string]interface{}) { end = e },
	})
	for range stream {
	}
	err := <-errCh
	if err == nil || err.Error() != "onError failed" || calls != 1 {
		t.Fatalf("stream err = %v, OnError calls = %d", err, calls)
	}
	if outcome, _ := end["outcome"].(map[string]interface{}); outcome["status"] != "failed" {
		t.Fatalf("outcome = %#v", end["outcome"])
	}
}
