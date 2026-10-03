package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestStreamText_BasicStream(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Hello "},
				{Type: provider.ChunkTypeText, Text: "World!"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Say hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error reading stream: %v", err)
	}

	if text != "Hello World!" {
		t.Errorf("unexpected text: %s", text)
	}
}

func TestStreamText_OnStartDefaultMaxRetriesMatchesTypeScript(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var captured OnStartEvent
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hello",
		OnStart: func(_ context.Context, e OnStartEvent) {
			mu.Lock()
			captured = e
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if captured.MaxRetries != 2 {
		t.Fatalf("OnStartEvent.MaxRetries = %d, want TypeScript default 2", captured.MaxRetries)
	}
}

func TestStreamText_OnEndTakesPrecedenceOverDeprecatedOnFinish(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan []string, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hello",
		OnEnd: func(result *StreamTextResult) {
			done <- []string{"OnEnd:" + result.Text()}
		},
		OnFinish: func(*StreamTextResult) {
			done <- []string{"OnFinish"}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if result == nil {
		t.Fatal("StreamText() result is nil")
	}
	select {
	case calls := <-done:
		if !reflect.DeepEqual(calls, []string{"OnEnd:done"}) {
			t.Fatalf("calls = %#v, want OnEnd only", calls)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for OnEnd")
	}
}

func TestStreamText_OnEndEventTakesPrecedenceOverDeprecatedOnFinishEvent(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan []string, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hello",
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			done <- []string{"OnEndEvent:" + e.Text}
		},
		OnFinishEvent: func(_ context.Context, _ OnFinishEvent) {
			done <- []string{"OnFinishEvent"}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if result == nil {
		t.Fatal("StreamText() result is nil")
	}
	select {
	case calls := <-done:
		if !reflect.DeepEqual(calls, []string{"OnEndEvent:done"}) {
			t.Fatalf("calls = %#v, want OnEndEvent only", calls)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for OnEndEvent")
	}
}

func TestStreamText_GatewayRetryableErrorsRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	maxRetries := 2
	model := &testutil.MockLanguageModel{
		ProviderName: "gateway",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls < 2 {
				return nil, gatewayerrors.NewGatewayInternalServerError("", 503, nil, "")
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if result.Text() != "ok" || calls != 2 {
		t.Fatalf("text=%q calls=%d, want success after one retry", result.Text(), calls)
	}
}

func TestStreamText_GatewayHTTPStatusErrorsRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After-Ms", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","type":"internal_server_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"text-delta\",\"textDelta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"finish\",\"finishReason\":\"stop\",\"usage\":{}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := gateway.New(gateway.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("gateway.New error = %v", err)
	}
	model, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	maxRetries := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "hello",
		MaxRetries: &maxRetries,
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if result.Text() != "ok" || calls != 2 {
		t.Fatalf("text=%q calls=%d, want success after gateway HTTP stream retry", result.Text(), calls)
	}
}

func TestStreamText_GatewayNonRetryableErrorsDoNotRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	maxRetries := 2
	model := &testutil.MockLanguageModel{
		ProviderName: "gateway",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return nil, gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	if _, err := result.ReadAll(); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want no retry", calls)
	}
}

func TestStreamText_GatewayPlainErrorsDoNotRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	maxRetries := 2
	model := &testutil.MockLanguageModel{
		ProviderName: "gateway",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return nil, errors.New("plain failure")
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	if _, err := result.ReadAll(); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want no retry for untyped errors", calls)
	}
}

func TestStreamText_GatewayZeroMaxRetriesDisablesRetry(t *testing.T) {
	t.Parallel()

	calls := 0
	maxRetries := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "gateway",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return nil, gatewayerrors.NewGatewayInternalServerError("", 503, nil, "")
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	if _, err := result.ReadAll(); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want explicit zero retries", calls)
	}
}

func TestStreamText_RejectsNegativeMaxRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	maxRetries := -1
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream(nil), nil
		},
	}

	_, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err == nil || !strings.Contains(err.Error(), "maxRetries must be >= 0") {
		t.Fatalf("StreamText error = %v, want maxRetries validation error", err)
	}
	if calls != 0 {
		t.Fatalf("DoStream calls = %d, want validation before provider call", calls)
	}
}

func TestStreamText_RejectsSystemMessagesByDefault(t *testing.T) {
	t.Parallel()

	doStreamCalled := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				doStreamCalled = true
				return nil, nil
			},
		},
		Messages: []types.Message{
			{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "be concise"}},
			},
		},
	})
	// Prompt normalization now happens in the background (matching TS
	// standardizePrompt, which runs inside streamText's async IIFE), so the
	// rejection surfaces through Err()/ReadAll(), not the return value.
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	_, err = result.ReadAll()
	var unsupported *promptutils.UnsupportedSystemMessageError
	if !errors.As(err, &unsupported) {
		t.Fatalf("StreamText() error = %T, want UnsupportedSystemMessageError", err)
	}
	if doStreamCalled {
		t.Fatal("DoStream should not be called when system messages are rejected")
	}
}

func TestStreamText_AllowsSystemMessagesWithOptIn(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if !opts.AllowSystemMessages {
				t.Fatal("AllowSystemMessages was not forwarded to provider options")
			}
			if got := opts.Prompt.Messages[0].Role; got != types.RoleSystem {
				t.Fatalf("role = %q, want system", got)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:               model,
		AllowSystemMessages: true,
		Messages: []types.Message{
			{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "be concise"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if text != "ok" {
		t.Fatalf("Text = %q, want ok", text)
	}
}

func TestStreamText_InstructionsAlias(t *testing.T) {
	t.Parallel()

	instructions := "Use instructions alias"
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.Prompt.System != instructions {
				t.Fatalf("expected instructions as system, got %q", opts.Prompt.System)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:        model,
		Prompt:       "hello",
		Instructions: &instructions,
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q, want ok", text)
	}
}

func TestStreamText_InstructionsTakesPrecedenceOverSystem(t *testing.T) {
	t.Parallel()

	instructions := "instructions"
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.Prompt.System != instructions {
				t.Fatalf("expected instructions precedence, got %q", opts.Prompt.System)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:        model,
		Prompt:       "hello",
		System:       "system",
		Instructions: &instructions,
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, err = result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
}

func TestStreamText_NilModel(t *testing.T) {
	t.Parallel()

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  nil,
		Prompt: "Hello",
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStreamText_ChunksChannel(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "chunk1"},
				{Type: provider.ChunkTypeText, Text: "chunk2"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Stream chunks",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunks := result.Chunks()
	var texts []string
	for chunk := range chunks {
		if chunk.Type == provider.ChunkTypeText {
			texts = append(texts, chunk.Text)
		}
	}

	if len(texts) != 2 {
		t.Errorf("expected 2 text chunks, got %d", len(texts))
	}
	if texts[0] != "chunk1" || texts[1] != "chunk2" {
		t.Errorf("unexpected chunks: %v", texts)
	}
}

func TestStreamText_OnChunkFiltersEmptyTextDeltas(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: ""},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				{Type: provider.ChunkTypeText, ID: "1", Text: ""},
				{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				{Type: provider.ChunkTypeText, ID: "1", Text: ""},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var texts []string
	done := make(chan struct{}, 1)
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test-input",
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeText {
				texts = append(texts, chunk.Text)
			}
		},
		OnFinish: func(*StreamTextResult) {
			done <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	<-done

	want := []string{"Hello", ", ", "world!"}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("text chunks = %#v, want %#v", texts, want)
	}
}

func TestStreamText_ReadAll(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Part 1 "},
				{Type: provider.ChunkTypeText, Text: "Part 2 "},
				{Type: provider.ChunkTypeText, Text: "Part 3"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Read all",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "Part 1 Part 2 Part 3" {
		t.Errorf("unexpected text: %s", text)
	}
}

func TestStreamText_OnChunkCallback(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "chunk1"},
				{Type: provider.ChunkTypeText, Text: "chunk2"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	chunkCallbackCount := 0
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			chunkCallbackCount++
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for stream processing
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// start + start-step + firstChunk + 2 text chunks + finish-step + finish + streamFinish
	if chunkCallbackCount != 8 {
		t.Errorf("expected 8 chunk callbacks, got %d", chunkCallbackCount)
	}
}

func TestStreamText_FirstChunkEmittedBeforeFirstContentNotMetadata(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "resp_1"}},
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan struct{})
	var mu sync.Mutex
	var got []provider.ChunkType
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			got = append(got, chunk.Type)
			mu.Unlock()
		},
		OnFinish: func(result *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	want := []provider.ChunkType{
		provider.ChunkTypeStart,
		provider.ChunkTypeStartStep,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeFirstChunk,
		provider.ChunkTypeText,
		provider.ChunkTypeFinishStep,
		provider.ChunkTypeFinish,
		provider.ChunkTypeStreamFinish,
	}
	if len(got) != len(want) {
		t.Fatalf("chunk types = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk types = %#v, want %#v", got, want)
		}
	}
}

