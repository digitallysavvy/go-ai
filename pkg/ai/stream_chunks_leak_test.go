package ai

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func manyChunkResult(t *testing.T) *StreamTextResult {
	t.Helper()
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			chunks := make([]provider.StreamChunk, 0, 102)
			for i := 0; i < 100; i++ {
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, Text: "x"})
			}
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop})
			return testutil.NewMockTextStream(chunks), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func waitGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= base {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	t.Fatalf("goroutine leak: %d > %d\n%s", runtime.NumGoroutine(), base, buf[:n])
}

func TestChunks_AbandonedConsumerExitsOnClose(t *testing.T) {
	res := manyChunkResult(t)
	_, _ = res.ReadAll()
	base := runtime.NumGoroutine()
	ch := res.Chunks()
	<-ch // consume one, then abandon
	_ = res.Close()
	waitGoroutines(t, base)
}

func TestChunksContext_AbandonedConsumerExitsOnCancel(t *testing.T) {
	res := manyChunkResult(t)
	_, _ = res.ReadAll()
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	ch := res.ChunksContext(ctx)
	<-ch
	cancel()
	waitGoroutines(t, base)
}
