package ai

import (
	"context"
	"errors"
	"io"
	"sync"
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

	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "use a tool",
		ToolChoice: types.RequiredToolChoice(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedChoice.Type != types.ToolChoiceRequired {
		t.Errorf("expected ToolChoiceRequired forwarded to provider, got %q", capturedChoice.Type)
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

	// Wait for the goroutine to finish.
	time.Sleep(100 * time.Millisecond)

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

// TestStreamTextToolsExecutedAfterStreamEnd verifies that tool Execute() is NOT
// called while the stream is in progress, only after all chunks are consumed.
// With Gap 3, tool-result chunks are forwarded to OnChunk AFTER execute fires.
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
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "What's the weather?",
		Tools:  []types.Tool{tool},
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
	// Gap 3: a tool-result chunk must appear after execute.
	if toolResultIdx == -1 {
		t.Error("expected tool-result chunk to be forwarded to OnChunk after execute (Gap 3)")
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
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Run all tools",
		Tools:  tools,
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
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "weather?",
		Tools:  []types.Tool{tool},
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
		Model:  model,
		Prompt: "Think then answer",
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