func TestStreamText_SuppressesReasoningBoundariesWhenSendReasoningFalse(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
				{Type: provider.ChunkTypeReasoning, ID: "reasoning-0", Reasoning: "thinking"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
				{Type: provider.ChunkTypeText, Text: "answer"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan struct{})
	var mu sync.Mutex
	var got []provider.ChunkType
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "Hello",
		SendReasoning: includeBoolPtr(false),
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			got = append(got, chunk.Type)
			mu.Unlock()
		},
		OnFinish: func(result *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	for _, chunkType := range got {
		if chunkType == provider.ChunkTypeReasoningStart || chunkType == provider.ChunkTypeReasoningEnd {
			t.Fatalf("unexpected reasoning boundary in chunks: %#v", got)
		}
	}
	want := []provider.ChunkType{
		provider.ChunkTypeStart,
		provider.ChunkTypeStartStep,
		provider.ChunkTypeFirstChunk,
		provider.ChunkTypeReasoning,
		provider.ChunkTypeText,
		provider.ChunkTypeFinishStep,
		provider.ChunkTypeFinish,
		provider.ChunkTypeStreamFinish,
	}
	if len(got) != len(want) {
		t.Fatalf("chunk types = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk types = %#v, want %#v", got, want)
		}
	}
}

func TestStreamText_OnFinishCallback(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	finishCalled := false
	var capturedResult *StreamTextResult

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnFinish: func(result *StreamTextResult) {
			mu.Lock()
			defer mu.Unlock()
			finishCalled = true
			capturedResult = result
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for stream processing
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !finishCalled {
		t.Error("expected OnFinish callback to be called")
	}
	if capturedResult == nil {
		t.Error("callback did not receive result")
	}
}

func TestStreamText_TextAccumulation(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Hello "},
				{Type: provider.ChunkTypeText, Text: "World"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read all to ensure accumulation happens
	_, _ = result.ReadAll()

	accumulated := result.Text()
	if accumulated != "Hello World" {
		t.Errorf("unexpected accumulated text: %s", accumulated)
	}
}

func TestStreamText_FinishReason(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = result.ReadAll()

	if result.FinishReason() != types.FinishReasonLength {
		t.Errorf("unexpected finish reason: %s", result.FinishReason())
	}
}

func TestStreamText_UsageTracking(t *testing.T) {
	t.Parallel()

	input, output, total := int64(10), int64(20), int64(30)
	expectedUsage := types.Usage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeUsage, Usage: &expectedUsage},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = result.ReadAll()

	usage := result.Usage()
	// Compare dereferenced values, not pointer identity: usage now always
	// flows through processStream's additive Usage.Add accumulation (even
	// for a single step, via ReadAll waiting on processingDone), which may
	// allocate fresh *int64s rather than reusing the chunk's pointers.
	if usage.InputTokens == nil || *usage.InputTokens != *expectedUsage.InputTokens {
		t.Errorf("unexpected input tokens: %v", usage.InputTokens)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != *expectedUsage.OutputTokens {
		t.Errorf("unexpected output tokens: %v", usage.OutputTokens)
	}
}

func TestStreamText_Close(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = result.Close()
	if err != nil {
		t.Errorf("unexpected error closing stream: %v", err)
	}
}

func TestStreamText_ErrorHandling(t *testing.T) {
	t.Parallel()

	expectedError := errors.New("stream failed")

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, expectedError
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	// StreamText returns immediately, matching TS streamText(): the first
	// provider stream request happens in the background, so a failure there
	// surfaces through Err()/ReadAll(), not the return value.
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	_, err = result.ReadAll()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedError) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestStreamText_StreamError(t *testing.T) {
	t.Parallel()

	streamError := errors.New("stream read error")

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(streamError), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}

	_, err = result.ReadAll()
	if err == nil {
		t.Fatal("expected error reading stream")
	}
	if !errors.Is(err, streamError) {
		t.Errorf("expected stream error, got: %v", err)
	}
}

func TestStreamTextResult_Stream(t *testing.T) {
	t.Parallel()

	mockStream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "test"},
	})

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return mockStream, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stream := result.Stream()
	if stream == nil {
		t.Error("expected stream to be non-nil")
	}
}

func TestStreamTextResult_Err(t *testing.T) {
	t.Parallel()

	streamError := errors.New("stream error")

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(streamError), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to read to trigger the error
	_, readErr := result.ReadAll()

	// The error should be returned from ReadAll
	if readErr == nil {
		t.Error("expected error from ReadAll")
	}
}

