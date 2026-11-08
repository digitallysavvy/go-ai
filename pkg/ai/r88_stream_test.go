package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// ---------------------------------------------------------------------------
// TS 9754014925: ping parts
// ---------------------------------------------------------------------------

func r88streamModel(chunks ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	call := 0
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		i := call
		call++
		if i >= len(chunks) {
			i = len(chunks) - 1
		}
		return testutil.NewMockTextStream(chunks[i]), nil
	}}
}

func r88streamStart(t *testing.T, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func r88streamParts(r *StreamTextResult) []provider.StreamChunk {
	var parts []provider.StreamChunk
	for c := range r.Chunks() {
		parts = append(parts, c)
	}
	return parts
}

func r88streamCount(parts []provider.StreamChunk, typ provider.ChunkType) int {
	n := 0
	for _, p := range parts {
		if p.Type == typ {
			n++
		}
	}
	return n
}

func r88streamUIChunks(t *testing.T, r *StreamTextResult) []UIMessageChunk {
	t.Helper()
	ch, errCh := r.ToUIMessageStream(context.Background())
	var out []UIMessageChunk
	for c := range ch {
		out = append(out, c)
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

var r88streamFinishStop = provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}}

// stream-text.test.ts 'ping forwarding' > should forward ping chunks without
// enabling raw chunks.
// Appendix A: stream-text.test.ts 24638 (TS 0f3a85f92d).
func TestR88streamPingForwardedWithoutRawChunks(t *testing.T) {
	r := r88streamStart(t, StreamTextOptions{
		Model:  r88streamModel([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypePing}, r88streamFinishStop}),
		Prompt: "test prompt",
	})
	parts := r88streamParts(r)
	if n := r88streamCount(parts, provider.ChunkTypePing); n != 1 {
		t.Fatalf("ping parts = %d, want 1: %v", n, parts)
	}
	if c := r.Content(); len(c) != 0 {
		t.Fatalf("content = %#v, want empty", c)
	}
	for _, c := range r88streamUIChunks(t, r) {
		if c["type"] == "ping" {
			t.Fatalf("UI stream has a ping chunk: %v", c)
		}
	}
}

// r88streamBlockingStream is a stream whose chunks are released one by one.
type r88streamBlockingStream struct {
	ch chan provider.StreamChunk
}

func (s *r88streamBlockingStream) Next() (*provider.StreamChunk, error) {
	c, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return &c, nil
}
func (s *r88streamBlockingStream) Err() error   { return nil }
func (s *r88streamBlockingStream) Close() error { return nil }

// stream-text.test.ts 'ping forwarding' > does not hold heartbeats behind
// buffered tool input during retries (TS stream-text.ts 2885-2890).
// Appendix A: stream-text.test.ts 24668 (TS 0f3a85f92d).
func TestR88streamPingNotHeldBehindBufferedToolInput(t *testing.T) {
	stream := &r88streamBlockingStream{ch: make(chan provider.StreamChunk, 8)}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypeStreamStart}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool"}}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypePing}
	sawPing := make(chan struct{})
	var once sync.Once
	retries := 1
	r := r88streamStart(t, StreamTextOptions{
		Model:         &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return stream, nil }},
		Prompt:        "test",
		StreamRetries: &retries,
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypePing {
				once.Do(func() { close(sawPing) })
			}
		},
	})
	done := make(chan error, 1)
	go func() { done <- r.ConsumeStream() }()
	select {
	case <-sawPing:
	case <-time.After(5 * time.Second):
		close(stream.ch)
		<-done
		t.Fatal("ping was held behind the buffered tool input")
	}
	stream.ch <- r88streamFinishStop
	close(stream.ch)
	<-done
	if c := r.Content(); len(c) != 0 {
		t.Fatalf("content = %#v, want empty", c)
	}
}

