package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Owner ruling 2026-11-02 (pending decision 1, option b): TS uiMessagesSchema
// under zod 4.4.3 rejects a data part without `data` (`data: z.unknown()` is
// nonoptional: "Invalid input: expected nonoptional, received undefined");
// any present value, null included, passes.
func TestR78integValidateUIMessagesDataPartRequiresData(t *testing.T) {
	for _, tc := range []struct {
		name, part string
		wantErr    string
	}{
		{"missing data", `{"type":"data-weather","id":"a"}`, "[0].parts[0]: Invalid input: expected nonoptional, received undefined for data"},
		{"missing data and id", `{"type":"data-weather"}`, "[0].parts[0]: Invalid input: expected nonoptional, received undefined for data"},
		{"bad id first", `{"type":"data-weather","id":1}`, "[0].parts[0]: Invalid input: expected string for id"},
		{"null data", `{"type":"data-weather","data":null}`, ""},
		{"object data", `{"type":"data-weather","id":"a","data":{"city":"Tokyo"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[` + tc.part + `]}]`),
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.HasSuffix(err.Error(), "].\nError message: "+tc.wantErr) || !strings.HasPrefix(err.Error(), "Type validation failed: Value: [") {
				t.Fatalf("error = %q, want \"Type validation failed: Value: ….\\nError message: %s\"", err, tc.wantErr)
			}
		})
	}
}

// PipeUIMessageStreamToResponseWithInit returns once the response is
// written, without waiting for ConsumeSSEStream or returning its error (owner
// ruling 2026-11-02, TS pipeUIMessageStreamToResponse); the callback still
// reads the whole stream.
func TestR78integPipeStreamDoesNotWaitForConsumeSSEStream(t *testing.T) {
	res := &StreamTextResult{stream: testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})}
	release := make(chan struct{})
	done := make(chan string, 1)
	var out bytes.Buffer
	err := PipeUIMessageStreamToResponseWithInit(context.Background(), res, &out, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			<-release
			b, _ := io.ReadAll(r)
			done <- string(b)
			return errors.New("consume failed")
		},
	})
	if err != nil {
		t.Fatalf("PipeUIMessageStreamToResponseWithInit() = %v, want nil", err)
	}
	written := out.String()
	close(release)
	if got := <-done; got != written || !strings.HasSuffix(got, "data: [DONE]\n\n") {
		t.Fatalf("consumer read %q, response %q", got, written)
	}
}

// Once ConsumeSSEStream returns, its buffered copy is dropped and later
// writes are not kept (no unbounded growth for a consumer that stopped
// early).
func TestR78integConsumeSSEStreamCopyDroppedAfterReturn(t *testing.T) {
	sideCh := make(chan *uiSSETeeBuffer, 1)
	chunks := make(chan UIMessageChunk, 64)
	for i := 0; i < 64; i++ {
		chunks <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": strings.Repeat("x", 128)}
	}
	close(chunks)
	err := PipeUIMessageChunksToResponse(chunks, &bytes.Buffer{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			sideCh <- r.(*uiSSETeeBuffer)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	side := <-sideCh
	deadline := time.Now().Add(5 * time.Second)
	for {
		side.mu.Lock()
		n, discarded := len(side.buf), side.discard
		side.mu.Unlock()
		if discarded && n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tee buffer keeps %d bytes after the consumer returned", n)
		}
		time.Sleep(time.Millisecond)
	}
}

// The Pipe helper still returns the UI conversion's own failure (a panicking
// MessageMetadata callback fails the stream).
func TestR78integPipeStreamReturnsConversionError(t *testing.T) {
	res := &StreamTextResult{stream: testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})}
	err := PipeUIMessageStreamToResponseWithInit(context.Background(), res, &bytes.Buffer{}, nil, UIMessageStreamResultOptions{
		MessageMetadata: func(map[string]interface{}) map[string]interface{} { panic("metadata boom") },
	})
	if err == nil || !strings.Contains(err.Error(), "metadata boom") {
		t.Fatalf("PipeUIMessageStreamToResponseWithInit() = %v, want the conversion failure", err)
	}
}
