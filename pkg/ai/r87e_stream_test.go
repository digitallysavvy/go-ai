package ai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r87estreamTSChunkJSON is JSON.stringify(toUIMessageChunk(part)) for parts
// with every optional field set, run with bun on TS 3fab50aa9b
// (packages/ai/src/ui-message-stream/to-ui-message-chunk.ts), plus the
// create-ui-message-stream.ts error chunk and the to-ui-message-stream.ts
// message-metadata chunk.
var r87estreamTSChunkJSON = []string{
	`{"type":"start","messageMetadata":{"m":1},"messageId":"msg-1"}`,
	`{"type":"start-step"}`,
	`{"type":"text-start","id":"t1","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"text-delta","id":"t1","delta":"hi","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"text-end","id":"t1","citations":[{"type":"url","url":"u"}],"providerMetadata":{"p":{"k":1}}}`,
	`{"type":"reasoning-start","id":"r1","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"reasoning-delta","id":"r1","delta":"th","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"reasoning-end","id":"r1","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"file","mediaType":"image/png","url":"data:image/png;base64,AA==","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"reasoning-file","mediaType":"image/png","url":"data:image/png;base64,AA==","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"source-url","sourceId":"s1","url":"https://x","title":"T","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"source-document","sourceId":"s2","mediaType":"text/plain","title":"D","filename":"f.txt","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"custom","kind":"a.b","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"tool-input-start","toolCallId":"c2","toolName":"unknown","inputEncoding":"json","providerExecuted":true,"providerMetadata":{"p":{"k":1}},"toolMetadata":{"owner":"x"},"dynamic":false,"title":"U"}`,
	`{"type":"tool-input-delta","toolCallId":"c1","inputTextDelta":"{}"}`,
	`{"type":"tool-input-available","toolCallId":"c2","toolName":"unknown","input":{"a":1},"providerExecuted":true,"providerMetadata":{"p":{"k":1}},"toolMetadata":{"owner":"x"},"dynamic":true,"title":"U"}`,
	`{"type":"tool-input-error","toolCallId":"c2","toolName":"unknown","input":"x","providerExecuted":true,"providerMetadata":{"p":{"k":1}},"toolMetadata":{"owner":"x"},"dynamic":true,"errorText":"An error occurred.","title":"U"}`,
	`{"type":"tool-approval-request","approvalId":"a1","toolCallId":"c1","reason":"r","isAutomatic":true,"signature":"sig"}`,
	`{"type":"tool-approval-response","approvalId":"a1","approved":true,"reason":"ok","providerExecuted":false}`,
	`{"type":"tool-output-available","toolCallId":"c2","output":{"o":1},"providerExecuted":true,"providerMetadata":{"p":{"k":1}},"toolMetadata":{"owner":"x"},"preliminary":true,"dynamic":true}`,
	`{"type":"tool-output-error","toolCallId":"c2","errorText":"{\"code\":\"x\"}","providerExecuted":true,"providerMetadata":{"p":{"k":1}},"toolMetadata":{"owner":"x"},"dynamic":true}`,
	`{"type":"tool-output-denied","toolCallId":"c1"}`,
	`{"type":"error","errorText":"An error occurred."}`,
	`{"type":"finish-step"}`,
	`{"type":"abort","reason":"why"}`,
	`{"type":"finish","finishReason":"stop","messageMetadata":{"m":1}}`,
	`{"type":"message-metadata","messageMetadata":{"m":1}}`,
}

// TS JsonToSseTransformStream writes JSON.stringify(chunk): the keys in the
// order toUIMessageChunk creates them, not sorted.
func TestR87estreamUISSEChunkKeyOrderMatchesTS(t *testing.T) {
	for _, want := range r87estreamTSChunkJSON {
		var chunk UIMessageChunk
		if err := json.Unmarshal([]byte(want), &chunk); err != nil {
			t.Fatal(err)
		}
		event, err := uiSSEEventBytes(chunk)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(event); got != "data: "+want+"\n\n" {
			t.Errorf("event = %q\nwant  %q", got, "data: "+want+"\n\n")
		}
	}
}

// Keys TS does not create follow in sorted order; a data chunk keeps the
// schema order (type, id, data, transient).
func TestR87estreamUISSEChunkKeyOrderUnknownKeys(t *testing.T) {
	for _, tc := range []struct {
		chunk UIMessageChunk
		want  string
	}{
		{UIMessageChunk{"transient": true, "data": map[string]interface{}{"b": 1, "a": 2}, "id": "d1", "type": "data-weather"}, `{"type":"data-weather","id":"d1","data":{"a":2,"b":1},"transient":true}`},
		{UIMessageChunk{"zeta": 1, "alpha": 2, "type": "x-custom"}, `{"type":"x-custom","alpha":2,"zeta":1}`},
		{UIMessageChunk{"errorText": "<b>&", "extra": "e", "type": "error"}, `{"type":"error","errorText":"<b>&","extra":"e"}`},
	} {
		event, err := uiSSEEventBytes(tc.chunk)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(event); got != "data: "+tc.want+"\n\n" {
			t.Errorf("event = %q, want %q", got, tc.want)
		}
	}
}

