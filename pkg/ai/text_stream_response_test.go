package ai

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

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
