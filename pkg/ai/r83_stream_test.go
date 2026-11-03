package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r83streamDelayedEOF is a delayedTextStream that waits before reporting
// io.EOF after its last chunk.
type r83streamDelayedEOF struct {
	delayedTextStream
	eofDelay time.Duration
}

func (s *r83streamDelayedEOF) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		time.Sleep(s.eofDelay)
	}
	return s.delayedTextStream.Next()
}

// TS stream-text.ts clears the first-chunk and chunk timers when the model
// call ends (model-call-end, emitted for the provider finish part), so a
// pause between the finish part and the end of the stream is not a timeout.
func TestR83streamFinishPartStopsChunkTimers(t *testing.T) {
	limit := 40 * time.Millisecond
	for name, tc := range map[string]*TimeoutConfig{
		"PerChunk":   {PerChunk: &limit},
		"FirstChunk": {FirstChunk: &limit},
	} {
		t.Run(name, func(t *testing.T) {
			var chunks []delayedChunk
			if tc.PerChunk != nil {
				chunks = append(chunks, delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "a"}})
			}
			chunks = append(chunks, delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}})
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &r83streamDelayedEOF{delayedTextStream: delayedTextStream{chunks: chunks}, eofDelay: 150 * time.Millisecond}, nil
			}}
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Timeout: tc, OnError: func(context.Context, error) {}})
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			if r.Err() != nil {
				t.Fatalf("Err() = %v, want none after the finish part", r.Err())
			}
			if got := r.FinishReason(); got != types.FinishReasonStop {
				t.Fatalf("finish reason = %q, want stop", got)
			}
		})
	}
}

func r83streamToolChunks() []delayedChunk {
	return []delayedChunk{
		{chunk: provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "slow", Arguments: map[string]interface{}{}}}},
		{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}},
	}
}

func r83streamChunkTypes(t *testing.T, r *StreamTextResult) []provider.ChunkType {
	t.Helper()
	var types []provider.ChunkType
	for c := range r.Chunks() {
		types = append(types, c.Type)
	}
	return types
}

