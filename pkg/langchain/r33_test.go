package langchain

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

func runAny(t *testing.T, events ...interface{}) []ai.UIMessageChunk {
	t.Helper()
	in := make(chan interface{}, len(events))
	for _, e := range events {
		in <- e
	}
	close(in)
	out, errs := ToUIMessageStreamFromAny(context.Background(), in)
	return collectOutputChunks(t, out, errs)
}

func chunkTypes(chunks []ai.UIMessageChunk) []string {
	var out []string
	for _, c := range chunks {
		s, _ := c["type"].(string)
		out = append(out, s)
	}
	return out
}

// TS isStreamEventsEvent accepts any object (typeof 'object', arrays too) as
// `data`; the event-level run_id then names the message.
func TestR33StreamEventsWithArrayDataIsDetected(t *testing.T) {
	chunks := runAny(t,
		map[string]interface{}{"event": "on_chat_model_start", "run_id": "run-1", "data": []interface{}{}},
		map[string]interface{}{"event": "on_chat_model_stream", "data": map[string]interface{}{"chunk": map[string]interface{}{"content": "hi"}}},
	)
	types := chunkTypes(chunks)
	want := []string{"start", "text-start", "text-delta", "text-end", "finish"}
	if len(types) != len(want) {
		t.Fatalf("chunks %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("chunks %v, want %v", types, want)
		}
	}
	if chunks[1]["id"] != "run-1" {
		t.Fatalf("text-start id %v, want the event run_id", chunks[1]["id"])
	}
}

// TS reads a message's fields from kwargs only for a serialized
// {type: "constructor"} message, and a tool message is type "tool" or a
// serialized ToolMessage: a "ToolMessage" type string is neither. A message
// without content has no text (there is no `text` fallback).
func TestR33MessageShapesMatchTSGuards(t *testing.T) {
	messages := func(msgs ...map[string]interface{}) []ai.UIMessageChunk {
		in := make(chan StreamEvent, len(msgs))
		for _, m := range msgs {
			in <- StreamEvent{Mode: "messages", Data: []interface{}{m}}
		}
		close(in)
		out, errs := ToUIMessageStream(context.Background(), in)
		return collectOutputChunks(t, out, errs)
	}
	// kwargs on a non-constructor message are not the data source.
	deltas := chunksOfType(messages(map[string]interface{}{"id": "m1", "type": "ai", "content": "top", "kwargs": map[string]interface{}{"content": "nested"}}), "text-delta")
	if len(deltas) != 1 || deltas[0]["delta"] != "top" {
		t.Fatalf("text-delta %v, want the top-level content", deltas)
	}
	// type "ToolMessage" is not a tool message.
	for _, c := range messages(map[string]interface{}{"id": "t1", "type": "ToolMessage", "tool_call_id": "c1", "content": "x"}) {
		if c["type"] == "tool-output-available" || c["type"] == "tool-input-start" {
			t.Fatalf("unexpected tool chunk %v", c)
		}
	}
	// No content: a `text` field is not read.
	for _, c := range messages(map[string]interface{}{"id": "m2", "type": "ai", "text": "ignored"}) {
		if c["type"] == "text-delta" {
			t.Fatalf("unexpected text-delta %v", c)
		}
	}
}