func TestStreamText_PromptParams(t *testing.T) {
	t.Parallel()

	temp := 0.7
	maxTokens := 100

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.Temperature == nil || *opts.Temperature != temp {
				t.Errorf("expected temperature %f, got %v", temp, opts.Temperature)
			}
			if opts.MaxTokens == nil || *opts.MaxTokens != maxTokens {
				t.Errorf("expected maxTokens %d, got %v", maxTokens, opts.MaxTokens)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:       model,
		Prompt:      "Hello",
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// BUG-T04: tool choice "required" must be forwarded to the provider (#12854)
func TestStreamText_ToolChoiceForwardedToProvider(t *testing.T) {
	t.Parallel()

	var capturedChoice types.ToolChoice
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			capturedChoice = opts.ToolChoice
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "use a tool",
		ToolChoice: types.RequiredToolChoice(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The first provider request now happens in the background (matching TS,
	// where streamText() returns before any I/O), so wait for it before
	// reading the value DoStreamFunc captured. The mock stream produces no
	// tool call despite ToolChoice: required, so this now correctly surfaces
	// ToolChoiceViolationError (audit row 36b3364/ccf98e7 / WG3) rather than
	// silently succeeding.
	if _, err := result.ReadAll(); !IsToolChoiceViolationError(err) {
		t.Fatalf("ReadAll error = %v, want ToolChoiceViolationError", err)
	}
	if capturedChoice.Type != types.ToolChoiceRequired {
		t.Errorf("expected ToolChoiceRequired forwarded to provider, got %q", capturedChoice.Type)
	}
}

// TestStreamText_ToolChoiceRequiredSatisfiedByAnyToolCall ports TS's
// ToolChoiceViolationError happy path to streamText (audit row 36b3364/
// ccf98e7 / WG3): any tool call satisfies "required".
func TestStreamText_ToolChoiceRequiredSatisfiedByAnyToolCall(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{{
		Name: "search",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "results", nil
		},
	}}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
	maxSteps := 1

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "search something",
		Tools:      tools,
		ToolChoice: types.RequiredToolChoice(),
		MaxSteps:   &maxSteps,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if len(result.ToolCalls()) != 1 || result.ToolCalls()[0].ToolName != "search" {
		t.Fatalf("ToolCalls() = %+v", result.ToolCalls())
	}
}

// BUG-T06: calling Resume() on a completed stream must return an error, not flash
// the status back to "submitted" (#12102)
func TestStreamTextResult_ResumeOnDoneStreamReturnsError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain the stream so it transitions to StreamStatusDone.
	_, _ = result.ReadAll()

	if result.Status() != StreamStatusDone {
		t.Fatalf("expected status Done after ReadAll, got %q", result.Status())
	}

	// Resume on a done stream must return an error and must NOT change the status.
	resumeErr := result.Resume(context.Background())
	if resumeErr == nil {
		t.Fatal("expected error from Resume() on completed stream")
	}
	// Status must remain Done — not flash to Submitted.
	if result.Status() != StreamStatusDone {
		t.Errorf("status should remain Done after failed Resume, got %q", result.Status())
	}
}

// TestStreamTextResult_StatusLifecycle verifies the Submitted→Streaming→Done transitions.
func TestStreamTextResult_StatusLifecycle(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	statuses := make([]StreamStatus, 0, 3)
	recordStatus := func(s StreamStatus) {
		mu.Lock()
		statuses = append(statuses, s)
		mu.Unlock()
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnFinish: func(r *StreamTextResult) {
			// By the time OnFinish fires the status must already be Done.
			recordStatus(r.Status())
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial status must be Submitted.
	if result.Status() != StreamStatusSubmitted {
		t.Errorf("expected Submitted immediately after StreamText, got %q", result.Status())
	}

	// Wait for the goroutine to finish (poll rather than a fixed sleep,
	// which flakes under -race with parallel package load).
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		observed := len(statuses)
		mu.Unlock()
		if result.Status() == StreamStatusDone && observed > 0 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if result.Status() != StreamStatusDone {
		t.Errorf("expected Done after stream completes, got %q", result.Status())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 1 || statuses[0] != StreamStatusDone {
		t.Errorf("expected OnFinish to observe Done, got %v", statuses)
	}
}

func TestStreamText_NoCallbacks(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		// No callbacks set
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Directly iterate on stream
	stream := result.Stream()
	for {
		_, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

// TestStreamTextReasoningPropagated verifies that the Reasoning field is forwarded
// from StreamTextOptions to the provider's DoStream call options.
func TestStreamTextReasoningPropagated(t *testing.T) {
	t.Parallel()

	level := types.ReasoningHigh
	var capturedReasoning *types.ReasoningLevel

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			capturedReasoning = opts.Reasoning
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "think hard",
		Reasoning: &level,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = result.ReadAll()

	if capturedReasoning == nil {
		t.Fatal("expected Reasoning to be propagated, got nil")
	}
	if *capturedReasoning != types.ReasoningHigh {
		t.Errorf("expected ReasoningHigh, got %v", *capturedReasoning)
	}
}

// TestStreamTextResultReasoningAccessors ports the TS StreamTextResult
// `reasoning`/`reasoningText` deprecated getters (stream-text-result.ts:
// "@deprecated Use `finalStep.reasoning` instead." /
// "@deprecated Use `finalStep.reasoningText` instead."): both must mirror
// FinalStep().Reasoning / FinalStep().ReasoningText.
func TestStreamTextResultReasoningAccessors(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "r1"},
				{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "thinking..."},
				{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "think hard",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = result.ReadAll()

	finalStep := result.FinalStep()
	if got := result.ReasoningText(); got != finalStep.ReasoningText {
		t.Errorf("ReasoningText() = %q, want FinalStep().ReasoningText = %q", got, finalStep.ReasoningText)
	}
	if result.ReasoningText() == "" {
		t.Error("expected non-empty ReasoningText()")
	}
	if got := result.Reasoning(); len(got) != len(finalStep.Reasoning) {
		t.Errorf("Reasoning() = %#v, want FinalStep().Reasoning = %#v", got, finalStep.Reasoning)
	}
	if len(result.Reasoning()) == 0 {
		t.Error("expected non-empty Reasoning()")
	}
}

// TestStreamTextToolsExecutedAfterStreamEnd verifies that tool Execute() is NOT
// called while the stream is in progress, only after all chunks are consumed.
// Tool-result chunks are forwarded to OnChunk after Execute fires.
// Invariant: all stream chunks → execute → tool-result chunks.
func TestStreamTextToolsExecutedAfterStreamEnd(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	// Events are either "chunk:<type>" or "execute".
	var events []string

	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			events = append(events, "execute")
			mu.Unlock()
			return "sunny", nil
		},
	}

	// Stream: text chunk, tool-call chunk, finish chunk — 3 chunks total.
	// Execute must NOT fire until the stream loop has completed.
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Checking weather..."},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "call_1",
					ToolName:  "get_weather",
					Arguments: map[string]interface{}{"city": "NY"},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	maxSteps := 1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "What's the weather?",
		Tools:    []types.Tool{tool},
		MaxSteps: &maxSteps,
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			events = append(events, "chunk:"+string(chunk.Type))
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Locate the execute event and the first tool-result chunk.
	executeIdx := -1
	toolResultIdx := -1
	streamChunksBefore := 0 // stream-originating chunks before execute
	for i, e := range events {
		if e == "execute" {
			executeIdx = i
		}
		if e == "chunk:tool-result" && toolResultIdx == -1 {
			toolResultIdx = i
		}
	}
	// Count stream chunks (all chunks before execute).
	for _, e := range events[:max(executeIdx+1, 0)] {
		if e != "execute" {
			streamChunksBefore++
		}
	}

	if executeIdx == -1 {
		t.Fatal("Execute was never called")
	}
	// The 3 stream chunks (text, tool-call, finish) must precede execute.
	if streamChunksBefore < 3 {
		t.Errorf("expected at least 3 stream chunks before execute, got %d; events: %v",
			streamChunksBefore, events)
	}
	// A tool-result chunk must appear after execute.
	if toolResultIdx == -1 {
		t.Error("expected tool-result chunk to be forwarded to OnChunk after execute")
	} else if toolResultIdx <= executeIdx {
		t.Errorf("tool-result chunk (idx %d) must come after execute (idx %d); events: %v",
			toolResultIdx, executeIdx, events)
	}
}

// max is a local helper for the test above (avoids importing math for a trivial op).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestStreamTextToolExecutionOrderPreserved verifies that multiple tool call chunks
// are executed in the order they were received from the stream.
func TestStreamTextToolExecutionOrderPreserved(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var executionOrder []string

	makeExec := func(name string) types.ToolExecutor {
		return func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			executionOrder = append(executionOrder, name)
			mu.Unlock()
			return name + "_result", nil
		}
	}

	tools := []types.Tool{
		{Name: "tool_a", Description: "A", Execute: makeExec("tool_a")},
		{Name: "tool_b", Description: "B", Execute: makeExec("tool_b")},
		{Name: "tool_c", Description: "C", Execute: makeExec("tool_c")},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "tool_a", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "2", ToolName: "tool_b", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "3", ToolName: "tool_c", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	maxSteps := 1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "Run all tools",
		Tools:    tools,
		MaxSteps: &maxSteps,
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()

	expected := []string{"tool_a", "tool_b", "tool_c"}
	if len(executionOrder) != len(expected) {
		t.Fatalf("expected %d executions, got %d: %v", len(expected), len(executionOrder), executionOrder)
	}
	for i, name := range expected {
		if executionOrder[i] != name {
			t.Errorf("execution[%d]: expected %q, got %q", i, name, executionOrder[i])
		}
	}
}

// TestStreamTextChunksDeliveredBeforeToolCallback verifies that the tool call
// chunk is forwarded to the OnChunk consumer before Execute fires.
func TestStreamTextChunksDeliveredBeforeToolCallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var events []string // "chunk:<type>" or "execute"

	tool := types.Tool{
		Name:        "weather",
		Description: "Get weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			events = append(events, "execute")
			mu.Unlock()
			return "sunny", nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "checking"},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "c1",
					ToolName:  "weather",
					Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	maxSteps := 1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "weather?",
		Tools:    []types.Tool{tool},
		MaxSteps: &maxSteps,
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			events = append(events, "chunk:"+string(chunk.Type))
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Find where the tool-call chunk and execute appear in the event sequence.
	toolCallChunkIdx := -1
	executeIdx := -1
	for i, e := range events {
		if e == "chunk:tool-call" && toolCallChunkIdx == -1 {
			toolCallChunkIdx = i
		}
		if e == "execute" {
			executeIdx = i
		}
	}

	if toolCallChunkIdx == -1 {
		t.Fatal("tool-call chunk was never delivered to OnChunk consumer")
	}
	if executeIdx == -1 {
		t.Fatal("Execute was never called")
	}
	if executeIdx <= toolCallChunkIdx {
		t.Errorf("Execute fired at event index %d but tool-call chunk was at index %d; "+
			"all chunks must be delivered before Execute fires; events: %v",
			executeIdx, toolCallChunkIdx, events)
	}
}

func TestStreamTextToolCallChunkIncludesToolTitleAndMetadata(t *testing.T) {
	t.Parallel()

	toolMetadata := map[string]interface{}{"source": "catalog"}
	tool := types.Tool{
		Name:     "weather",
		Title:    "Weather Lookup",
		Metadata: toolMetadata,
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "c1",
					ToolName:  "weather",
					Arguments: map[string]interface{}{"city": "Paris"},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	var seen types.ToolCall
	maxSteps := 1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "weather?",
		Tools:    []types.Tool{tool},
		MaxSteps: &maxSteps,
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
				seen = *chunk.ToolCall
			}
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	if seen.Title != "Weather Lookup" {
		t.Fatalf("stream tool-call title = %q, want Weather Lookup", seen.Title)
	}
	if got := seen.ToolMetadata["source"]; got != "catalog" {
		t.Fatalf("stream tool-call metadata source = %#v, want catalog", got)
	}
}

func TestStreamText_ToolApprovalDeniedSkipsExecution(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name: "danger",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("denied stream tool should not execute")
			return nil, nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "call_1",
					ToolName:  "danger",
					Arguments: map[string]interface{}{"x": 1},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	maxSteps := 1
	var toolResults []types.ToolResult
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Tools:    []types.Tool{tool},
		MaxSteps: &maxSteps,
		ToolApproval: map[string]types.ToolApprovalValue{
			"danger": types.ToolApprovalStatusDenied,
		},
		OnFinish: func(r *StreamTextResult) {
			toolResults = r.ToolResults()
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	if len(toolResults) != 1 {
		t.Fatalf("expected 1 denied tool result, got %d", len(toolResults))
	}
	if toolResults[0].ApprovalStatus != types.ToolApprovalStatusDenied {
		t.Fatalf("expected denied approval status, got %s", toolResults[0].ApprovalStatus)
	}
	// No reason was supplied, so Go must leave it unset here (matching TS,
	// which leaves `reason` undefined) rather than synthesize a default;
	// each provider converter applies its own default denial text when it
	// serializes the tool result (audit row 58a2ad7 / G6).
	if toolResults[0].ApprovalReason != nil {
		t.Fatalf("expected no synthesized denial reason, got %#v", *toolResults[0].ApprovalReason)
	}
	output, ok := toolResults[0].Result.(types.ToolResultOutput)
	if !ok {
		t.Fatalf("expected ToolResultOutput, got %T", toolResults[0].Result)
	}
	if output.Type != types.ToolResultOutputExecutionDenied || output.Reason != "" {
		t.Fatalf("unexpected denied output: %#v", output)
	}
}

func TestStreamText_ToolApprovalEmitsTSApprovalChunks(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name: "danger",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("user-approval stream tool should not execute")
			return nil, nil
		},
	}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "call_1",
					ToolName:  "danger",
					Arguments: map[string]interface{}{"x": 1},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan struct{})
	maxSteps := 1
	var chunks []provider.StreamChunk
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:                          model,
		Tools:                          []types.Tool{tool},
		MaxSteps:                       &maxSteps,
		ExperimentalToolApprovalSecret: []byte{},
		ToolApproval: map[string]types.ToolApprovalValue{
			"danger": types.ToolApprovalStatusUserApproval,
		},
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done

	var approval *types.ToolApprovalRequestContent
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeToolResult {
			t.Fatalf("user approval emitted legacy tool-result chunk: %#v", chunks[i])
		}
		if chunks[i].Type == provider.ChunkTypeToolApprovalRequest {
			approval = chunks[i].ToolApprovalRequest
		}
	}
	if approval == nil {
		t.Fatalf("missing tool-approval-request chunk in %#v", chunks)
	}
	if approval.ApprovalID == "" || approval.ApprovalID == "call_1" {
		t.Fatalf("approval id = %q, want generated id distinct from tool call id", approval.ApprovalID)
	}
	if approval.Signature == "" {
		t.Fatal("expected signed approval request chunk")
	}
	valid, err := VerifyToolApprovalSignature([]byte{}, approval.Signature, approval.ApprovalID, approval.ToolCallID, approval.ToolCall.ToolName, approval.ToolCall.Arguments)
	if err != nil || !valid {
		t.Fatalf("signature valid = %v, err = %v", valid, err)
	}
}

func TestStreamText_ContinuesWhenToolCallFinishesWithStop(t *testing.T) {
	t.Parallel()

	var toolCalled int
	tool := types.Tool{
		Name:        "weather",
		Description: "Get weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			toolCalled++
			return "sunny", nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			switch callCount {
			case 1:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "weather",
						Arguments: map[string]interface{}{"location": "NYC"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			case 2:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "The weather in NYC is sunny!"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			}
			t.Fatalf("unexpected stream call count: %d", callCount)
			return nil, nil
		},
	}

	done := make(chan struct{})
	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "weather?",
		Tools:    []types.Tool{tool},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}

	if callCount != 2 {
		t.Fatalf("expected 2 stream calls, got %d", callCount)
	}
	if toolCalled != 1 {
		t.Fatalf("expected tool to be called once, got %d", toolCalled)
	}
	if result.Text() != "The weather in NYC is sunny!" {
		t.Fatalf("unexpected text: %q", result.Text())
	}
}