// stream-text.test.ts 'ping forwarding' > delivers a heartbeat once while
// discarding failed-attempt tool input and content.
// Appendix A: stream-text.test.ts 24707 (TS 0f3a85f92d).
func TestR88streamPingDeliveredOnceAcrossStreamRetry(t *testing.T) {
	retries := 0
	streamRetries := 1
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "failed"},
				{Type: provider.ChunkTypeText, ID: "failed", Text: "partial"},
				{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool"}},
				{Type: provider.ChunkTypePing},
				{Type: provider.ChunkTypeError, Err: errors.New("provider error"), Text: "provider error"},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "recovered"},
			{Type: provider.ChunkTypeText, ID: "recovered", Text: "recovered"},
			{Type: provider.ChunkTypeTextEnd, ID: "recovered"},
			r88streamFinishStop,
		}), nil
	}}
	r := r88streamStart(t, StreamTextOptions{
		Model: model, Prompt: "test", MaxRetries: &retries, StreamRetries: &streamRetries,
		OnError: func(context.Context, error) {},
	})
	parts := r88streamParts(r)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if n := r88streamCount(parts, provider.ChunkTypePing); n != 1 {
		t.Fatalf("ping parts = %d, want 1", n)
	}
	if n := r88streamCount(parts, provider.ChunkTypeToolInputStart); n != 0 {
		t.Fatalf("tool-input-start parts = %d, want 0", n)
	}
	if got := r.Text(); got != "recovered" {
		t.Fatalf("text = %q", got)
	}
	content := r.Content()
	if len(content) != 1 {
		t.Fatalf("content = %#v", content)
	}
	if text, ok := content[0].(types.TextContent); !ok || text.Text != "recovered" {
		t.Fatalf("content = %#v", content)
	}
}

// stream-text.test.ts 'ping forwarding' > does not treat a ping-only
// incomplete stream as generated output.
// Appendix A: stream-text.test.ts 24757 (TS 0f3a85f92d).
func TestR88streamPingOnlyStreamIsNoOutput(t *testing.T) {
	r := r88streamStart(t, StreamTextOptions{
		Model:   r88streamModel([]provider.StreamChunk{{Type: provider.ChunkTypePing}}),
		Prompt:  "test",
		OnError: func(context.Context, error) {},
	})
	parts := r88streamParts(r)
	if r88streamCount(parts, provider.ChunkTypePing) != 1 {
		t.Fatalf("parts = %v, want a ping", parts)
	}
	found := false
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError && IsNoOutputGeneratedError(streamChunkError(&p)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("parts = %v, want a NoOutputGeneratedError error part", parts)
	}
	if err := r.OutputErr(); !IsNoOutputGeneratedError(err) {
		t.Fatalf("OutputErr = %v, want NoOutputGeneratedError", err)
	}
}

// stream-text-timeout.test.ts it.each(['raw', 'ping']) should not reset
// chunkMs for non-output chunks: the ping at 40ms does not extend the 50ms
// chunk timeout, so the stream times out before the chunk at 70ms.
func TestR88streamPingDoesNotResetChunkTimeout(t *testing.T) {
	var signal context.Context
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		signal = ctx
		return &delayedTextStream{chunks: []delayedChunk{
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}},
			{delay: 40 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypePing}},
			{delay: 30 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: " world"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}},
			{chunk: r88streamFinishStop},
		}}, nil
	}}
	limit := 50 * time.Millisecond
	r := r88streamStart(t, StreamTextOptions{Model: model, Prompt: "test-input", Timeout: &TimeoutConfig{PerChunk: &limit}, OnError: func(context.Context, error) {}})
	_ = r.ConsumeStream()
	var timeoutErr *TimeoutError
	if !errors.As(r.Err(), &timeoutErr) {
		t.Fatalf("Err = %v, want a TimeoutError", r.Err())
	}
	if signal == nil || signal.Err() == nil {
		t.Fatal("model call context was not cancelled")
	}
}

