package ai

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// ---- steering interrupt in StreamText (TS e4039889ba,
// stream-text-steering-interrupt.test.ts) ----

// port2streamHangingStream delivers its chunks, then waits: for the chunks of
// release (then it ends), until its context is cancelled (it fails with the
// cause, like a provider stream whose request was aborted), or until it is
// closed.
type port2streamHangingStream struct {
	ctx       context.Context
	mu        sync.Mutex
	chunks    []provider.StreamChunk
	release   chan []provider.StreamChunk
	released  bool
	closed    chan struct{}
	closeOnce sync.Once
	body      interface{}
}

func port2streamHang(ctx context.Context, release chan []provider.StreamChunk, chunks ...provider.StreamChunk) *port2streamHangingStream {
	return &port2streamHangingStream{ctx: ctx, chunks: chunks, release: release, closed: make(chan struct{})}
}

func (s *port2streamHangingStream) Next() (*provider.StreamChunk, error) {
	for {
		s.mu.Lock()
		if len(s.chunks) > 0 {
			c := s.chunks[0]
			s.chunks = s.chunks[1:]
			s.mu.Unlock()
			return &c, nil
		}
		released := s.released
		s.mu.Unlock()
		if released {
			return nil, io.EOF
		}
		select {
		case <-s.ctx.Done():
			return nil, context.Cause(s.ctx)
		case <-s.closed:
			return nil, io.EOF
		case more := <-s.release:
			s.mu.Lock()
			s.chunks, s.released = more, true
			s.mu.Unlock()
		}
	}
}

func (s *port2streamHangingStream) Err() error { return nil }
func (s *port2streamHangingStream) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}
func (s *port2streamHangingStream) RequestBody() interface{} { return s.body }

// port2streamBodyStream is a finished stream that reports a request body.
type port2streamBodyStream struct {
	provider.TextStream
	body interface{}
}

func (s port2streamBodyStream) RequestBody() interface{} { return s.body }

type port2streamCalls struct {
	mu   sync.Mutex
	opts []*provider.GenerateOptions
	ctxs []context.Context
}

func (c *port2streamCalls) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.opts)
}

// port2streamModel is a model whose call n is answered by call(n, ctx).
func port2streamModel(calls *port2streamCalls, call func(n int, ctx context.Context) (provider.TextStream, error)) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls.mu.Lock()
			calls.opts = append(calls.opts, opts)
			calls.ctxs = append(calls.ctxs, ctx)
			n := len(calls.opts)
			calls.mu.Unlock()
			return call(n, ctx)
		},
	}
}

func port2streamUsage(output int64) *types.Usage {
	return &types.Usage{InputTokens: int64Ptr(3), OutputTokens: &output}
}

func port2streamTextChunks(text string) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "2"},
		{Type: provider.ChunkTypeText, ID: "2", Text: text},
		{Type: provider.ChunkTypeTextEnd, ID: "2"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: port2streamUsage(10)},
	}
}

func port2streamLastUserText(opts *provider.GenerateOptions) string {
	messages := opts.Prompt.Messages
	if len(messages) == 0 || messages[len(messages)-1].Role != types.RoleUser {
		return ""
	}
	return userText(messages[len(messages)-1])
}

func port2streamUserTexts(opts *provider.GenerateOptions) []string {
	var out []string
	for _, m := range opts.Prompt.Messages {
		if m.Role == types.RoleUser {
			out = append(out, userText(m))
		}
	}
	return out
}

func port2streamRoles(opts *provider.GenerateOptions) []string {
	var out []string
	for _, m := range opts.Prompt.Messages {
		out = append(out, string(m.Role))
	}
	return out
}

// port2streamCollect reads the full stream.
func port2streamCollect(t *testing.T, result *StreamTextResult) []provider.StreamChunk {
	t.Helper()
	var out []provider.StreamChunk
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, *chunk)
	}
}

func port2streamTypes(chunks []provider.StreamChunk) []provider.ChunkType {
	var out []provider.ChunkType
	for _, c := range chunks {
		out = append(out, c.Type)
	}
	return out
}

func port2streamCount(chunks []provider.StreamChunk, chunkType provider.ChunkType) int {
	n := 0
	for _, c := range chunks {
		if c.Type == chunkType {
			n++
		}
	}
	return n
}