// stream-text-timeout.test.ts 466: should clear chunkMs before a long
// streaming tool (streamRetries 0 and 1): firstChunkMs 50 / chunkMs 50 and a
// tool that yields 4 values 40ms apart; the model call is not aborted, there
// are 5 tool results (4 preliminary, 1 final) and the stream ends with
// finish-step, finish.
func TestR83streamChunkTimeoutClearedBeforeLongStreamingTool(t *testing.T) {
	for _, retries := range []int{0, 1} {
		retries := retries
		t.Run(fmt.Sprintf("streamRetries=%d", retries), func(t *testing.T) {
			var signal context.Context
			model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				signal = ctx
				return &delayedTextStream{chunks: r83streamToolChunks()}, nil
			}}
			limit := 50 * time.Millisecond
			r, err := StreamText(context.Background(), StreamTextOptions{
				Model:         model,
				Prompt:        "test",
				StreamRetries: &retries,
				Timeout:       &TimeoutConfig{FirstChunk: &limit, PerChunk: &limit},
				Tools: []types.Tool{{
					Name:       "slow",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: types.StreamingExecutor(func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
						return func(yield func(interface{}, error) bool) {
							for i := 0; i < 4; i++ {
								select {
								case <-time.After(40 * time.Millisecond):
								case <-ctx.Done():
									yield(nil, ctx.Err())
									return
								}
								if !yield(i, nil) {
									return
								}
							}
						}
					}),
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			parts := r83streamChunkTypes(t, r)
			// Go releases the model call's ctx when the step ends (cancel);
			// TS signal.aborted false means no timeout fired on it.
			if errors.Is(signal.Err(), context.DeadlineExceeded) {
				t.Errorf("model call ctx timed out: %v", signal.Err())
			}
			results := 0
			for _, p := range parts {
				if p == provider.ChunkTypeToolResult {
					results++
				}
			}
			if results != 5 {
				t.Errorf("tool-result parts = %d, want 5 (%v)", results, parts)
			}
			if n := len(parts); n < 2 || parts[n-2] != provider.ChunkTypeFinishStep || parts[n-1] != provider.ChunkTypeFinish {
				t.Errorf("last parts = %v, want [finish-step finish]", parts)
			}
			if r.Err() != nil {
				t.Errorf("Err() = %v", r.Err())
			}
		})
	}
}

// stream-text-timeout.test.ts 504: should keep stepMs active during local
// tool execution: stepMs 100 with firstChunkMs/chunkMs 50 and a 200ms tool;
// the step times out (not the chunk timer) and the stream ends with abort.
func TestR83streamStepTimeoutActiveDuringToolExecution(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &delayedTextStream{chunks: r83streamToolChunks()}, nil
	}}
	step, limit := 100*time.Millisecond, 50*time.Millisecond
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{PerStep: &step, FirstChunk: &limit, PerChunk: &limit},
		OnError: func(context.Context, error) {},
		Tools: []types.Tool{{
			Name:       "slow",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-time.After(200 * time.Millisecond):
					return "done", nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := r83streamChunkTypes(t, r)
	var te *TimeoutError
	if !errors.As(r.Err(), &te) || te.Reason != TimeoutReasonStep {
		t.Errorf("Err() = %v, want a step TimeoutError", r.Err())
	}
	if n := len(parts); n == 0 || parts[n-1] != provider.ChunkTypeAbort {
		t.Errorf("parts = %v, want the last to be abort", parts)
	}
}

// stream-text-timeout.test.ts 545: should keep toolMs active after the model
// finishes: toolMs 100 with firstChunkMs/chunkMs 50 and a 200ms tool; the
// model call is not aborted, the tool errors and the stream ends with finish.
func TestR83streamToolTimeoutAfterModelFinish(t *testing.T) {
	var signal context.Context
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		signal = ctx
		return &delayedTextStream{chunks: r83streamToolChunks()}, nil
	}}
	tool, limit := 100*time.Millisecond, 50*time.Millisecond
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "test",
		Timeout: &TimeoutConfig{ToolMs: &tool, FirstChunk: &limit, PerChunk: &limit},
		Tools: []types.Tool{{
			Name:       "slow",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-time.After(200 * time.Millisecond):
					return "done", nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []provider.ChunkType
	sawToolError := false
	for c := range r.Chunks() {
		parts = append(parts, c.Type)
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && c.ToolResult.Error != nil {
			sawToolError = true
		}
	}
	// Go releases the model call's ctx when the step ends (cancel); TS
	// signal.aborted false means no timeout fired on it.
	if errors.Is(signal.Err(), context.DeadlineExceeded) {
		t.Errorf("model call ctx timed out: %v", signal.Err())
	}
	if !sawToolError {
		t.Errorf("parts = %v, want a tool-error", parts)
	}
	if n := len(parts); n == 0 || parts[n-1] != provider.ChunkTypeFinish {
		t.Errorf("parts = %v, want the last to be finish", parts)
	}
}

// stream-translate.ts default branch: an unknown part type fails the stream
// with TS's error text, `Unsupported part type: ${part}` for a part object.
func TestR83streamTranslateUnsupportedPartTypeErrorText(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(context.Context, *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: "not-a-real-type"},
			), nil
		},
	}
	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = result.TranslationText()
	if err == nil || err.Error() != "Unsupported part type: [object Object]" {
		t.Fatalf("TranslationText() error = %v, want %q", err, "Unsupported part type: [object Object]")
	}
}

func r83streamModel(chunks ...provider.StreamChunk) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream(chunks), nil
	}}
}

func r83streamUsage() *types.Usage {
	in, out := int64(3), int64(10)
	return &types.Usage{InputTokens: &in, OutputTokens: &out}
}

