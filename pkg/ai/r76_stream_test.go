package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r76streamMeta is a typed message metadata value, as a Go caller writes it
// (TS: a plain object).
type r76streamMeta struct {
	CreatedAt string `json:"createdAt,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
}

// r76streamScore is a named number type: JSON number, not an object.
type r76streamScore int

// TS processUIMessageStream merges message metadata with
// mergeObjects(existing, incoming), which treats every object alike. A Go
// value that encodes as a JSON object (a struct, a typed map,
// json.RawMessage) is merged like a map[string]interface{}, also nested.
func TestR76StreamMetadataMergeTypedObjects(t *testing.T) {
	for _, c := range []struct {
		name     string
		existing interface{}
		incoming interface{}
		want     string
	}{
		{"struct then map", r76streamMeta{CreatedAt: "t0"}, map[string]interface{}{"model": "m"}, `{"createdAt":"t0","model":"m"}`},
		{"map then struct", map[string]interface{}{"model": "m"}, r76streamMeta{Tokens: 5}, `{"model":"m","tokens":5}`},
		{"struct then struct", r76streamMeta{CreatedAt: "t0"}, &r76streamMeta{Tokens: 5}, `{"createdAt":"t0","tokens":5}`},
		{"typed map then map", map[string]string{"a": "1"}, map[string]interface{}{"b": "2"}, `{"a":"1","b":"2"}`},
		{"raw then map", json.RawMessage(`{"a":{"x":1}}`), map[string]interface{}{"a": map[string]interface{}{"y": 2.0}}, `{"a":{"x":1,"y":2}}`},
		{"nested struct", map[string]interface{}{"usage": r76streamMeta{CreatedAt: "t0"}}, map[string]interface{}{"usage": map[string]interface{}{"tokens": 3.0}}, `{"usage":{"createdAt":"t0","tokens":3}}`},
		{"struct then named number", r76streamMeta{CreatedAt: "t0"}, r76streamScore(4), `{"createdAt":"t0"}`},
		{"struct then string", r76streamMeta{CreatedAt: "t0"}, "s", `"s"`},
		{"nil pointer then map", (*r76streamMeta)(nil), map[string]interface{}{"a": 1.0}, `{"a":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newUIMessageCallbackState(nil, "m")
			s.message["metadata"] = c.existing
			s.mergeMetadata(c.incoming)
			r75streamAssertJSON(t, "metadata", s.message["metadata"], c.want)
		})
	}

	// Through CreateUIMessageStreamWithOptions: the continued assistant
	// message has typed metadata, the start chunk adds a key, the finish
	// chunk another; onEnd's response message carries all three.
	var mu sync.Mutex
	var endMessage interface{}
	_, errs := r75streamRunUIChunks(t, []UIMessageChunk{
		{"type": "start", "messageMetadata": map[string]interface{}{"model": "m"}},
		{"type": "finish", "messageMetadata": r76streamMeta{Tokens: 7}},
	}, UIMessageStreamOptions{
		OriginalMessages: []UIMessageChunk{
			{"id": "u1", "role": "user", "parts": []interface{}{}},
			{"id": "a1", "role": "assistant", "parts": []interface{}{}, "metadata": r76streamMeta{CreatedAt: "t0"}},
		},
		OnEnd: func(event map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			endMessage = event["responseMessage"]
		},
	})
	if len(errs) != 0 {
		t.Fatalf("stream errors: %v", errs)
	}
	mu.Lock()
	defer mu.Unlock()
	msg, ok := endMessage.(UIMessageChunk)
	if !ok {
		t.Fatalf("response message is %T", endMessage)
	}
	r75streamAssertJSON(t, "onEnd metadata", msg["metadata"], `{"createdAt":"t0","model":"m","tokens":7}`)
}

// cr-r76-carry-to-stream.md: the WorkflowAgent option exists (honored by gen's
// tool executor and the legacy agent loop; behavior tests are gen's/carry's).
func TestR76StreamInternalClientToolResultsWithoutCallMetadataField(t *testing.T) {
	opts := InternalOptions{ClientToolResultsWithoutCallMetadata: true}
	if !opts.ClientToolResultsWithoutCallMetadata {
		t.Fatal("field not set")
	}
}

// r76streamPullStream serves chunks, then runs fail and returns its error
// (TS ReadableStream pull that aborts and errors the controller).
type r76streamPullStream struct {
	chunks []provider.StreamChunk
	fail   func() error
	i      int
}

func (s *r76streamPullStream) Next() (*provider.StreamChunk, error) {
	if s.i < len(s.chunks) {
		c := s.chunks[s.i]
		s.i++
		return &c, nil
	}
	return nil, s.fail()
}
func (s *r76streamPullStream) Err() error   { return nil }
func (s *r76streamPullStream) Close() error { return nil }