func TestStreamText_PreservesAllowSystemInMessagesAcrossSteps(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if !opts.AllowSystemMessages || !opts.AllowSystemInMessages {
				t.Fatalf("allowSystem flags not preserved on call %d: %+v", callCount, opts)
			}
			switch callCount {
			case 1:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "weather",
						Arguments: map[string]interface{}{"city": "NYC"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			default:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			}
		},
	}

	tool := types.Tool{
		Name: "weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}

	done := make(chan struct{})
	maxSteps := 2
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:                 model,
		System:                "sys",
		Messages:              []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "existing"}}}},
		AllowSystemInMessages: true,
		Tools:                 []types.Tool{tool},
		MaxSteps:              &maxSteps,
		OnFinish: func(_ *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream finish")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 stream calls, got %d", callCount)
	}
}

func TestStreamText_StopWhenStepCount(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name: "loop",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "call_1",
					ToolName:  "loop",
					Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	done := make(chan *StreamTextResult, 1)
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "loop",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnFinish: func(r *StreamTextResult) {
			done <- r
		},
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	var result *StreamTextResult
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}

	if callCount != 2 {
		t.Fatalf("expected 2 stream calls, got %d", callCount)
	}
	if result.StopReason() != "maximum number of steps (2) reached" {
		t.Fatalf("unexpected stop reason: %q", result.StopReason())
	}
}

