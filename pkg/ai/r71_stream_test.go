package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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

// Round 71 (stream): ports of TS stream-text.test.ts options.onStart,
// options.onStepStart, options.onToolExecutionStart/End and their ordering
// blocks, with field-by-field assertions.

// r71streamModel returns a mock model ("mock-provider" / "mock-model-id")
// whose n-th DoStream call streams steps[n] (the last one repeats), and
// records "doStream" into order when it is set.
func r71streamModel(order *r71streamOrder, steps ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i >= len(steps) {
			i = len(steps) - 1
		}
		if order != nil {
			order.add("doStream")
		}
		return testutil.NewMockTextStream(steps[i]), nil
	}}
}

type r71streamOrder struct {
	mu    sync.Mutex
	items []string
}

func (o *r71streamOrder) add(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items = append(o.items, s)
}

func (o *r71streamOrder) get() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.items...)
}

// r71streamHelloWorld is TS createTestModel's default stream.
var r71streamHelloWorld = []provider.StreamChunk{
	{Type: provider.ChunkTypeTextStart, ID: "1"},
	{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
	{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
	{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
	{Type: provider.ChunkTypeTextEnd, ID: "1"},
	{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
}

func r71streamToolCallStep(calls ...[2]string) []provider.StreamChunk {
	var out []provider.StreamChunk
	for _, c := range calls {
		out = append(out, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: c[0], ToolName: "tool1", Arguments: map[string]interface{}{"value": c[1]}, RawArguments: `{ "value": "` + c[1] + `" }`}})
	}
	return append(out, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r71streamUsage})
}

var r71streamValueSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}

func r71streamTool1(execute bool, fail error, order *r71streamOrder) types.Tool {
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema}
	if execute {
		tool.Execute = func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			if order != nil {
				order.add("execute")
			}
			if fail != nil {
				return nil, fail
			}
			return args["value"].(string) + "-result", nil
		}
	}
	return tool
}

var r71streamInternal = &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }}

func r71streamUserMessages(text string) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}}
}

func r71streamRun(t *testing.T, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}
	return r
}

// TS 9717: options.onStart > should send correct information with text prompt
// (snapshot). Go leaves unset settings at their zero values; ToolsContext is
// {} as in TS, while RuntimeContext stays nil where TS defaults it to {}
// (open item, r71).
func TestR71StreamOnStartTextPrompt(t *testing.T) {
	var got OnStartEvent
	calls := 0
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		Telemetry: &TelemetrySettings{FunctionID: "test-function"},
		Internal:  r71streamInternal,
		OnStart:   func(_ context.Context, e OnStartEvent) { calls++; got = e },
	})
	if calls != 1 {
		t.Fatalf("onStart calls = %d", calls)
	}
	want := OnStartEvent{
		CallID: "test-telemetry-call-id", OperationID: "ai.streamText",
		Provider: "mock-provider", ModelProvider: "mock-provider", ModelID: "mock-model-id",
		Prompt: "test-input", Messages: r71streamUserMessages("test-input"), MaxRetries: 2,
		ToolsContext: map[string]interface{}{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("onStart event:\n got  %#v\n want %#v", got, want)
	}
}

// TS 9969: options.onStart > should send correct information with system and messages.
func TestR71StreamOnStartSystemAndMessages(t *testing.T) {
	var got OnStartEvent
	instructions := "you are a helpful assistant"
	maxTokens := 100
	temperature := 0.5
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Instructions: &instructions,
		Messages: r71streamUserMessages("test-message"), MaxTokens: &maxTokens, Temperature: &temperature,
		OnStart: func(_ context.Context, e OnStartEvent) { got = e },
	})
	if got.Provider != "mock-provider" || got.ModelID != "mock-model-id" || got.Instructions != instructions || got.Prompt != "" {
		t.Fatalf("onStart = %#v", got)
	}
	if !reflect.DeepEqual(got.Messages, r71streamUserMessages("test-message")) {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 100 || got.Temperature == nil || *got.Temperature != 0.5 || got.MaxRetries != 2 {
		t.Fatalf("settings: maxTokens %v temperature %v maxRetries %d", got.MaxTokens, got.Temperature, got.MaxRetries)
	}
}