// smooth-stream.test.ts 'forwards pings without flushing a partial word'.
func TestR88streamSmoothStreamForwardsPingWithoutFlushing(t *testing.T) {
	zero := 0
	smooth, err := SmoothStream(SmoothStreamOptions{DelayInMs: &zero})
	if err != nil {
		t.Fatal(err)
	}
	var out []provider.StreamChunk
	for _, c := range []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hel"},
		{Type: provider.ChunkTypePing},
		{Type: provider.ChunkTypeText, ID: "1", Text: "lo "},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	} {
		out = append(out, smooth(context.Background(), c)...)
	}
	var got []string
	for _, c := range out {
		got = append(got, string(c.Type)+":"+c.Text)
	}
	want := []string{"text-start:", "ping:", "text:Hello ", "text-end:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunks = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// TS 1a1da8d68c: steering messages streamed to the UI
// ---------------------------------------------------------------------------

func r88streamWeatherTool(onRun func()) []types.Tool {
	return []types.Tool{{Name: "weather", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		onRun()
		return "sunny", nil
	}}}
}

var r88streamToolCallChunks = []provider.StreamChunk{
	{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Paris"}, RawArguments: `{"city":"Paris"}`}},
	{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
}

func r88streamTextChunks(text string) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: text},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		r88streamFinishStop,
	}
}

func r88streamTypes(parts []provider.StreamChunk) []string {
	var out []string
	for _, p := range parts {
		s := string(p.Type)
		if p.Type == provider.ChunkTypeSteeringMessage {
			step := -1
			if p.StepNumber != nil {
				step = *p.StepNumber
			}
			s += ":" + p.ID + ":" + strconv.Itoa(step)
		}
		out = append(out, s)
	}
	return out
}

// stream-text-steering.test.ts 'steering message stream part' > should
// stream a delivered message before the output of its step.
// Appendix A: stream-text-steering.test.ts 700 (TS 0f3a85f92d).
func TestR88streamSteeringPartBeforeItsStep(t *testing.T) {
	steering := NewSteering()
	r := r88streamStart(t, StreamTextOptions{
		Model:  r88streamModel(r88streamToolCallChunks, r88streamTextChunks("It is sunny in Paris, Texas.")),
		Prompt: "What is the weather in Paris?",
		Tools: r88streamWeatherTool(func() {
			steering.Steer("I meant Paris, Texas.", WithSteerID("steer-1"))
		}),
		StopWhen:             []StopCondition{IsStepCount(5)},
		ExperimentalSteering: steering,
	})
	parts := r88streamParts(r)
	want := []string{"start", "start-step", "tool-call", "tool-result", "finish-step", "steering-message:steer-1:1",
		"start-step", "text-start", "text", "text-end", "finish-step", "finish"}
	if got := r88streamTypes(parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %q\nwant %q", got, want)
	}
	for _, p := range parts {
		if p.Type != provider.ChunkTypeSteeringMessage {
			continue
		}
		if p.SteeringMessage == nil || p.SteeringMessage.Role != types.RoleUser || userText(*p.SteeringMessage) != "I meant Paris, Texas." {
			t.Fatalf("steering message = %#v", p.SteeringMessage)
		}
	}
}

// ... should stream a message sent before the stream starts before the first
// step.
// Appendix A: stream-text-steering.test.ts 756 (TS 0f3a85f92d).
func TestR88streamSteeringPartBeforeFirstStep(t *testing.T) {
	steering := NewSteering()
	steering.Steer("Use Celsius.", WithSteerID("steer-1"))
	r := r88streamStart(t, StreamTextOptions{
		Model:                r88streamModel(r88streamTextChunks("It is 20 degrees.")),
		Prompt:               "What is the weather in Paris?",
		ExperimentalSteering: steering,
	})
	want := []string{"start", "steering-message:steer-1:0", "start-step", "text-start", "text", "text-end", "finish-step", "finish"}
	if got := r88streamTypes(r88streamParts(r)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %q\nwant %q", got, want)
	}
}

