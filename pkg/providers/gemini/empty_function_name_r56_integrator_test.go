package gemini

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r56 integrator: TS `name != null` also decides the streaming
// classification, so an empty-string name opens a streaming call, starts one
// that carries partialArgs, and is not a terminal chunk.
func TestR56StreamEmptyFunctionCallNameStreamingClassification(t *testing.T) {
	findCall := func(t *testing.T, chunks []string, id string) *types.ToolCall {
		t.Helper()
		frames := make([]string, 0, len(chunks)+1)
		for _, c := range chunks {
			frames = append(frames, `{"candidates":[{"content":{"parts":[{"functionCall":`+c+`}]}}]}`)
		}
		frames = append(frames, "[DONE]")
		for _, c := range toolCallsOf(drainStream(t, newTestStream(sseStream(frames...)))) {
			if c.ToolCall.ID == id {
				return c.ToolCall
			}
		}
		return nil
	}

	t.Run("willContinue opens a streaming call", func(t *testing.T) {
		call := findCall(t, []string{
			`{"id":"c1","name":"","willContinue":true}`,
			`{"partialArgs":[{"jsonPath":"$.a","stringValue":"x"}]}`,
			`{}`,
		}, "c1")
		if call == nil || call.ToolName != "" || call.Arguments["a"] != "x" {
			t.Fatalf("tool call = %#v, want c1 with an empty name and a=x", call)
		}
	})

	t.Run("partialArgs with an empty name starts a call", func(t *testing.T) {
		call := findCall(t, []string{
			`{"id":"c1","name":"","partialArgs":[{"jsonPath":"$.a","stringValue":"x"}]}`,
		}, "c1")
		if call == nil || call.ToolName != "" || call.Arguments["a"] != "x" {
			t.Fatalf("tool call = %#v, want c1 with an empty name and a=x", call)
		}
	})

	t.Run("empty name is not a terminal chunk", func(t *testing.T) {
		chunks := []string{
			`{"id":"c1","name":"f","partialArgs":[{"jsonPath":"$.a","stringValue":"x","willContinue":true}]}`,
			`{"id":"c2","name":""}`,
			`{}`,
		}
		if call := findCall(t, chunks, "c2"); call == nil || call.ToolName != "" {
			t.Fatalf("tool call c2 = %#v, want a no-args call with an empty name", call)
		}
	})
}