// TS 9998 / 10237: onStart, then onStepStart, run before doStream (each step).
func TestR71StreamOnStartAndStepStartBeforeDoStream(t *testing.T) {
	order := &r71streamOrder{}
	r71streamRun(t, StreamTextOptions{
		Model:       r71streamModel(order, r71streamToolCallStep([2]string{"call-1", "test"}), r71streamHelloWorld),
		Prompt:      "test-input",
		Tools:       []types.Tool{r71streamTool1(true, nil, nil)},
		StopWhen:    []StopCondition{IsStepCount(3)},
		OnStart:     func(context.Context, OnStartEvent) { order.add("onStart") },
		OnStepStart: func(context.Context, OnStepStartEvent) { order.add("onStepStart") },
	})
	want := []string{"onStart", "onStepStart", "doStream", "onStepStart", "doStream"}
	if got := order.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TS 10041 / 10280: a panicking onStart or onStepStart does not break generation.
func TestR71StreamStartCallbacksPanicDoNotBreak(t *testing.T) {
	for name, opts := range map[string]StreamTextOptions{
		"onStart":     {OnStart: func(context.Context, OnStartEvent) { panic(errors.New("callback error")) }},
		"onStepStart": {OnStepStart: func(context.Context, OnStepStartEvent) { panic(errors.New("callback error")) }},
	} {
		opts.Model = r71streamModel(nil, r71streamHelloWorld)
		opts.Prompt = "test-input"
		if got := r71streamRun(t, opts).Text(); got != "Hello, world!" {
			t.Fatalf("%s: text = %q", name, got)
		}
	}
}

// TS 10054 / 10078 / 10098: onStart exposes tools, toolChoice,
// providerOptions and timeout.
func TestR71StreamOnStartExposesSettings(t *testing.T) {
	var got OnStartEvent
	total, step := 5000*time.Millisecond, 1000*time.Millisecond
	tool := types.Tool{Name: "myTool", Parameters: r71streamValueSchema}
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		Tools: []types.Tool{tool}, ToolChoice: types.AutoToolChoice(),
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}},
		Timeout:         &TimeoutConfig{Total: &total, PerStep: &step},
		StopWhen:        []StopCondition{IsStepCount(3)},
		OnStart:         func(_ context.Context, e OnStartEvent) { got = e },
	})
	if len(got.Tools) != 1 || got.Tools[0].Name != "myTool" || !reflect.DeepEqual(got.Tools[0].Parameters, r71streamValueSchema) {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if got.ToolChoice != types.AutoToolChoice() {
		t.Fatalf("toolChoice = %#v", got.ToolChoice)
	}
	if !reflect.DeepEqual(got.ProviderOptions, map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}}) {
		t.Fatalf("providerOptions = %#v", got.ProviderOptions)
	}
	if got.Timeout == nil || got.Timeout.Total == nil || *got.Timeout.Total != total || got.Timeout.PerStep == nil || *got.Timeout.PerStep != step {
		t.Fatalf("timeout = %#v", got.Timeout)
	}
}

// TS 10119: onStepStart > should be called with correct data for a single step.
func TestR71StreamOnStepStartSingleStep(t *testing.T) {
	var events []OnStepStartEvent
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if len(events) != 1 {
		t.Fatalf("onStepStart calls = %d", len(events))
	}
	e := events[0]
	if e.StepNumber != 0 || len(e.Steps) != 0 || e.Provider != "mock-provider" || e.ModelID != "mock-model-id" {
		t.Fatalf("event = %#v", e)
	}
	if !reflect.DeepEqual(e.Messages, r71streamUserMessages("test-input")) {
		t.Fatalf("messages = %#v", e.Messages)
	}
}

// TS 10148: onStepStart > should be called once per step in a multi-step tool loop.
func TestR71StreamOnStepStartPerStep(t *testing.T) {
	var events []OnStepStartEvent
	r71streamRun(t, StreamTextOptions{
		Model:       r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"}), r71streamHelloWorld),
		Prompt:      "test-input",
		Tools:       []types.Tool{r71streamTool1(true, nil, nil)},
		StopWhen:    []StopCondition{IsStepCount(3)},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if len(events) != 2 || events[0].StepNumber != 0 || len(events[0].Steps) != 0 || events[1].StepNumber != 1 || len(events[1].Steps) != 1 {
		t.Fatalf("events = %d %#v", len(events), events)
	}
}

// TS 10597: onStepStart exposes providerOptions and runtimeContext.
func TestR71StreamOnStepStartProviderOptionsAndRuntimeContext(t *testing.T) {
	var got OnStepStartEvent
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}},
		RuntimeContext:  map[string]interface{}{"userId": "test-user"},
		OnStepStart:     func(_ context.Context, e OnStepStartEvent) { got = e },
	})
	if !reflect.DeepEqual(got.ProviderOptions, map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}}) {
		t.Fatalf("providerOptions = %#v", got.ProviderOptions)
	}
	if !reflect.DeepEqual(got.RuntimeContext, map[string]interface{}{"userId": "test-user"}) {
		t.Fatalf("runtimeContext = %#v", got.RuntimeContext)
	}
}

