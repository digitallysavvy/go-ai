package ai

import (
	"context"
	"encoding/json"
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

// r85streamPartialRecorder is a model stream that records the result's
// PartialOutput each time the stream loop asks for the next chunk. The loop
// asks only after it has fully processed the previous chunk, so the recorded
// values are the partial outputs in publish order (TS partialOutputStream).
type r85streamPartialRecorder struct {
	chunks []provider.StreamChunk
	idx    int
	ready  chan struct{}
	res    **StreamTextResult

	mu       sync.Mutex
	partials []interface{}
}

func (s *r85streamPartialRecorder) observe() {
	select {
	case <-s.ready:
	case <-time.After(5 * time.Second):
		return
	}
	p := (*s.res).PartialOutput()
	if p == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.partials); n > 0 && reflect.DeepEqual(s.partials[n-1], p) {
		return
	}
	s.partials = append(s.partials, p)
}

func (s *r85streamPartialRecorder) Next() (*provider.StreamChunk, error) {
	s.observe()
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return &c, nil
}
func (s *r85streamPartialRecorder) Err() error   { return nil }
func (s *r85streamPartialRecorder) Close() error { return nil }

// stream-text.test.ts 23189: options.output > object output > should send
// partial output stream when last chunk contains content. The last delta
// closes the object, and its partial is still published.
func TestR85streamObjectPartialOutputLastChunkHasContent(t *testing.T) {
	var res *StreamTextResult
	rec := &r85streamPartialRecorder{
		chunks: r73streamTextStep(`{ `, `"value": `, `"Hello, `, `world!" }`),
		ready:  make(chan struct{}),
		res:    &res,
	}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return rec, nil }}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "prompt",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r73streamValueObjectSchema}),
	})
	if err != nil {
		t.Fatal(err)
	}
	res = r
	close(rec.ready)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	rec.observe()

	got, _ := json.Marshal(rec.partials)
	if want := `[{},{"value":"Hello, "},{"value":"Hello, world!"}]`; string(got) != want {
		t.Errorf("partial outputs = %s, want %s", got, want)
	}
}

// r85streamDiceUsage is the TS fixture's provider usage (total and the
// noCache/text details; cache and reasoning undefined).
func r85streamDiceUsage(in, out int64) *types.Usage {
	return &types.Usage{InputTokens: &in, OutputTokens: &out,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &in},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &out}}
}