func TestStreamText_UsesStepTextForContinuedPrompt(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name: "next",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	callCount := 0
	var thirdPrompt []types.Message
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			switch callCount {
			case 1:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "first"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "next",
						Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			case 2:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "second"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_2",
						ToolName:  "next",
						Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			case 3:
				thirdPrompt = append([]types.Message(nil), opts.Prompt.Messages...)
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			}
			t.Fatalf("unexpected stream call count: %d", callCount)
			return nil, nil
		},
	}

	done := make(chan *StreamTextResult, 1)
	_, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "start",
		Tools:    []types.Tool{tool},
		OnFinish: func(r *StreamTextResult) {
			done <- r
		},
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	var result *StreamTextResult
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}

	if result.Text() != "firstseconddone" {
		t.Fatalf("unexpected accumulated text: %q", result.Text())
	}
	var assistantTexts []string
	for _, msg := range thirdPrompt {
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, part := range msg.Content {
			if text, ok := part.(types.TextContent); ok {
				assistantTexts = append(assistantTexts, text.Text)
			}
		}
	}
	expected := []string{"first", "second"}
	if !reflect.DeepEqual(assistantTexts, expected) {
		t.Fatalf("assistant prompt texts = %#v, want %#v", assistantTexts, expected)
	}
}

// TestStreamEmitsReasoningFile verifies that a provider can emit a
// ChunkTypeReasoningFile chunk and that it flows through to the OnChunk consumer.
func TestStreamEmitsReasoningFile(t *testing.T) {
	t.Parallel()

	fileData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	rfContent := &types.ReasoningFileContent{
		MediaType: "image/png",
		Data:      fileData,
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Here is a chart:"},
				{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: rfContent},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Generate a chart",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for processStream goroutine to finish.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	// Find the reasoning-file chunk among received chunks
	var rfChunk *provider.StreamChunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeReasoningFile {
			rfChunk = &chunks[i]
			break
		}
	}
	if rfChunk == nil {
		t.Fatal("reasoning-file chunk was not emitted to OnChunk consumer")
	}
	if rfChunk.ReasoningFileContent == nil {
		t.Fatal("ReasoningFileContent field is nil in reasoning-file chunk")
	}
	if rfChunk.ReasoningFileContent.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want \"image/png\"", rfChunk.ReasoningFileContent.MediaType)
	}
	if len(rfChunk.ReasoningFileContent.Data) != len(fileData) {
		t.Errorf("Data length = %d, want %d", len(rfChunk.ReasoningFileContent.Data), len(fileData))
	}
}

// TestStreamEmitsSourceContent verifies that a ChunkTypeSource chunk flows
// through to the OnChunk consumer.
func TestStreamEmitsSourceContent(t *testing.T) {
	t.Parallel()

	srcContent := &types.SourceContent{
		SourceType: "url",
		ID:         "src-1",
		URL:        "https://example.com",
		Title:      "Example",
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "According to sources:"},
				{Type: provider.ChunkTypeSource, SourceContent: srcContent},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Tell me something sourced",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	var found *provider.StreamChunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeSource {
			found = &chunks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("source chunk was not emitted to OnChunk consumer")
	}
	if found.SourceContent == nil {
		t.Fatal("SourceContent field is nil in source chunk")
	}
	if found.SourceContent.URL != "https://example.com" {
		t.Errorf("URL = %q, want \"https://example.com\"", found.SourceContent.URL)
	}
}

// TestStreamEmitsGeneratedFile verifies that a ChunkTypeFile chunk flows
// through to the OnChunk consumer.
func TestStreamEmitsGeneratedFile(t *testing.T) {
	t.Parallel()

	fileData := []byte{0x89, 0x50, 0x4E, 0x47}
	gfc := &types.GeneratedFileContent{
		MediaType: "image/png",
		Data:      fileData,
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Here is the generated image."},
				{Type: provider.ChunkTypeFile, GeneratedFileContent: gfc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Generate an image",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	var found *provider.StreamChunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeFile {
			found = &chunks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("file chunk was not emitted to OnChunk consumer")
	}
	if found.GeneratedFileContent == nil {
		t.Fatal("GeneratedFileContent field is nil in file chunk")
	}
	if found.GeneratedFileContent.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want \"image/png\"", found.GeneratedFileContent.MediaType)
	}
}

// TestStreamTextResult_FilesAccumulatesAcrossSteps verifies that
// StreamTextResult.Files() accumulates generated files from every step,
// matching the TypeScript SDK's accumulative StreamTextResult.files.
func TestStreamTextResult_FilesAccumulatesAcrossSteps(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name:        "makeFile",
		Description: "make a file",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	file1 := &types.GeneratedFileContent{MediaType: "image/png", Data: []byte{1}}
	file2 := &types.GeneratedFileContent{MediaType: "image/jpeg", Data: []byte{2}}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			switch callCount {
			case 1:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeFile, GeneratedFileContent: file1},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "call_1", ToolName: "makeFile", Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			case 2:
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeFile, GeneratedFileContent: file2},
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			}
			t.Fatalf("unexpected stream call count: %d", callCount)
			return nil, nil
		},
	}

	done := make(chan struct{})
	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "make two files",
		Tools:    []types.Tool{tool},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}

	files := result.Files()
	if len(files) != 2 {
		t.Fatalf("Files() = %d files, want 2: %+v", len(files), files)
	}
	if files[0].MediaType != "image/png" || files[1].MediaType != "image/jpeg" {
		t.Errorf("unexpected file media types: %q, %q", files[0].MediaType, files[1].MediaType)
	}
}

// TestStreamTextResult_FilesEmptyWhenNoFiles verifies Files() returns an
// empty slice (never panics/nil-dereferences) when no files were generated.
func TestStreamTextResult_FilesEmptyWhenNoFiles(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	done := make(chan struct{})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}
	if got := result.Files(); len(got) != 0 {
		t.Errorf("Files() = %+v, want empty", got)
	}
}

// TestStreamEmitsCustomContent verifies that a provider can emit a
// ChunkTypeCustom chunk and that it flows through to the OnChunk consumer.
func TestStreamEmitsCustomContent(t *testing.T) {
	t.Parallel()

	ccContent := &types.CustomContent{
		Kind: "mock-citation",
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "See citation."},
				{Type: provider.ChunkTypeCustom, CustomContent: ccContent},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Tell me something with citations",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	var customChunk *provider.StreamChunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeCustom {
			customChunk = &chunks[i]
			break
		}
	}
	if customChunk == nil {
		t.Fatal("custom chunk was not emitted to OnChunk consumer")
	}
	if customChunk.CustomContent == nil {
		t.Fatal("CustomContent field is nil in custom chunk")
	}
	if customChunk.CustomContent.Kind != "mock-citation" {
		t.Errorf("Kind = %q, want \"mock-citation\"", customChunk.CustomContent.Kind)
	}
}