// TS 11080 / 11124 / 11516 / 11560: the deprecated OnToolCallStart /
// OnToolCallFinish run only when OnToolExecutionStart / End are unset.
func TestR71StreamToolExecutionCallbackAliases(t *testing.T) {
	for _, preferNew := range []bool{false, true} {
		order := &r71streamOrder{}
		opts := StreamTextOptions{
			Model:            r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"})),
			Prompt:           "test-input",
			Tools:            []types.Tool{r71streamTool1(true, nil, nil)},
			OnToolCallStart:  func(context.Context, OnToolCallStartEvent) { order.add("experimental_onToolCallStart") },
			OnToolCallFinish: func(context.Context, OnToolCallFinishEvent) { order.add("experimental_onToolCallFinish") },
		}
		want := []string{"experimental_onToolCallStart", "experimental_onToolCallFinish"}
		if preferNew {
			opts.OnToolExecutionStart = func(context.Context, OnToolCallStartEvent) { order.add("onToolExecutionStart") }
			opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { order.add("onToolExecutionEnd") }
			want = []string{"onToolExecutionStart", "onToolExecutionEnd"}
		}
		r71streamRun(t, opts)
		if got := order.get(); !reflect.DeepEqual(got, want) {
			t.Fatalf("preferNew=%v: order = %v, want %v", preferNew, got, want)
		}
	}
}

// TS 11171 / 11607 / 11854: onToolExecutionStart and onToolExecutionEnd
// payloads (callId, messages, toolCall, toolContext; toolOutput and
// toolExecutionMs on end), without and with a tool context.
func TestR71StreamToolExecutionPayloads(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		var starts []OnToolCallStartEvent
		var ends []OnToolCallFinishEvent
		tool := r71streamTool1(true, nil, nil)
		opts := StreamTextOptions{
			Model:                r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test-arg"})),
			Prompt:               "test-input",
			Tools:                []types.Tool{tool},
			Internal:             r71streamInternal,
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) { starts = append(starts, e) },
			OnToolExecutionEnd:   func(_ context.Context, e OnToolCallFinishEvent) { ends = append(ends, e) },
		}
		var wantContext interface{}
		if withContext {
			opts.Tools[0].ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []interface{}{"context"}})
			opts.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}}
			wantContext = map[string]interface{}{"context": "test"}
		}
		r71streamRun(t, opts)
		if len(starts) != 1 || len(ends) != 1 {
			t.Fatalf("withContext=%v: starts %d ends %d", withContext, len(starts), len(ends))
		}
		s, e := starts[0], ends[0]
		wantCall := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test-arg"}, RawArguments: `{ "value": "test-arg" }`}
		if s.CallID != "test-telemetry-call-id" || s.ToolCallID != "call-1" || s.ToolName != "tool1" || !reflect.DeepEqual(s.ToolCall, wantCall) || !reflect.DeepEqual(s.ToolContext, wantContext) || !reflect.DeepEqual(s.Messages, r71streamUserMessages("test-input")) {
			t.Fatalf("withContext=%v: start = %#v", withContext, s)
		}
		if e.CallID != "test-telemetry-call-id" || !reflect.DeepEqual(e.ToolCall, wantCall) || !reflect.DeepEqual(e.ToolContext, wantContext) || !reflect.DeepEqual(e.Messages, r71streamUserMessages("test-input")) || e.ToolExecutionMs < 0 {
			t.Fatalf("withContext=%v: end = %#v", withContext, e)
		}
		out := e.ToolOutput
		if out.ToolCallID != "call-1" || out.ToolName != "tool1" || out.Dynamic || out.Error != nil || out.Result != "test-arg-result" || !reflect.DeepEqual(out.Input, map[string]interface{}{"value": "test-arg"}) {
			t.Fatalf("withContext=%v: toolOutput = %#v", withContext, out)
		}
	}
}

// TS 11654 / 11939: onToolExecutionEnd carries the tool error (TS
// toolOutput type tool-error, error is the thrown error).
func TestR71StreamToolExecutionEndError(t *testing.T) {
	toolErr := errors.New("Tool execution failed")
	var ends []OnToolCallFinishEvent
	r71streamRun(t, StreamTextOptions{
		Model:              r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"})),
		Prompt:             "test-input",
		Tools:              []types.Tool{r71streamTool1(true, toolErr, nil)},
		OnToolExecutionEnd: func(_ context.Context, e OnToolCallFinishEvent) { ends = append(ends, e) },
	})
	if len(ends) != 1 {
		t.Fatalf("ends = %d", len(ends))
	}
	e := ends[0]
	if e.ToolCall.ToolName != "tool1" || e.ToolCall.ID != "call-1" || e.ToolExecutionMs < 0 {
		t.Fatalf("end = %#v", e)
	}
	if !errors.Is(e.ToolOutput.Error, toolErr) || e.ToolOutput.ToolCallID != "call-1" || !reflect.DeepEqual(e.ToolOutput.Input, map[string]interface{}{"value": "test"}) {
		t.Fatalf("toolOutput = %#v", e.ToolOutput)
	}
}

// TS 11246: once per tool call in a multi-tool step.
func TestR71StreamToolExecutionStartPerCall(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	r71streamRun(t, StreamTextOptions{
		Model:  r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "a"}, [2]string{"call-2", "b"})),
		Prompt: "test-input",
		Tools:  []types.Tool{r71streamTool1(true, nil, nil)},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			ids = append(ids, e.ToolCallID)
			mu.Unlock()
		},
	})
	if len(ids) != 2 {
		t.Fatalf("starts = %v", ids)
	}
}