// ... should stream queued messages when they are delivered.
func TestR88streamSteeringPartForQueuedMessage(t *testing.T) {
	steering := NewSteering()
	steering.Queue("Then update the readme.", WithSteerID("queued-1"))
	r := r88streamStart(t, StreamTextOptions{
		Model:                r88streamModel(r88streamTextChunks("It is sunny in Paris."), r88streamTextChunks("The readme is updated.")),
		Prompt:               "What is the weather in Paris?",
		StopWhen:             []StopCondition{IsStepCount(5)},
		ExperimentalSteering: steering,
	})
	want := []string{"start", "start-step", "text-start", "text", "text-end", "finish-step", "steering-message:queued-1:1",
		"start-step", "text-start", "text", "text-end", "finish-step", "finish"}
	if got := r88streamTypes(r88streamParts(r)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %q\nwant %q", got, want)
	}
}

// ... should not stream messages that prepareStep removes.
func TestR88streamSteeringPartNotStreamedWhenPrepareStepRemovesIt(t *testing.T) {
	steering := NewSteering()
	r := r88streamStart(t, StreamTextOptions{
		Model:  r88streamModel(r88streamToolCallChunks, r88streamTextChunks("It is sunny in Paris.")),
		Prompt: "What is the weather in Paris?",
		Tools:  r88streamWeatherTool(func() { steering.Steer("I meant Paris, Texas.") }),
		PrepareStep: func(_ context.Context, opts PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{Messages: append(append([]types.Message(nil), opts.InitialMessages...), opts.ResponseMessages...)}
		},
		StopWhen:             []StopCondition{IsStepCount(5)},
		ExperimentalSteering: steering,
	})
	if n := r88streamCount(r88streamParts(r), provider.ChunkTypeSteeringMessage); n != 0 {
		t.Fatalf("steering-message parts = %d, want 0", n)
	}
}

func r88streamReadUIMessage(t *testing.T, r *StreamTextResult) UIMessage {
	t.Helper()
	chunks, _ := r.ToUIMessageStream(context.Background())
	messages, errCh := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: chunks})
	var last UIMessage
	for m := range messages {
		last = m
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	return last
}

// ... should keep provider options of delivered messages in converted model
// messages.
// Appendix A: stream-text-steering.test.ts 867 (TS 0f3a85f92d).
func TestR88streamSteeringProviderOptionsRoundTrip(t *testing.T) {
	steering := NewSteering()
	if _, err := steering.SteerMessage(types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{
		Text:            "Use Celsius.",
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	r := r88streamStart(t, StreamTextOptions{
		Model:                r88streamModel(r88streamTextChunks("It is 20 degrees.")),
		Prompt:               "What is the weather in Paris?",
		ExperimentalSteering: steering,
	})
	message := r88streamReadUIMessage(t, r)
	converted, err := ConvertToModelMessages(context.Background(), []UIMessage{message})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(converted[0])
	want := `{"role":"user","content":[{"type":"text","text":"Use Celsius.","providerOptions":{"anthropic":{"cacheControl":{"type":"ephemeral"}}}}]}`
	if string(got) != want {
		t.Fatalf("steering message = %s\nwant %s", got, want)
	}
}

// ... should add delivered messages to the ui message and keep them in
// converted model messages.
// Appendix A: stream-text-steering.test.ts 913 (TS 0f3a85f92d).
func TestR88streamSteeringUIMessageAndModelMessages(t *testing.T) {
	steering := NewSteering()
	var mu sync.Mutex
	var prompts [][]types.MessageRole
	model := &testutil.MockLanguageModel{DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		var roles []types.MessageRole
		for _, m := range opts.Prompt.Messages {
			roles = append(roles, m.Role)
		}
		prompts = append(prompts, roles)
		if len(prompts) == 1 {
			return testutil.NewMockTextStream(r88streamToolCallChunks), nil
		}
		return testutil.NewMockTextStream(r88streamTextChunks("It is sunny in Paris, Texas.")), nil
	}}
	r := r88streamStart(t, StreamTextOptions{
		Model:                model,
		Prompt:               "What is the weather in Paris?",
		Tools:                r88streamWeatherTool(func() { steering.Steer("I meant Paris, Texas.", WithSteerID("steer-1")) }),
		StopWhen:             []StopCondition{IsStepCount(5)},
		ExperimentalSteering: steering,
	})
	message := r88streamReadUIMessage(t, r)
	var partTypes []string
	for _, p := range message.Parts {
		partTypes = append(partTypes, p.UIPartType())
	}
	if want := []string{"step-start", "tool-weather", "steering-message", "step-start", "text"}; !reflect.DeepEqual(partTypes, want) {
		t.Fatalf("part types = %q, want %q", partTypes, want)
	}
	steeringJSON, _ := json.Marshal(message.Parts[2])
	if want := `{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas."}]}`; string(steeringJSON) != want {
		t.Fatalf("steering part = %s\nwant %s", steeringJSON, want)
	}
	modelMessages, err := ConvertToModelMessages(context.Background(), []UIMessage{
		{ID: "user-1", Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "What is the weather in Paris?"}}},
		message,
	})
	if err != nil {
		t.Fatal(err)
	}
	var roles []types.MessageRole
	for _, m := range modelMessages {
		roles = append(roles, m.Role)
	}
	if want := []types.MessageRole{types.RoleUser, types.RoleAssistant, types.RoleTool, types.RoleUser, types.RoleAssistant}; !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if got, _ := json.Marshal(modelMessages[3]); string(got) != `{"role":"user","content":[{"type":"text","text":"I meant Paris, Texas."}]}` {
		t.Fatalf("steering model message = %s", got)
	}
	// The model saw the same history before its final answer.
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || !reflect.DeepEqual(prompts[1], roles[:len(roles)-1]) {
		t.Fatalf("second prompt roles = %v, want %v", prompts, roles[:len(roles)-1])
	}
}