const port2streamSteerText = "Use Paris, Texas instead."

// TS: should cancel a model stream without output and redo it in the same
// step with the message
func TestPort2streamInterruptCancelsAndRedoesTheStep(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	chunks := port2streamCollect(t, result)
	if calls.len() != 2 || calls.ctxs[0].Err() == nil {
		t.Fatalf("calls = %d, first call cancelled = %v", calls.len(), calls.ctxs[0].Err() != nil)
	}
	if got := port2streamLastUserText(calls.opts[1]); got != port2streamSteerText {
		t.Fatalf("second call last user text = %q", got)
	}
	if n := port2streamCount(chunks, provider.ChunkTypeStartStep); n != 1 {
		t.Fatalf("start-step parts = %d", n)
	}
	if n := port2streamCount(chunks, provider.ChunkTypeFinishStep); n != 1 {
		t.Fatalf("finish-step parts = %d", n)
	}
	if n := port2streamCount(chunks, provider.ChunkTypeError); n != 0 {
		t.Fatalf("error parts = %d", n)
	}
	var steeringParts []provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSteeringMessage {
			steeringParts = append(steeringParts, c)
		}
	}
	if len(steeringParts) != 1 || *steeringParts[0].StepNumber != 0 || userText(*steeringParts[0].SteeringMessage) != port2streamSteerText {
		t.Fatalf("steering parts = %+v", steeringParts)
	}
	if result.Text() != "It is sunny in Paris, Texas." || len(result.Steps()) != 1 {
		t.Fatalf("text = %q, steps = %d", result.Text(), len(result.Steps()))
	}
	// the cancelled stream is left out of the usage
	if u := result.TotalUsage(); u.OutputTokens == nil || *u.OutputTokens != 10 {
		t.Fatalf("total usage = %+v", u)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should close text and reasoning parts the ui saw before the interrupt
// and keep them as empty ui message parts
func TestPort2streamInterruptClosesPartsTheUISaw(t *testing.T) {
	steering := NewSteering()
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			return port2streamHang(ctx, nil,
				provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
				provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "0"},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}), nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	chunksCh, errCh := result.ToUIMessageStream(context.Background())
	var types_ []string
	var steered bool
	for chunk := range chunksCh {
		typ, _ := chunk["type"].(string)
		types_ = append(types_, typ)
		if typ == "text-start" && !steered {
			steered = true
			steering.Steer(port2streamSteerText, WithSteerID("steer-1"), WithSteerInterrupt())
		}
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	want := []string{"start", "start-step", "reasoning-start", "text-start", "text-end", "reasoning-end", "steering-message", "text-start", "text-delta", "text-end", "finish-step", "finish"}
	if !reflect.DeepEqual(types_, want) {
		t.Fatalf("ui chunk types = %v\nwant %v", types_, want)
	}
	steps := result.Steps()
	if len(steps) != 1 || len(steps[0].Content) != 1 {
		t.Fatalf("step content = %+v", steps[0].Content)
	}
	if text, ok := steps[0].Content[0].(types.TextContent); !ok || text.Text != "It is sunny in Paris, Texas." {
		t.Fatalf("step content = %+v", steps[0].Content)
	}
}

// TS: should send one start-step when the interrupt arrives after the ui saw
// the step start
func TestPort2streamInterruptSendsOneStartStep(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			return port2streamHang(ctx, nil,
				provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}), nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	var got []provider.ChunkType
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk.Type)
		if chunk.Type == provider.ChunkTypeTextStart && handle == nil {
			handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
		}
	}
	want := []provider.ChunkType{
		provider.ChunkTypeStart, provider.ChunkTypeStartStep, provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd,
		provider.ChunkTypeSteeringMessage, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd,
		provider.ChunkTypeFinishStep, provider.ChunkTypeFinish,
	}
	if calls.len() != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %d, parts = %v\nwant %v", calls.len(), got, want)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should use the request and warnings of the redo when the cancelled call