// TS 11299 / 12029: onToolExecutionStart runs before execute, and before
// onToolExecutionEnd.
func TestR71StreamToolExecutionOrder(t *testing.T) {
	order := &r71streamOrder{}
	r71streamRun(t, StreamTextOptions{
		Model:                r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"})),
		Prompt:               "test-input",
		Tools:                []types.Tool{r71streamTool1(true, nil, order)},
		OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { order.add("onToolExecutionStart") },
		OnToolExecutionEnd:   func(context.Context, OnToolCallFinishEvent) { order.add("onToolExecutionEnd") },
	})
	if got, want := order.get(), []string{"onToolExecutionStart", "execute", "onToolExecutionEnd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TS 11349 / 11763: panicking tool execution callbacks do not break the run.
func TestR71StreamToolExecutionCallbackPanics(t *testing.T) {
	for name, opts := range map[string]StreamTextOptions{
		"start": {OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { panic(errors.New("callback error")) }},
		"end":   {OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) { panic(errors.New("callback error")) }},
	} {
		opts.Model = r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"}))
		opts.Prompt = "test-input"
		opts.Tools = []types.Tool{r71streamTool1(true, nil, nil)}
		results := r71streamRun(t, opts).ToolResults()
		if len(results) != 1 || results[0].Result != "test-result" {
			t.Fatalf("%s: tool results = %#v", name, results)
		}
	}
}

// TS 11394 / 11808: no tool execution callbacks for a tool without Execute.
func TestR71StreamToolExecutionCallbacksSkipToolsWithoutExecute(t *testing.T) {
	starts, ends := 0, 0
	r71streamRun(t, StreamTextOptions{
		Model:                r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"})),
		Prompt:               "test-input",
		Tools:                []types.Tool{r71streamTool1(false, nil, nil)},
		OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { starts++ },
		OnToolExecutionEnd:   func(context.Context, OnToolCallFinishEvent) { ends++ },
	})
	if starts != 0 || ends != 0 {
		t.Fatalf("starts %d ends %d", starts, ends)
	}
}

// TS 12077: tool execution callbacks fire for each tool in a multi-step loop.
func TestR71StreamToolExecutionCallbacksMultiStep(t *testing.T) {
	var mu sync.Mutex
	var starts, ends []string
	r71streamRun(t, StreamTextOptions{
		Model:    r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "step0"}), r71streamToolCallStep([2]string{"call-2", "step1"}), r71streamHelloWorld),
		Prompt:   "test-input",
		Tools:    []types.Tool{r71streamTool1(true, nil, nil)},
		StopWhen: []StopCondition{IsStepCount(3)},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e.ToolCallID+":"+e.ToolCall.Arguments["value"].(string))
			mu.Unlock()
		},
		OnToolExecutionEnd: func(_ context.Context, e OnToolCallFinishEvent) {
			mu.Lock()
			ends = append(ends, e.ToolCallID+":"+e.ToolOutput.Result.(string))
			mu.Unlock()
		},
	})
	if want := []string{"call-1:step0", "call-2:step1"}; !reflect.DeepEqual(starts, want) {
		t.Fatalf("starts = %v", starts)
	}
	if want := []string{"call-1:step0-result", "call-2:step1-result"}; !reflect.DeepEqual(ends, want) {
		t.Fatalf("ends = %v", ends)
	}
}