// tool-loop-agent-steering.test.ts 'should stream delivered messages in
// stream()': the ToolLoopAgent streams through StreamText, so a message
// steered during the first model call is streamed for step 1.
func TestR88streamSteeringSteeredDuringModelCall(t *testing.T) {
	steering := NewSteering()
	var mu sync.Mutex
	call := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		call++
		if call == 1 {
			steering.Steer("Also tell me about Tokyo.", WithSteerID("steer-1"))
			return testutil.NewMockTextStream(r88streamTextChunks("Sunny.")), nil
		}
		return testutil.NewMockTextStream(r88streamTextChunks("Tokyo is rainy.")), nil
	}}
	// ToolLoopAgent's default stop condition is isStepCount(20).
	r := r88streamStart(t, StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(20)}, ExperimentalSteering: steering})
	var steeringParts []provider.StreamChunk
	for _, p := range r88streamParts(r) {
		if p.Type == provider.ChunkTypeSteeringMessage {
			steeringParts = append(steeringParts, p)
		}
	}
	if len(steeringParts) != 1 || steeringParts[0].ID != "steer-1" || steeringParts[0].StepNumber == nil || *steeringParts[0].StepNumber != 1 ||
		userText(*steeringParts[0].SteeringMessage) != "Also tell me about Tokyo." {
		t.Fatalf("steering parts = %#v", steeringParts)
	}
}