// TestStreamEmitsTextBlockBoundaries verifies that ChunkTypeTextStart and
// ChunkTypeTextEnd flow through to the OnChunk consumer with their block IDs,
// and that only ChunkTypeText deltas contribute to the accumulated text.
func TestStreamEmitsTextBlockBoundaries(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "block-1"},
				{Type: provider.ChunkTypeText, ID: "block-1", Text: "hello"},
				{Type: provider.ChunkTypeTextEnd, ID: "block-1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Say hello",
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	// Only ChunkTypeText deltas should accumulate into result.Text.
	if text := result.Text(); text != "hello" {
		t.Errorf("Text = %q, want \"hello\"", text)
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	// Verify start and end chunks were forwarded with the correct ID.
	var startChunk, endChunk *provider.StreamChunk
	for i := range chunks {
		switch chunks[i].Type {
		case provider.ChunkTypeTextStart:
			startChunk = &chunks[i]
		case provider.ChunkTypeTextEnd:
			endChunk = &chunks[i]
		}
	}
	if startChunk == nil {
		t.Fatal("text-start chunk was not forwarded to OnChunk")
	}
	if startChunk.ID != "block-1" {
		t.Errorf("text-start ID = %q, want \"block-1\"", startChunk.ID)
	}
	if endChunk == nil {
		t.Fatal("text-end chunk was not forwarded to OnChunk")
	}
	if endChunk.ID != "block-1" {
		t.Errorf("text-end ID = %q, want \"block-1\"", endChunk.ID)
	}
}

// TestStreamEmitsReasoningBlockBoundaries verifies that ChunkTypeReasoningStart
// and ChunkTypeReasoningEnd flow through to the OnChunk consumer with their IDs,
// and that only ChunkTypeReasoning deltas affect the reasoning content.
func TestStreamEmitsReasoningBlockBoundaries(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "thinking-1"},
				{Type: provider.ChunkTypeReasoning, ID: "thinking-1", Reasoning: "I think..."},
				{Type: provider.ChunkTypeReasoningEnd, ID: "thinking-1"},
				{Type: provider.ChunkTypeText, Text: "Answer."},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var receivedChunks []provider.StreamChunk
	done := make(chan struct{})

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "Think then answer",
		SendReasoning: includeBoolPtr(true),
		OnChunk: func(chunk provider.StreamChunk) {
			mu.Lock()
			receivedChunks = append(receivedChunks, chunk)
			mu.Unlock()
		},
		OnFinish: func(r *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete within timeout")
	}

	mu.Lock()
	chunks := make([]provider.StreamChunk, len(receivedChunks))
	copy(chunks, receivedChunks)
	mu.Unlock()

	var startChunk, endChunk *provider.StreamChunk
	for i := range chunks {
		switch chunks[i].Type {
		case provider.ChunkTypeReasoningStart:
			startChunk = &chunks[i]
		case provider.ChunkTypeReasoningEnd:
			endChunk = &chunks[i]
		}
	}
	if startChunk == nil {
		t.Fatal("reasoning-start chunk was not forwarded to OnChunk")
	}
	if startChunk.ID != "thinking-1" {
		t.Errorf("reasoning-start ID = %q, want \"thinking-1\"", startChunk.ID)
	}
	if endChunk == nil {
		t.Fatal("reasoning-end chunk was not forwarded to OnChunk")
	}
	if endChunk.ID != "thinking-1" {
		t.Errorf("reasoning-end ID = %q, want \"thinking-1\"", endChunk.ID)
	}
}

func TestStreamTextAbortDoesNotCallFinish(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(context.Canceled), nil
		},
	}

	abortCalled := make(chan struct{}, 1)
	finishCalled := make(chan struct{}, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "abort",
		OnAbort: func(context.Context, []types.StepResult) {
			abortCalled <- struct{}{}
		},
		OnFinish: func(*StreamTextResult) {
			finishCalled <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if result == nil {
		t.Fatal("StreamText() result is nil")
	}

	select {
	case <-abortCalled:
	case <-time.After(time.Second):
		t.Fatal("OnAbort was not called")
	}
	select {
	case <-finishCalled:
		t.Fatal("OnFinish should not be called for aborted stream")
	default:
	}
}

// TestStreamTextAbortEventCarriesCallIDAndReason ports TS's onAbort event
// shape (audit row a8e8ad0 / WG5): the stable OnAbortEvent must carry the
// call ID and abort reason, and take precedence over the deprecated OnAbort.
func TestStreamTextAbortEventCarriesCallIDAndReason(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(context.Canceled), nil
		},
	}

	eventCh := make(chan GenerateTextAbortEvent, 1)
	deprecatedCalled := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "abort",
		OnAbort: func(context.Context, []types.StepResult) {
			deprecatedCalled = true
		},
		OnAbortEvent: func(ctx context.Context, e GenerateTextAbortEvent) {
			eventCh <- e
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	select {
	case e := <-eventCh:
		if e.CallID == "" {
			t.Error("GenerateTextAbortEvent.CallID is empty")
		}
		if e.Reason == nil {
			t.Error("GenerateTextAbortEvent.Reason is nil")
		}
	case <-time.After(time.Second):
		t.Fatal("OnAbortEvent was not called")
	}
	if deprecatedCalled {
		t.Error("deprecated OnAbort was called even though OnAbortEvent is set")
	}
	_ = result
}

func TestStreamTextRejectsIncompleteMetadataOnlyStream(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock"}},
			}), nil
		},
	}

	var onError error
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
		OnError: func(_ context.Context, err error) {
			onError = err
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	err = result.ensureConsumed()
	if err == nil {
		t.Fatal("ensureConsumed() expected incomplete stream error")
	}
	if !IsNoOutputGeneratedError(err) {
		t.Fatalf("ensureConsumed() error = %T %v, want NoOutputGeneratedError", err, err)
	}
	if onError == nil || !IsNoOutputGeneratedError(onError) {
		t.Fatalf("OnError = %T %v, want NoOutputGeneratedError", onError, onError)
	}
}

func TestStreamTextIncompleteMetadataOnlyStreamEmitsErrorChunk(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock"}},
			}), nil
		},
	}

	var chunks []provider.StreamChunk
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	err = result.ensureConsumed()
	if err == nil || !IsNoOutputGeneratedError(err) {
		t.Fatalf("ensureConsumed() error = %T %v, want NoOutputGeneratedError", err, err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected error chunk")
	}
	got := chunks[len(chunks)-1]
	if got.Type != provider.ChunkTypeError {
		t.Fatalf("last chunk type = %q, want error", got.Type)
	}
	if got.Text != "No output generated. The model stream ended without a finish chunk." {
		t.Fatalf("error chunk text = %q", got.Text)
	}
}

func TestStreamTextAllowsIncompleteStreamWithPartialOutput(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				{Type: provider.ChunkTypeText, ID: "1", Text: ", world"},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if text != "Hello, world" {
		t.Fatalf("text = %q, want Hello, world", text)
	}
	if result.FinishReason() != types.FinishReasonOther {
		t.Fatalf("finishReason = %q, want other", result.FinishReason())
	}
}

