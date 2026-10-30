package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 71 (stream): standalone ToUIMessageStream, owner ruling 2026-10-30
// option (a). A source whose first part is a ChunkTypeStart part is a
// StreamText-shaped TextStreamPart sequence and is mapped one to one like TS
// toUIMessageStream (ui-message-stream/to-ui-message-stream.test.ts); a raw
// provider stream keeps Go's framing.

func r71streamCollectFrom(t *testing.T, ctx context.Context, stream provider.TextStream, opts UIMessageStreamResultOptions) ([]UIMessageChunk, error) {
	t.Helper()
	chunks, errs := ToUIMessageStream(ctx, stream, opts)
	var got []UIMessageChunk
	for c := range chunks {
		got = append(got, c)
	}
	var err error
	for e := range errs {
		if err == nil {
			err = e
		}
	}
	return got, err
}

func r71streamCollect(t *testing.T, parts []provider.StreamChunk, opts UIMessageStreamResultOptions) ([]UIMessageChunk, error) {
	t.Helper()
	return r71streamCollectFrom(t, context.Background(), testutil.NewMockTextStream(parts), opts)
}

func r71streamJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func r71streamAssertJSON(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	var wantValue interface{}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("%s: bad want JSON: %v", label, err)
	}
	if g, w := r71streamJSON(t, got), r71streamJSON(t, wantValue); g != w {
		t.Fatalf("%s:\n got  %s\n want %s", label, g, w)
	}
}

var r71streamUsage = &types.Usage{InputTokens: r71streamInt64(1), OutputTokens: r71streamInt64(1), TotalTokens: r71streamInt64(2)}

func r71streamInt64(v int64) *int64 { return &v }

func r71streamFinishStep(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, Response: &types.StepResponse{ID: id, ModelID: "m"}}
}

func r71streamFinish() provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage}
}

var (
	r71streamStart     = provider.StreamChunk{Type: provider.ChunkTypeStart}
	r71streamStartStep = provider.StreamChunk{Type: provider.ChunkTypeStartStep}
)

// recorder for UI callbacks shared with the stream goroutine.
type r71streamCalls struct {
	mu     sync.Mutex
	events []map[string]interface{}
}

func (c *r71streamCalls) record(event map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *r71streamCalls) all() []map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]interface{}(nil), c.events...)
}