// TS stream-text.ts: a steering message delivered to a step that fails
// before its model stream opens is still streamed. For the first step TS
// adds it to the stitchable stream before the pipeline catch adds the error
// part; for a later step the step catch enqueues the error into the previous
// step's stream, which the stitchable stream reads first.
func TestR88streamSteeringPartWhenTheStepFailsToOpen(t *testing.T) {
	t.Run("first step", func(t *testing.T) {
		steering := NewSteering()
		steering.Steer("Use Celsius.", WithSteerID("steer-1"))
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("open failed")
		}}
		retries := 0
		r := r88streamStart(t, StreamTextOptions{Model: model, Prompt: "p", MaxRetries: &retries, ExperimentalSteering: steering, OnError: func(context.Context, error) {}})
		want := []string{"start", "steering-message:steer-1:0", "error"}
		if got := r88streamTypes(r88streamParts(r)); !reflect.DeepEqual(got, want) {
			t.Fatalf("parts = %q\nwant %q", got, want)
		}
	})
	t.Run("later step", func(t *testing.T) {
		steering := NewSteering()
		var mu sync.Mutex
		call := 0
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			call++
			if call == 1 {
				return testutil.NewMockTextStream(r88streamToolCallChunks), nil
			}
			return nil, errors.New("open failed")
		}}
		retries := 0
		r := r88streamStart(t, StreamTextOptions{
			Model: model, Prompt: "p", MaxRetries: &retries,
			Tools:                r88streamWeatherTool(func() { steering.Steer("I meant Paris, Texas.", WithSteerID("steer-1")) }),
			StopWhen:             []StopCondition{IsStepCount(5)},
			ExperimentalSteering: steering, OnError: func(context.Context, error) {},
		})
		want := []string{"start", "start-step", "tool-call", "tool-result", "finish-step", "error", "steering-message:steer-1:1"}
		if got := r88streamTypes(r88streamParts(r)); !reflect.DeepEqual(got, want) {
			t.Fatalf("parts = %q\nwant %q", got, want)
		}
	})
	t.Run("later step settings", func(t *testing.T) {
		// prepareStepCallSettings fails after the steering messages were
		// resolved: TS step catch, error part first.
		steering := NewSteering()
		zero := 0
		r := r88streamStart(t, StreamTextOptions{
			Model:  r88streamModel(r88streamToolCallChunks, r88streamTextChunks("unused")),
			Prompt: "p",
			Tools:  r88streamWeatherTool(func() { steering.Steer("I meant Paris, Texas.", WithSteerID("steer-1")) }),
			PrepareStep: func(_ context.Context, opts PrepareStepOptions) PrepareStepOptions {
				if opts.StepNumber == 1 {
					return PrepareStepOptions{MaxOutputTokens: &zero}
				}
				return PrepareStepOptions{}
			},
			StopWhen:             []StopCondition{IsStepCount(5)},
			ExperimentalSteering: steering, OnError: func(context.Context, error) {},
		})
		want := []string{"start", "start-step", "tool-call", "tool-result", "finish-step", "error", "steering-message:steer-1:1"}
		if got := r88streamTypes(r88streamParts(r)); !reflect.DeepEqual(got, want) {
			t.Fatalf("parts = %q\nwant %q", got, want)
		}
	})
}

// to-ui-message-chunk.test.ts 'maps steering messages with text content',
// 'keeps provider options of steering message parts as provider metadata',
// 'maps files in steering messages to ui file parts'. The want strings are
// the TS chunks as JsonToSseTransformStream writes them (key order of
// convertUserMessageToUIParts).
func TestR88streamSteeringUIMessageChunk(t *testing.T) {
	step := 1
	for _, tc := range []struct {
		name    string
		message types.Message
		want    string
	}{
		{"text content", types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "I meant Paris, Texas."}}},
			`{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas."}]}`},
		{"provider options", types.Message{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "I meant Paris, Texas.", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/map.png"}, MediaType: "image/png", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"imageDetail": "low"}}},
		}}, `{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas.","providerMetadata":{"anthropic":{"cacheControl":{"type":"ephemeral"}}}},` +
			`{"type":"file","mediaType":"image/png","providerMetadata":{"openai":{"imageDetail":"low"}},"url":"https://example.com/map.png"}]}`},
		{"files", types.Message{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "Look at these."},
			types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png"},
			types.ImageContent{URL: "https://example.com/cat.png"},
			types.ImageContent{URL: "data:image/gif;base64,AQID"},
			types.FileContent{URL: "data:application/pdf;base64,AQID", MediaType: "application/pdf", Filename: "report.pdf"},
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "Hello"}, MediaType: "text/plain"},
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"openai": "file-1"}}, MediaType: "application/pdf"},
		}}, `{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"Look at these."},` +
			`{"type":"file","mediaType":"image/png","url":"data:image/png;base64,AQID"},` +
			`{"type":"file","mediaType":"image","url":"https://example.com/cat.png"},` +
			`{"type":"file","mediaType":"image/gif","url":"data:image/gif;base64,AQID"},` +
			`{"type":"file","mediaType":"application/pdf","filename":"report.pdf","url":"data:application/pdf;base64,AQID"},` +
			`{"type":"file","mediaType":"text/plain","url":"data:text/plain;base64,SGVsbG8="},` +
			`{"type":"file","mediaType":"application/pdf","url":"","providerReference":{"openai":"file-1"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := tc.message
			chunk, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeSteeringMessage, ID: "steer-1", SteeringMessage: &message, StepNumber: &step}, UIMessageStreamResultOptions{})
			if !ok {
				t.Fatal("no UI chunk")
			}
			event, err := uiSSEEventBytes(chunk)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(event); got != "data: "+tc.want+"\n\n" {
				t.Fatalf("event = %s\nwant  data: %s", got, tc.want)
			}
		})
	}
	if _, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypePing}, UIMessageStreamResultOptions{}); ok {
		t.Fatal("a ping has a UI chunk")
	}
}

// ui-message-chunks.test.ts 'accepts steering message chunks over SSE'.
func TestR88streamSteeringChunkOverSSE(t *testing.T) {
	raw := `{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas."},{"type":"file","mediaType":"image/png","url":"https://example.com/map.png"}]}`
	chunks, err := ReadUIMessageStream(strings.NewReader("data: " + raw + "\n\n"))
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks = %v, err = %v", chunks, err)
	}
	var want UIMessageChunk
	_ = json.Unmarshal([]byte(raw), &want)
	if !reflect.DeepEqual(chunks[0], want) {
		t.Fatalf("chunk = %v, want %v", chunks[0], want)
	}
}