// TestStreamText_PublishesEmptyStringPartialOutput ports TS's "stream null
// and empty-string JSON partial outputs" case (audit row 84f5d1b / WG4): a
// genuine first empty-string partial must be published, not suppressed by
// comparing against the "nothing published yet" sentinel.
func TestStreamText_PublishesEmptyStringPartialOutput(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: ""},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
		Output: TextOutput(),
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	partial := result.PartialOutput()
	s, ok := partial.(string)
	if !ok {
		t.Fatalf("PartialOutput() = %#v (%T), want the published empty string, not the unpublished zero value", partial, partial)
	}
	if s != "" {
		t.Fatalf("PartialOutput() = %q, want empty string", s)
	}
}

func TestStreamTextIncompletePartialOutputEmitsFinishStep(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			}), nil
		},
	}

	var chunks []provider.StreamChunk
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if err := result.ensureConsumed(); err != nil {
		t.Fatalf("ensureConsumed() error = %v", err)
	}
	var sawFinish bool
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkTypeFinish {
			sawFinish = true
			if chunk.FinishReason != types.FinishReasonOther {
				t.Fatalf("synthetic finish reason = %q, want other", chunk.FinishReason)
			}
		}
	}
	if !sawFinish {
		t.Fatalf("chunks did not include finish-step equivalent: %#v", chunks)
	}
}

func TestStreamTextErrorChunkIsTerminalForIncompleteStreamDetection(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock"}},
				{Type: provider.ChunkTypeError, Text: "chunk error"},
			}), nil
		},
	}

	var onError error
	var chunks []provider.StreamChunk
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
		OnError: func(_ context.Context, err error) {
			onError = err
		},
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
		OnFinish: func(*StreamTextResult) {},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if err := result.ensureConsumed(); err != nil {
		t.Fatalf("ensureConsumed() error = %T %v, want nil", err, err)
	}
	if onError == nil || onError.Error() != "chunk error" {
		t.Fatalf("OnError = %v, want chunk error", onError)
	}
	if result.FinishReason() != types.FinishReasonError {
		t.Fatalf("finishReason = %q, want error", result.FinishReason())
	}
	var sawError bool
	var sawFinishAfterError bool
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkTypeError {
			sawError = true
			continue
		}
		if sawError && chunk.Type == provider.ChunkTypeFinish {
			sawFinishAfterError = true
			if chunk.FinishReason != types.FinishReasonError {
				t.Fatalf("synthetic finish reason = %q, want error", chunk.FinishReason)
			}
			break
		}
	}
	if !sawFinishAfterError {
		t.Fatalf("chunks after error did not include finish-step equivalent: %#v", chunks)
	}
}

func TestStreamTextStepTimeoutCoversStalledStreamRead(t *testing.T) {
	stepTimeout := 20 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &blockingTextStream{done: make(chan struct{})}, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, err = result.ReadAll()
	if err == nil {
		t.Fatal("ReadAll() expected step timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep {
		t.Fatalf("ReadAll() error = %T %v, want step TimeoutError", err, err)
	}
}

func TestStreamTextStepTimeoutCallsAbortNotError(t *testing.T) {
	stepTimeout := 20 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedEOFTextStream{delay: 100 * time.Millisecond}, nil
		},
	}

	var onError error
	abortCalled := make(chan struct{}, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
		OnError: func(_ context.Context, err error) {
			onError = err
		},
		OnAbort: func(context.Context, []types.StepResult) {
			abortCalled <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	err = result.ensureConsumed()
	if err == nil {
		t.Fatal("ensureConsumed() expected step timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep {
		t.Fatalf("ensureConsumed() error = %T %v, want step TimeoutError", err, err)
	}
	if onError != nil {
		t.Fatalf("OnError = %v, want nil for abort/timeout", onError)
	}
	select {
	case <-abortCalled:
	default:
		t.Fatal("OnAbort was not called")
	}
}

func TestStreamTextStepTimeoutEmitsAbortChunk(t *testing.T) {
	stepTimeout := 20 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedEOFTextStream{delay: 100 * time.Millisecond}, nil
		},
	}

	var chunks []provider.StreamChunk
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
		OnAbort: func(context.Context, []types.StepResult) {},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	err = result.ensureConsumed()
	if err == nil {
		t.Fatal("ensureConsumed() expected step timeout")
	}
	if len(chunks) == 0 {
		t.Fatal("expected abort chunk")
	}
	got := chunks[len(chunks)-1]
	if got.Type != provider.ChunkTypeAbort {
		t.Fatalf("last chunk type = %q, want abort", got.Type)
	}
	if got.AbortReason == "" {
		t.Fatal("abort reason is empty")
	}
}

func TestStreamTextStepTimeoutDuringToolExecutionCallsAbortNotError(t *testing.T) {
	stepTimeout := 20 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID:        "call-1",
					ToolName:  "slow",
					Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	var onError error
	abortCalled := make(chan struct{}, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
		Tools: []types.Tool{{
			Name: "slow",
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}},
		OnError: func(_ context.Context, err error) {
			onError = err
		},
		OnAbort: func(context.Context, []types.StepResult) {
			abortCalled <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	err = result.ensureConsumed()
	if err == nil {
		t.Fatal("ensureConsumed() expected step timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep {
		t.Fatalf("ensureConsumed() error = %T %v, want step TimeoutError", err, err)
	}
	if onError != nil {
		t.Fatalf("OnError = %v, want nil for abort/timeout", onError)
	}
	select {
	case <-abortCalled:
	default:
		t.Fatal("OnAbort was not called")
	}
}

func TestStreamTextStepTimeoutCoversInitialDoStream(t *testing.T) {
	stepTimeout := 20 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
	})
	if err != nil {
		t.Fatalf("unexpected error starting stream: %v", err)
	}
	// The first provider request (and the step timeout that covers it) now
	// happens in the background, so wait for it via ReadAll instead of
	// checking StreamText's return value.
	_, err = result.ReadAll()
	if err == nil {
		t.Fatal("StreamText() expected step timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep {
		t.Fatalf("StreamText() error = %T %v, want step TimeoutError", err, err)
	}
}

type blockingTextStream struct {
	done chan struct{}
}

func (s *blockingTextStream) Next() (*provider.StreamChunk, error) {
	<-s.done
	return nil, io.EOF
}

func (s *blockingTextStream) Close() error {
	close(s.done)
	return nil
}

func (s *blockingTextStream) Err() error {
	return nil
}

type delayedEOFTextStream struct {
	delay time.Duration
}

func (s *delayedEOFTextStream) Next() (*provider.StreamChunk, error) {
	time.Sleep(s.delay)
	return nil, io.EOF
}

func (s *delayedEOFTextStream) Close() error {
	return nil
}

func (s *delayedEOFTextStream) Err() error {
	return nil
}

// --- Regression tests: StreamText without callbacks must still execute
// tools and run later steps (bug confirmed in
// state/parity/sep_23_2026/review-p0-1-p0-2-round1.md and HANDOFF.md item 2).
//
// Before the fix, processStream (which executes accumulated tool calls and
// starts subsequent steps) only ran when the caller registered a callback
// such as OnChunk or OnFinish. StreamTextResult.Stream() and .ReadAll()
// instead read the raw single-step provider stream directly, so a caller
// using only Stream()/ReadAll()/Chunks() (the plain Go-idiomatic API, no
// callbacks) never saw tools execute or a second step run — diverging from
// the TypeScript SDK, where the step loop and tool execution always run
// regardless of how the consumer reads the stream.

// TestStreamTextResult_NoCallbacksToolsExecuteAndContinue verifies that,
// with zero callbacks configured, calling only ReadAll() still executes
// tool calls and continues into a second model step.
func TestStreamTextResult_NoCallbacksToolsExecuteAndContinue(t *testing.T) {
	t.Parallel()

	var doStreamCalls int32
	var executeCalls int32

	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(&executeCalls, 1)
			return "sunny", nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			call := atomic.AddInt32(&doStreamCalls, 1)
			if call == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "Checking weather..."},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "get_weather",
						Arguments: map[string]interface{}{"city": "NY"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "It's sunny in NY."},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	// Deliberately no callbacks of any kind — the plain Go-idiomatic API.
	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "What's the weather in NY?",
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if got := atomic.LoadInt32(&executeCalls); got != 1 {
		t.Fatalf("expected tool Execute to run exactly once, got %d", got)
	}
	if got := atomic.LoadInt32(&doStreamCalls); got != 2 {
		t.Fatalf("expected DoStream to be called twice (step 1 + step 2), got %d", got)
	}
	// r.text accumulates across every step, matching processStream's
	// existing multi-step behavior.
	if want := "Checking weather...It's sunny in NY."; text != want {
		t.Fatalf("expected accumulated text across both steps %q, got %q", want, text)
	}
	if len(result.ToolCalls()) != 1 {
		t.Fatalf("expected 1 recorded tool call, got %d", len(result.ToolCalls()))
	}
	if len(result.ToolResults()) != 1 {
		t.Fatalf("expected 1 recorded tool result, got %d", len(result.ToolResults()))
	}
	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d: %+v", len(steps), steps)
	}
}

