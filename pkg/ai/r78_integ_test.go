package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
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

// PipeUIMessageStreamToResponseWithInit keeps its v0.5 contract (pending
// owner decision on the Pipe* helpers): it returns after ConsumeSSEStream,
// with its error.
func TestR78integPipeStreamWaitsForConsumeSSEStream(t *testing.T) {
	res := &StreamTextResult{stream: testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})}
	var finished atomic.Bool
	err := PipeUIMessageStreamToResponseWithInit(context.Background(), res, &bytes.Buffer{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			time.Sleep(20 * time.Millisecond)
			finished.Store(true)
			return errors.New("consume failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "consume failed") {
		t.Fatalf("PipeUIMessageStreamToResponseWithInit() = %v, want the consumer error", err)
	}
	if !finished.Load() {
		t.Fatal("returned before ConsumeSSEStream finished")
	}
}

// Once ConsumeSSEStream returns, its buffered copy is dropped and later
// writes are not kept (no unbounded growth for a consumer that stopped
// early).
func TestR78integConsumeSSEStreamCopyDroppedAfterReturn(t *testing.T) {
	var side *uiSSETeeBuffer
	chunks := make(chan UIMessageChunk, 64)
	for i := 0; i < 64; i++ {
		chunks <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": strings.Repeat("x", 128)}
	}
	close(chunks)
	err := PipeUIMessageChunksToResponse(chunks, &bytes.Buffer{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			side, _ = r.(*uiSSETeeBuffer)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if side == nil {
		t.Fatal("ConsumeSSEStream did not get the tee buffer")
	}
	side.mu.Lock()
	n := len(side.buf)
	side.mu.Unlock()
	if n != 0 {
		t.Fatalf("tee buffer keeps %d bytes after the consumer returned", n)
	}
}