// process-ui-message-stream.test.ts 'steering-message' > adds steering
// messages before the step that received them and keeps them when the step
// is reset.
// Appendix A: process-ui-message-stream.test.ts 282 (TS 0f3a85f92d).
func TestR88streamProcessSteeringChunkSurvivesReset(t *testing.T) {
	ch := make(chan UIMessageChunk, 32)
	for _, c := range []UIMessageChunk{
		{"type": "start-step"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "It is sunny."},
		{"type": "text-end", "id": "text-1"},
		{"type": "finish-step"},
		{"type": "steering-message", "id": "steer-1", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "I meant Paris, Texas."}}},
		{"type": "start-step"},
		{"type": "text-start", "id": "stale-text"},
		{"type": "text-delta", "id": "stale-text", "delta": "It is"},
		{"type": "reset-step"},
		{"type": "text-start", "id": "text-2"},
		{"type": "text-delta", "id": "text-2", "delta": "It is hot in Texas."},
		{"type": "text-end", "id": "text-2"},
		{"type": "finish-step"},
	} {
		ch <- c
	}
	close(ch)
	messages, errCh := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: ch, TerminateOnError: true})
	var last UIMessage
	for m := range messages {
		last = m
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _ := json.Marshal(last.Parts)
	want := `[{"type":"step-start"},{"type":"text","text":"It is sunny.","state":"done"},` +
		`{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas."}]},` +
		`{"type":"step-start"},{"type":"text","text":"It is hot in Texas.","state":"done"}]`
	if string(got) != want {
		t.Fatalf("parts = %s\nwant  %s", got, want)
	}
}