// TS 47: includes finish-step metadata in the snapshot before forwarding the boundary.
func TestR71StreamToUIFinishStepMetadataBeforeBoundary(t *testing.T) {
	var stepEnds, ends r71streamCalls
	got, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamStartStep, r71streamFinishStep("r"), r71streamFinish()}, UIMessageStreamResultOptions{
		GenerateMessageID: func() string { return "assistant-1" },
		MessageMetadata: func(input map[string]interface{}) map[string]interface{} {
			if input["type"] != "finish-step" {
				return nil
			}
			part := input["part"].(provider.StreamChunk)
			return map[string]interface{}{"totalTokens": *part.Usage.TotalTokens}
		},
		OnStepEnd: stepEnds.record,
		OnEnd:     ends.record,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[
		{"type":"start","messageId":"assistant-1"},
		{"type":"start-step"},
		{"type":"message-metadata","messageMetadata":{"totalTokens":2}},
		{"type":"finish-step"},
		{"type":"finish","finishReason":"stop"}]`)
	steps, finals := stepEnds.all(), ends.all()
	if len(steps) != 1 || len(finals) != 1 {
		t.Fatalf("onStepEnd calls = %d, onEnd calls = %d, want 1 and 1", len(steps), len(finals))
	}
	r71streamAssertJSON(t, "step responseMessage", steps[0]["responseMessage"], `{"id":"assistant-1","role":"assistant","parts":[{"type":"step-start"}],"metadata":{"totalTokens":2}}`)
	if a, b := r71streamJSON(t, steps[0]["responseMessage"]), r71streamJSON(t, finals[0]["responseMessage"]); a != b {
		t.Fatalf("step responseMessage %s != end responseMessage %s", a, b)
	}
}

// TS 92: resolves the deprecated step callback alias.
func TestR71StreamToUIDeprecatedStepAlias(t *testing.T) {
	for _, hasOnStepEnd := range []bool{false, true} {
		var stepEnds, stepFinishes r71streamCalls
		opts := UIMessageStreamResultOptions{OnStepFinish: stepFinishes.record}
		if hasOnStepEnd {
			opts.OnStepEnd = stepEnds.record
		}
		got, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamFinishStep("r")}, opts)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		r71streamAssertJSON(t, "chunks", got, `[{"type":"start"},{"type":"finish-step"}]`)
		wantEnd, wantFinish := 0, 1
		if hasOnStepEnd {
			wantEnd, wantFinish = 1, 0
		}
		if len(stepEnds.all()) != wantEnd || len(stepFinishes.all()) != wantFinish {
			t.Fatalf("hasOnStepEnd=%v: onStepEnd=%d onStepFinish=%d", hasOnStepEnd, len(stepEnds.all()), len(stepFinishes.all()))
		}
	}
}

// TS 110: awaits the UI callback before delivering finish-step, and the
// stream ends right after it (no synthesized finish).
func TestR71StreamToUIAwaitsStepCallbackBeforeBoundary(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	chunks, errs := ToUIMessageStream(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{r71streamStart, r71streamFinishStep("r")}), UIMessageStreamResultOptions{
		OnStepEnd: func(map[string]interface{}) {
			close(started)
			<-release
		},
	})
	first := <-chunks
	r71streamAssertJSON(t, "first", first, `{"type":"start"}`)
	<-started
	select {
	case c := <-chunks:
		t.Fatalf("boundary delivered before the callback finished: %v", c)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	next := <-chunks
	r71streamAssertJSON(t, "next", next, `{"type":"finish-step"}`)
	if c, ok := <-chunks; ok {
		t.Fatalf("extra chunk after finish-step: %v", c)
	}
	if err := <-errs; err != nil {
		t.Fatalf("err = %v", err)
	}
}

// TS 131: maps text and lifecycle parts to UI message chunks.
func TestR71StreamToUIMapsTextAndLifecycle(t *testing.T) {
	got, err := r71streamCollect(t, []provider.StreamChunk{
		r71streamStart, r71streamStartStep,
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: ", world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		r71streamFinishStep("r"), r71streamFinish(),
	}, UIMessageStreamResultOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[
		{"type":"start"},{"type":"start-step"},
		{"type":"text-start","id":"t1"},
		{"type":"text-delta","id":"t1","delta":"Hello"},
		{"type":"text-delta","id":"t1","delta":", world!"},
		{"type":"text-end","id":"t1"},
		{"type":"finish-step"},{"type":"finish","finishReason":"stop"}]`)
}

// TS 184: attaches the generated message id to the start chunk; [start] → [start].
func TestR71StreamToUIStartOnlyWithGeneratedID(t *testing.T) {
	calls := 0
	got, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart}, UIMessageStreamResultOptions{
		GenerateMessageID: func() string { calls++; return "msg-123" },
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"start","messageId":"msg-123"}]`)
	if calls != 1 {
		t.Fatalf("generateMessageId calls = %d, want 1", calls)
	}
}

// TS 200: suppresses start/finish chunks when sendStart/sendFinish are false.
func TestR71StreamToUISuppressStartFinish(t *testing.T) {
	f := false
	got, err := r71streamCollect(t, []provider.StreamChunk{
		r71streamStart,
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		r71streamFinish(),
	}, UIMessageStreamResultOptions{SendStart: &f, SendFinish: &f})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"text-start","id":"t1"},{"type":"text-end","id":"t1"}]`)
}

