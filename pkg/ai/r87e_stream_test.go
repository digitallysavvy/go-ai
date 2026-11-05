package ai

import (
	"encoding/json"
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
