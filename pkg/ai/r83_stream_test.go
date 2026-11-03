package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
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

// stream-text.test.ts 10944: should fire the model-call callbacks before tool
// execution and step finish, and the model-call end event carries the call
// ID, the content (text then tool call), finish reason tool-calls and the
// response model ID.
func TestR83streamModelCallCallbacksBeforeToolExecution(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var ends []LanguageModelCallEndEvent
	push := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r83streamModel(
			provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "response-1", ModelID: "response-model", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Before tool."},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test-arg"}}},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r83streamUsage()},
		),
		Prompt: "test-input",
		Tools: []types.Tool{{
			Name:       "tool1",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
			Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return fmt.Sprint(in["value"]) + "-result", nil
			},
		}},
		OnStepStart:              func(context.Context, OnStepStartEvent) { push("onStepStart") },
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) { push("onLanguageModelCallStart") },
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			push("onLanguageModelCallEnd")
			mu.Lock()
			ends = append(ends, e)
			mu.Unlock()
		},
		OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { push("onToolExecutionStart") },
		OnToolExecutionEnd:   func(context.Context, OnToolCallFinishEvent) { push("onToolExecutionEnd") },
		OnStepFinish:         func(context.Context, types.StepResult, interface{}) { push("onStepFinish") },
		OnError:              func(context.Context, error) {},
		Internal:             &InternalOptions{GenerateCallID: func() string { return "test-telemetry-call-id" }},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	want := []string{"onStepStart", "onLanguageModelCallStart", "onLanguageModelCallEnd", "onToolExecutionStart", "onToolExecutionEnd", "onStepFinish"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("callback order = %v, want %v", order, want)
	}
	if len(ends) != 1 {
		t.Fatalf("model-call end events = %d", len(ends))
	}
	e := ends[0]
	if e.CallID != "test-telemetry-call-id" || e.FinishReason != types.FinishReasonToolCalls || e.ModelID != "response-model" || e.ResponseID != "response-1" {
		t.Errorf("end event = callId %q finish %q model %q response %q", e.CallID, e.FinishReason, e.ModelID, e.ResponseID)
	}
	if len(e.Content) != 2 {
		t.Fatalf("end content = %#v", e.Content)
	}
	if tc, ok := e.Content[0].(types.TextContent); !ok || tc.Text != "Before tool." {
		t.Errorf("content[0] = %#v", e.Content[0])
	}
	call, ok := e.Content[1].(types.ToolCallContent)
	if !ok || call.ToolCallID != "call-1" || call.ToolName != "tool1" || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"value": "test-arg"}) {
		t.Errorf("content[1] = %#v", e.Content[1])
	}
}

// r83streamValidateCase is one validate-ui-messages.test.ts case: the input
// messages and the TS expected messages (or an error prefix), compared as
// JSON values.
type r83streamValidateCase struct {
	line      int
	agent     bool
	tools     []types.Tool
	input     string
	want      string // expected messages JSON; "" means same as input
	wantError string // error prefix, when the case rejects
}

func r83streamArtifactTool() types.Tool {
	return types.Tool{Name: "create_artifact", Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"identifier": map[string]interface{}{"type": "string"},
			"type":       map[string]interface{}{"type": "string", "enum": []interface{}{"application/vnd.react", "text/html"}},
			"language":   map[string]interface{}{"type": "string"},
			"code":       map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"identifier", "type", "language", "code"},
	}}
}

