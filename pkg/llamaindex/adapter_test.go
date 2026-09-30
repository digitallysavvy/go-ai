package llamaindex

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// mirrors TS test "should convert AsyncIterable<EngineResponse>"
// (llamaindex-adapter.test.ts)
func TestToUIMessageStreamConvertsEngineResponses(t *testing.T) {
	stream := make(chan EngineResponse, 2)
	stream <- EngineResponse{Delta: "Hello"}
	stream <- EngineResponse{Delta: "World"}
	close(stream)

	chunks := collectChunks(t, stream, nil)

	want := []ai.UIMessageChunk{
		{"type": "text-start", "id": "1"},
		{"type": "text-delta", "id": "1", "delta": "Hello"},
		{"type": "text-delta", "id": "1", "delta": "World"},
		{"type": "text-end", "id": "1"},
	}
	assertChunksEqual(t, chunks, want)
}

func TestToUIMessageStreamTrimsLeadingWhitespaceOnce(t *testing.T) {
	stream := make(chan EngineResponse, 4)
	stream <- EngineResponse{Delta: "  "}
	stream <- EngineResponse{Delta: " \nHello"}
	stream <- EngineResponse{Delta: "  World"}
	close(stream)

	chunks := collectChunks(t, stream, nil)

	want := []ai.UIMessageChunk{
		{"type": "text-start", "id": "1"},
		{"type": "text-delta", "id": "1", "delta": ""},
		{"type": "text-delta", "id": "1", "delta": "Hello"},
		{"type": "text-delta", "id": "1", "delta": "  World"},
		{"type": "text-end", "id": "1"},
	}
	assertChunksEqual(t, chunks, want)
}

func TestToUIMessageStreamEmptyStream(t *testing.T) {
	stream := make(chan EngineResponse)
	close(stream)

	chunks := collectChunks(t, stream, nil)
	want := []ai.UIMessageChunk{
		{"type": "text-start", "id": "1"},
		{"type": "text-end", "id": "1"},
	}
	assertChunksEqual(t, chunks, want)
}

func TestToUIMessageStreamCallbackLifecycleOrder(t *testing.T) {
	stream := make(chan EngineResponse, 2)
	stream <- EngineResponse{Delta: "Hello"}
	stream <- EngineResponse{Delta: " World"}
	close(stream)

	var order []string
	var tokens []string
	var texts []string
	var final string
	callbacks := &StreamCallbacks{
		OnStart: func() error {
			order = append(order, "start")
			return nil
		},
		OnToken: func(token string) error {
			order = append(order, "token:"+token)
			tokens = append(tokens, token)
			return nil
		},
		OnText: func(text string) error {
			order = append(order, "text:"+text)
			texts = append(texts, text)
			return nil
		},
		OnFinal: func(completion string) error {
			order = append(order, "final:"+completion)
			final = completion
			return nil
		},
	}

	chunks := collectChunks(t, stream, callbacks)

	wantOrder := []string{"start", "token:Hello", "text:Hello", "token: World", "text: World", "final:Hello World"}
	if len(order) != len(wantOrder) {
		t.Fatalf("callback order = %#v, want %#v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("callback order = %#v, want %#v", order, wantOrder)
		}
	}
	if final != "Hello World" {
		t.Fatalf("final = %q, want %q", final, "Hello World")
	}
	if len(chunks) != 4 {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestToUIMessageStreamOnTokenAndOnTextCalledForEmptyTrimmedDelta(t *testing.T) {
	// Regression for the TypeScript adapter's unconditional
	// `onToken`/`onText` calls (no non-empty guard): a leading
	// whitespace-only delta still triggers both callbacks with an empty
	// string, unlike pkg/langchain's lazy/skip-empty behavior.
	stream := make(chan EngineResponse, 1)
	stream <- EngineResponse{Delta: "   "}
	close(stream)

	var tokenCalls, textCalls int
	callbacks := &StreamCallbacks{
		OnToken: func(token string) error {
			tokenCalls++
			if token != "" {
				t.Fatalf("token = %q, want empty", token)
			}
			return nil
		},
		OnText: func(text string) error {
			textCalls++
			return nil
		},
	}
	collectChunks(t, stream, callbacks)
	if tokenCalls != 1 || textCalls != 1 {
		t.Fatalf("tokenCalls=%d textCalls=%d, want 1/1", tokenCalls, textCalls)
	}
}

func TestToUIMessageStreamOnStartErrorAbortsBeforeAnyChunk(t *testing.T) {
	stream := make(chan EngineResponse, 1)
	stream <- EngineResponse{Delta: "Hello"}
	close(stream)

	want := errors.New("boom")
	out, errs := ToUIMessageStream(context.Background(), stream, &StreamCallbacks{
		OnStart: func() error { return want },
	})
	var chunks []ai.UIMessageChunk
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want none", chunks)
	}
	if err := <-errs; !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestToUIMessageStreamOnFinalErrorSuppressesTextEnd(t *testing.T) {
	stream := make(chan EngineResponse, 1)
	stream <- EngineResponse{Delta: "Hello"}
	close(stream)

	want := errors.New("final failed")
	out, errs := ToUIMessageStream(context.Background(), stream, &StreamCallbacks{
		OnFinal: func(string) error { return want },
	})
	var chunks []ai.UIMessageChunk
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	textEnds := chunksOfType(chunks, "text-end")
	if len(textEnds) != 0 {
		t.Fatalf("text-end chunks = %#v, want none after OnFinal error", textEnds)
	}
	if err := <-errs; !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestToUIMessageStreamContextCancellationReportsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := make(chan EngineResponse)
	cancel()

	out, errs := ToUIMessageStream(ctx, stream, nil)
	for range out {
	}
	if err := <-errs; err == nil {
		t.Fatal("expected context error")
	}
}

func collectChunks(t *testing.T, stream <-chan EngineResponse, callbacks *StreamCallbacks) []ai.UIMessageChunk {
	t.Helper()
	out, errs := ToUIMessageStream(context.Background(), stream, callbacks)
	var chunks []ai.UIMessageChunk
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	return chunks
}

func chunksOfType(chunks []ai.UIMessageChunk, typ string) []ai.UIMessageChunk {
	var out []ai.UIMessageChunk
	for _, chunk := range chunks {
		if chunk["type"] == typ {
			out = append(out, chunk)
		}
	}
	return out
}

func assertChunksEqual(t *testing.T, got, want []ai.UIMessageChunk) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("chunks = %#v, want %#v", got, want)
	}
	for i := range want {
		for k, v := range want[i] {
			if got[i][k] != v {
				t.Fatalf("chunk[%d] = %#v, want %#v", i, got[i], want[i])
			}
		}
		if len(got[i]) != len(want[i]) {
			t.Fatalf("chunk[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
