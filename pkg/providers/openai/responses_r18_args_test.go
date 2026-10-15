package openai

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS resolves reasoningEffort once (option ?? top-level reasoning) and then
// applies the GPT-6+ supported-efforts check to the result, so a top-level
// `reasoning: minimal` on gpt-6 is warned about and dropped.
func TestResponsesTopLevelReasoningCheckedAgainstSupportedEfforts(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), ModelGPT6Sol)
	lvl := types.ReasoningMinimal
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &lvl,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := body["reasoning"].(map[string]interface{}); ok {
		if _, has := r["effort"]; has {
			t.Fatalf("effort must be dropped, got %v", r)
		}
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "reasoningEffort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reasoningEffort warning, got %v", warnings)
	}
}

// TS include order: user values, logprobs, web search sources, code
// interpreter outputs, then reasoning.encrypted_content.
func TestResponsesIncludeOrderMatchesTS(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "o3")
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "web_search", ProviderID: "openai.web_search", Type: "provider"},
			{Name: "code_interpreter", ProviderID: "openai.code_interpreter", Type: "provider"},
		},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": false, "logprobs": true}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"message.output_text.logprobs", "web_search_call.action.sources", "code_interpreter_call.outputs", "reasoning.encrypted_content"}
	if got, _ := body["include"].([]string); !reflect.DeepEqual(got, want) {
		t.Fatalf("include = %v, want %v", body["include"], want)
	}
}

// logprobs: 0 is sent as top_logprobs 0 but, being falsy in TS, adds no include.
func TestResponsesZeroLogprobsSentWithoutInclude(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 0}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["top_logprobs"]; !ok || v != 0 {
		t.Fatalf("top_logprobs = %v (present=%v), want 0", v, ok)
	}
	if _, ok := body["include"]; ok {
		t.Fatalf("include must be absent, got %v", body["include"])
	}
}

// When the GPT-6 logprobs filter empties the include list TS sends none.
func TestResponsesEmptiedIncludeIsOmitted(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), ModelGPT6Sol)
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 3, "reasoningEffort": "low"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["include"]; ok {
		t.Fatalf("include must be absent, got %v", v)
	}
	if _, ok := body["top_logprobs"]; ok {
		t.Fatal("top_logprobs must be dropped")
	}
}

// TS counts only client-side tool calls for the tool-calls finish reason: a
// server-executed computer_call (null call_id) does not.
func TestResponsesFinishReasonIgnoresServerExecutedComputerCall(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	item, _ := json.Marshal(map[string]interface{}{"type": "computer_call", "id": "c1", "status": "completed"})
	result, err := model.convertResponse(responses.ResponsesAPIResponse{Output: []json.RawMessage{item}}, true, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("doGenerate finish = %q, want stop", result.FinishReason)
	}

	finishOf := func(sse string) types.FinishReason {
		stream := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
		defer stream.Close() //nolint:errcheck
		for {
			c, err := stream.Next()
			if err != nil {
				t.Fatalf("no finish chunk: %v", err)
			}
			if c.Type == provider.ChunkTypeFinish {
				return c.FinishReason
			}
		}
	}
	done := `data: {"type":"response.completed","response":{"id":"r","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}

`
	computer := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"computer_call","id":"c1","status":"in_progress"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"computer_call","id":"c1","status":"completed"}}

`
	if got := finishOf(computer + done); got != types.FinishReasonStop {
		t.Fatalf("doStream computer finish = %q, want stop", got)
	}
}

// TS output_item.done for a reasoning item emits reasoning-end only for an
// activeReasoning entry created at output_item.added.
func TestResponsesReasoningDoneWithoutAddedEmitsNoEnd(t *testing.T) {
	sse := `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"enc","summary":[]}}

data: {"type":"response.completed","response":{"id":"r","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}

`
	stream := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	defer stream.Close() //nolint:errcheck
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeReasoningEnd {
			t.Fatalf("unexpected reasoning-end %+v", c)
		}
	}
}

// TS encounteredStreamError: an `error` event followed by response.completed
// keeps finishReason error (raw 'error'), and a later response.failed with an
// error object does not enqueue a second error part.
func TestResponsesErrorEventKeepsErrorFinishReason(t *testing.T) {
	started := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[]}}

data: {"type":"error","code":"server_error","message":"boom","param":null,"sequence_number":2}

`
	run := func(tail string) (errs int, finish *provider.StreamChunk) {
		stream := newResponsesStream(io.NopCloser(strings.NewReader(started+tail)), false)
		defer stream.Close() //nolint:errcheck
		for {
			c, err := stream.Next()
			if err != nil {
				return
			}
			switch c.Type {
			case provider.ChunkTypeError:
				errs++
			case provider.ChunkTypeFinish:
				finish = c
			}
		}
	}
	errs, finish := run(`data: {"type":"response.completed","response":{"id":"r","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}

`)
	if errs != 1 || finish == nil || finish.FinishReason != types.FinishReasonError || finish.RawFinishReason != "error" {
		t.Fatalf("completed after error: errs=%d finish=%+v", errs, finish)
	}
	errs, finish = run(`data: {"type":"response.failed","sequence_number":3,"response":{"id":"r","error":{"code":"x","message":"y"}}}

`)
	if errs != 1 || finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("failed after error: errs=%d finish=%+v", errs, finish)
	}
}

// TS flush(): an undeclared "parallel" wrapper call cut off before
// output_item.done still surfaces its buffered input at stream end.
func TestResponsesUnfinishedSuppressedToolCallFlushed(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc1","call_id":"call_p","name":"parallel","arguments":""}}

data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc1","delta":"{\"a\":"}

`
	stream := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	defer stream.Close() //nolint:errcheck
	var kinds []provider.ChunkType
	var deltas []string
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			kinds = append(kinds, c.Type)
		case provider.ChunkTypeToolInputDelta:
			deltas = append(deltas, c.Text)
		}
	}
	if len(kinds) != 1 || len(deltas) != 1 || deltas[0] != `{"a":` {
		t.Fatalf("starts=%v deltas=%q", kinds, deltas)
	}
}