// validate-ui-messages.test.ts 1566, 1729, 2035, 2146, 2173, 2206, 2248:
// persisted aborted tool history, schema-incompatible output-error input and
// terminal calls from unavailable agent tools become dynamic tool parts
// (dynamic: false); stale terminal input of an available agent tool is
// rejected; approval reasons and descriptors pass unchanged.
func TestR83streamValidateUIMessagesTSCases(t *testing.T) {
	foo := []types.Tool{testFooTool()}
	cases := []r83streamValidateCase{
		{line: 1566, tools: []types.Tool{r83streamArtifactTool()},
			input: `[{"id":"u1","role":"user","parts":[{"type":"text","text":"make me a chart"}]},
				{"id":"a1","role":"assistant","parts":[{"type":"step-start"},{"type":"tool-create_artifact","toolCallId":"toolu_demo_aborted","state":"output-available","input":{},"output":"{\"error\":\"Tool was aborted by the user.\"}"}]},
				{"id":"u2","role":"user","parts":[{"type":"text","text":"are you working?"}]}]`,
			want: `[{"id":"u1","role":"user","parts":[{"type":"text","text":"make me a chart"}]},
				{"id":"a1","role":"assistant","parts":[{"type":"step-start"},{"type":"dynamic-tool","dynamic":false,"toolName":"create_artifact","toolCallId":"toolu_demo_aborted","state":"output-available","input":{},"output":"{\"error\":\"Tool was aborted by the user.\"}"}]},
				{"id":"u2","role":"user","parts":[{"type":"text","text":"are you working?"}]}]`},
		{line: 1729, tools: foo,
			input: `[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"output-error","input":{"foo":123},"errorText":"AI_InvalidToolInputError","providerExecuted":false}]}]`,
			want:  `[{"id":"1","role":"assistant","parts":[{"type":"dynamic-tool","dynamic":false,"toolName":"foo","toolCallId":"1","state":"output-error","input":{"foo":123},"errorText":"AI_InvalidToolInputError","providerExecuted":false}]}]`},
		{line: 2035, agent: true, tools: foo,
			input: `[{"id":"1","role":"assistant","parts":[
				{"type":"tool-bar","toolCallId":"1","state":"output-available","input":{},"output":{"result":"success"}},
				{"type":"tool-bar","toolCallId":"2","state":"output-error","errorText":"Tool execution failed"},
				{"type":"tool-bar","toolCallId":"3","state":"output-denied","input":{},"approval":{"id":"approval-1","approved":false}}]}]`,
			want: `[{"id":"1","role":"assistant","parts":[
				{"type":"dynamic-tool","dynamic":false,"toolName":"bar","toolCallId":"1","state":"output-available","input":{},"output":{"result":"success"}},
				{"type":"dynamic-tool","dynamic":false,"toolName":"bar","toolCallId":"2","state":"output-error","errorText":"Tool execution failed"},
				{"type":"dynamic-tool","dynamic":false,"toolName":"bar","toolCallId":"3","state":"output-denied","input":{},"approval":{"id":"approval-1","approved":false}}]}]`},
		{line: 2146, agent: true, tools: foo,
			input:     `[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":123},"output":{"result":"success"}}]}]`,
			wantError: "Type validation failed for messages[0].parts[0].input"},
		{line: 2173, tools: foo,
			input: `[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":"bar"},"output":{"result":"ok"},"approval":{"id":"approval-1","approved":true,"isAutomatic":true,"reason":"trusted internal tool"}}]}]`},
		{line: 2206, tools: foo,
			input: `[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"approval-requested","input":{"foo":"bar"},"approval":{"id":"approval-1","requestReason":"requires operator review"}},
				{"type":"tool-foo","toolCallId":"2","state":"approval-responded","input":{"foo":"baz"},"approval":{"id":"approval-2","approved":true,"requestReason":"requires security review","reason":"approved by operator"}}]}]`},
		{line: 2248, tools: foo,
			input: `[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"approval-requested","input":{"foo":"bar"},"approval":{"id":"approval-1","descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}}},
				{"type":"tool-foo","toolCallId":"2","state":"approval-responded","input":{"foo":"baz"},"approval":{"id":"approval-2","approved":true,"descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}}}]}]`},
	}
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprint(c.line), func(t *testing.T) {
			opts := ValidateUIMessagesOptions{Messages: json.RawMessage(c.input), Tools: c.tools}
			var msgs []UIMessage
			var err error
			if c.agent {
				msgs, err = ValidateUIMessagesForAgent(context.Background(), opts)
			} else {
				msgs, err = ValidateUIMessages(context.Background(), opts)
			}
			if c.wantError != "" {
				if err == nil || !strings.HasPrefix(err.Error(), c.wantError) {
					t.Fatalf("TS %d error = %v, want prefix %q", c.line, err, c.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("TS %d: %v", c.line, err)
			}
			want := c.want
			if want == "" {
				want = c.input
			}
			var gotV, wantV interface{}
			b, _ := json.Marshal(msgs)
			_ = json.Unmarshal(b, &gotV)
			if err := json.Unmarshal([]byte(want), &wantV); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotV, wantV) {
				t.Errorf("TS %d messages differ\n got: %s\nwant: %s", c.line, b, want)
			}
		})
	}
}
