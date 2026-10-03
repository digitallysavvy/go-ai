package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A custom stream (writer + data parts) can now be served over HTTP, as with
// TS pipeUIMessageStreamToResponse({ stream }) / createUIMessageStreamResponse({ stream }).
func customChunkStream(ctx context.Context) <-chan UIMessageChunk {
	chunks, _ := CreateUIMessageStreamWithOptions(ctx, UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "start"})
			w.Write(UIMessageChunk{"type": "data-progress", "id": "p1", "data": map[string]interface{}{"step": 1}})
			w.Write(UIMessageChunk{"type": "finish"})
		},
	})
	return chunks
}

func TestPipeUIMessageChunksToResponse(t *testing.T) {
	var buf bytes.Buffer
	if err := PipeUIMessageChunksToResponse(customChunkStream(context.Background()), &buf, nil); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data: {"data":{"step":1},"id":"p1","type":"data-progress"}`) {
		t.Fatalf("missing data part:\n%s", out)
	}
	if !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Fatalf("missing [DONE]:\n%s", out)
	}
}

func TestPipeUIMessageChunksToResponse_KeepAliveAndConsume(t *testing.T) {
	keepAlive := time.Hour
	var side bytes.Buffer
	var buf bytes.Buffer
	err := PipeUIMessageChunksToResponse(customChunkStream(context.Background()), &buf, &UIMessageStreamResponseInit{
		KeepAliveMs: &keepAlive,
		ConsumeSSEStream: func(r io.Reader) error {
			_, err := io.Copy(&side, r)
			return err
		},
	})
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if !strings.HasPrefix(buf.String(), sseStreamOpenComment) {
		t.Fatalf("expected the stream-open comment first, got %q", buf.String())
	}
	if side.String() != buf.String() {
		t.Fatal("ConsumeSSEStream should receive a copy of the SSE stream")
	}
}

func TestPipeUIMessageChunksToResponse_RequiresStreamAndWriter(t *testing.T) {
	if err := PipeUIMessageChunksToResponse(nil, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("expected an error for a nil stream")
	}
	if err := PipeUIMessageChunksToResponse(make(chan UIMessageChunk), nil, nil); err == nil {
		t.Fatal("expected an error for a nil writer")
	}
}

type chunkWriteFailer struct{}

func (chunkWriteFailer) Write([]byte) (int, error) { return 0, errors.New("client went away") }

func TestPipeUIMessageChunksToResponse_ReturnsWriteError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := PipeUIMessageChunksToResponse(customChunkStream(ctx), chunkWriteFailer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "client went away") {
		t.Fatalf("err = %v, want the write error", err)
	}
}

func TestCreateUIMessageChunksResponse(t *testing.T) {
	resp, err := CreateUIMessageChunksResponse(customChunkStream(context.Background()), &UIMessageStreamResponseInit{
		Status:  http.StatusAccepted,
		Headers: map[string]string{"X-Demo": "1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || resp.Header.Get("X-Demo") != "1" {
		t.Fatalf("status/header not applied: %d %v", resp.StatusCode, resp.Header)
	}
	for key, values := range UIMessageStreamHeaders() {
		if resp.Header.Get(key) != values[0] {
			t.Fatalf("header %s = %q, want %q", key, resp.Header.Get(key), values[0])
		}
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data-progress") || !strings.HasSuffix(string(body), "data: [DONE]\n\n") {
		t.Fatalf("unexpected body:\n%s", body)
	}
	if _, err := CreateUIMessageChunksResponse(nil, nil); err == nil {
		t.Fatal("expected an error for a nil stream")
	}
}