type r76streamRootTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	calls []string
	steps []int
}

func (r *r76streamRootTelemetry) OnAbort(_ context.Context, e telemetry.TelemetryAbortEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "onAbort")
	r.steps = append(r.steps, len(e.Steps))
}

func (r *r76streamRootTelemetry) OnEnd(context.Context, telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "onEnd")
}

func (r *r76streamRootTelemetry) OnError(context.Context, telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "onError")
}

// stream-text.test.ts 25660 / 25665 / 25678 (abort signal > basic abort):
// the signal fires while the model stream is read. onError is not called;
// onAbort gets {callId, reason, steps: []}; the telemetry integration gets
// onAbort with no steps and neither onEnd nor onError. (The full-stream and
// UI snapshots of that block depend on JS stream timing: TS skips them on
// newer Node versions.)
func TestR76StreamBasicAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tel := &r76streamRootTelemetry{}
	var mu sync.Mutex
	var onErrors []error
	var aborts []GenerateTextAbortEvent
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r76streamPullStream{
			chunks: []provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			},
			fail: func() error { cancel(); return context.Canceled },
		}, nil
	}}
	r, err := StreamText(ctx, StreamTextOptions{
		Model:  model,
		Prompt: "test-input",
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			onErrors = append(onErrors, err)
		},
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) {
			mu.Lock()
			defer mu.Unlock()
			aborts = append(aborts, e)
		},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
		Internal:  &InternalOptions{GenerateCallID: func() string { return "test-telemetry-call-id" }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConsumeStream error = %v, want context.Canceled", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(onErrors) != 0 {
		t.Errorf("onError calls = %v, want none", onErrors)
	}
	if len(aborts) != 1 || aborts[0].CallID != "test-telemetry-call-id" || len(aborts[0].Steps) != 0 || !errors.Is(aborts[0].Reason, context.Canceled) {
		t.Errorf("onAbort = %+v, want one {callId test-telemetry-call-id, steps [], reason canceled}", aborts)
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.calls) != 1 || tel.calls[0] != "onAbort" || tel.steps[0] != 0 {
		t.Errorf("telemetry = %v (steps %v), want [onAbort] with no steps", tel.calls, tel.steps)
	}
}

// stream-text.test.ts 25742 (abort signal > basic abort > should include
// abort reason when provided): the signal fires with a reason right after
// stream-start. The full stream is start then abort with the reason (no
// start-step: stream-start opens none), and so is the UI message stream.
func TestR76StreamAbortReasonAfterStreamStart(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r76streamPullStream{
			chunks: []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}},
			fail:   func() error { cancel(errors.New("manual abort")); return context.Canceled },
		}, nil
	}}
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	var parts []provider.StreamChunk
	s := r.NewStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	if len(parts) != 2 || parts[0].Type != provider.ChunkTypeStart || parts[1].Type != provider.ChunkTypeAbort || parts[1].AbortReason != "manual abort" {
		t.Fatalf("full stream = %+v, want [start, abort(manual abort)]", parts)
	}
	ch, errs := r.ToUIMessageStream(context.Background())
	var ui []UIMessageChunk
	for c := range ch {
		ui = append(ui, c)
	}
	for e := range errs {
		t.Fatalf("ui stream error: %v", e)
	}
	r75streamAssertJSON(t, "ui chunks", ui, `[{"type":"start"},{"type":"abort","reason":"manual abort"}]`)
}

const r76streamHelloUI = `[{"type":"start"},{"type":"start-step"},{"type":"text-start","id":"1"},` +
	`{"type":"text-delta","id":"1","delta":"Hello"},{"type":"text-delta","id":"1","delta":", "},` +
	`{"type":"text-delta","id":"1","delta":"world!"},{"type":"text-end","id":"1"},` +
	`{"type":"finish-step"},{"type":"finish","finishReason":"stop"}]`

// stream-text.test.ts 6767: an onFinish without generateMessageId adds no
// message id to the start chunk (handleUIMessageStreamFinish's messageId is
// responseMessageId ?? generateMessageId?.(), undefined here).
func TestR76StreamUIOnFinishWithoutGenerateMessageID(t *testing.T) {
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "prompt"},
		r76streamHelloUI, UIMessageStreamResultOptions{OnFinish: func(map[string]interface{}) {}})
}