// TS 228: skips reasoning parts when sendReasoning is false. TS's source has
// no start part; the StreamText-shaped port starts with one (and SendStart
// false, so the expected output is TS's []). The bare TS sequence is a raw
// provider stream in Go and keeps Go's framing (known difference).
func TestR71StreamToUISkipsReasoningWhenDisabled(t *testing.T) {
	f := false
	reasoning := []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "r1"},
		{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
	}
	got, err := r71streamCollect(t, append([]provider.StreamChunk{r71streamStart}, reasoning...), UIMessageStreamResultOptions{SendReasoning: &f, SendStart: &f})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "one-to-one", got, `null`)

	got, err = r71streamCollect(t, reasoning, UIMessageStreamResultOptions{SendReasoning: &f})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "raw provider framing", got, `[{"type":"start"},{"type":"finish-step"},{"type":"finish","finishReason":"other"}]`)
}

// TS 246: routes error parts through onError.
func TestR71StreamToUIRoutesErrorThroughOnError(t *testing.T) {
	f := false
	onError := func(err error) string { return "handled: " + err.Error() }
	errPart := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("boom")}
	got, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, errPart}, UIMessageStreamResultOptions{OnError: onError, SendStart: &f})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "one-to-one", got, `[{"type":"error","errorText":"handled: boom"}]`)

	got, err = r71streamCollect(t, []provider.StreamChunk{r71streamStart, errPart}, UIMessageStreamResultOptions{OnError: onError})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "with start", got, `[{"type":"start"},{"type":"error","errorText":"handled: boom"}]`)

	got, err = r71streamCollect(t, []provider.StreamChunk{errPart}, UIMessageStreamResultOptions{OnError: onError})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "raw provider framing", got, `[{"type":"start"},{"type":"error","errorText":"handled: boom"},{"type":"finish-step"},{"type":"finish","finishReason":"error"}]`)
}

