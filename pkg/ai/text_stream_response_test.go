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

func TestToTextStream_StandaloneEmitsOnlyTextDeltas(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "text-1"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: ""},
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: " world"},
		{Type: provider.ChunkTypeFinish},
	})

	text, errs := ToTextStream(context.Background(), stream)
	var got []string
	for delta := range text {
		got = append(got, delta)
	}
	if len(got) != 3 || got[0] != "hello" || got[1] != "" || got[2] != " world" {
		t.Fatalf("text deltas = %#v", got)
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestPipeTextStreamToWriter_Standalone(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeToolCall},
		{Type: provider.ChunkTypeText, Text: "b"},
		{Type: provider.ChunkTypeFinish},
	})
	var buf bytes.Buffer
	if err := PipeTextStreamToWriter(context.Background(), stream, &buf); err != nil {
		t.Fatalf("PipeTextStreamToWriter() error = %v", err)
	}
	if got := buf.String(); got != "ab" {
		t.Fatalf("written text = %q", got)
	}
}

func TestCreateTextStreamResponseFromStream_Standalone(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "ok"},
		{Type: provider.ChunkTypeFinish},
	})
	httpRes, err := CreateTextStreamResponseFromStream(context.Background(), stream, &TextStreamResponseInit{
		Status:  http.StatusCreated,
		Headers: map[string]string{"X-Standalone": "text"},
	})
	if err != nil {
		t.Fatalf("CreateTextStreamResponseFromStream() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	body, err := io.ReadAll(httpRes.Body)
	if err != nil {
		t.Fatalf("read body = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestCreateTextStreamResponse_Defaults(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateTextStreamResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("CreateTextStreamResponse() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
	if got := httpRes.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	_, err = io.ReadAll(httpRes.Body)
	if err != nil {
		t.Fatalf("read body = %v", err)
	}
}

func TestCreateTextStreamResponse_WithInit(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateTextStreamResponseWithInit(context.Background(), res, &TextStreamResponseInit{
		Status:     http.StatusAccepted,
		StatusText: "Accepted",
		Headers:    map[string]string{"X-Test": "go"},
	})
	if err != nil {
		t.Fatalf("CreateTextStreamResponseWithInit() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", got, http.StatusAccepted)
	}
	if got := httpRes.Status; got != "Accepted" {
		t.Fatalf("status text = %q", got)
	}
	if got := httpRes.Header.Get("X-Test"); got != "go" {
		t.Fatalf("header X-Test = %q", got)
	}
}

func TestStreamTextResult_ToTextStreamResponse(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "ok"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := res.ToTextStreamResponse(context.Background(), &TextStreamResponseInit{
		Status:  http.StatusCreated,
		Headers: map[string]string{"X-Method": "text"},
	})
	if err != nil {
		t.Fatalf("ToTextStreamResponse() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := httpRes.Header.Get("X-Method"); got != "text" {
		t.Fatalf("header X-Method = %q", got)
	}
}
