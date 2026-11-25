package ai

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS processUIMessageStream throws before it enqueues the failing chunk, and
// the thrown error errors the transform: neither that chunk nor any later one
// reaches the reader of a CreateUIMessageStreamWithOptions stream.
func TestR51IntegratorCreateUIStreamNothingAfterProcessingFailure(t *testing.T) {
	stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "text-delta", "id": "missing", "delta": "x"})
			w.Write(UIMessageChunk{"type": "text-start", "id": "t2"})
			w.Write(UIMessageChunk{"type": "text-delta", "id": "t2", "delta": "y"})
		},
		OnEnd: func(map[string]interface{}) {},
	})
	var got []string
	for c := range stream {
		got = append(got, stringValue(c["type"]))
	}
	var target *UIMessageStreamError
	if err := <-errCh; !errors.As(err, &target) {
		t.Fatalf("stream err = %v, want UIMessageStreamError", err)
	}
	for _, ty := range got {
		if ty == "text-delta" || ty == "text-start" {
			t.Fatalf("delivered %q at or after the failure: %v", ty, got)
		}
	}
}

// ToUIMessageStream: the message-metadata chunk TS would write for the part
// whose chunk failed processing is not delivered either.
func TestR51IntegratorToUIStreamNoMetadataAfterProcessingFailure(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "unknown", ToolName: "t", Result: "x"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	out, errCh := ToUIMessageStream(context.Background(), src, UIMessageStreamResultOptions{
		OnEnd: func(map[string]interface{}) {},
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			return map[string]interface{}{"for": part["type"]}
		},
	})
	var forTypes []interface{}
	for c := range out {
		if c["type"] == "message-metadata" {
			md, _ := c["messageMetadata"].(map[string]interface{})
			forTypes = append(forTypes, md["for"])
		}
	}
	<-errCh
	for _, f := range forTypes {
		if f != "text-delta" && f != "start-step" {
			t.Fatalf("message-metadata after the failure: %v", forTypes)
		}
	}
}

// blockingAfterStream yields its chunks, then blocks in Next until Close.
type blockingAfterStream struct {
	chunks []provider.StreamChunk
	i      int
	once   sync.Once
	closed chan struct{}
}

func (s *blockingAfterStream) Next() (*provider.StreamChunk, error) {
	if s.i < len(s.chunks) {
		c := s.chunks[s.i]
		s.i++
		return &c, nil
	}
	<-s.closed
	return nil, io.EOF
}
func (s *blockingAfterStream) Err() error { return nil }
func (s *blockingAfterStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// After a processing failure ToUIMessageStream stops reading its source (TS:
// the errored transform cancels the pipe), so a source that would block on
// its next read does not hold the stream open.
func TestR51IntegratorToUIStreamStopsReadingAfterProcessingFailure(t *testing.T) {
	src := &blockingAfterStream{closed: make(chan struct{}), chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "unknown", ToolName: "t", Result: "x"}},
	}}
	defer func() { _ = src.Close() }()
	out, errCh := ToUIMessageStream(context.Background(), src, UIMessageStreamResultOptions{OnEnd: func(map[string]interface{}) {}})
	done := make(chan error, 1)
	go func() {
		for range out {
		}
		done <- <-errCh
	}()
	select {
	case err := <-done:
		var target *UIMessageStreamError
		if !errors.As(err, &target) {
			t.Fatalf("stream err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream still reading its source after the processing failure")
	}
}