// stream-text.test.ts 6820: generateMessageId gives the start chunk its id.
func TestR76StreamUIGenerateMessageID(t *testing.T) {
	n := 0
	want := `[{"type":"start","messageId":"message-0"}` + r76streamHelloUI[len(`[{"type":"start"}`):]
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "prompt"},
		want, UIMessageStreamResultOptions{GenerateMessageID: func() string {
			id := "message-" + string(rune('0'+n))
			n++
			return id
		}})
}

// stream-text.test.ts 6874 (Go form: the consumer cancels its context after
// four chunks instead of reader.cancel()): onFinish runs once with the
// partial assistant message, its text part still streaming, isAborted false.
func TestR76StreamUIOnFinishWhenConsumerCancels(t *testing.T) {
	gate := make(chan struct{})
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "msg-2", ModelID: "test-model"}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Streaming"},
		{Type: provider.ChunkTypeText, ID: "1", Text: " content"},
		{Type: provider.ChunkTypeText, ID: "1", Text: " that"},
	}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r76streamPullStream{chunks: step, fail: func() error { <-gate; return errors.New("released") }}, nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "Generate content", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { close(gate); _ = r.Close() }()
	var mu sync.Mutex
	var finishes []map[string]interface{}
	ctx, cancel := context.WithCancel(context.Background())
	ch, errs := r.ToUIMessageStream(ctx, UIMessageStreamResultOptions{OnFinish: func(e map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		finishes = append(finishes, e)
	}})
	for i := 0; i < 4; i++ {
		if _, ok := <-ch; !ok {
			t.Fatalf("stream ended after %d chunks", i)
		}
	}
	cancel()
	for range ch {
	}
	for range errs {
	}
	mu.Lock()
	defer mu.Unlock()
	if len(finishes) != 1 {
		t.Fatalf("onFinish calls = %d, want 1", len(finishes))
	}
	e := finishes[0]
	msg, _ := e["responseMessage"].(UIMessageChunk)
	if msg == nil || msg["role"] != "assistant" {
		t.Fatalf("responseMessage = %#v", e["responseMessage"])
	}
	var text UIMessageChunk
	for _, p := range uiParts(msg) {
		if part := asUIPartChunk(p); part["type"] == "text" {
			text = part
		}
	}
	if text == nil || !strings.Contains(stringValue(text["text"]), "Streaming") || text["state"] != "streaming" {
		t.Fatalf("text part = %#v, want partial streaming text", text)
	}
	if e["isAborted"] != false {
		t.Fatalf("isAborted = %v, want false", e["isAborted"])
	}
}

// stream-text.test.ts 7466 / 7489 (Go form, known difference "ConsumeStream
// returns the error"): a stream error during consumption does not panic; Go's
// ConsumeStream returns it (TS swallows it and passes it to onError).
func TestR76StreamConsumeStreamReturnsStreamError(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r76streamPullStream{
			chunks: []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}},
			fail:   func() error { return errors.New("Some error") },
		}, nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err == nil || err.Error() != "Some error" {
		t.Fatalf("ConsumeStream error = %v, want Some error", err)
	}
}

func r76streamTextChunks(delta string) []UIMessageChunk {
	return []UIMessageChunk{
		{"type": "text-start", "id": "1"},
		{"type": "text-delta", "id": "1", "delta": delta},
		{"type": "text-end", "id": "1"},
	}
}

// create-ui-message-stream.test.ts 492 / 551: onFinish and onEnd without
// original messages: one call with the generated response message id, the
// done text part, no continuation, no abort, outcome unknown and no finish
// reason (Go: nil).
func TestR76StreamCreateUIStreamOnFinishWithoutOriginalMessages(t *testing.T) {
	const want = `[{"finishReason":null,"isAborted":false,"isContinuation":false,` +
		`"messages":[{"id":"response-message-id","parts":[{"state":"done","text":"1a","type":"text"}],"role":"assistant"}],` +
		`"outcome":{"status":"unknown"},` +
		`"responseMessage":{"id":"response-message-id","parts":[{"state":"done","text":"1a","type":"text"}],"role":"assistant"}}]`
	for _, useOnEnd := range []bool{false, true} {
		var mu sync.Mutex
		var recorded []map[string]interface{}
		record := func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			recorded = append(recorded, e)
		}
		opts := UIMessageStreamOptions{GenerateMessageID: r75streamIDGen("response-message-id")}
		if useOnEnd {
			opts.OnEnd = record
		} else {
			opts.OnFinish = record
		}
		_, errs := r75streamRunUIChunks(t, r76streamTextChunks("1a"), opts)
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		mu.Lock()
		r75streamAssertJSON(t, "recorded", recorded, want)
		mu.Unlock()
	}
}

