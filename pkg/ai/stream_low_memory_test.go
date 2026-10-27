package ai

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-text-with-minimal-retention.test.ts (#22528, #23555).

func lowMemoryTextModel(deltas int) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			chunks := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "t"}}
			for i := 0; i < deltas; i++ {
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "t", Text: "x"})
			}
			chunks = append(chunks,
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "t"},
				provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
			)
			return testutil.NewMockTextStream(chunks), nil
		},
	}
}

func drainText(t *testing.T, s provider.TextStream) string {
	t.Helper()
	var sb strings.Builder
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func TestStreamTextLowMemory_StreamsTextWithBackpressureAndMetadata(t *testing.T) {
	t.Parallel()

	n := singleConsumerBufferLimit*10 + 3 // far more chunks than the bounded buffer holds
	res, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(n), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := res.Stream()
	if err != nil {
		t.Fatal(err)
	}
	if got := drainText(t, stream); len(got) != n {
		t.Fatalf("streamed %d chars, want %d", len(got), n)
	}
	if res.FinishReason() != types.FinishReasonStop {
		t.Fatalf("finish reason = %q", res.FinishReason())
	}
	// plain text streaming does not retain the generated text
	if res.r.Text() != "" {
		t.Fatalf("text retained: %d chars", len(res.r.Text()))
	}
}

func TestStreamTextLowMemory_OnlyOneConsumer(t *testing.T) {
	t.Parallel()

	res, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(2), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := res.Stream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.Stream(); !errors.Is(err, ErrStreamAlreadyConsumed) {
		t.Fatalf("second Stream err = %v", err)
	}
	if err := res.ConsumeStream(); !errors.Is(err, ErrStreamAlreadyConsumed) {
		t.Fatalf("ConsumeStream err = %v", err)
	}
	if _, err := res.ToUIMessageStreamResponse(context.Background(), nil); !errors.Is(err, ErrStreamAlreadyConsumed) {
		t.Fatalf("ToUIMessageStreamResponse err = %v", err)
	}
	_ = drainText(t, stream)
}

func TestStreamTextLowMemory_ConsumeStreamAndTextHelper(t *testing.T) {
	t.Parallel()

	res, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(100), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if res.FinishReason() != types.FinishReasonStop {
		t.Fatalf("finish reason = %q", res.FinishReason())
	}

	res, err = StreamTextLowMemory(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(50), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := res.PipeTextStreamToResponse(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 50 {
		t.Fatalf("piped %d bytes", buf.Len())
	}
}

func TestStreamTextLowMemory_CloseReleasesBlockedProducer(t *testing.T) {
	t.Parallel()

	res, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(1000), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := res.Stream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(); err != nil { // read one chunk, leave the producer blocked
		t.Fatal(err)
	}
	_ = stream.Close()
	select {
	case <-res.r.processingDone:
	case <-time.After(3 * time.Second):
		t.Fatal("producer stayed blocked after Close")
	}
}

func TestStreamTextLowMemory_KeepsContentWhenToolsAreConfigured(t *testing.T) {
	t.Parallel()

	opts := StreamTextOptions{Model: lowMemoryTextModel(3), Prompt: "go", Tools: []types.Tool{{Name: "t"}}}
	if !lowMemoryNeedsContent(opts) {
		t.Fatal("tools need complete content")
	}
	if lowMemoryNeedsContent(StreamTextOptions{Model: lowMemoryTextModel(3), Prompt: "go"}) {
		t.Fatal("plain streaming must not collect content")
	}
	res, err := StreamTextLowMemory(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if res.r.Text() != "xxx" {
		t.Fatalf("text = %q, want it collected with tools", res.r.Text())
	}
}

func TestStreamText_DefaultStillReplays(t *testing.T) {
	t.Parallel()

	res, err := StreamText(context.Background(), StreamTextOptions{Model: lowMemoryTextModel(5), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	first, second := drainText(t, res.NewStream()), drainText(t, res.NewStream())
	if first != "xxxxx" || second != "xxxxx" || res.Text() != "xxxxx" {
		t.Fatalf("first=%q second=%q text=%q", first, second, res.Text())
	}
}