// TS 262: emits separate metadata chunks for non-lifecycle parts. TS's
// sequence has a text-delta with no text-start; Go opens the block for it
// (Go providers may stream text without text-start), so a text-start
// precedes the delta.
func TestR71StreamToUIMetadataForNonLifecycleParts(t *testing.T) {
	var seen []string
	got, err := r71streamCollect(t, []provider.StreamChunk{
		r71streamStart,
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		r71streamFinish(),
	}, UIMessageStreamResultOptions{
		MessageMetadata: func(input map[string]interface{}) map[string]interface{} {
			partType := input["type"].(string)
			seen = append(seen, partType)
			return map[string]interface{}{"partType": partType}
		},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[
		{"type":"start","messageMetadata":{"partType":"start"}},
		{"type":"text-start","id":"t1"},
		{"type":"text-delta","id":"t1","delta":"Hello"},
		{"type":"message-metadata","messageMetadata":{"partType":"text-delta"}},
		{"type":"finish","finishReason":"stop","messageMetadata":{"partType":"finish"}}]`)
	r71streamAssertJSON(t, "messageMetadata calls", seen, `["start","text-delta","finish"]`)
}

// The finish part's messageMetadata input is the TS finish part
// (finishReason, rawFinishReason, totalUsage).
func TestR71StreamToUIFinishMetadataSeesFinishPart(t *testing.T) {
	var finishPart provider.StreamChunk
	_, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamFinish()}, UIMessageStreamResultOptions{
		MessageMetadata: func(input map[string]interface{}) map[string]interface{} {
			if input["type"] == "finish" {
				finishPart = input["part"].(provider.StreamChunk)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if finishPart.Type != "finish" || finishPart.FinishReason != types.FinishReasonStop || finishPart.RawFinishReason != "stop" || finishPart.Usage == nil || *finishPart.Usage.TotalTokens != 2 {
		t.Fatalf("finish part = %+v", finishPart)
	}
}

var r71streamOriginal = []UIMessageChunk{{"id": "user-msg-1", "role": "user", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "Hi"}}}}

func r71streamHelloParts() []provider.StreamChunk {
	return []provider.StreamChunk{
		r71streamStart,
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		r71streamFinish(),
	}
}

// TS 304: injects generated message id and calls onFinish.
func TestR71StreamToUIInjectsIDAndCallsOnFinish(t *testing.T) {
	var finishes r71streamCalls
	calls := 0
	got, err := r71streamCollect(t, r71streamHelloParts(), UIMessageStreamResultOptions{
		OriginalMessages:  r71streamOriginal,
		GenerateMessageID: func() string { calls++; return "msg-123" },
		OnFinish:          finishes.record,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[
		{"type":"start","messageId":"msg-123"},
		{"type":"text-start","id":"t1"},{"type":"text-delta","id":"t1","delta":"Hello"},{"type":"text-end","id":"t1"},
		{"type":"finish","finishReason":"stop"}]`)
	if calls != 1 {
		t.Fatalf("generateMessageId calls = %d", calls)
	}
	events := finishes.all()
	if len(events) != 1 {
		t.Fatalf("onFinish calls = %d", len(events))
	}
	r71streamAssertJSON(t, "onFinish", events[0], `{
		"isAborted":false,"isContinuation":false,"finishReason":"stop",
		"outcome":{"status":"completed"},
		"responseMessage":{"id":"msg-123","role":"assistant","parts":[{"type":"text","text":"Hello","state":"done"}]},
		"messages":[
			{"id":"user-msg-1","role":"user","parts":[{"type":"text","text":"Hi"}]},
			{"id":"msg-123","role":"assistant","parts":[{"type":"text","text":"Hello","state":"done"}]}]}`)
}

// TS 360: calls onStepEnd with the accumulated UI message.
func TestR71StreamToUIStepEndAccumulates(t *testing.T) {
	var stepEnds r71streamCalls
	_, err := r71streamCollect(t, []provider.StreamChunk{
		r71streamStart, r71streamStartStep,
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "First"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		r71streamFinishStep("r1"),
		r71streamStartStep,
		{Type: provider.ChunkTypeTextStart, ID: "t2"},
		{Type: provider.ChunkTypeText, ID: "t2", Text: "Second"},
		{Type: provider.ChunkTypeTextEnd, ID: "t2"},
		r71streamFinishStep("r2"), r71streamFinish(),
	}, UIMessageStreamResultOptions{
		OriginalMessages:  r71streamOriginal,
		GenerateMessageID: func() string { return "assistant-msg-1" },
		OnStepEnd:         stepEnds.record,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	events := stepEnds.all()
	if len(events) != 2 {
		t.Fatalf("onStepEnd calls = %d", len(events))
	}
	r71streamAssertJSON(t, "step 1", events[0], `{
		"isContinuation":false,
		"responseMessage":{"id":"assistant-msg-1","role":"assistant","parts":[{"type":"step-start"},{"type":"text","text":"First","state":"done"}]},
		"messages":[
			{"id":"user-msg-1","role":"user","parts":[{"type":"text","text":"Hi"}]},
			{"id":"assistant-msg-1","role":"assistant","parts":[{"type":"step-start"},{"type":"text","text":"First","state":"done"}]}]}`)
	r71streamAssertJSON(t, "step 2 parts", events[1]["responseMessage"].(UIMessageChunk)["parts"], `[
		{"type":"step-start"},{"type":"text","text":"First","state":"done"},
		{"type":"step-start"},{"type":"text","text":"Second","state":"done"}]`)
}

// TS 458: calls onEnd when stream finishes, and not the deprecated onFinish.
func TestR71StreamToUICallsOnEndNotOnFinish(t *testing.T) {
	var ends, finishes r71streamCalls
	_, err := r71streamCollect(t, r71streamHelloParts(), UIMessageStreamResultOptions{
		GenerateMessageID: func() string { return "msg-123" },
		OnEnd:             ends.record,
		OnFinish:          finishes.record,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	events := ends.all()
	if len(events) != 1 || len(finishes.all()) != 0 {
		t.Fatalf("onEnd = %d, onFinish = %d", len(events), len(finishes.all()))
	}
	r71streamAssertJSON(t, "onEnd", events[0], `{
		"isAborted":false,"isContinuation":false,"finishReason":"stop",
		"outcome":{"status":"completed"},
		"responseMessage":{"id":"msg-123","role":"assistant","parts":[{"type":"text","text":"Hello","state":"done"}]},
		"messages":[{"id":"msg-123","role":"assistant","parts":[{"type":"text","text":"Hello","state":"done"}]}]}`)
}

// TS 500: reports the source outcome to onEnd (StreamText-shaped sources, so
// each starts with a start part; TS's [] is [start] here).
func TestR71StreamToUIReportsSourceOutcome(t *testing.T) {
	finishPart := r71streamFinish()
	abortPart := provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user cancelled"}
	errorPart := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("generation failed")}
	observe := func(parts ...provider.StreamChunk) map[string]interface{} {
		var ends r71streamCalls
		_, _ = r71streamCollect(t, append([]provider.StreamChunk{r71streamStart}, parts...), UIMessageStreamResultOptions{OnEnd: ends.record})
		events := ends.all()
		if len(events) != 1 {
			t.Fatalf("onEnd calls = %d", len(events))
		}
		outcome := events[0]["outcome"].(map[string]interface{})
		result := map[string]interface{}{"status": outcome["status"]}
		if err, ok := outcome["error"].(error); ok && outcome["status"] == "failed" {
			result["errorMessage"] = err.Error()
		}
		return result
	}
	got := map[string]interface{}{
		"completed":            observe(finishPart),
		"failed":               observe(errorPart),
		"aborted":              observe(abortPart),
		"unknown":              observe(),
		"completedBeforeError": observe(finishPart, errorPart),
		"completedAfterError":  observe(errorPart, finishPart),
	}
	r71streamAssertJSON(t, "outcomes", got, `{
		"aborted":{"status":"aborted"},
		"completed":{"status":"completed"},
		"completedAfterError":{"status":"completed"},
		"completedBeforeError":{"status":"completed"},
		"failed":{"errorMessage":"generation failed","status":"failed"},
		"unknown":{"status":"unknown"}}`)
}

// TS [finish, error] / [error, finish] keep TS's chunk order: the finish chunk
// is written at the finish part, not at the end of the source.
func TestR71StreamToUIFinishChunkWrittenInPlace(t *testing.T) {
	errorPart := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("generation failed")}
	got, _ := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamFinish(), errorPart}, UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "finish then error", got, `[{"type":"start"},{"type":"finish","finishReason":"stop"},{"type":"error","errorText":"An error occurred."}]`)
	got, _ = r71streamCollect(t, []provider.StreamChunk{r71streamStart, errorPart, r71streamFinish()}, UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "error then finish", got, `[{"type":"start"},{"type":"error","errorText":"An error occurred."},{"type":"finish","finishReason":"stop"}]`)
	abortPart := provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user cancelled"}
	got, _ = r71streamCollect(t, []provider.StreamChunk{r71streamStart, abortPart}, UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "abort", got, `[{"type":"start"},{"type":"abort","reason":"user cancelled"}]`)
}

// A one-to-one source ending with open blocks gets no closing chunks,
// finish-step or finish (TS adds nothing at the end of the source).
func TestR71StreamToUINoFramingAtSourceEnd(t *testing.T) {
	got, err := r71streamCollect(t, []provider.StreamChunk{
		r71streamStart, r71streamStartStep,
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "partial"},
	}, UIMessageStreamResultOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"start"},{"type":"start-step"},{"type":"text-start","id":"t1"},{"type":"text-delta","id":"t1","delta":"partial"}]`)
}

// TS 560: reports an errored source stream as failed exactly once. Go's
// errored source writes a masked error chunk and ends (owner ruling
// 2026-10-29, known difference); Err() returns the source error.
func TestR71StreamToUIErroredSourceFailedOnce(t *testing.T) {
	sourceErr := errors.New("source stream failed")
	var ends r71streamCalls
	got, err := r71streamCollectFrom(t, context.Background(), testutil.NewMockTextStreamWithError(sourceErr), UIMessageStreamResultOptions{OnEnd: ends.record})
	if !errors.Is(err, sourceErr) {
		t.Fatalf("err = %v, want %v", err, sourceErr)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"start"},{"type":"error","errorText":"An error occurred."}]`)
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome := events[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != sourceErr {
		t.Fatalf("outcome = %v", outcome)
	}
}