// r85streamPart projects a full-stream part onto the TS snapshot keys
// (performance timings are left out: Go measures real time where the TS
// test mocks the clock).
func r85streamPart(c provider.StreamChunk) map[string]interface{} {
	p := map[string]interface{}{"type": string(c.Type)}
	switch c.Type {
	case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd:
		p["id"] = c.ID
	case provider.ChunkTypeText:
		p["id"], p["text"] = c.ID, c.Text
	case provider.ChunkTypeToolCall:
		p["toolCallId"], p["toolName"], p["input"], p["providerExecuted"] = c.ToolCall.ID, c.ToolCall.ToolName, c.ToolCall.Arguments, c.ToolCall.ProviderExecuted
	case provider.ChunkTypeToolResult:
		tr := c.ToolResult
		p["toolCallId"], p["toolName"], p["output"], p["providerExecuted"], p["dynamic"] = tr.ToolCallID, tr.ToolName, tr.Result, tr.ProviderExecuted, tr.Dynamic
		if tr.Input != nil {
			p["input"] = tr.Input
		}
	case provider.ChunkTypeFinishStep, provider.ChunkTypeFinish:
		p["finishReason"], p["rawFinishReason"] = string(c.FinishReason), c.RawFinishReason
		p["usage"] = c.Usage
		if len(c.ProviderMetadata) > 0 {
			p["providerMetadata"] = c.ProviderMetadata
		}
		if c.Response != nil {
			p["response"] = map[string]interface{}{"id": c.Response.ID, "modelId": c.Response.ModelID, "timestamp": c.Response.Timestamp.UnixMilli(), "headers": c.Response.Headers}
		}
	}
	b, _ := json.Marshal(p)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// stream-text.test.ts 28911: programmatic tool calling > 5 steps:
// code_execution triggers client tool across multiple turns (dice game
// fixture) > stream events > should emit correct stream parts including tool
// calls and deferred results. Same fixture as TS (ids, response metadata,
// headers, usage, container metadata, stdout shortened). The code_execution
// input is the parsed `{code}`: TS shows it after zod strips the fixture's
// extra "type" key, and Go JSON Schema validation does not strip keys.
func TestR85streamDiceGameStreamParts(t *testing.T) {
	const codeExecID = "srvtoolu_01MzSrFWsmzBdcoQkGWLyRjK"
	meta := json.RawMessage(`{"anthropic":{"container":{"id":"container_011CWHPPTDTn1XufeRB9uHeH"}}}`)
	respMeta := func(id string, ms int64, call string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
			ID: id, ModelID: "claude-sonnet-4-5-20250929", Timestamp: time.UnixMilli(ms).UTC(), Headers: map[string]string{"call": call}}}
	}
	roll := func(id, player string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "rollDie", Arguments: map[string]interface{}{"player": player}}}
	}
	toolStep := func(msg string, ms int64, call, id, player string) []provider.StreamChunk {
		return []provider.StreamChunk{respMeta(msg, ms, call), roll(id, player),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r85streamDiceUsage(0, 0), ProviderMetadata: meta}}
	}
	codeResult := map[string]interface{}{"type": "code_execution_result", "stdout": "=== DICE GAME ===\n", "stderr": "", "return_code": 0, "content": []interface{}{}}
	steps := [][]provider.StreamChunk{
		{
			respMeta("msg_01ERcBqAvLTHWQDk9c9qJLWC", 0, "1"),
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "I'll help you simulate this game."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: codeExecID, ToolName: "code_execution", Arguments: map[string]interface{}{"code": "game_loop()"}, ProviderExecuted: true}},
			roll("toolu_019jKkXz4jAdwHweHBw92CVY", "player1"),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r85streamDiceUsage(3369, 725), ProviderMetadata: meta},
		},
		toolStep("msg_01KSVw3xmXbMNJPNMt46BC5W", 1000, "2", "toolu_015dGLMbwBKv1ZRQr6KdJzeH", "player2"),
		toolStep("msg_016fLapHzDx8DG2SUcsGKyPA", 2000, "3", "toolu_01YYqBNq5mk1wMtv3PAqY44m", "player1"),
		toolStep("msg_01MQHz6AzmwmZoTry5nk5EQC", 3000, "4", "toolu_018WxjDkQG8h7i63poySGT2x", "player2"),
		{
			respMeta("msg_01CfmDducyrt61n4Q7QS8VFK", 4000, "5"),
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: codeExecID, ToolName: "code_execution", ProviderExecuted: true, Result: codeResult}},
			{Type: provider.ChunkTypeTextStart, ID: "2"},
			{Type: provider.ChunkTypeText, ID: "2", Text: "## Game Results"},
			{Type: provider.ChunkTypeTextEnd, ID: "2"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r85streamDiceUsage(4551, 197)},
		},
	}
	var mu sync.Mutex
	n := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			s := steps[n]
			n++
			return testutil.NewMockTextStream(s), nil
		}}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true, Parameters: map[string]interface{}{"type": "object"}},
			{Name: "rollDie", Description: "Roll a die and return the result.", Parameters: map[string]interface{}{"type": "object"},
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					if in["player"] == "player1" {
						return 6, nil
					}
					return 3, nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]interface{}
	fs := res.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		got = append(got, r85streamPart(*c))
	}

	want := []string{`{"type":"start"}`, `{"type":"start-step"}`}
	want = append(want,
		`{"type":"text-start","id":"1"}`,
		`{"type":"text","id":"1","text":"I'll help you simulate this game."}`,
		`{"type":"text-end","id":"1"}`,
		`{"type":"tool-call","toolCallId":"`+codeExecID+`","toolName":"code_execution","input":{"code":"game_loop()"},"providerExecuted":true}`,
		`{"type":"tool-call","toolCallId":"toolu_019jKkXz4jAdwHweHBw92CVY","toolName":"rollDie","input":{"player":"player1"},"providerExecuted":false}`,
		`{"type":"tool-result","toolCallId":"toolu_019jKkXz4jAdwHweHBw92CVY","toolName":"rollDie","input":{"player":"player1"},"output":6,"providerExecuted":false,"dynamic":false}`,
		`{"type":"finish-step","finishReason":"tool-calls","rawFinishReason":"","providerMetadata":`+string(meta)+`,"response":{"id":"msg_01ERcBqAvLTHWQDk9c9qJLWC","modelId":"claude-sonnet-4-5-20250929","timestamp":0,"headers":{"call":"1"}},"usage":{"inputTokens":3369,"inputTokenDetails":{"noCacheTokens":3369},"outputTokens":725,"outputTokenDetails":{"textTokens":725},"totalTokens":4094}}`,
	)
	for i, s := range []struct{ msg, call, id, player string }{
		{"msg_01KSVw3xmXbMNJPNMt46BC5W", "2", "toolu_015dGLMbwBKv1ZRQr6KdJzeH", "player2"},
		{"msg_016fLapHzDx8DG2SUcsGKyPA", "3", "toolu_01YYqBNq5mk1wMtv3PAqY44m", "player1"},
		{"msg_01MQHz6AzmwmZoTry5nk5EQC", "4", "toolu_018WxjDkQG8h7i63poySGT2x", "player2"},
	} {
		out := "3"
		if s.player == "player1" {
			out = "6"
		}
		ts := []string{"1000", "2000", "3000"}[i]
		want = append(want, `{"type":"start-step"}`,
			`{"type":"tool-call","toolCallId":"`+s.id+`","toolName":"rollDie","input":{"player":"`+s.player+`"},"providerExecuted":false}`,
			`{"type":"tool-result","toolCallId":"`+s.id+`","toolName":"rollDie","input":{"player":"`+s.player+`"},"output":`+out+`,"providerExecuted":false,"dynamic":false}`,
			`{"type":"finish-step","finishReason":"tool-calls","rawFinishReason":"","providerMetadata":`+string(meta)+`,"response":{"id":"`+s.msg+`","modelId":"claude-sonnet-4-5-20250929","timestamp":`+ts+`,"headers":{"call":"`+s.call+`"}},"usage":{"inputTokens":0,"inputTokenDetails":{"noCacheTokens":0},"outputTokens":0,"outputTokenDetails":{"textTokens":0},"totalTokens":0}}`,
		)
	}
	resultJSON, _ := json.Marshal(codeResult)
	want = append(want, `{"type":"start-step"}`,
		`{"type":"tool-result","toolCallId":"`+codeExecID+`","toolName":"code_execution","output":`+string(resultJSON)+`,"providerExecuted":true,"dynamic":false}`,
		`{"type":"text-start","id":"2"}`,
		`{"type":"text","id":"2","text":"## Game Results"}`,
		`{"type":"text-end","id":"2"}`,
		`{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"msg_01CfmDducyrt61n4Q7QS8VFK","modelId":"claude-sonnet-4-5-20250929","timestamp":4000,"headers":{"call":"5"}},"usage":{"inputTokens":4551,"inputTokenDetails":{"noCacheTokens":4551},"outputTokens":197,"outputTokenDetails":{"textTokens":197},"totalTokens":4748}}`,
		`{"type":"finish","finishReason":"stop","rawFinishReason":"stop","usage":{"inputTokens":7920,"inputTokenDetails":{"noCacheTokens":7920},"outputTokens":922,"outputTokenDetails":{"textTokens":922},"totalTokens":8842}}`,
	)
	var wantParts []map[string]interface{}
	for _, s := range want {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("bad want %s: %v", s, err)
		}
		wantParts = append(wantParts, m)
	}
	if !reflect.DeepEqual(got, wantParts) {
		g, _ := json.MarshalIndent(got, "", " ")
		t.Errorf("stream parts mismatch; got:\n%s", g)
	}
}