// TestStreamTextResult_NoCallbacksStreamYieldsFullMultiStep verifies that,
// with zero callbacks configured, Stream() (not ReadAll) yields the entire
// multi-step chunk sequence — including the tool-call, the tool-result
// produced by executing it, and the second step's text — rather than only
// the first step's raw provider chunks.
func TestStreamTextResult_NoCallbacksStreamYieldsFullMultiStep(t *testing.T) {
	t.Parallel()

	var doStreamCalls int32
	var executeCalls int32

	tool := types.Tool{
		Name: "get_weather",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(&executeCalls, 1)
			return "sunny", nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if atomic.AddInt32(&doStreamCalls, 1) == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "step2-text"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	// No callbacks — consume exclusively via Stream().
	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "weather?",
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	stream := result.Stream()
	var sawToolCall, sawToolResult, sawStep2Text bool
	var finishCount, finishStepCount int
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeToolCall:
			sawToolCall = true
		case provider.ChunkTypeToolResult:
			sawToolResult = true
		case provider.ChunkTypeText:
			if chunk.Text == "step2-text" {
				sawStep2Text = true
			}
		case provider.ChunkTypeFinish:
			finishCount++
		case provider.ChunkTypeFinishStep:
			finishStepCount++
		}
	}

	if !sawToolCall {
		t.Error("expected Stream() to include the tool-call chunk")
	}
	if !sawToolResult {
		t.Error("expected Stream() to include the tool-result chunk produced by executing the tool")
	}
	if !sawStep2Text {
		t.Error("expected Stream() to include step 2's text chunk")
	}
	// Exactly one call-level finish chunk (mirrors TS's single top-level
	// 'finish' fullStream part), plus one finish-step chunk per step.
	if finishCount != 1 {
		t.Errorf("expected 1 finish chunk (call-level), got %d", finishCount)
	}
	if finishStepCount != 2 {
		t.Errorf("expected 2 finish-step chunks (one per step), got %d", finishStepCount)
	}
	if got := atomic.LoadInt32(&executeCalls); got != 1 {
		t.Fatalf("expected tool Execute to run exactly once, got %d", got)
	}
	if got := atomic.LoadInt32(&doStreamCalls); got != 2 {
		t.Fatalf("expected DoStream to be called twice, got %d", got)
	}

	// ReadAll (or any other accessor) must still work after Stream() has been
	// fully drained — it just waits for the already-running processStream to
	// finish.
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() after Stream() error = %v", err)
	}
}

// TestStreamTextResult_NoCallbacksChunksChannelYieldsFullMultiStep is the
// Chunks()-channel counterpart of the Stream() test above.
func TestStreamTextResult_NoCallbacksChunksChannelYieldsFullMultiStep(t *testing.T) {
	t.Parallel()

	var doStreamCalls int32
	var executeCalls int32

	tool := types.Tool{
		Name: "noop",
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(&executeCalls, 1)
			return "ok", nil
		},
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if atomic.AddInt32(&doStreamCalls, 1) == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "noop", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	var sawToolResult bool
	for chunk := range result.Chunks() {
		if chunk.Type == provider.ChunkTypeToolResult {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Error("expected Chunks() to include the tool-result chunk")
	}
	if got := atomic.LoadInt32(&executeCalls); got != 1 {
		t.Fatalf("expected tool Execute to run exactly once, got %d", got)
	}
	if got := atomic.LoadInt32(&doStreamCalls); got != 2 {
		t.Fatalf("expected DoStream to be called twice, got %d", got)
	}
}

// TestStreamText_PanickingOnChunkDoesNotAbortStream ports TS's callback
// exception containment (audit row 9a37469 / WG5): a panicking OnChunk must
// not kill the stream-processing goroutine, and the full text/finish must
// still be produced.
func TestStreamText_PanickingOnChunkDoesNotAbortStream(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Hello, "},
				{Type: provider.ChunkTypeText, Text: "world!"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var onErrorCalls int
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		OnChunk: func(chunk provider.StreamChunk) {
			panic("boom from OnChunk")
		},
		OnError: func(ctx context.Context, err error) {
			onErrorCalls++
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if text != "Hello, world!" {
		t.Fatalf("text = %q, want %q", text, "Hello, world!")
	}
	if result.FinishReason() != types.FinishReasonStop {
		t.Fatalf("FinishReason() = %q, want stop", result.FinishReason())
	}
	// The panic must not be misreported as a stream error.
	if onErrorCalls != 0 {
		t.Fatalf("onErrorCalls = %d, want 0", onErrorCalls)
	}
}

// TS: streamText defaults to stopWhen = isStepCount(1), like generateText.
func TestStreamText_DefaultStopWhenIsOneStep(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call_1", ToolName: "tool", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Loop",
		Tools: []types.Tool{{Name: "tool", Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_ = result.Text()
	if steps := result.Steps(); len(steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(steps))
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 model call, got %d", calls)
	}
}

// TS's UI message chunk schema only accepts these finish reasons; a "finish"
// chunk with anything else fails validation in useChat.
var tsFinishReasons = map[string]bool{
	"stop": true, "length": true, "content-filter": true, "tool-calls": true, "error": true, "other": true,
}

func TestStreamText_ToolApprovalPauseKeepsModelFinishReasonInUIStream(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		Name:         "danger",
		ToolApproval: true,
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("tool awaiting approval should not execute")
			return nil, nil
		},
	}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call_1", ToolName: "danger", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{IsStepCount(5)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks, errs := CreateUIMessageStream(context.Background(), result)
	var finishes []interface{}
	for chunk := range chunks {
		if chunk["type"] == "finish" || chunk["type"] == "finish-step" {
			if reason, ok := chunk["finishReason"]; ok {
				finishes = append(finishes, reason)
			}
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("ui stream error: %v", err)
		}
	}
	if len(finishes) == 0 {
		t.Fatal("expected a finish chunk with a finishReason")
	}
	for _, reason := range finishes {
		if s, _ := reason.(string); !tsFinishReasons[s] {
			t.Fatalf("finishReason %v is not a TS FinishReason", reason)
		}
	}
	if got := result.FinishReason(); got != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason() = %q, want %q", got, types.FinishReasonToolCalls)
	}
}