// TS 13176 / 13526 / 13557: onEnd sends sources, custom parts and files, in
// content order, with the same content on the final step.
func TestR71StreamOnEndSourcesCustomFiles(t *testing.T) {
	src1 := types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value"}}`)}
	src2 := types.SourceContent{SourceType: "url", ID: "456", URL: "https://example.com/2", Title: "Example 2", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value2"}}`)}
	file1 := types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: "data", DataString: "Hello World"}}
	file2 := types.GeneratedFileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: "data", DataString: "QkFVRw=="}}
	custom := types.CustomContent{Kind: "openai.compaction", ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"cmp_123"}}`)}
	text := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"}, {Type: provider.ChunkTypeTextEnd, ID: "1"}}
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	cat := func(groups ...[]provider.StreamChunk) []provider.StreamChunk {
		var out []provider.StreamChunk
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}
	hello := types.TextContent{Text: "Hello!"}
	cases := []struct {
		name        string
		chunks      []provider.StreamChunk
		wantContent []types.ContentPart
		wantSources []types.SourceContent
		wantFiles   []types.GeneratedFileContent
	}{
		{"sources", cat([]provider.StreamChunk{{Type: provider.ChunkTypeSource, SourceContent: &src1}}, text, []provider.StreamChunk{{Type: provider.ChunkTypeSource, SourceContent: &src2}, finish}),
			[]types.ContentPart{src1, hello, src2}, []types.SourceContent{src1, src2}, nil},
		{"custom", cat(text, []provider.StreamChunk{{Type: provider.ChunkTypeCustom, CustomContent: &custom}, finish}),
			[]types.ContentPart{hello, custom}, nil, nil},
		{"files", cat([]provider.StreamChunk{{Type: provider.ChunkTypeFile, GeneratedFileContent: &file1}}, text, []provider.StreamChunk{{Type: provider.ChunkTypeFile, GeneratedFileContent: &file2}, finish}),
			[]types.ContentPart{file1, hello, file2}, nil, []types.GeneratedFileContent{file1, file2}},
	}
	for _, c := range cases {
		var events []OnFinishEvent
		r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, c.chunks), Prompt: "prompt", Internal: r71streamInternal,
			OnEndEvent: func(_ context.Context, e OnFinishEvent) { events = append(events, e) }})
		if len(events) != 1 {
			t.Fatalf("%s: onEnd calls = %d", c.name, len(events))
		}
		e := events[0]
		if e.CallID != "test-telemetry-call-id" || e.Text != "Hello!" || e.FinishReason != types.FinishReasonStop || len(e.Steps) != 1 || len(e.DynamicToolCalls) != 0 || len(e.DynamicToolResults) != 0 {
			t.Fatalf("%s: event = %#v", c.name, e)
		}
		if !reflect.DeepEqual(e.Content, c.wantContent) || !reflect.DeepEqual(e.FinalStep.Content, c.wantContent) {
			t.Fatalf("%s: content = %#v, final step content = %#v", c.name, e.Content, e.FinalStep.Content)
		}
		if !reflect.DeepEqual(e.Sources, c.wantSources) || !reflect.DeepEqual(e.Files, c.wantFiles) {
			t.Fatalf("%s: sources = %#v, files = %#v", c.name, e.Sources, e.Files)
		}
	}
}

// ---- TS ui-message-stream/handle-ui-message-stream-finish.test.ts ports,
// through CreateUIMessageStreamWithOptions (TS createUIMessageStream wraps
// its stream in handleUIMessageStreamFinish). ----

func r71streamUIChunks(t *testing.T, raw string) []UIMessageChunk {
	t.Helper()
	var chunks []UIMessageChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		t.Fatalf("bad chunks JSON: %v", err)
	}
	return chunks
}

func r71streamCreate(t *testing.T, input []UIMessageChunk, opts UIMessageStreamOptions) ([]UIMessageChunk, error) {
	t.Helper()
	opts.Execute = func(w UIMessageStreamWriter) {
		for _, c := range input {
			w.Write(cloneUIMessageChunk(c))
		}
	}
	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), opts)
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

func r71streamParts(event map[string]interface{}) []interface{} {
	return uiParts(event["responseMessage"].(UIMessageChunk))
}

// TS 703 / 752: onStepFinish once per finish-step with the accumulated
// message, and both onStepFinish and onFinish when both are set.
func TestR71StreamHandleFinishStepCallbacks(t *testing.T) {
	input := r71streamUIChunks(t, `[
		{"type":"start","messageId":"msg-multi-step"},
		{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Step 1"},{"type":"text-end","id":"text-1"},
		{"type":"finish-step"},
		{"type":"start-step"},
		{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"Step 2"},{"type":"text-end","id":"text-2"},
		{"type":"finish-step"},
		{"type":"start-step"},
		{"type":"text-start","id":"text-3"},{"type":"text-delta","id":"text-3","delta":"Step 3"},{"type":"text-end","id":"text-3"},
		{"type":"finish-step"},
		{"type":"finish"}]`)
	var steps, finishes r71streamCalls
	got, err := r71streamCreate(t, input, UIMessageStreamOptions{OnStepFinish: steps.record, OnFinish: finishes.record})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r71streamJSON(t, got) != r71streamJSON(t, input) {
		t.Fatalf("chunks = %s", r71streamJSON(t, got))
	}
	events := steps.all()
	if len(events) != 3 || len(finishes.all()) != 1 {
		t.Fatalf("onStepFinish %d onFinish %d", len(events), len(finishes.all()))
	}
	for i, want := range []int{1, 3, 5} {
		if n := len(r71streamParts(events[i])); n != want {
			t.Fatalf("step %d parts = %d, want %d", i+1, n, want)
		}
	}
	r71streamAssertJSON(t, "final parts", r71streamParts(finishes.all()[0]), `[
		{"type":"text","text":"Step 1","state":"done"},
		{"type":"step-start"},{"type":"text","text":"Step 2","state":"done"},
		{"type":"step-start"},{"type":"text","text":"Step 3","state":"done"}]`)
}

// TS 781: an onStepFinish that fails is reported through onError for each
// step and the stream keeps going.
func TestR71StreamHandleFinishStepCallbackFailureContinues(t *testing.T) {
	input := r71streamUIChunks(t, `[
		{"type":"start","messageId":"msg-error"},
		{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Step 1"},{"type":"text-end","id":"text-1"},
		{"type":"finish-step"},
		{"type":"start-step"},
		{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"Step 2"},{"type":"text-end","id":"text-2"},
		{"type":"finish-step"},
		{"type":"finish"}]`)
	var mu sync.Mutex
	stepCalls := 0
	var onErrors []error
	got, err := r71streamCreate(t, input, UIMessageStreamOptions{
		OnStepFinish: func(map[string]interface{}) {
			mu.Lock()
			stepCalls++
			mu.Unlock()
			panic(errors.New("DB error"))
		},
		OnError: func(err error) string {
			mu.Lock()
			onErrors = append(onErrors, err)
			mu.Unlock()
			return err.Error()
		},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r71streamJSON(t, got) != r71streamJSON(t, input) {
		t.Fatalf("chunks = %s", r71streamJSON(t, got))
	}
	mu.Lock()
	defer mu.Unlock()
	if stepCalls != 2 || len(onErrors) != 2 || onErrors[0].Error() != "DB error" || onErrors[1].Error() != "DB error" {
		t.Fatalf("step calls %d, onError %v", stepCalls, onErrors)
	}
}

// TS 820: continuation with onStepFinish.
func TestR71StreamHandleFinishStepContinuation(t *testing.T) {
	input := r71streamUIChunks(t, `[
		{"type":"start","messageId":"assistant-msg-1"},
		{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":" continued"},{"type":"text-end","id":"text-1"},
		{"type":"finish-step"},{"type":"finish"}]`)
	original := r71streamUIChunks(t, `[
		{"id":"user-msg-1","role":"user","parts":[{"type":"text","text":"Continue this"}]},
		{"id":"assistant-msg-1","role":"assistant","parts":[{"type":"text","text":"This is"}]}]`)
	var steps r71streamCalls
	if _, err := r71streamCreate(t, input, UIMessageStreamOptions{OriginalMessages: original, GenerateMessageID: func() string { return "msg-999" }, OnStepFinish: steps.record}); err != nil {
		t.Fatalf("err = %v", err)
	}
	events := steps.all()
	if len(events) != 1 {
		t.Fatalf("onStepFinish calls = %d", len(events))
	}
	e := events[0]
	if e["isContinuation"] != true || e["responseMessage"].(UIMessageChunk)["id"] != "assistant-msg-1" || len(e["messages"].([]UIMessageChunk)) != 2 {
		t.Fatalf("event = %v", e)
	}
	r71streamAssertJSON(t, "continued parts", r71streamParts(e), `[{"type":"text","text":"This is"},{"type":"text","text":" continued","state":"done"}]`)
}

// TS 864: onStepFinish gets a deep copy; its mutations do not reach onFinish.
func TestR71StreamHandleFinishStepDeepCopy(t *testing.T) {
	input := r71streamUIChunks(t, `[
		{"type":"start","messageId":"msg-clone"},
		{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello"},{"type":"text-end","id":"text-1"},
		{"type":"finish-step"},{"type":"finish"}]`)
	var stepParts int
	var finishes r71streamCalls
	_, err := r71streamCreate(t, input, UIMessageStreamOptions{
		OnStepFinish: func(e map[string]interface{}) {
			message := e["responseMessage"].(UIMessageChunk)
			parts := uiParts(message)
			asUIPartChunk(parts[0])["text"] = "MUTATED"
			message["parts"] = append(parts, map[string]interface{}{"type": "text", "text": "MUTATION!"})
			stepParts = len(uiParts(message))
		},
		OnFinish: finishes.record,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if stepParts != 2 || len(finishes.all()) != 1 {
		t.Fatalf("step parts %d, onFinish calls %d", stepParts, len(finishes.all()))
	}
	r71streamAssertJSON(t, "onFinish parts", r71streamParts(finishes.all()[0]), `[{"type":"text","text":"Hello","state":"done"}]`)
}

// TS 230: empty original messages.
func TestR71StreamHandleFinishEmptyOriginalMessages(t *testing.T) {
	input := r71streamUIChunks(t, `[
		{"type":"start","messageId":"msg-789"},
		{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Response"},{"type":"text-end","id":"text-1"},
		{"type":"finish"}]`)
	var finishes r71streamCalls
	if _, err := r71streamCreate(t, input, UIMessageStreamOptions{OriginalMessages: []UIMessageChunk{}, OnFinish: finishes.record}); err != nil {
		t.Fatalf("err = %v", err)
	}
	events := finishes.all()
	if len(events) != 1 || events[0]["isContinuation"] != false {
		t.Fatalf("events = %v", events)
	}
	messages := events[0]["messages"].([]UIMessageChunk)
	if len(messages) != 1 || r71streamJSON(t, messages[0]) != r71streamJSON(t, events[0]["responseMessage"]) {
		t.Fatalf("messages = %v", messages)
	}
}

// TS 392 / 456 / 481 / 903: abort chunks pass through unchanged, with and
// without callbacks; any abort chunk makes isAborted true.
func TestR71StreamHandleFinishAbortPassThrough(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		withFinish  bool
		wantAborted bool
	}{
		{"abort reason", `[{"type":"start","messageId":"msg-abort-reason"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Starting text"},{"type":"abort","reason":"manual abort"},{"type":"finish"}]`, true, true},
		{"no onFinish", `[{"type":"start","messageId":"msg-abort-passthrough"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Text before abort"},{"type":"abort"},{"type":"finish"}]`, false, false},
		{"multiple aborts", `[{"type":"start","messageId":"msg-multiple-abort"},{"type":"text-start","id":"text-1"},{"type":"abort"},{"type":"text-delta","id":"text-1","delta":"Some text"},{"type":"abort"},{"type":"finish"}]`, true, true},
		{"no callbacks", `[{"type":"start","messageId":"msg-passthrough"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Test"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}]`, false, false},
	}
	for _, c := range cases {
		input := r71streamUIChunks(t, c.input)
		var finishes r71streamCalls
		onErrors := 0
		opts := UIMessageStreamOptions{OnError: func(error) string { onErrors++; return "" }}
		if c.withFinish {
			opts.OnFinish = finishes.record
		}
		got, err := r71streamCreate(t, input, opts)
		if err != nil {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if r71streamJSON(t, got) != r71streamJSON(t, input) {
			t.Fatalf("%s: chunks = %s", c.name, r71streamJSON(t, got))
		}
		if onErrors != 0 {
			t.Fatalf("%s: onError calls = %d", c.name, onErrors)
		}
		if c.withFinish {
			events := finishes.all()
			if len(events) != 1 || events[0]["isAborted"] != c.wantAborted {
				t.Fatalf("%s: onFinish = %v", c.name, events)
			}
		}
	}
}

type r71streamPartsThenBlock struct {
	ctx   context.Context
	parts []provider.StreamChunk
	i     int
}

func (s *r71streamPartsThenBlock) Next() (*provider.StreamChunk, error) {
	if s.i < len(s.parts) {
		s.i++
		return &s.parts[s.i-1], nil
	}
	<-s.ctx.Done()
	return nil, io.EOF
}
func (s *r71streamPartsThenBlock) Close() error { return nil }
func (s *r71streamPartsThenBlock) Err() error   { return nil }

// TS 546 / 579: a consumer cancellation after the source declared its
// outcome keeps that outcome and does not report isCancelled; an observed
// abort wins over the cancellation.
func TestR71StreamToUICancelKeepsDeclaredOutcome(t *testing.T) {
	failure := errors.New("stream failed")
	cases := []struct {
		name   string
		part   provider.StreamChunk
		status string
	}{
		{"completed", r71streamFinish(), "completed"},
		{"failed", provider.StreamChunk{Type: provider.ChunkTypeError, Err: failure}, "failed"},
		{"aborted", provider.StreamChunk{Type: provider.ChunkTypeAbort}, "aborted"},
	}
	for _, c := range cases {
		ctx, cancel := context.WithCancel(context.Background())
		var ends r71streamCalls
		chunks, errs := ToUIMessageStream(ctx, &r71streamPartsThenBlock{ctx: ctx, parts: []provider.StreamChunk{r71streamStart, c.part}}, UIMessageStreamResultOptions{ResponseMessageID: "msg-1", OnEnd: ends.record})
		<-chunks
		<-chunks
		cancel()
		for range chunks {
		}
		for range errs {
		}
		events := ends.all()
		if len(events) != 1 {
			t.Fatalf("%s: onEnd calls = %d", c.name, len(events))
		}
		outcome := events[0]["outcome"].(map[string]interface{})
		if outcome["status"] != c.status {
			t.Fatalf("%s: outcome = %v", c.name, outcome)
		}
		if c.status == "failed" && outcome["error"] != failure {
			t.Fatalf("%s: outcome error = %v", c.name, outcome["error"])
		}
		if _, ok := events[0]["isCancelled"]; ok {
			t.Fatalf("%s: isCancelled set: %v", c.name, events[0])
		}
		if c.status == "aborted" && events[0]["isAborted"] != true {
			t.Fatalf("%s: isAborted = %v", c.name, events[0]["isAborted"])
		}
	}
}

func r71streamTypes(s provider.TextStream) []provider.ChunkType {
	var got []provider.ChunkType
	for {
		c, err := s.Next()
		if err != nil {
			return got
		}
		got = append(got, c.Type)
	}
}

// TS HarnessStreamTextResult enqueues { type: 'start' } first (#16717), as
// streamText does; a src that already starts with one keeps a single start.
func TestR71StreamExternalResultStartsWithStartPart(t *testing.T) {
	cases := []struct {
		name  string
		parts []provider.StreamChunk
		want  []provider.ChunkType
	}{
		{"harness turn", []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, []provider.ChunkType{provider.ChunkTypeStart, provider.ChunkTypeStartStep, provider.ChunkTypeText, provider.ChunkTypeFinishStep, provider.ChunkTypeFinish}},
		{"own start part", []provider.StreamChunk{
			{Type: provider.ChunkTypeStart},
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, []provider.ChunkType{provider.ChunkTypeStart, provider.ChunkTypeStartStep, provider.ChunkTypeText, provider.ChunkTypeFinishStep, provider.ChunkTypeFinish}},
		{"empty source", nil, []provider.ChunkType{provider.ChunkTypeStart}},
	}
	for _, c := range cases {
		r := NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream(c.parts), ExternalStreamOptions{TerminalFinishClosesTurn: true})
		if got := r71streamTypes(r.FullStream()); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: full stream = %v, want %v", c.name, got, c.want)
		}
	}
}

// A harness turn's UI stream is the TS toUIMessageStream mapping of its part
// sequence: one to one, the finish chunk at the finish part.
func TestR71StreamExternalResultUIStreamOneToOne(t *testing.T) {
	r := NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "one"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonToolCalls},
		{Type: provider.ChunkTypeTextStart, ID: "t2"},
		{Type: provider.ChunkTypeText, ID: "t2", Text: "two"},
		{Type: provider.ChunkTypeTextEnd, ID: "t2"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), ExternalStreamOptions{TerminalFinishClosesTurn: true})
	var stepEnds r71streamCalls
	chunks, errs := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{ResponseMessageID: "msg-1", OnStepEnd: stepEnds.record})
	var got []UIMessageChunk
	for c := range chunks {
		got = append(got, c)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	}
	r71streamAssertJSON(t, "chunks", got, `[
		{"type":"start","messageId":"msg-1"},
		{"type":"start-step"},{"type":"text-start","id":"t1"},{"type":"text-delta","id":"t1","delta":"one"},{"type":"text-end","id":"t1"},{"type":"finish-step"},
		{"type":"start-step"},{"type":"text-start","id":"t2"},{"type":"text-delta","id":"t2","delta":"two"},{"type":"text-end","id":"t2"},{"type":"finish-step"},
		{"type":"finish","finishReason":"stop"}]`)
	if n := len(stepEnds.all()); n != 2 {
		t.Fatalf("onStepEnd calls = %d", n)
	}
}

// TS to-ui-message-chunk.test.ts 212: custom and lifecycle parts. A custom
// part keeps its own providerMetadata (providers such as Anthropic set it on
// CustomContent only); a start part maps to the start chunk (undefined with
// sendStart false).
func TestR71StreamToUIChunkCustomAndLifecycle(t *testing.T) {
	md := json.RawMessage(`{"testProvider":{"signature":"sig-1"}}`)
	custom := provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "openai.compaction", ProviderMetadata: md}}
	got, ok := ToUIMessageChunk(custom, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("custom: no chunk")
	}
	r71streamAssertJSON(t, "custom", got, `{"type":"custom","kind":"openai.compaction","providerMetadata":{"testProvider":{"signature":"sig-1"}}}`)

	got, ok = ToUIMessageChunk(r71streamStart, UIMessageStreamResultOptions{
		ResponseMessageID: "msg-1",
		MessageMetadata:   func(map[string]interface{}) map[string]interface{} { return map[string]interface{}{"model": "test-model"} },
	})
	if !ok {
		t.Fatal("start: no chunk")
	}
	r71streamAssertJSON(t, "start", got, `{"type":"start","messageId":"msg-1","messageMetadata":{"model":"test-model"}}`)
	f := false
	if got, ok := ToUIMessageChunk(r71streamStart, UIMessageStreamResultOptions{SendStart: &f}); ok {
		t.Fatalf("start with sendStart false = %v", got)
	}
	got, _ = ToUIMessageChunk(r71streamStartStep, UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "start-step", got, `{"type":"start-step"}`)
	got, _ = ToUIMessageChunk(r71streamFinishStep("r"), UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "finish-step", got, `{"type":"finish-step"}`)
	got, _ = ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user"}, UIMessageStreamResultOptions{})
	r71streamAssertJSON(t, "abort", got, `{"type":"abort","reason":"user"}`)

	// The UI stream of a StreamText-shaped source keeps it too.
	chunks, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, custom}, UIMessageStreamResultOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r71streamAssertJSON(t, "stream", chunks, `[{"type":"start"},{"type":"custom","kind":"openai.compaction","providerMetadata":{"testProvider":{"signature":"sig-1"}}}]`)
}

// TS to-ui-message-chunk.test.ts 679: parts that produce no UI chunk.
func TestR71StreamToUIChunkUndefinedParts(t *testing.T) {
	for _, part := range []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
		{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"x": 1}},
	} {
		if got, ok := ToUIMessageChunk(part, UIMessageStreamResultOptions{}); ok {
			t.Fatalf("%s: got %v", part.Type, got)
		}
	}
}