// TS (bun, 3fab50aa9b): streamText sets dynamic on every tool-input-start
// (`chunk.dynamic ?? tool?.type === 'dynamic'`) and toUIMessageChunk copies
// it for a tool missing from the tool set: {"dynamic":false} for an unknown
// tool, nothing for a static tool in the set, true for a dynamic tool.
func TestR87estreamToolInputStartDynamicFalse(t *testing.T) {
	tools := []types.Tool{{Name: "known"}, {Name: "dyn", Type: types.ToolTypeDynamic}}
	start := func(name string, dynamic bool) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c-" + name, ToolName: name, Dynamic: dynamic}}
	}
	for _, tc := range []struct {
		name    string
		tools   []types.Tool
		chunk   provider.StreamChunk
		dynamic interface{}
	}{
		{"static tool in the set", tools, start("known", false), nil},
		{"unknown tool", tools, start("unknown", false), false},
		{"provider dynamic tool not in the set", tools, start("provdyn", true), true},
		{"dynamic tool in the set", tools, start("dyn", true), true},
		{"no tool set, static tool", nil, start("known", false), false},
		{"no tool set, dynamic tool", nil, start("dyn", true), true},
	} {
		parts := convertProviderChunkToUIMessageChunks(tc.chunk, resultChunkConversionOptions{OnError: func(error) string { return "" }, Tools: tc.tools})
		if len(parts) != 1 {
			t.Fatalf("%s: parts = %v", tc.name, parts)
		}
		got, present := parts[0]["dynamic"]
		if tc.dynamic == nil {
			if present {
				t.Errorf("%s: dynamic = %v, want absent", tc.name, got)
			}
			continue
		}
		if !present || got != tc.dynamic {
			t.Errorf("%s: dynamic = %v (present %v), want %v", tc.name, got, present, tc.dynamic)
		}
	}
}

// TS (bun, 3fab50aa9b): a provider-executed tool-error whose value is null
// writes errorText JSON.stringify(null), "null". Go reads a nil result as
// JSON null (v0.5 wrote "tool error").
func TestR87estreamProviderToolErrorNilResult(t *testing.T) {
	isErr := true
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "web", ProviderExecuted: true, Dynamic: true, RawArguments: "{}"}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c1", ToolName: "web", IsError: &isErr, ProviderExecuted: true, Dynamic: true}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}),
		Prompt: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks, _ := CreateUIMessageStream(context.Background(), r)
	var got UIMessageChunk
	for c := range chunks {
		if c["type"] == "tool-output-error" {
			got = c
		}
	}
	if got == nil || got["errorText"] != "null" {
		t.Fatalf("tool-output-error = %v, want errorText \"null\"", got)
	}
}

// TS mergeObjects (bun, 3fab50aa9b, util/merge-objects.ts): base + incoming
// => JSON.stringify(result), or the TypeError `key in base` throws (V8 text,
// node 24).
var r87estreamTSMergeCases = []struct{ base, incoming, want string }{
	{`{"a":1,"n":{"x":1}}`, `"ab"`, `{"0":"a","1":"b","a":1,"n":{"x":1}}`},
	{`{"a":1,"n":{"x":1}}`, `[1,2]`, `{"0":1,"1":2,"a":1,"n":{"x":1}}`},
	{`{"a":1,"n":{"x":1}}`, `5`, `{"a":1,"n":{"x":1}}`},
	{`{"a":1,"n":{"x":1}}`, `true`, `{"a":1,"n":{"x":1}}`},
	{`{"a":1,"n":{"x":1}}`, `{"n":{"y":2},"b":"z"}`, `{"a":1,"n":{"x":1,"y":2},"b":"z"}`},
	{`"ab"`, `{"a":1,"n":{"x":1}}`, `TypeError: Cannot use 'in' operator to search for 'a' in ab`},
	{`"ab"`, `"ab"`, `TypeError: Cannot use 'in' operator to search for '0' in ab`},
	{`"ab"`, `5`, `{"0":"a","1":"b"}`},
	{`"ab"`, `true`, `{"0":"a","1":"b"}`},
	{`[1,2]`, `{"a":1,"n":{"x":1}}`, `{"0":1,"1":2,"a":1,"n":{"x":1}}`},
	{`[1,2]`, `"ab"`, `{"0":"a","1":"b"}`},
	{`[1,2]`, `5`, `{"0":1,"1":2}`},
	{`5`, `{"a":1,"n":{"x":1}}`, `TypeError: Cannot use 'in' operator to search for 'a' in 5`},
	{`5`, `[1,2]`, `TypeError: Cannot use 'in' operator to search for '0' in 5`},
	{`5`, `5`, `{}`},
	{`true`, `true`, `{}`},
	{`true`, `"ab"`, `TypeError: Cannot use 'in' operator to search for '0' in true`},
	{`1.5`, `{"a":1}`, `TypeError: Cannot use 'in' operator to search for 'a' in 1.5`},
}