type r71streamPartsThenErr struct {
	parts []provider.StreamChunk
	err   error
	i     int
}

func (s *r71streamPartsThenErr) Next() (*provider.StreamChunk, error) {
	if s.i < len(s.parts) {
		s.i++
		return &s.parts[s.i-1], nil
	}
	return nil, s.err
}
func (s *r71streamPartsThenErr) Close() error { return nil }
func (s *r71streamPartsThenErr) Err() error   { return nil }

// TS 634: does not let an earlier finish part hide a source stream failure.
func TestR71StreamToUIEarlierFinishDoesNotHideFailure(t *testing.T) {
	sourceErr := errors.New("source stream failed after finish")
	var ends r71streamCalls
	got, err := r71streamCollectFrom(t, context.Background(), &r71streamPartsThenErr{parts: []provider.StreamChunk{r71streamStart, r71streamFinish()}, err: sourceErr}, UIMessageStreamResultOptions{OnEnd: ends.record})
	if !errors.Is(err, sourceErr) {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "chunks", got, `[{"type":"start"},{"type":"finish","finishReason":"stop"},{"type":"error","errorText":"An error occurred."}]`)
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome := events[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != sourceErr {
		t.Fatalf("outcome = %v", outcome)
	}
}

// TS 669: reports message metadata failures as failed exactly once.
func TestR71StreamToUIMetadataFailureFailedOnce(t *testing.T) {
	metadataErr := errors.New("message metadata failed")
	var ends r71streamCalls
	_, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamFinish()}, UIMessageStreamResultOptions{
		MessageMetadata: func(map[string]interface{}) map[string]interface{} { panic(metadataErr) },
		OnEnd:           ends.record,
	})
	if !errors.Is(err, metadataErr) {
		t.Fatalf("err = %v", err)
	}
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome := events[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != metadataErr {
		t.Fatalf("outcome = %v", outcome)
	}
}

