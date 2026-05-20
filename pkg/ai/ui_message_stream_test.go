package ai

import (
	"bytes"
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestReadUIMessageStream_RoundTrip(t *testing.T) {
	input := []byte("data: {\"type\":\"text\",\"value\":\"hi\"}\n\ndata: {\"type\":\"finish\"}\n\n")
	chunks, err := ReadUIMessageStream(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("ReadUIMessageStream() error = %v", err)
	}
	if len(chunks) != 2 || chunks[0]["type"] != "text" || chunks[1]["type"] != "finish" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestCreateUIMessageStream_FromStreamTextResult(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	chunks, errs := CreateUIMessageStream(context.Background(), res)

	var got []UIMessageChunk
	for ch := range chunks {
		got = append(got, ch)
	}
	if len(got) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(got))
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
}

func TestCreateUIMessageStreamResponse_Headers(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateUIMessageStreamResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponse() error = %v", err)
	}
	if got := httpRes.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q", got)
	}
}