// stream-text.test.ts 2851: should preserve provider metadata from empty text
// deltas: the empty delta with providerMetadata is forwarded, and the step's
// text part carries that metadata.
func TestR83streamEmptyTextDeltaProviderMetadata(t *testing.T) {
	meta := json.RawMessage(`{"testProvider":{"signature":"test-signature"}}`)
	r83streamSameJSON := func(a, b interface{}) bool {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		var va, vb interface{}
		_ = json.Unmarshal(ja, &va)
		_ = json.Unmarshal(jb, &vb)
		return reflect.DeepEqual(va, vb)
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r83streamModel(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "", ProviderMetadata: meta},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r83streamUsage()},
		),
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	sawEmpty := false
	for c := range r.Chunks() {
		if c.Type == provider.ChunkTypeText && c.Text == "" && c.ID == "1" && r83streamSameJSON(c.ProviderMetadata, meta) {
			sawEmpty = true
		}
	}
	if !sawEmpty {
		t.Error("the empty text delta with providerMetadata was not forwarded")
	}
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	found := false
	for _, p := range steps[0].Content {
		if tc, ok := p.(types.TextContent); ok && tc.Text == "Hello" && r83streamSameJSON(tc.ProviderMetadata, meta) {
			found = true
		}
	}
	if !found {
		t.Errorf("step content = %#v, want a text part Hello with the delta's providerMetadata", steps[0].Content)
	}
}

// stream-text.test.ts 3030: should call onFinish even when error chunk occurs
// mid-stream: onError gets the chunk error, onFinish gets finishReason
// error, text Hello and the usage.
func TestR83streamOnFinishAfterMidStreamErrorChunk(t *testing.T) {
	var mu sync.Mutex
	var gotErr error
	var fin *OnFinishEvent
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r83streamModel(
			provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("chunk error")},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError, Usage: r83streamUsage()},
		),
		Prompt: "test-input",
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			gotErr = err
			mu.Unlock()
		},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			fin = &e
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	if gotErr == nil || gotErr.Error() != "chunk error" {
		t.Errorf("onError error = %v, want chunk error", gotErr)
	}
	if fin == nil {
		t.Fatal("onFinish was not called")
	}
	if fin.FinishReason != types.FinishReasonError || fin.Text != "Hello" {
		t.Errorf("onFinish finishReason = %q text = %q, want error / Hello", fin.FinishReason, fin.Text)
	}
	if fin.Usage.GetInputTokens() != 3 || fin.Usage.GetOutputTokens() != 10 || fin.Usage.TotalTokens == nil || *fin.Usage.TotalTokens != 13 {
		t.Errorf("onFinish usage = %+v, want 3/10/13", fin.Usage)
	}
}

// stream-text.test.ts 4755: should not call onStepError when the step
// completes.
func TestR83streamNoStepErrorOnCompletedStep(t *testing.T) {
	called := false
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r83streamModel(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r83streamUsage()},
		),
		Prompt:      "test-input",
		OnStepError: func(context.Context, OnStepErrorEvent) { called = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if r.Text() != "Hello, world!" {
		t.Errorf("text = %q", r.Text())
	}
	if called {
		t.Error("onStepError was called for a completed step")
	}
}

// stream-text.test.ts 4879: should resolve with partial output when the
// provider stream closes before the finish chunk after producing output:
// text "Hello, world", finishReason other, one step, no onError.
func TestR83streamPartialOutputWithoutFinish(t *testing.T) {
	onErr := false
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r83streamModel(
			provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: ", world"},
		),
		Prompt:  "test-input",
		OnError: func(context.Context, error) { onErr = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if r.Text() != "Hello, world" {
		t.Errorf("text = %q", r.Text())
	}
	if r.FinishReason() != types.FinishReasonOther {
		t.Errorf("finishReason = %q, want other", r.FinishReason())
	}
	if n := len(r.Steps()); n != 1 {
		t.Errorf("steps = %d, want 1", n)
	}
	if onErr {
		t.Error("onError was called")
	}
	if r.Err() != nil {
		t.Errorf("Err() = %v", r.Err())
	}
}