// already sent start-step
func TestPort2streamInterruptUsesRequestAndWarningsOfTheRedo(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			s := port2streamHang(ctx, nil,
				provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{{Type: "other", Details: "cancelled call"}}},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"})
			s.body = "cancelled request"
			return s, nil
		}
		chunks := port2streamTextChunks("It is sunny in Paris, Texas.")
		chunks[0].Warnings = []types.Warning{{Type: "other", Details: "redo call"}}
		return port2streamBodyStream{TextStream: testutil.NewMockTextStream(chunks), body: "redo request"}, nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		Include: &IncludeOptions{RequestBody: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Type == provider.ChunkTypeTextStart && handle == nil {
			handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
		}
	}
	steps := result.Steps()
	if calls.len() != 2 || len(steps) != 1 {
		t.Fatalf("calls = %d, steps = %d", calls.len(), len(steps))
	}
	if steps[0].Request.Body != "redo request" {
		t.Fatalf("step request body = %v", steps[0].Request.Body)
	}
	if len(steps[0].Warnings) != 1 || steps[0].Warnings[0].Details != "redo call" {
		t.Fatalf("step warnings = %+v", steps[0].Warnings)
	}
	if len(steps[0].Content) != 1 {
		t.Fatalf("step content = %+v", steps[0].Content)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should redo the step when the interrupt arrives while a stream retry
// waits for the provider
func TestPort2streamInterruptDuringStreamRetryRedoesTheStep(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		switch n {
		case 1:
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeError, Err: errors.New("overloaded"), Text: "overloaded"},
			}), nil
		case 2:
			handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
			<-ctx.Done()
			return nil, context.Cause(ctx)
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		StreamRetries: intPtr(1), OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks := port2streamCollect(t, result)
	if calls.len() != 3 || port2streamLastUserText(calls.opts[2]) != port2streamSteerText {
		t.Fatalf("calls = %d", calls.len())
	}
	if port2streamCount(chunks, provider.ChunkTypeError) != 0 || result.Text() != "It is sunny in Paris, Texas." {
		t.Fatalf("parts = %v, text = %q", port2streamTypes(chunks), result.Text())
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should keep steering messages that the step already delivered in the
// redo
func TestPort2streamInterruptKeepsMessagesTheStepDelivered(t *testing.T) {
	steering := NewSteering()
	earlier := steering.Steer("Answer in Celsius.")
	var interrupt *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			interrupt = steering.Steer(port2streamSteerText, WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is 22 degrees in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if got := port2streamUserTexts(calls.opts[1]); !reflect.DeepEqual(got, []string{"What is the weather in Paris?", "Answer in Celsius.", port2streamSteerText}) {
		t.Fatalf("second call users = %v", got)
	}
	messages := result.Steps()[0].ExperimentalSteeringMessages
	if len(messages) != 2 || userText(messages[0]) != "Answer in Celsius." || userText(messages[1]) != port2streamSteerText {
		t.Fatalf("steering messages = %+v", messages)
	}
	if o := waitOutcome(t, earlier); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("earlier outcome = %+v", o)
	}
	if o := waitOutcome(t, interrupt); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("interrupt outcome = %+v", o)
	}
}

// TS: should redo the step for every interrupt and keep the messages in order
func TestPort2streamInterruptRedoesTheStepForEveryInterrupt(t *testing.T) {
	steering := NewSteering()
	var mu sync.Mutex
	var outcomes []*SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n <= 2 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			h := steering.Steer("Correction "+string(rune('0'+n))+".", WithSteerInterrupt())
			mu.Lock()
			outcomes = append(outcomes, h)
			mu.Unlock()
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	chunks := port2streamCollect(t, result)
	if calls.len() != 3 || port2streamCount(chunks, provider.ChunkTypeStartStep) != 1 || port2streamCount(chunks, provider.ChunkTypeFinishStep) != 1 || len(result.Steps()) != 1 {
		t.Fatalf("calls = %d, parts = %v", calls.len(), port2streamTypes(chunks))
	}
	messages := result.Steps()[0].ExperimentalSteeringMessages
	if len(messages) != 2 || userText(messages[0]) != "Correction 1." || userText(messages[1]) != "Correction 2." {
		t.Fatalf("steering messages = %+v", messages)
	}
	for i, h := range outcomes {
		if o := waitOutcome(t, h); !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("outcome %d = %+v", i, o)
		}
	}
}

