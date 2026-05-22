package ai

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

func TestCreateUIMessageStreamResponse_WithInit(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, &UIMessageStreamResponseInit{
		Status:     http.StatusCreated,
		StatusText: "Created",
		Headers:    map[string]string{"X-Test": "go"},
	})
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponseWithInit() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := httpRes.Status; got != "Created" {
		t.Fatalf("status text = %q, want %q", got, "Created")
	}
	if got := httpRes.Header.Get("X-Test"); got != "go" {
		t.Fatalf("header X-Test = %q, want go", got)
	}
}

func TestStreamTextResult_ToUIMessageStreamResponse(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := res.ToUIMessageStreamResponse(context.Background(), &UIMessageStreamResponseInit{
		Headers: map[string]string{"X-Method": "stream"},
	})
	if err != nil {
		t.Fatalf("ToUIMessageStreamResponse() error = %v", err)
	}
	if got := httpRes.Header.Get("X-Method"); got != "stream" {
		t.Fatalf("header X-Method = %q, want stream", got)
	}
}

func TestStreamTextResult_ToUIMessageStream(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	chunks, errs := res.ToUIMessageStream(context.Background())

	var got []UIMessageChunk
	for ch := range chunks {
		got = append(got, ch)
	}
	if len(got) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(got))
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateUIMessageStreamWithOptions_WriteAndCallbacks(t *testing.T) {
	ch1 := make(chan UIMessageChunk)
	go func() {
		ch1 <- UIMessageChunk{"type": "merged"}
		close(ch1)
	}()

	stepDone := make(chan struct{}, 1)
	finishDone := make(chan struct{}, 1)

	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{"type": "start"})
			writer.Merge(ch1)
			writer.Write(UIMessageChunk{"type": "end"})
		},
		OnError: func(err error) string {
			return err.Error()
		},
		OnStepFinish: func(_ map[string]interface{}) {
			stepDone <- struct{}{}
		},
		OnFinish: func(_ map[string]interface{}) {
			finishDone <- struct{}{}
		},
	})

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(got))
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
	<-stepDone
	<-finishDone
}

func TestPipeUIMessageStreamToResponse_WithConsumeSSEStream(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}

	buf := &bytes.Buffer{}
	var consumeBuf bytes.Buffer
	httpRes, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			_, err := io.Copy(&consumeBuf, r)
			return err
		},
		StatusText: "OK",
	})
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponseWithInit() error = %v", err)
	}
	if httpRes.Status != "OK" {
		t.Fatalf("status text = %q", httpRes.Status)
	}
	_, err = io.Copy(buf, httpRes.Body)
	if err != nil {
		t.Fatalf("copy body = %v", err)
	}
	if got := buf.Len(); got == 0 {
		t.Fatalf("response body is empty")
	}
	if got := consumeBuf.Len(); got == 0 {
		t.Fatalf("consume stream body is empty")
	}
}