// create-ui-message-stream.test.ts 611: onFinish with original messages
// continues the last assistant message: the new text part is appended to it,
// isContinuation is true and the response message keeps id "1".
func TestR76StreamCreateUIStreamOnFinishWithMessages(t *testing.T) {
	var mu sync.Mutex
	var recorded []map[string]interface{}
	_, errs := r75streamRunUIChunks(t, r76streamTextChunks("1b"), UIMessageStreamOptions{
		OriginalMessages: []UIMessageChunk{
			{"id": "0", "role": "user", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "0a"}}},
			{"id": "1", "role": "assistant", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "1a", "state": "done"}}},
		},
		OnFinish: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			recorded = append(recorded, e)
		},
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	assistant := `{"id":"1","parts":[{"state":"done","text":"1a","type":"text"},{"state":"done","text":"1b","type":"text"}],"role":"assistant"}`
	mu.Lock()
	defer mu.Unlock()
	r75streamAssertJSON(t, "recorded", recorded, `[{"finishReason":null,"isAborted":false,"isContinuation":true,`+
		`"messages":[{"id":"0","parts":[{"text":"0a","type":"text"}],"role":"user"},`+assistant+`],`+
		`"outcome":{"status":"unknown"},"responseMessage":`+assistant+`}]`)
}

// create-ui-message-stream.test.ts 586: the consumer cancels after the start
// chunk, before an outcome is declared: onEnd runs once with isCancelled
// true, isAborted false and outcome unknown.
func TestR76StreamCreateUIStreamConsumerCancelBeforeOutcome(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	var ends []map[string]interface{}
	ctx, cancel := context.WithCancel(context.Background())
	out, errCh := CreateUIMessageStreamWithOptions(ctx, UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "start"})
			<-release
		},
		OnEnd: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			ends = append(ends, e)
		},
		GenerateMessageID: r75streamIDGen("response-message-id"),
	})
	go func() {
		defer close(done)
		for range errCh {
		}
	}()
	if _, ok := <-out; !ok {
		t.Fatal("no start chunk")
	}
	cancel()
	close(release)
	for range out {
	}
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	e := ends[0]
	if e["isAborted"] != false || e["isCancelled"] != true {
		t.Fatalf("isAborted = %v, isCancelled = %v; want false, true", e["isAborted"], e["isCancelled"])
	}
	r75streamAssertJSON(t, "outcome", e["outcome"], `{"status":"unknown"}`)
}

func r76streamChan(chunks ...UIMessageChunk) <-chan UIMessageChunk {
	ch := make(chan UIMessageChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// read-ui-message-stream.test.ts 647: with terminateOnError an `error` chunk
// ends reading with its text as the error.
func TestR76StreamReadUIMessagesTerminateOnError(t *testing.T) {
	msgs, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{
		Stream: r76streamChan(
			UIMessageChunk{"type": "start", "messageId": "msg-123"},
			UIMessageChunk{"type": "text-start", "id": "text-1"},
			UIMessageChunk{"type": "text-delta", "id": "text-1", "delta": "Hello"},
			UIMessageChunk{"type": "error", "errorText": "Test error message"},
		),
		TerminateOnError: true,
	})
	for range msgs {
	}
	var got []error
	for err := range errs {
		got = append(got, err)
	}
	if len(got) != 1 || got[0].Error() != "Test error message" {
		t.Fatalf("errors = %v, want [Test error message]", got)
	}
}

// read-ui-message-stream.test.ts 578 (Go form): snapshots do not share nested
// values with each other or with the inputs. Changing the first snapshot's
// metadata and data parts leaves the second snapshot, the seed message and
// the chunk data as they were.
func TestR76StreamReadUIMessagesSnapshotIsolation(t *testing.T) {
	metadata := map[string]interface{}{"custom": map[string]interface{}{"value": "custom"}}
	seedData := map[string]interface{}{"value": "seed"}
	chunkData := map[string]interface{}{"value": "chunk"}
	msgs, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{
		Message: &UIMessage{ID: "msg-123", Role: "assistant", Metadata: metadata, Parts: []UIMessagePart{DataUIPart{Type: "data-test", Data: seedData}}},
		Stream: r76streamChan(
			UIMessageChunk{"type": "data-test", "data": chunkData},
			UIMessageChunk{"type": "text-start", "id": "text-1"},
		),
	})
	var snapshots []UIMessage
	for m := range msgs {
		snapshots = append(snapshots, m)
	}
	for err := range errs {
		t.Fatalf("error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snapshots))
	}
	first := snapshots[0]
	first.Metadata.(map[string]interface{})["custom"].(map[string]interface{})["value"] = "changed"
	for _, p := range first.Parts[:2] {
		r76streamDataOf(t, p)["value"] = "changed"
	}
	r75streamAssertJSON(t, "second metadata", snapshots[1].Metadata, `{"custom":{"value":"custom"}}`)
	if got := r76streamDataOf(t, snapshots[1].Parts[0])["value"]; got != "seed" {
		t.Errorf("second seed part data = %v", got)
	}
	if got := r76streamDataOf(t, snapshots[1].Parts[1])["value"]; got != "chunk" {
		t.Errorf("second chunk part data = %v", got)
	}
	r75streamAssertJSON(t, "input metadata", metadata, `{"custom":{"value":"custom"}}`)
	if seedData["value"] != "seed" || chunkData["value"] != "chunk" {
		t.Errorf("inputs changed: seed %v, chunk %v", seedData, chunkData)
	}
}

func r76streamDataOf(t *testing.T, p UIMessagePart) map[string]interface{} {
	t.Helper()
	var data interface{}
	switch v := p.(type) {
	case DataUIPart:
		data = v.Data
	case *DataUIPart:
		data = v.Data
	default:
		t.Fatalf("part is %T, want a data part", p)
	}
	m, ok := data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is %T", data)
	}
	return m
}

