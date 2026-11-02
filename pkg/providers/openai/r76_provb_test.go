package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r76provbProgramItems = `{"type":"program","id":"cm_1","call_id":"call_p","code":"return 1","fingerprint":"fp"},` +
	`{"type":"program_output","id":"po_1","call_id":"call_p","result":"1","status":"completed"}`

// r76provbJSON renders v as JSON.
func r76provbJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// r76provbDrain reads every chunk of a stream.
func r76provbDrain(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, c)
	}
}

// TestR76ProvbProgramPartsCarryItemID: TS doGenerate and doStream give the
// programmatic tool calling tool-call ("program") and tool-result
// ("program_output") parts providerMetadata {openai: {itemId}}. Go left it
// off the generate tool-call content part and both program_output parts.
func TestR76ProvbProgramPartsCarryItemID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-5.6","output":[`+r76provbProgramItems+`],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-5.6")
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var call, result string
	for _, c := range res.Content {
		switch p := c.(type) {
		case types.ToolCallContent:
			call = string(p.ProviderMetadata)
		case types.ToolResultContent:
			result = string(p.ProviderMetadata)
		}
	}
	if call != `{"openai":{"itemId":"cm_1"}}` || result != `{"openai":{"itemId":"po_1"}}` {
		t.Errorf("generate: tool-call metadata %s, tool-result metadata %s", call, result)
	}

	sse := `data: {"type":"response.output_item.done","output_index":0,"sequence_number":1,"item":{"type":"program","id":"cm_1","call_id":"call_p","code":"return 1","fingerprint":"fp"}}` + "\n\n" +
		`data: {"type":"response.output_item.done","output_index":1,"sequence_number":2,"item":{"type":"program_output","id":"po_1","call_id":"call_p","result":"1","status":"completed"}}` + "\n\n"
	var streamCall, streamResult map[string]interface{}
	for _, c := range r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)) {
		switch {
		case c.Type == provider.ChunkTypeToolCall && c.ToolCall != nil:
			streamCall = c.ToolCall.ProviderMetadata
		case c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil:
			streamResult = c.ToolResult.ProviderMetadata
		}
	}
	if r76provbJSON(streamCall) != `{"openai":{"itemId":"cm_1"}}` || r76provbJSON(streamResult) != `{"openai":{"itemId":"po_1"}}` {
		t.Errorf("stream: tool-call metadata %v, tool-result metadata %v", streamCall, streamResult)
	}
}

// TestR76ProvbStreamContinuesAfterCompleted: TS records the finish state at
// response.completed and keeps transforming the events after it; flush()
// enqueues the one finish part when the stream ends, with the last usage and
// service tier. Go stopped reading at the first response.completed.
func TestR76ProvbStreamContinuesAfterCompleted(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","created_at":1,"model":"m"}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"one","sequence_number":1}`) +
		ev(`{"type":"response.completed","sequence_number":2,"response":{"id":"resp_1","status":"completed","service_tier":"default","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`) +
		ev(`{"type":"response.created","sequence_number":3,"response":{"id":"resp_2","created_at":2,"model":"m"}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"m2","output_index":0,"content_index":0,"delta":"two","sequence_number":4}`) +
		ev(`{"type":"response.incomplete","sequence_number":5,"response":{"id":"resp_2","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}}`)
	chunks := r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false))
	var text string
	var finishes []*provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		}
	}
	if text != "onetwo" {
		t.Errorf("text = %q, want the deltas after response.completed too", text)
	}
	if len(finishes) != 1 || chunks[len(chunks)-1] != finishes[0] {
		t.Fatalf("want one finish, last: %d finishes", len(finishes))
	}
	f := finishes[0]
	if f.FinishReason != types.FinishReasonLength || f.RawFinishReason != "max_output_tokens" {
		t.Errorf("finish reason = %q/%q", f.FinishReason, f.RawFinishReason)
	}
	if f.Usage == nil || f.Usage.InputTokens == nil || *f.Usage.InputTokens != 5 {
		t.Errorf("usage = %+v, want the last event's", f.Usage)
	}
	if string(f.ProviderMetadata) != `{"openai":{"responseId":"resp_2","serviceTier":"default"}}` {
		t.Errorf("metadata = %s", f.ProviderMetadata)
	}
}

// TestR76ProvbStreamInvalidJSONFinishesWithError: a frame that is not JSON
// is a failed chunk parse in TS (encounteredStreamError, finish reason
// {unified: 'error', raw: undefined}), which a later response.completed does
// not replace. Go reported the completed event's finish reason.
func TestR76ProvbStreamInvalidJSONFinishesWithError(t *testing.T) {
	sse := `data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"a","sequence_number":1}` + "\n\n" +
		"data: {not json\n\n" +
		`data: {"type":"response.completed","sequence_number":2,"response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"
	chunks := r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false))
	f := chunks[len(chunks)-1]
	if f.Type != provider.ChunkTypeFinish || f.FinishReason != types.FinishReasonError || f.RawFinishReason != "" {
		t.Fatalf("last chunk = %+v", f)
	}
	if f.Usage == nil || f.Usage.InputTokens == nil || *f.Usage.InputTokens != 1 {
		t.Errorf("usage = %+v, want response.completed's", f.Usage)
	}
}

// TestR76ProvbStreamItemIDPerMessage: TS activeOutputItemIds holds the id of
// the message or reasoning item added at an output_index, replaced by the
// next one there; other items are not recorded. Go kept the first id ever
// seen at the index (any item type), so a message following a function call
// at the same output_index (the next response of the stream) streamed its
// text under the function call's item id.
func TestR76ProvbStreamItemIDPerMessage(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.output_item.added","output_index":0,"sequence_number":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"","status":"in_progress"}}`) +
		ev(`{"type":"response.output_item.done","output_index":0,"sequence_number":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}","status":"completed"}}`) +
		ev(`{"type":"response.output_item.added","output_index":0,"sequence_number":3,"item":{"type":"message","id":"msg_2","role":"assistant","status":"in_progress","content":[]}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"msg_2_rotated","output_index":0,"content_index":0,"delta":"hi","sequence_number":4}`) +
		ev(`{"type":"response.output_item.done","output_index":0,"sequence_number":5,"item":{"type":"message","id":"msg_2_rotated","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}}`)
	var ids []string
	for _, c := range r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)) {
		switch c.Type {
		case provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd:
			ids = append(ids, string(c.Type)+":"+c.ID)
		}
	}
	if got, want := strings.Join(ids, " "), "text-start:msg_2 text:msg_2 text-end:msg_2"; got != want {
		t.Errorf("text parts = %s\nwant %s", got, want)
	}
}

// TestR76ProvbStreamApplyPatchWithoutAddedIsNotAToolCall: TS counts an
// apply_patch_call toward the tool-calls finish reason only where it emits
// its tool-call: a completed item announced by output_item.added. Go also
// counted a completed done item that was never announced (and emitted no
// tool call for it), finishing with tool-calls.
func TestR76ProvbStreamApplyPatchWithoutAddedIsNotAToolCall(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.output_item.done","output_index":0,"sequence_number":1,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_ap","status":"completed","operation":{"type":"delete_file","path":"a.txt"}}}`) +
		ev(`{"type":"response.completed","sequence_number":2,"response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	chunks := r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false))
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			t.Fatalf("unexpected tool call %+v", c.ToolCall)
		}
	}
	if f := chunks[len(chunks)-1]; f.FinishReason != types.FinishReasonStop {
		t.Errorf("finish reason = %q, want stop", f.FinishReason)
	}
}
