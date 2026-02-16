package ai

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	chunkCallbackCount := 0
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnChunk: func(chunk provider.StreamChunk) {
			chunkCallbackCount++
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for stream processing
	time.Sleep(100 * time.Millisecond)

	if chunkCallbackCount != 3 { // 2 text chunks + 1 finish chunk
		t.Errorf("expected 3 chunk callbacks, got %d", chunkCallbackCount)
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

	finishCalled := false
	var capturedResult *StreamTextResult

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnFinish: func(result *StreamTextResult) {
			finishCalled = true
			capturedResult = result
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for stream processing
	time.Sleep(100 * time.Millisecond)

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
	if usage.InputTokens != expectedUsage.InputTokens {
		t.Errorf("unexpected input tokens: %d", usage.InputTokens)
	}
	if usage.OutputTokens != expectedUsage.OutputTokens {
		t.Errorf("unexpected output tokens: %d", usage.OutputTokens)
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

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Hello",
	})

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
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeText {
			// Expected text chunk in test

			// Just process the chunk
		}
	}
}