func port2streamWeatherCall() []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Paris"}, RawArguments: `{ "city": "Paris" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: port2streamUsage(5)},
	}
}

// TS: should redo a later step with the tool results of the step before it
func TestPort2streamInterruptRedoesALaterStepWithTheToolResults(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	var executions int
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		switch n {
		case 1:
			return testutil.NewMockTextStream(port2streamWeatherCall()), nil
		case 2:
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			handle = steering.Steer("Answer in Celsius.", WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is 22 degrees in Paris.")), nil
	})
	tools := []types.Tool{{
		Name: "weather",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executions++
			return map[string]interface{}{"temperature": 72}, nil
		},
	}}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", Tools: tools,
		StopWhen: []StopCondition{IsStepCount(3)}, ExperimentalSteering: steering,
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if calls.len() != 3 || executions != 1 {
		t.Fatalf("calls = %d, executions = %d", calls.len(), executions)
	}
	if got := port2streamRoles(calls.opts[2]); !reflect.DeepEqual(got, []string{"user", "assistant", "tool", "user"}) {
		t.Fatalf("third call roles = %v", got)
	}
	if port2streamLastUserText(calls.opts[2]) != "Answer in Celsius." || len(result.Steps()) != 2 {
		t.Fatalf("last user = %q, steps = %d", port2streamLastUserText(calls.opts[2]), len(result.Steps()))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should not deliver an interrupt message that prepareStep removes on the
// redo
func TestPort2streamInterruptPrepareStepRemovesTheMessage(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			var kept []types.Message
			for _, m := range step.Messages {
				if m.Role != types.RoleUser || userText(m) != port2streamSteerText {
					kept = append(kept, m)
				}
			}
			step.Messages = kept
			return step
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks := port2streamCollect(t, result)
	if calls.len() != 2 || port2streamLastUserText(calls.opts[1]) != "What is the weather in Paris?" {
		t.Fatalf("calls = %d", calls.len())
	}
	if port2streamCount(chunks, provider.ChunkTypeSteeringMessage) != 0 {
		t.Fatalf("parts = %v", port2streamTypes(chunks))
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should run prepareStep again for the redo, notify onStepStart once, and
// start two model calls
func TestPort2streamInterruptRunsPrepareStepAgainAndStartsTwoModelCalls(t *testing.T) {
	steering := NewSteering()
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			steering.Steer(port2streamSteerText, WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	var prepared []PrepareStepOptions
	var stepStarts, callStarts, callEnds int
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			prepared = append(prepared, step)
			return step
		},
		OnStepStart:              func(context.Context, OnStepStartEvent) { stepStarts++ },
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) { callStarts++ },
		OnLanguageModelCallEnd:   func(context.Context, LanguageModelCallEndEvent) { callEnds++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if len(prepared) != 2 {
		t.Fatalf("prepareStep calls = %d", len(prepared))
	}
	second := prepared[1]
	if second.StepNumber != 0 || len(second.ExperimentalSteeringMessages) != 1 || userText(second.ExperimentalSteeringMessages[0]) != port2streamSteerText {
		t.Fatalf("second prepareStep = step %d, steering %+v", second.StepNumber, second.ExperimentalSteeringMessages)
	}
	if len(prepared[0].ExperimentalSteeringMessages) != 0 {
		t.Fatalf("first prepareStep steering = %+v", prepared[0].ExperimentalSteeringMessages)
	}
	if stepStarts != 1 || callStarts != 2 || callEnds != 1 {
		t.Fatalf("onStepStart = %d, call starts = %d, call ends = %d", stepStarts, callStarts, callEnds)
	}
}

// TS: should run prepareStep for the redo with the instructions and contexts
// that the step started with
func TestPort2streamInterruptPrepareStepGetsTheStartInstructionsAndContexts(t *testing.T) {
	steering := NewSteering()
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			steering.Steer(port2streamSteerText, WithSteerInterrupt())
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	type input struct {
		instructions string
		runtime      interface{}
		tools        map[string]interface{}
	}
	var inputs []input
	instructions := "Answer briefly."
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Instructions: &instructions, Prompt: "What is the weather in Paris?",
		RuntimeContext:       map[string]interface{}{"prepareStepCalls": 0},
		ToolsContext:         map[string]interface{}{"weather": map[string]interface{}{"unit": "fahrenheit"}},
		ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			inputs = append(inputs, input{*step.Instructions, step.RuntimeContext, step.ToolsContext})
			next := *step.Instructions + " Use Celsius."
			step.Instructions = &next
			step.RuntimeContext = map[string]interface{}{"prepareStepCalls": len(inputs)}
			step.ToolsContext = map[string]interface{}{"weather": map[string]interface{}{"unit": "celsius"}}
			return step
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	start := input{"Answer briefly.", map[string]interface{}{"prepareStepCalls": 0}, map[string]interface{}{"weather": map[string]interface{}{"unit": "fahrenheit"}}}
	if len(inputs) != 2 || !reflect.DeepEqual(inputs[0], start) || !reflect.DeepEqual(inputs[1], start) {
		t.Fatalf("prepareStep inputs = %+v", inputs)
	}
	if got := calls.opts[1].Prompt.System; got != "Answer briefly. Use Celsius." {
		t.Fatalf("second call system = %q", got)
	}
}

// TS: should fall back to the next step when output already started
func TestPort2streamInterruptFallsBackAfterOutputStarted(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	release := make(chan []provider.StreamChunk, 1)
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			return port2streamHang(ctx, release,
				provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "It is "}), nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		StopWhen: []StopCondition{IsStepCount(3)},
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeText && handle == nil {
				handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
				release <- []provider.StreamChunk{
					{Type: provider.ChunkTypeText, ID: "1", Text: "sunny in Paris."},
					{Type: provider.ChunkTypeTextEnd, ID: "1"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: port2streamUsage(10)},
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if calls.ctxs[0].Err() != nil && !errors.Is(calls.ctxs[0].Err(), context.Canceled) {
		t.Fatalf("first call ctx = %v", calls.ctxs[0].Err())
	}
	if len(result.Steps()) != 2 {
		t.Fatalf("steps = %d", len(result.Steps()))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should fall back to the next step after a tool input started
func TestPort2streamInterruptFallsBackAfterToolInputStarted(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	release := make(chan []provider.StreamChunk, 1)
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			return port2streamHang(ctx, release,
				provider.StreamChunk{Type: provider.ChunkTypeStreamStart},
				provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather"}}), nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		StopWhen: []StopCondition{IsStepCount(3)},
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeToolInputStart && handle == nil {
				handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
				release <- []provider.StreamChunk{
					{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather"}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: port2streamUsage(10)},
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if calls.len() < 2 || calls.ctxs[0].Err() != nil && !errors.Is(calls.ctxs[0].Err(), context.Canceled) {
		t.Fatalf("calls = %d", calls.len())
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should treat a caller abort as an abort and not as an interrupt
func TestPort2streamInterruptCallerAbortIsNotAnInterrupt(t *testing.T) {
	steering := NewSteering()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var handle *SteerHandle
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, c context.Context) (provider.TextStream, error) {
		stream := port2streamHang(c, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
		handle = steering.Steer(port2streamSteerText, WithSteerInterrupt())
		cancel(errors.New("stopped by user"))
		return stream, nil
	})
	result, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := result.ReadAll()
	if calls.len() != 1 {
		t.Fatalf("calls = %d", calls.len())
	}
	if readErr == nil || !strings.Contains(readErr.Error(), "stopped by user") {
		t.Fatalf("error = %v", readErr)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringAborted {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should not cancel the model stream for a steer without interrupt
func TestPort2streamSteerWithoutInterruptDoesNotCancel(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	release := make(chan []provider.StreamChunk, 1)
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		if n == 1 {
			stream := port2streamHang(ctx, release, provider.StreamChunk{Type: provider.ChunkTypeStreamStart})
			handle = steering.Steer(port2streamSteerText)
			go func() {
				time.Sleep(5 * time.Millisecond)
				release <- []provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "Sunny."},
					{Type: provider.ChunkTypeTextEnd, ID: "1"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: port2streamUsage(10)},
				}
			}()
			return stream, nil
		}
		return testutil.NewMockTextStream(port2streamTextChunks("It is sunny in Paris, Texas.")), nil
	})
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		StopWhen: []StopCondition{IsStepCount(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	port2streamCollect(t, result)
	if len(result.Steps()) != 2 {
		t.Fatalf("steps = %d", len(result.Steps()))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

// TS: should keep the step timeout running across the redo
func TestPort2streamInterruptKeepsTheStepTimeoutRunning(t *testing.T) {
	steering := NewSteering()
	calls := &port2streamCalls{}
	model := port2streamModel(calls, func(n int, ctx context.Context) (provider.TextStream, error) {
		return port2streamHang(ctx, nil, provider.StreamChunk{Type: provider.ChunkTypeStreamStart}), nil
	})
	step := 400 * time.Millisecond
	start := time.Now()
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		Timeout: &TimeoutConfig{PerStep: &step},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(200*time.Millisecond, func() { steering.Steer(port2streamSteerText, WithSteerInterrupt()) })
	_, readErr := result.ReadAll()
	elapsed := time.Since(start)
	if calls.len() != 2 {
		t.Fatalf("calls = %d", calls.len())
	}
	var timeoutErr *TimeoutError
	if !errors.As(readErr, &timeoutErr) {
		t.Fatalf("error = %v", readErr)
	}
	// a timeout that started again with the redo would end after 600ms
	if elapsed < 380*time.Millisecond || elapsed > 560*time.Millisecond {
		t.Fatalf("the step ended after %v, want about 400ms", elapsed)
	}
}

// TS 7760e33531: the call-level finish part carries stoppedBy, the same value
// as the result and the onEnd event.
func TestPort2streamFinishChunkCarriesStoppedBy(t *testing.T) {
	call := func() provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}}
	}
	withExec := types.Tool{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}
	noExec := types.Tool{Name: "tool1"}
	toolStep := func() []provider.StreamChunk {
		return []provider.StreamChunk{call(), {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
	}
	textStep := func(reason types.FinishReason) []provider.StreamChunk {
		return []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "hi"}, {Type: provider.ChunkTypeFinish, FinishReason: reason}}
	}
	cases := []struct {
		name  string
		steps [][]provider.StreamChunk
		tool  types.Tool
		opts  func(*StreamTextOptions)
		want  StoppedBy
	}{
		{"final answer", [][]provider.StreamChunk{textStep(types.FinishReasonStop)}, withExec, nil, StoppedByModel},
		{"cut off by length", [][]provider.StreamChunk{textStep(types.FinishReasonLength)}, withExec, nil, StoppedByModel},
		{"step limit", [][]provider.StreamChunk{toolStep(), toolStep()}, withExec, func(o *StreamTextOptions) { o.StopWhen = []StopCondition{IsStepCount(1)} }, StoppedByStopCondition},
		{"tool without execute", [][]provider.StreamChunk{toolStep()}, noExec, nil, StoppedByToolWithoutExecute},
		{"needs approval", [][]provider.StreamChunk{toolStep()}, withExec, func(o *StreamTextOptions) {
			o.ToolApproval = map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval}
		}, StoppedByApproval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				mu.Lock()
				defer mu.Unlock()
				i := n
				n++
				if i >= len(tc.steps) {
					i = len(tc.steps) - 1
				}
				return testutil.NewMockTextStream(append([]provider.StreamChunk(nil), tc.steps[i]...)), nil
			}}
			opts := StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{tc.tool}}
			if tc.opts != nil {
				tc.opts(&opts)
			}
			result, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			chunks := port2streamCollect(t, result)
			var finishes []provider.StreamChunk
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeFinish {
					finishes = append(finishes, c)
				}
			}
			if len(finishes) != 1 || finishes[0].StoppedBy != string(tc.want) {
				t.Fatalf("finish parts = %+v, want one with StoppedBy %q", finishes, tc.want)
			}
			for _, c := range chunks {
				if c.Type != provider.ChunkTypeFinish && c.StoppedBy != "" {
					t.Fatalf("%s part carries StoppedBy %q", c.Type, c.StoppedBy)
				}
			}
		})
	}
}
