package gemini

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tests for the per-chunk independent function-call streaming model ported
// from TS google-language-model.ts (commits 5036db8, a2609df, cfca634):
// GoogleJSONAccumulator-backed partialArgs streaming, replacing the old
// whole-value-diffing toolInputAccum.

// drainStream consumes every chunk from s until EOF/error.
func drainStream(t *testing.T, s *stream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("unexpected stream error: %v", err)
			}
			break
		}
		chunks = append(chunks, c)
	}
	return chunks
}

func toolCallsOf(chunks []*provider.StreamChunk) []*provider.StreamChunk {
	var out []*provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			out = append(out, c)
		}
	}
	return out
}

// TestStream_PartialArgsStreaming_SingleCall ports the TS behavior of a
// function call whose arguments arrive incrementally via `partialArgs`
// leaves across multiple SSE chunks, finishing on a terminal empty chunk.
func TestStream_PartialArgsStreaming_SingleCall(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"get_weather","partialArgs":[{"jsonPath":"$.location","stringValue":"Bos","willContinue":true}]}}]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.location","stringValue":"ton"}]}}]}}]}`
	chunk3 := `{"candidates":[{"content":{"parts":[{"functionCall":{}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2, chunk3, "[DONE]"))
	chunks := drainStream(t, s)

	var gotStart, gotEnd bool
	var deltas []string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			gotStart = true
			if c.ToolCall.ID != "call-1" || c.ToolCall.ToolName != "get_weather" {
				t.Fatalf("tool-input-start = %+v", c.ToolCall)
			}
		case provider.ChunkTypeToolInputDelta:
			if c.ID != "call-1" {
				t.Fatalf("tool-input-delta ID = %q, want call-1", c.ID)
			}
			deltas = append(deltas, c.Text)
		case provider.ChunkTypeToolInputEnd:
			gotEnd = true
		}
	}
	if !gotStart || !gotEnd {
		t.Fatalf("expected tool-input-start and tool-input-end, chunks=%v", chunkTypes(chunks))
	}

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.ID != "call-1" || tc.ToolName != "get_weather" {
		t.Fatalf("tool-call = %+v", tc)
	}
	if got, want := tc.Arguments["location"], "Boston"; got != want {
		t.Fatalf("Arguments[location] = %v, want %v", got, want)
	}
	if tc.RawArguments != `{"location":"Boston"}` {
		t.Fatalf("RawArguments = %q", tc.RawArguments)
	}

	joined := ""
	for _, d := range deltas {
		joined += d
	}
	if joined != tc.RawArguments {
		t.Fatalf("concatenated deltas %q != final RawArguments %q", joined, tc.RawArguments)
	}
}

// TestStream_PartialArgsStreaming_PreservesInsertionOrder verifies that keys
// arriving out of alphabetical order stay in that (wire) order in the final
// RawArguments string — the core "insertion-order-preserving args" guarantee
// that a plain Go map (whose json.Marshal sorts keys) cannot provide.
func TestStream_PartialArgsStreaming_PreservesInsertionOrder(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"set","partialArgs":[{"jsonPath":"$.zebra","stringValue":"z"},{"jsonPath":"$.apple","stringValue":"a"}]}}]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[{"functionCall":{}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d", len(calls))
	}
	if got, want := calls[0].ToolCall.RawArguments, `{"zebra":"z","apple":"a"}`; got != want {
		t.Fatalf("RawArguments = %q, want %q (insertion order, not alphabetical)", got, want)
	}
}

// TestStream_PartialArgsStreaming_TwoConcurrentCalls verifies the LIFO stack
// model: a continuation chunk carrying `partialArgs` with no `name` always
// applies to the most recently opened (topmost) active call.
func TestStream_PartialArgsStreaming_TwoConcurrentCalls(t *testing.T) {
	openFirst := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-a","name":"tool_a","partialArgs":[{"jsonPath":"$.x","numberValue":1,"willContinue":true}]}}]}}]}`
	openSecond := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-b","name":"tool_b","partialArgs":[{"jsonPath":"$.y","numberValue":2}]}}]}}]}` // completes immediately (no willContinue)
	continueFirst := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.x2","numberValue":3}]}}]}}]}`
	finishFirst := `{"candidates":[{"content":{"parts":[{"functionCall":{}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(openFirst, openSecond, continueFirst, finishFirst, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool-calls, got %d: %v", len(calls), chunkTypes(chunks))
	}

	// tool_b (pushed second, with no willContinue) finishes first.
	if calls[0].ToolCall.ID != "call-b" || calls[0].ToolCall.RawArguments != `{"y":2}` {
		t.Fatalf("first finished call = %+v", calls[0].ToolCall)
	}
	// tool_a's continuation (after tool_b closed) must still reach call-a,
	// not be silently dropped or misattributed.
	if calls[1].ToolCall.ID != "call-a" || calls[1].ToolCall.RawArguments != `{"x":1,"x2":3}` {
		t.Fatalf("second finished call = %+v", calls[1].ToolCall)
	}
}

// TestStream_PartialArgsStreaming_EmptyArrayWithWillContinueFalse ports the
// cfca634 case: a streamed functionCall with no args at all — partialArgs is
// present but empty, and willContinue is false — must still emit a complete
// call with `{}` input via the streaming (accumulator) path.
func TestStream_PartialArgsStreaming_EmptyArrayWithWillContinueFalse(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"no_args_tool","partialArgs":[],"willContinue":false}}]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.ID != "call-1" || tc.ToolName != "no_args_tool" {
		t.Fatalf("tool-call = %+v", tc)
	}
	if tc.RawArguments != "{}" {
		t.Fatalf("RawArguments = %q, want {}", tc.RawArguments)
	}
	if len(tc.Arguments) != 0 {
		t.Fatalf("Arguments = %v, want empty", tc.Arguments)
	}
}

// TestStream_CompleteCall_SingleChunk_PreservesOrder verifies a single-chunk
// complete function call (name + args together, no partialArgs) emits its
// full input immediately (no accumulator needed) and preserves wire key
// order in RawArguments.
func TestStream_CompleteCall_SingleChunk_PreservesOrder(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-x","name":"lookup","args":{"zebra":"z","apple":"a"}}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.RawArguments != `{"zebra":"z","apple":"a"}` {
		t.Fatalf("RawArguments = %q, want wire order preserved", tc.RawArguments)
	}
	if tc.Arguments["zebra"] != "z" || tc.Arguments["apple"] != "a" {
		t.Fatalf("Arguments = %v", tc.Arguments)
	}
}

// TestStream_NoArgsCompleteCall_SingleChunk ports the TS
// `isNoArgsCompleteCall` branch: a single chunk names a tool with no `args`,
// no `partialArgs`, and no `willContinue: true` — it must complete
// immediately with `{}` input, not wait for a terminal chunk.
func TestStream_NoArgsCompleteCall_SingleChunk(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-z","name":"ping"}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.ID != "call-z" || tc.ToolName != "ping" || tc.RawArguments != "{}" {
		t.Fatalf("tool-call = %+v", tc)
	}
}

// TestStream_NoArgsCompleteCall_ExplicitWillContinueFalse is the same as
// TestStream_NoArgsCompleteCall_SingleChunk but with an explicit
// `willContinue: false` rather than the field being absent — TS treats
// `willContinue !== true` the same way in both cases (isNoArgsCompleteCall).
func TestStream_NoArgsCompleteCall_ExplicitWillContinueFalse(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-z2","name":"ping","willContinue":false}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.ID != "call-z2" || tc.ToolName != "ping" || tc.RawArguments != "{}" {
		t.Fatalf("tool-call = %+v", tc)
	}
}

// TestStream_PartialArgsStreaming_OpeningChunkWithoutPartialArgs covers the
// edge case where the opening chunk only sets `willContinue: true` with no
// `partialArgs` yet (isStreamingChunk via the name+willContinue branch, not
// the partialArgs branch): the call must still be tracked as active so a
// later continuation-only chunk (no name) can find it on the stack.
func TestStream_PartialArgsStreaming_OpeningChunkWithoutPartialArgs(t *testing.T) {
	openNoArgsYet := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"delayed_tool","willContinue":true}}]}}]}`
	firstArgs := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.a","numberValue":1}]}}]}}]}`
	finish := `{"candidates":[{"content":{"parts":[{"functionCall":{}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(openNoArgsYet, firstArgs, finish, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	if calls[0].ToolCall.RawArguments != `{"a":1}` {
		t.Fatalf("RawArguments = %q", calls[0].ToolCall.RawArguments)
	}
}

// TestStream_PartialArgsStreaming_NestedArrayAndObject exercises a nested
// path (object inside an array) end-to-end through the stream, ensuring the
// GoogleJSONAccumulator wiring inside processFuncCallPart produces valid,
// concatenation-consistent JSON.
func TestStream_PartialArgsStreaming_NestedArrayAndObject(t *testing.T) {
	c1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-n","name":"add_ingredient","partialArgs":[{"jsonPath":"$.recipe.ingredients[0].amount","stringValue":"16 oz","willContinue":true}]}}]}}]}`
	c2 := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.recipe.ingredients[0].name","stringValue":"Noodles","willContinue":true}]}}]}}]}`
	// Last partialArgs chunk has no willContinue, so the call finishes as
	// soon as it's processed — no separate terminal `{}` chunk is needed.
	c3 := `{"candidates":[{"content":{"parts":[{"functionCall":{"partialArgs":[{"jsonPath":"$.recipe.name","stringValue":"Lasagna"}]}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(c1, c2, c3, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	tree, err := decodeOrderedJSON([]byte(tc.RawArguments))
	if err != nil {
		t.Fatalf("RawArguments not valid JSON: %v (%q)", err, tc.RawArguments)
	}
	plain, _ := toPlainJSON(tree).(map[string]interface{})
	recipe, _ := plain["recipe"].(map[string]interface{})
	if recipe == nil {
		t.Fatalf("plain = %#v", plain)
	}
	if recipe["name"] != "Lasagna" {
		t.Fatalf("recipe.name = %v", recipe["name"])
	}
	ingredients, _ := recipe["ingredients"].([]interface{})
	if len(ingredients) != 1 {
		t.Fatalf("recipe.ingredients = %#v", recipe["ingredients"])
	}
	first, _ := ingredients[0].(map[string]interface{})
	if first["amount"] != "16 oz" || first["name"] != "Noodles" {
		t.Fatalf("ingredients[0] = %#v", first)
	}
}

// TestStream_CompleteCall_StringArgs ports TS google-language-model.ts's
// `typeof part.functionCall.args === 'string' ? part.functionCall.args :
// JSON.stringify(...)` branch (line ~1180): a rare wire shape where "args"
// itself decodes to a JSON string rather than an object, used verbatim as
// the tool call's input text instead of being re-encoded. Before this fix,
// FunctionCall.UnmarshalJSON hard-failed trying to unmarshal a JSON string
// into a map, which would have aborted the whole SSE event.
func TestStream_CompleteCall_StringArgs(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-s","name":"raw_tool","args":"already-json-text"}}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, "[DONE]"))
	chunks := drainStream(t, s)

	calls := toolCallsOf(chunks)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool-call, got %d: %v", len(calls), chunkTypes(chunks))
	}
	tc := calls[0].ToolCall
	if tc.RawArguments != "already-json-text" {
		t.Fatalf("RawArguments = %q, want the string used verbatim", tc.RawArguments)
	}
}

