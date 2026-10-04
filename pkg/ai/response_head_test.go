package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func helloStreamResult(t *testing.T) *StreamTextResult {
	t.Helper()
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	return result
}

// Like TS pipeUIMessageStreamToResponse on a Node ServerResponse, the Pipe*
// helpers write the status and headers on an http.ResponseWriter.
func TestPipeUIMessageStreamToResponse_WritesHeadOnResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "abc") // set by the caller; kept
	if err := PipeUIMessageStreamToResponse(context.Background(), helloStreamResult(t), rec); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for key, values := range UIMessageStreamHeaders() {
		if got := rec.Header().Get(key); got != values[0] {
			t.Fatalf("header %s = %q, want %q", key, got, values[0])
		}
	}
	if rec.Header().Get("X-Request-Id") != "abc" {
		t.Fatal("caller-set header was dropped")
	}
	if !strings.Contains(rec.Body.String(), `"delta":"hello"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPipeUIMessageChunksToResponse_InitStatusAndHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	err := PipeUIMessageChunksToResponse(customChunkStream(context.Background()), rec, &UIMessageStreamResponseInit{
		Status:  http.StatusCreated,
		Headers: map[string]string{"Cache-Control": "no-store", "X-Demo": "1"},
	})
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Demo") != "1" {
		t.Fatalf("init headers not applied: %v", rec.Header())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("default Content-Type missing: %v", rec.Header())
	}
}

func TestPipeTextStreamToResponse_WritesHeadOnResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := PipeTextStreamToResponse(context.Background(), helloStreamResult(t), rec); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	err := PipeTextStreamToResponseWithInit(context.Background(), helloStreamResult(t), rec, &TextStreamResponseInit{
		Status:  http.StatusAccepted,
		Headers: map[string]string{"Content-Type": "text/markdown"},
	})
	if err != nil {
		t.Fatalf("pipe with init: %v", err)
	}
	if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Type") != "text/markdown" {
		t.Fatalf("init not applied: status %d, headers %v", rec.Code, rec.Header())
	}
}
