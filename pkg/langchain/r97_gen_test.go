package langchain

import (
	"context"
	"io"
	"strings"
	"testing"
)

func r97genRun(t *testing.T, events ...StreamEvent) []map[string]interface{} {
	t.Helper()
	ch := make(chan StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	var out []map[string]interface{}
	for _, c := range collectChunks(t, ch) {
		out = append(out, c)
	}
	return out
}

func r97genTypes(chunks []map[string]interface{}) []string {
	types := make([]string, 0, len(chunks))
	for _, c := range chunks {
		types = append(types, c["type"].(string))
	}
	return types
}

// TS `typeof metadata.langgraph_step === 'number'` compares the step as is:
// 1 then 1.5 is a step change (finish-step, start-step).
func TestR97genLangGraphFractionalStep(t *testing.T) {
	msg := func(id string, step float64) StreamEvent {
		return StreamEvent{Mode: "messages", Data: []interface{}{
			map[string]interface{}{"type": "ai", "id": id, "content": "x"},
			map[string]interface{}{"langgraph_step": step},
		}}
	}
	chunks := r97genRun(t, msg("m1", 1), msg("m2", 1.5))
	got := r97genTypes(chunks)
	want := []string{"start", "start-step", "text-start", "text-delta", "text-end", "finish-step", "start-step", "text-start", "text-delta", "text-end", "finish-step", "finish"}
	if len(got) != len(want) {
		t.Fatalf("chunks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", got, want)
		}
	}
}

// TS keys streamed tool call info by `toolCallChunk.index ?? 0` in a Map: a
// chunk with index 1.5 or "1" does not find the call stored under index 1.
func TestR97genToolCallChunkIndexKey(t *testing.T) {
	for _, index := range []interface{}{1.5, "1"} {
		chunks := r97genRun(t,
			StreamEvent{Mode: "messages", Data: []interface{}{map[string]interface{}{
				"type": "ai", "id": "m1", "content": "",
				"tool_call_chunks": []interface{}{map[string]interface{}{"id": "c1", "name": "get", "index": float64(1), "args": ""}},
			}}},
			StreamEvent{Mode: "messages", Data: []interface{}{map[string]interface{}{
				"type": "ai", "id": "m1", "content": "",
				"tool_call_chunks": []interface{}{map[string]interface{}{"index": index, "args": `{"a":1}`}},
			}}},
		)
		if deltas := r97genOfType(chunks, "tool-input-delta"); len(deltas) != 0 {
			t.Fatalf("index %#v: tool-input-delta = %#v, want none", index, deltas)
		}
		if starts := r97genOfType(chunks, "tool-input-start"); len(starts) != 1 {
			t.Fatalf("index %#v: tool-input-start = %#v, want one", index, starts)
		}
	}
	// The same number keeps finding the call; an absent index is 0.
	chunks := r97genRun(t,
		StreamEvent{Mode: "messages", Data: []interface{}{map[string]interface{}{
			"type": "ai", "id": "m1", "content": "",
			"tool_call_chunks": []interface{}{map[string]interface{}{"id": "c1", "name": "get", "args": ""}},
		}}},
		StreamEvent{Mode: "messages", Data: []interface{}{map[string]interface{}{
			"type": "ai", "id": "m1", "content": "",
			"tool_call_chunks": []interface{}{map[string]interface{}{"index": float64(0), "args": `{}`}},
		}}},
	)
	if deltas := r97genOfType(chunks, "tool-input-delta"); len(deltas) != 1 || deltas[0]["toolCallId"] != "c1" {
		t.Fatalf("tool-input-delta = %#v, want one for c1", deltas)
	}
}

// The source-document id is a TS template literal: numbers are JS
// String(number).
func TestR97genSourceDocumentIDNumberText(t *testing.T) {
	chunks := r97genRun(t, StreamEvent{Mode: "messages", Data: []interface{}{map[string]interface{}{
		"type": "ai", "id": "m1", "content": []interface{}{map[string]interface{}{
			"type": "text", "text": "hi",
			"annotations": []interface{}{map[string]interface{}{
				"type": "citation", "title": "T", "citedText": "x", "startIndex": 1e21, "endIndex": 0.0000001,
			}},
		}},
	}}})
	docs := r97genOfType(chunks, "source-document")
	if len(docs) != 1 || docs[0]["sourceId"] != "m1:T:x:1e+21:1e-7" {
		t.Fatalf("source-document = %#v, want sourceId m1:T:x:1e+21:1e-7", docs)
	}
}

// TS handles an action request with an empty name like any other name.
func TestR97genInterruptEmptyActionName(t *testing.T) {
	chunks := r97genRun(t, StreamEvent{Mode: "values", Data: map[string]interface{}{
		"__interrupt__": []interface{}{map[string]interface{}{"value": map[string]interface{}{
			"actionRequests": []interface{}{
				map[string]interface{}{"name": "", "args": map[string]interface{}{"a": float64(1)}, "id": "r1"},
				map[string]interface{}{"args": map[string]interface{}{"a": float64(2)}, "id": "r2"},
			},
		}}},
	}})
	starts := r97genOfType(chunks, "tool-input-start")
	approvals := r97genOfType(chunks, "tool-approval-request")
	if len(starts) != 1 || starts[0]["toolCallId"] != "r1" || starts[0]["toolName"] != "" {
		t.Fatalf("tool-input-start = %#v, want one for r1 with toolName \"\"", starts)
	}
	if len(approvals) != 1 || approvals[0]["toolCallId"] != "r1" {
		t.Fatalf("tool-approval-request = %#v, want one for r1", approvals)
	}
}

func r97genOfType(chunks []map[string]interface{}, typ string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, c := range chunks {
		if c["type"] == typ {
			out = append(out, c)
		}
	}
	return out
}

// TS matches an interrupt's action request to a tool call by
// `${name}:${JSON.stringify(args)}` in wire key order: the same keys in a
// different order do not match, so the approval gets its own id.
func TestR97genInterruptToolCallKeyOrder(t *testing.T) {
	restore := nowMillis
	nowMillis = func() int64 { return 42 }
	defer func() { nowMillis = restore }()
	run := func(callArgs, requestArgs string) []map[string]interface{} {
		sse := "event: values\ndata: {\"messages\":[{\"type\":\"ai\",\"id\":\"m1\",\"content\":\"\",\"tool_calls\":[{\"id\":\"c1\",\"name\":\"get\",\"args\":" + callArgs + "}]}]," +
			"\"__interrupt__\":[{\"value\":{\"actionRequests\":[{\"name\":\"get\",\"args\":" + requestArgs + "}]}}]}\n\n"
		events := readRemoteGraphEvents(context.Background(), io.NopCloser(strings.NewReader(sse)))
		var out []map[string]interface{}
		for _, c := range collectChunks(t, events) {
			out = append(out, c)
		}
		return out
	}
	approvals := r97genOfType(run(`{"a":1,"b":2}`, `{"a":1,"b":2}`), "tool-approval-request")
	if len(approvals) != 1 || approvals[0]["toolCallId"] != "c1" {
		t.Fatalf("same order: approvals = %#v, want c1", approvals)
	}
	approvals = r97genOfType(run(`{"a":1,"b":2}`, `{"b":2,"a":1}`), "tool-approval-request")
	if len(approvals) != 1 || approvals[0]["toolCallId"] != "hitl-get-42" {
		t.Fatalf("different order: approvals = %#v, want hitl-get-42", approvals)
	}
}