func r87estreamNormJSON(t *testing.T, text string) string {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// UI message metadata merges like TS mergeObjects for every JSON value pair,
// through the chunk path; a merge TS throws on fails the processing.
func TestR87estreamMessageMetadataMergeMatchesTS(t *testing.T) {
	for _, tc := range r87estreamTSMergeCases {
		var base, incoming interface{}
		if err := json.Unmarshal([]byte(tc.base), &base); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tc.incoming), &incoming); err != nil {
			t.Fatal(err)
		}
		s := newUIMessageCallbackState(nil, "m")
		if err := applyUIStateChunk(s, UIMessageChunk{"type": "start", "messageMetadata": base}, nil); err != nil {
			t.Fatal(err)
		}
		err := applyUIStateChunk(s, UIMessageChunk{"type": "message-metadata", "messageMetadata": incoming}, nil)
		if strings.HasPrefix(tc.want, "TypeError: ") {
			if err == nil || err.Error() != strings.TrimPrefix(tc.want, "TypeError: ") {
				t.Errorf("%s + %s: err = %v, want %s", tc.base, tc.incoming, err, tc.want)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s + %s: %v", tc.base, tc.incoming, err)
		}
		b, _ := json.Marshal(s.message["metadata"])
		if got, want := string(b), r87estreamNormJSON(t, tc.want); got != want {
			t.Errorf("%s + %s = %s, want %s", tc.base, tc.incoming, got, want)
		}
	}
}

// The TS TypeError ends a CreateUIMessageStreamWithOptions run as a
// processing failure: the error is returned and onEnd sees a failed outcome.
func TestR87estreamMessageMetadataMergeTypeErrorFailsStream(t *testing.T) {
	var ended map[string]interface{}
	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "start", "messageMetadata": "ab"})
			w.Write(UIMessageChunk{"type": "message-metadata", "messageMetadata": map[string]interface{}{"a": 1.0}})
		},
		OnEnd: func(e map[string]interface{}) { ended = e },
	})
	for range chunks {
	}
	err := <-errs
	if err == nil || err.Error() != "Cannot use 'in' operator to search for 'a' in ab" {
		t.Fatalf("err = %v", err)
	}
	if ended == nil {
		t.Fatal("onEnd not called")
	}
	if outcome, _ := ended["outcome"].(map[string]interface{}); outcome["status"] != string(UIMessageStreamOutcomeFailed) {
		t.Fatalf("outcome = %#v", ended["outcome"])
	}
}

// TS pipeUIMessageStreamToResponse / createUIMessageStreamResponse end an
// errored stream without [DONE]. With StreamErrors the chunk-channel helpers
// see the failure (a callback that panics errors the TS stream).
func TestR87estreamChunkHelpersStreamErrors(t *testing.T) {
	run := func() (<-chan UIMessageChunk, <-chan error) {
		return CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				w.Write(UIMessageChunk{"type": "start"})
				w.Write(UIMessageChunk{"type": "finish-step"})
			},
			OnStepEnd: func(map[string]interface{}) { panic("step callback failed") },
			OnError:   func(error) string { panic("onError failed") },
		})
	}
	chunks, errs := run()
	var buf strings.Builder
	err := PipeUIMessageChunksToResponse(chunks, &buf, &UIMessageStreamResponseInit{StreamErrors: errs})
	if err == nil || strings.Contains(buf.String(), "[DONE]") {
		t.Fatalf("pipe err = %v, body = %q; want the stream error and no [DONE]", err, buf.String())
	}
	chunks, errs = run()
	resp, err := CreateUIMessageChunksResponse(chunks, &UIMessageStreamResponseInit{StreamErrors: errs})
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr == nil || strings.Contains(string(body), "[DONE]") {
		t.Fatalf("body err = %v, body = %q; want the stream error and no [DONE]", readErr, body)
	}
	// Without StreamErrors the v0.5 behavior stays.
	chunks, _ = run()
	buf.Reset()
	if err := PipeUIMessageChunksToResponse(chunks, &buf, nil); err != nil || !strings.HasSuffix(buf.String(), "data: [DONE]\n\n") {
		t.Fatalf("pipe err = %v, body = %q", err, buf.String())
	}
	// A clean stream still ends with [DONE].
	chunks, errs = CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{Execute: func(w UIMessageStreamWriter) {
		w.Write(UIMessageChunk{"type": "start"})
	}})
	buf.Reset()
	if err := PipeUIMessageChunksToResponse(chunks, &buf, &UIMessageStreamResponseInit{StreamErrors: errs}); err != nil || !strings.HasSuffix(buf.String(), "data: [DONE]\n\n") {
		t.Fatalf("clean pipe err = %v, body = %q", err, buf.String())
	}
}