// convert-to-model-messages.test.ts 'steering messages'.
func TestR88streamConvertSteeringMessages(t *testing.T) {
	t.Run("between the steps of an assistant message", func(t *testing.T) {
		var messages []UIMessage
		if err := json.Unmarshal([]byte(`[
			{"id":"u","role":"user","parts":[{"type":"text","text":"What is the weather in Paris?"}]},
			{"id":"a","role":"assistant","parts":[
				{"type":"step-start"},
				{"type":"tool-weather","state":"output-available","toolCallId":"call-1","input":{"city":"Paris"},"output":"sunny"},
				{"type":"steering-message","id":"steer-1","parts":[
					{"type":"text","text":"I meant Paris, Texas."},
					{"type":"file","mediaType":"image/png","url":"https://example.com/map.png"},
					{"type":"file","mediaType":"application/pdf","url":"","providerReference":{"openai":"file-1"}}
				]},
				{"type":"step-start"},
				{"type":"text","text":"It is hot in Texas.","state":"done"}
			]}]`), &messages); err != nil {
			t.Fatal(err)
		}
		result, err := ConvertToModelMessages(context.Background(), messages)
		if err != nil {
			t.Fatal(err)
		}
		var roles []types.MessageRole
		for _, m := range result {
			roles = append(roles, m.Role)
		}
		if want := []types.MessageRole{types.RoleUser, types.RoleAssistant, types.RoleTool, types.RoleUser, types.RoleAssistant}; !reflect.DeepEqual(roles, want) {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
		steering := result[3].Content
		if len(steering) != 3 {
			t.Fatalf("steering content = %#v", steering)
		}
		if text, ok := steering[0].(types.TextContent); !ok || text.Text != "I meant Paris, Texas." {
			t.Fatalf("part 0 = %#v", steering[0])
		}
		if file, ok := steering[1].(types.FileContent); !ok || file.MediaType != "image/png" || file.FileData.Type != types.FileDataTypeURL || file.FileData.URL != "https://example.com/map.png" {
			t.Fatalf("part 1 = %#v", steering[1])
		}
		if file, ok := steering[2].(types.FileContent); !ok || file.MediaType != "application/pdf" || file.FileData.Type != types.FileDataTypeReference || file.FileData.Reference["openai"] != "file-1" {
			t.Fatalf("part 2 = %#v", steering[2])
		}
		if text, ok := result[4].Content[0].(types.TextContent); !ok || text.Text != "It is hot in Texas." {
			t.Fatalf("final assistant = %#v", result[4])
		}
	})
	t.Run("received before the first step", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{{ID: "a", Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
			SteeringMessageUIPart{ID: "steer-1", Parts: []UIMessagePart{TextUIPart{Text: "Use Celsius."}}},
			StepStartUIPart{},
			TextUIPart{Text: "It is 20 degrees.", State: UIPartStateDone},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 2 || result[0].Role != types.RoleUser || result[1].Role != types.RoleAssistant {
			t.Fatalf("result = %#v", result)
		}
	})
}

// validate-ui-messages.test.ts 'steering message parts'.
func TestR88streamValidateSteeringMessageParts(t *testing.T) {
	raw := `[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"steer-1","parts":[` +
		`{"type":"text","text":"I meant Paris, Texas."},` +
		`{"type":"file","mediaType":"application/pdf","url":"","providerReference":{"openai":"file-1"}}]},{"type":"step-start"}]}]`
	messages, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: raw})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(messages)
	if string(got) != raw {
		t.Fatalf("messages = %s\nwant %s", got, raw)
	}
	result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: `[{"id":"1","role":"assistant","parts":[` +
		`{"type":"steering-message","parts":[{"type":"text","text":"I meant Paris, Texas."}]}]}]`})
	if result.Success {
		t.Fatal("a steering message part without an id was accepted")
	}
	result = SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: `[{"id":"1","role":"assistant","parts":[` +
		`{"type":"steering-message","id":"s","parts":[{"type":"reasoning","text":"x"}]}]}]`})
	if result.Success {
		t.Fatal("a steering message part with a reasoning part was accepted")
	}
}

// process-ui-message-stream.ts 964-972: a steering-message chunk writes a
// message snapshot.
func TestR88streamSteeringChunkWritesSnapshot(t *testing.T) {
	ch := make(chan UIMessageChunk, 1)
	ch <- UIMessageChunk{"type": "steering-message", "id": "steer-1", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "Use Celsius."}}}
	close(ch)
	messages, _ := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: ch})
	var snapshots []UIMessage
	for m := range messages {
		snapshots = append(snapshots, m)
	}
	if len(snapshots) != 1 || len(snapshots[0].Parts) != 1 || snapshots[0].Parts[0].UIPartType() != "steering-message" {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}
