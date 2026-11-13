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
	"golang.org/x/net/websocket"
)

// TestR97provbChatFractionalUsageRawExact: TS chat usage counts are
// z.number() and usage.raw is the parsed usage object, so a fractional
// count stays exact in Usage.Raw (Go's raw held the truncated retry value).
func TestR97provbChatFractionalUsageRawExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10.5,"completion_tokens":3,"total_tokens":13.5,"prompt_tokens_details":{"cached_tokens":1.5}}}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Usage.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want 10", *res.Usage.InputTokens)
	}
	details, _ := res.Usage.Raw["prompt_tokens_details"].(map[string]interface{})
	if res.Usage.Raw["prompt_tokens"] != 10.5 || details["cached_tokens"] != 1.5 {
		t.Fatalf("raw = %#v, want the exact fractional counts", res.Usage.Raw)
	}
}

// r97provbNullMessageServer accepts the realtime handshake, reads the
// session update and sends a JSON null text message.
func r97provbNullMessageServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, _ *http.Request) error {
		if len(cfg.Protocol) > 0 {
			cfg.Protocol = cfg.Protocol[:1]
		}
		return nil
	}, Handler: func(ws *websocket.Conn) {
		var msg string
		_ = websocket.Message.Receive(ws, &msg)
		_ = websocket.Message.Send(ws, "null")
		_ = websocket.Message.Receive(ws, &msg)
	}})
	t.Cleanup(ts.Close)
	return ts
}

// TestR97provbRealtimeNullMessageTypeError: TS safeParseJSON parses a JSON
// null message, then reads raw.type of it, and the TypeError fails the
// stream through onProcessingError (after the raw part). Go treated it as
// an object without a type and kept the stream open.
func TestR97provbRealtimeNullMessageTypeError(t *testing.T) {
	const want = "Cannot read properties of null (reading 'type')"
	ts := r97provbNullMessageServer(t)
	rate := 24000
	tr, err := newTestTranscriptionModel(ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"}, IncludeRawChunks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Stream.Close() //nolint:errcheck
	var parts []string
	for {
		p, err := tr.Stream.Next()
		if err != nil {
			if err.Error() != want {
				t.Errorf("transcription: err = %v, want %q", err, want)
			}
			break
		}
		parts = append(parts, string(p.Type))
	}
	if strings.Join(parts, ",") != "stream-start,raw" {
		t.Errorf("transcription parts = %v", parts)
	}

	st, err := newTestSpeechTranslationModel(ts.URL).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}, TargetLanguage: "es"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stream.Close() //nolint:errcheck
	for {
		_, err := st.Stream.Next()
		if err != nil {
			if err.Error() != want {
				t.Errorf("translation: err = %v, want %q", err, want)
			}
			break
		}
	}
}

// TestR97provbCompletionUsageRaw: TS convertOpenAICompletionUsage returns
// raw: usage, the schema output. The OpenAI z.object keeps the three counts
// as sent (a fraction included); the openai-compatible z.looseObject keeps
// every key. Go rebuilt raw from its truncated int fields.
func TestR97provbCompletionUsageRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"text":"hi","finish_reason":"stop","index":0}],"usage":{"prompt_tokens":10.5,"completion_tokens":3,"total_tokens":13.5,"cost":0.25}}`))
	}))
	defer srv.Close()
	for _, compat := range []bool{false, true} {
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: compat}).CompletionModel("gpt-3.5-turbo-instruct")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if *res.Usage.InputTokens != 10 || res.Usage.Raw["prompt_tokens"] != 10.5 || res.Usage.Raw["total_tokens"] != 13.5 {
			t.Errorf("compat %v: input = %d, raw = %#v", compat, *res.Usage.InputTokens, res.Usage.Raw)
		}
		if _, has := res.Usage.Raw["cost"]; has != compat {
			t.Errorf("compat %v: raw = %#v, want cost only for the loose openai-compatible schema", compat, res.Usage.Raw)
		}
	}
}

// TestR97provbLiveServerEventInfinity: the OpenAI Live server event schemas
// use z.number() (nonnegative), which rejects Infinity; a number beyond the
// double range reads as +Inf (main ruling 4), so the event is the
// invalid_server_event error (Go accepted it).
func TestR97provbLiveServerEventInfinity(t *testing.T) {
	for _, frame := range []string{
		`{"type":"session.usage.updated","usage":{"seconds":1e400}}`,
		`{"type":"session.output_transcript.delta","delta":"x","start_ms":0,"end_ms":1e400}`,
		`{"type":"session.delegation.created","delegation":{"id":"d"},"offset_ms":1e400}`,
	} {
		events := parseOpenAILiveServerEvent([]byte(frame))
		if len(events) != 1 || events[0].Type != "error" || events[0].Code != "invalid_server_event" {
			t.Errorf("%s: events = %+v, want invalid_server_event", frame, events)
		}
	}
}

// TestR97provbApprovalRequestIDEmptyKey: TS
// extractApprovalRequestIdToToolCallIdMapping maps every approvalRequestId
// that is != null, a present "" included.
func TestR97provbApprovalRequestIDEmptyKey(t *testing.T) {
	m := extractApprovalRequestIDToToolCallIDFromPrompt(types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "tc1", ToolName: "mcp.x",
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"approvalRequestId": ""}}}},
	}}})
	if m[""] != "tc1" {
		t.Fatalf("mapping = %#v, want \"\" -> tc1", m)
	}
}

// TestR97provbRealtimeParseNullEvent: TS parseOpenAIRealtimeServerEvent
// reads event.type of the parsed message, so a JSON null throws a TypeError
// (Go returned a custom event).
func TestR97provbRealtimeParseNullEvent(t *testing.T) {
	_, err := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime").ParseServerEvent([]byte("null"))
	if err == nil || err.Error() != "Cannot read properties of null (reading 'type')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}

// TestR97provbCustomToolCallNameMapping: TS names a custom_tool_call
// toolNameMapping.toCustomToolName(item.name) in generate and stream, so a
// name equal to a mapped provider tool's wire name reports that tool's
// declared name.
func TestR97provbCustomToolCallNameMapping(t *testing.T) {
	tools := []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "search", ProviderID: "openai.web_search"}}
	item, _ := json.Marshal(map[string]interface{}{
		"type": "custom_tool_call", "id": "ct_1", "call_id": "call_1", "name": "web_search", "input": "x",
	})
	result, err := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5").convertResponse(mockResponsesResponseWith(item), true, "", tools, nil)
	if err != nil || len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "search" {
		t.Fatalf("generate ToolCalls = %#v, err = %v, want name search", result, err)
	}

	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"web_search","input":""}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"web_search","input":"x","status":"completed"}}

`)), false)
	stream.tools = tools
	defer stream.Close() //nolint:errcheck
	var names []string
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.ToolCall != nil && chunk.ToolCall.ToolName != "" {
			names = append(names, string(chunk.Type)+":"+chunk.ToolCall.ToolName)
		}
	}
	want := []string{string(provider.ChunkTypeToolInputStart) + ":search", string(provider.ChunkTypeToolCall) + ":search"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("stream names = %v, want %v", names, want)
	}
}

