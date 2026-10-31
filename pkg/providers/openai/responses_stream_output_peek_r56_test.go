package openai

import (
	"strings"
	"testing"
)

// TS isResponseOutputChunk: an output_item.added/done whose item type the
// chunk schema does not model parses as unknown_chunk, which does not end the
// early-error peek, so an error frame after it still fails the call with
// nothing delivered.
func TestResponsesStreamUnmodeledItemDoesNotEndErrorPeek(t *testing.T) {
	for _, event := range []string{"response.output_item.added", "response.output_item.done"} {
		t.Run(event, func(t *testing.T) {
			sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"` + event + `","output_index":0,"item":{"type":"some_future_call","id":"x1"}}

data: {"type":"error","sequence_number":2,"code":"server_error","message":"boom"}

`
			got, err := drainResponsesRawLabels(t, sse)
			if err == nil {
				t.Fatalf("expected the call to fail before output, got chunks %v", got)
			}
			if len(got) != 0 {
				t.Fatalf("chunks before the error = %v, want none", got)
			}
		})
	}
}

// response.function_call_arguments.done is a modeled chunk, so it ends the
// peek: a later error frame is an error part of a delivered stream.
func TestResponsesStreamFunctionCallArgumentsDoneEndsErrorPeek(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.function_call_arguments.done","item_id":"fc1","output_index":0,"arguments":"{}"}

data: {"type":"error","sequence_number":2,"code":"server_error","message":"boom"}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err != nil {
		t.Fatalf("stream error = %v, want an error part (chunks %v)", err, got)
	}
	want := []string{
		"raw:response.created",
		"response-metadata",
		"raw:response.function_call_arguments.done",
		"raw:error",
		"error",
		"finish",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks =\n%v\nwant\n%v", got, want)
	}
}