// TestStream_ServerToolCallAndResult ports TS google-language-model.test.ts's
// streaming server tool call/result fixture (~line 6540): a `toolCall`/
// `toolResponse` part pair (distinct from a user-invoked `functionCall`)
// becomes a `server:<toolType>` tool-call chunk (providerExecuted+dynamic
// true) and a matching tool-result chunk, each carrying
// serverToolCallId/serverToolType/thoughtSignature (when present) in
// providerMetadata.google — and does not itself flip the finish reason to
// tool-calls (only user-invoked function calls do that).
func TestStream_ServerToolCallAndResult(t *testing.T) {
	chunk1 := `{"candidates":[{"content":{"parts":[` +
		`{"toolCall":{"toolType":"GOOGLE_SEARCH_WEB","args":{"query":"SF weather"},"id":"sc-1"},"thoughtSignature":"sig-1"},` +
		`{"toolResponse":{"toolType":"GOOGLE_SEARCH_WEB","response":{"results":[{"title":"Weather"}]},"id":"sc-1"}}` +
		`]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[{"text":"It is sunny."}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2, "[DONE]"))
	chunks := drainStream(t, s)

	var toolCallChunk, toolResultChunk *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolCall:
			toolCallChunk = c
		case provider.ChunkTypeToolResult:
			toolResultChunk = c
		}
	}
	if toolCallChunk == nil || toolResultChunk == nil {
		t.Fatalf("missing tool-call/tool-result chunk: %v", chunkTypes(chunks))
	}

	call := toolCallChunk.ToolCall
	if call.ID != "sc-1" || call.ToolName != "server:GOOGLE_SEARCH_WEB" {
		t.Fatalf("tool call = %#v", call)
	}
	if !call.ProviderExecuted || !call.Dynamic {
		t.Fatalf("expected providerExecuted+dynamic tool call, got %#v", call)
	}
	if call.Arguments["query"] != "SF weather" {
		t.Fatalf("Arguments = %#v", call.Arguments)
	}
	var callMeta struct {
		Google map[string]interface{} `json:"google"`
	}
	if err := json.Unmarshal(toolCallChunk.ProviderMetadata, &callMeta); err != nil {
		t.Fatalf("unmarshal tool call metadata: %v", err)
	}
	if callMeta.Google["serverToolCallId"] != "sc-1" || callMeta.Google["thoughtSignature"] != "sig-1" {
		t.Fatalf("tool call provider metadata = %#v", callMeta.Google)
	}

	result := toolResultChunk.ToolResult
	if result.ToolCallID != "sc-1" || result.ToolName != "server:GOOGLE_SEARCH_WEB" {
		t.Fatalf("tool result = %#v", result)
	}
	var resultMeta struct {
		Google map[string]interface{} `json:"google"`
	}
	if err := json.Unmarshal(toolResultChunk.ProviderMetadata, &resultMeta); err != nil {
		t.Fatalf("unmarshal tool result metadata: %v", err)
	}
	// The toolResponse part carries no thoughtSignature of its own, so it
	// must not inherit the toolCall's.
	if _, hasSig := resultMeta.Google["thoughtSignature"]; hasSig {
		t.Fatalf("tool result metadata should not carry a thoughtSignature: %#v", resultMeta.Google)
	}

	// A server-executed tool does not flip STOP to tool-calls (only a
	// user-invoked function call does).
	var finish *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %v, want stop", finish)
	}
}