// TestR97provbApplyPatchToolCallIDFromAdded: TS emits the final apply_patch
// tool-call under the call id recorded at output_item.added
// (toolCall.toolCallId); the done item's call_id only goes into the input.
func TestR97provbApplyPatchToolCallIDFromAdded(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_a","status":"in_progress","operation":{"type":"delete_file","path":"f.txt"}}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_b","status":"completed","operation":{"type":"delete_file","path":"f.txt"}}}

`)), false)
	defer stream.Close() //nolint:errcheck
	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatalf("no tool-call chunk (err %v)", err)
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			if chunk.ToolCall.ID != "call_a" || chunk.ToolCall.Arguments["callId"] != "call_b" {
				t.Fatalf("tool-call = %#v, want ID call_a with input callId call_b", chunk.ToolCall)
			}
			return
		}
	}
}

// r97provbStreamParts drains a Responses stream into "type:id" strings and
// returns its terminal error (nil at io.EOF).
func r97provbStreamParts(t *testing.T, sse string) ([]string, error) {
	t.Helper()
	stream := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	defer stream.Close() //nolint:errcheck
	var parts []string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return parts, err
		}
		id := chunk.ID
		if id == "" && chunk.ToolCall != nil {
			id = chunk.ToolCall.ID
		}
		if chunk.Type == provider.ChunkTypeToolInputDelta {
			id += "=" + chunk.Text
		}
		if chunk.Type != provider.ChunkTypeFinish {
			parts = append(parts, string(chunk.Type)+":"+id)
		}
	}
}

// TestR97provbOngoingToolCallSharedAcrossTypes: TS keeps one
// ongoingToolCalls entry per output_index whatever its tool type, so an
// input delta event streams into the call holding the index, and
// code_interpreter_call_code.done keeps its entry (a repeat emits again) and
// throws reading codeInterpreter!.containerId of another tool type.
func TestR97provbOngoingToolCallSharedAcrossTypes(t *testing.T) {
	parts, err := r97provbStreamParts(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_f","name":"f","arguments":""}}

data: {"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"fc_1","delta":"x"}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}

data: {"type":"response.function_call_arguments.delta","output_index":1,"item_id":"ws_1","delta":"y"}

`)
	want := "tool-input-start:call_f,tool-input-delta:call_f=x,tool-input-start:ws_1,tool-input-end:ws_1,tool-call:ws_1,tool-input-delta:ws_1=y"
	if err != nil || strings.Join(parts, ",") != want {
		t.Fatalf("parts = %v (err %v), want %s", parts, err, want)
	}

	parts, err = r97provbStreamParts(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"code_interpreter_call","id":"ci_1","container_id":"c1","code":"","outputs":null,"status":"in_progress"}}

data: {"type":"response.code_interpreter_call_code.done","output_index":0,"item_id":"ci_1","code":"1"}

data: {"type":"response.code_interpreter_call_code.done","output_index":0,"item_id":"ci_1","code":"1"}

`)
	if err != nil || strings.Count(strings.Join(parts, ","), "tool-call:ci_1") != 2 {
		t.Fatalf("parts = %v (err %v), want two tool-call:ci_1", parts, err)
	}

	parts, err = r97provbStreamParts(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_c","name":"c","input":""}}

data: {"type":"response.code_interpreter_call_code.done","output_index":0,"item_id":"ct_1","code":"1"}

`)
	want = `tool-input-start:call_c,tool-input-delta:call_c="},tool-input-end:call_c`
	if err == nil || err.Error() != "Cannot read properties of undefined (reading 'containerId')" || strings.Join(parts, ",") != want {
		t.Fatalf("parts = %v (err %v), want %s then the TypeError", parts, err, want)
	}
}