// TS 700: reports UI chunk conversion failures as failed exactly once.
func TestR71StreamToUIConversionFailureFailedOnce(t *testing.T) {
	conversionErr := errors.New("UI chunk conversion failed")
	var ends r71streamCalls
	_, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, {Type: provider.ChunkTypeError, Err: errors.New("generation failed")}}, UIMessageStreamResultOptions{
		OnError: func(error) string { panic(conversionErr) },
		OnEnd:   ends.record,
	})
	if !errors.Is(err, conversionErr) {
		t.Fatalf("err = %v", err)
	}
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome := events[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != conversionErr {
		t.Fatalf("outcome = %v", outcome)
	}
}

type r71streamStartThenBlock struct {
	ctx  context.Context
	sent bool
}

func (s *r71streamStartThenBlock) Next() (*provider.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return &provider.StreamChunk{Type: provider.ChunkTypeStart}, nil
	}
	<-s.ctx.Done()
	return nil, io.EOF
}
func (s *r71streamStartThenBlock) Close() error { return nil }
func (s *r71streamStartThenBlock) Err() error   { return nil }

// TS 610: consumer cancellation after the start chunk: onEnd once with
// isAborted false, isCancelled true, outcome unknown.
func TestR71StreamToUICancellationAfterStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ends r71streamCalls
	chunks, errs := ToUIMessageStream(ctx, &r71streamStartThenBlock{ctx: ctx}, UIMessageStreamResultOptions{OnEnd: ends.record})
	r71streamAssertJSON(t, "first", <-chunks, `{"type":"start"}`)
	cancel()
	for range chunks {
	}
	for range errs {
	}
	events := ends.all()
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	if events[0]["isAborted"] != false || events[0]["isCancelled"] != true {
		t.Fatalf("onEnd = %v", events[0])
	}
	if outcome := events[0]["outcome"].(map[string]interface{}); outcome["status"] != "unknown" {
		t.Fatalf("outcome = %v", outcome)
	}
}