// TS convertToModelMessages (Node v24, packages/ai dist): an output-denied
// tool part's error text is `approval.reason ?? 'Tool call execution
// denied.'`, so a decoded empty reason stays empty and null or absent uses the
// default.
func TestR85streamOutputDeniedReasonNullish(t *testing.T) {
	for _, c := range []struct{ reason, want string }{
		{`,"reason":""`, ""},
		{``, "Tool call execution denied."},
		{`,"reason":null`, "Tool call execution denied."},
		{`,"reason":"no"`, "no"},
	} {
		var msg UIMessage
		raw := `{"id":"m","role":"assistant","parts":[{"type":"tool-x","toolCallId":"c1","state":"output-denied","input":{"a":1},"approval":{"id":"a1","approved":false` + c.reason + `}}]}`
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			t.Fatal(err)
		}
		out, err := ConvertToModelMessages(context.Background(), []UIMessage{msg})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 || len(out[1].Content) != 2 {
			t.Fatalf("%s: messages = %#v", c.reason, out)
		}
		tr, ok := out[1].Content[1].(types.ToolResultContent)
		if !ok || tr.Output == nil || tr.Output.Value != c.want {
			t.Errorf("%s: tool result = %#v, want error-text %q", c.reason, out[1].Content[1], c.want)
		}
		// The reason round-trips through the UI message JSON.
		b, _ := json.Marshal(msg)
		if c.reason == `,"reason":""` && !strings.Contains(string(b), `"reason":""`) {
			t.Errorf("empty reason not re-encoded: %s", b)
		}
	}
}