// handle-ui-message-stream-finish.test.ts 74 / 117 / 144 (through
// CreateUIMessageStreamWithOptions, TS createUIMessageStream's
// handleUIMessageStreamFinish): the chunks pass through unchanged (the start
// chunk already has its id); onEnd, preferred over onFinish, runs once with
// a new response message after the original user message; the outcome the
// stream owner declared reaches onEnd.
func TestR76StreamHandleFinishProcessesStream(t *testing.T) {
	input := []UIMessageChunk{
		{"type": "start", "messageId": "msg-456"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "Hello"},
		{"type": "text-delta", "id": "text-1", "delta": " World"},
		{"type": "text-end", "id": "text-1"},
		{"type": "finish"},
	}
	user := UIMessageChunk{"id": "user-msg-1", "role": "user", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "Hello"}}}
	var mu sync.Mutex
	var ends, finishes []map[string]interface{}
	out, errs := r75streamRunUIChunks(t, input, UIMessageStreamOptions{
		OriginalMessages:  []UIMessageChunk{user},
		GenerateMessageID: r75streamIDGen("msg-456"),
		OnEnd: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			ends = append(ends, e)
		},
		OnFinish: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			finishes = append(finishes, e)
		},
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	r75streamAssertJSON(t, "output", out, string(r76streamMustJSON(t, input)))
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 || len(finishes) != 0 {
		t.Fatalf("onEnd calls = %d, onFinish calls = %d; want 1, 0", len(ends), len(finishes))
	}
	e := ends[0]
	if e["isContinuation"] != false {
		t.Errorf("isContinuation = %v", e["isContinuation"])
	}
	resp, _ := e["responseMessage"].(UIMessageChunk)
	if resp == nil || resp["id"] != "msg-456" || resp["role"] != "assistant" {
		t.Fatalf("responseMessage = %#v", e["responseMessage"])
	}
	messages, _ := e["messages"].([]UIMessageChunk)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want 2", e["messages"])
	}
	r75streamAssertJSON(t, "messages[0]", messages[0], string(r76streamMustJSON(t, user)))
	r75streamAssertJSON(t, "messages[1]", messages[1], string(r76streamMustJSON(t, resp)))
}

// handle-ui-message-stream-finish.test.ts 144: the outcome the stream owner
// declared (TS getOutcome; Go writer.SetOutcome) reaches onEnd with isAborted
// false.
func TestR76StreamHandleFinishDeclaredOutcome(t *testing.T) {
	failure := errors.New("stream failed")
	var mu sync.Mutex
	var outcome map[string]interface{}
	var aborted interface{}
	outCh, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		GenerateMessageID: r75streamIDGen("msg-456"),
		OnError:           func(error) string { return "masked error" },
		OnEnd: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			outcome, _ = e["outcome"].(map[string]interface{})
			aborted = e["isAborted"]
		},
		Execute: func(w UIMessageStreamWriter) {
			w.SetOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: failure})
			w.Write(UIMessageChunk{"type": "start", "messageId": "msg-456"})
			w.Write(UIMessageChunk{"type": "error", "errorText": "masked error"})
		},
	})
	for range outCh {
	}
	for range errCh {
	}
	mu.Lock()
	defer mu.Unlock()
	if aborted != false || outcome["status"] != "failed" || outcome["error"] != failure {
		t.Fatalf("onEnd isAborted = %v, outcome = %#v; want false, failed with the declared error", aborted, outcome)
	}
}

func r76streamMustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