// TS readUIMessageStream (Node v24, packages/ai dist): a chunk type
// processUIMessageStream does not know (and abort) writes no snapshot, so
// start(messageId), text-start, text-delta and text-end give 4 snapshots.
func TestR85streamReadUIMessagesUnknownChunkNoSnapshot(t *testing.T) {
	chunks := []UIMessageChunk{
		{"type": "start", "messageId": "m1"},
		{"type": "text-start", "id": "t"},
		{"type": "text-delta", "id": "t", "delta": "hi"},
		{"type": "unknown-thing", "x": 1},
		{"type": "abort"},
		{"type": "text-end", "id": "t"},
		{"type": "finish"},
	}
	in := make(chan UIMessageChunk, len(chunks))
	for _, c := range chunks {
		in <- c
	}
	close(in)
	out, errCh := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: in})
	var got []string
	for m := range out {
		b, _ := json.Marshal(m.Parts)
		got = append(got, string(b))
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	want := []string{`[]`, `[{"type":"text","text":"","state":"streaming"}]`, `[{"type":"text","text":"hi","state":"streaming"}]`, `[{"type":"text","text":"hi","state":"done"}]`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("snapshots = %q, want %q", got, want)
	}
}

// r85streamReadUISnapshots runs chunks through ReadUIMessages and returns
// each snapshot's parts as JSON.
func r85streamReadUISnapshots(t *testing.T, chunks []UIMessageChunk) string {
	t.Helper()
	in := make(chan UIMessageChunk, len(chunks))
	for _, c := range chunks {
		in <- c
	}
	close(in)
	out, errCh := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: in})
	var parts []interface{}
	for m := range out {
		b, _ := json.Marshal(m.Parts)
		var v interface{}
		_ = json.Unmarshal(b, &v)
		parts = append(parts, v)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(parts)
	return string(b)
}

func r85streamJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Snapshots recorded from TS readUIMessageStream (Node v24, packages/ai dist).
// A tool part that gets its output (and approval request) before any input
// keeps the input key absent instead of null.
func TestR85streamReadUIToolPartWithoutInput(t *testing.T) {
	got := r85streamReadUISnapshots(t, []UIMessageChunk{
		{"type": "tool-input-start", "toolCallId": "c1", "toolName": "t1"},
		{"type": "tool-output-available", "toolCallId": "c1", "output": map[string]interface{}{"r": 1}},
		{"type": "tool-approval-request", "toolCallId": "c1", "approvalId": "ap1"},
	})
	r85streamJSONEqual(t, got, `[[{"type":"tool-t1","toolCallId":"c1","state":"input-streaming"}],[{"type":"tool-t1","toolCallId":"c1","state":"output-available","output":{"r":1}}],[{"type":"tool-t1","toolCallId":"c1","state":"approval-requested","output":{"r":1},"approval":{"id":"ap1"}}]]`)

	// A decode/encode round trip of such a part keeps the key absent, and a
	// Go-built part with a nil Input still encodes input: null.
	var p ToolUIPart
	if err := json.Unmarshal([]byte(`{"type":"tool-t1","toolCallId":"c1","state":"output-available","output":1}`), &p); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(p); strings.Contains(string(b), `"input"`) {
		t.Errorf("round trip added input: %s", b)
	}
	if b, _ := json.Marshal(ToolUIPart{Type: "tool-t1", ToolCallID: "c1", State: ToolStateOutputAvailable, Output: 1}); !strings.Contains(string(b), `"input":null`) {
		t.Errorf("Go-built part lost input null: %s", b)
	}
}

// TS updateDynamicToolPart pushes a new dynamic part without rawInput and
// assigns rawInput only to an existing one.
func TestR85streamReadUINewDynamicPartWithoutRawInput(t *testing.T) {
	got := r85streamReadUISnapshots(t, []UIMessageChunk{
		{"type": "tool-input-start", "toolCallId": "c2", "toolName": "t2", "dynamic": true},
		{"type": "tool-output-error", "toolCallId": "c2", "errorText": "boom"},
		{"type": "start-step"},
		{"type": "tool-input-delta", "toolCallId": "c2", "inputTextDelta": `{"a":1`},
		{"type": "tool-input-delta", "toolCallId": "c2", "inputTextDelta": `}`},
	})
	r85streamJSONEqual(t, got, `[[{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"input-streaming"}],[{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"output-error","errorText":"boom"}],[{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"output-error","errorText":"boom"},{"type":"step-start"},{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"input-streaming","input":{"a":1}}],[{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"output-error","errorText":"boom"},{"type":"step-start"},{"type":"dynamic-tool","toolName":"t2","toolCallId":"c2","state":"input-streaming","input":{"a":1},"rawInput":"{\"a\":1}"}]]`)
}
