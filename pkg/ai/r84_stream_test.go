package ai

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Round 84 stream review ports.

// r84streamProcessUIRun is one processUIMessageStream run recorded from the
// TS test file ui/process-ui-message-stream.test.ts at e64ed82f27: the
// state arguments, the chunks the stream consumed, every write() snapshot
// (structuredClone(state.message)), the final state.message and the onError
// messages. Recorded by running the TS test file itself under bun with a
// recording wrapper around processUIMessageStream (the recorder lives in
// state/parity/oct_27_2026/reports/r84-stream-tsrec/); runs that share state
// and chunks (a beforeEach used by several `it`) are stored once with every
// test name. JSON round trip drops `undefined` fields, as the comparison
// below does for Go.
type r84streamProcessUIRun struct {
	Names       []string                 `json:"names"`
	MessageID   string                   `json:"messageId"`
	LastMessage map[string]interface{}   `json:"lastMessage"`
	Chunks      []map[string]interface{} `json:"chunks"`
	Writes      []interface{}            `json:"writes"`
	Final       interface{}              `json:"final"`
	Errors      []string                 `json:"errors"`
	Flags       []string                 `json:"flags"`
}

func r84streamLoadProcessUIRuns(t *testing.T) []r84streamProcessUIRun {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(r84streamProcessUIFixture, "\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var runs []r84streamProcessUIRun
	if err := json.Unmarshal(body, &runs); err != nil {
		t.Fatal(err)
	}
	return runs
}

func r84streamJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestR84streamTSProcessUIMessageStreamReplay replays every recorded
// processUIMessageStream run (TS process-ui-message-stream.test.ts: the
// "should call the update function with the correct arguments" write lists
// and the "should have the correct final message state" states, among
// others) through the Go UI message state that ReadUIMessages uses, and
// compares each snapshot, the final message and the processing errors.
// The message-state assertions of these TS cases are covered (the onToolCall,
// onData, onFinish and onError callback assertions and the error property
// checks are not, and are not cited):
// process-ui-message-stream.test.ts 137, 196, 309, 396, 444, 448, 469, 500, 531, 566, 606, 646
// process-ui-message-stream.test.ts 690, 721, 878, 1058, 1157, 1392, 1492, 1817, 1934, 2445, 2553, 2725
// process-ui-message-stream.test.ts 2818, 2989, 3054, 3143, 3210, 3295, 3372, 3515, 3546, 3626, 3713, 3803
// process-ui-message-stream.test.ts 3811, 4333, 4529, 4693, 4906, 4982, 5049, 5080, 5148, 5163, 5226, 5275
// process-ui-message-stream.test.ts 5337, 5392, 5489, 5692, 5732, 5861, 6070, 6112, 6164, 6230, 6469, 6690
// process-ui-message-stream.test.ts 6789, 6955, 7042, 7062, 7138, 7243, 7293, 7415, 7422, 7429, 7521, 7643
// process-ui-message-stream.test.ts 7721, 7730, 7829, 7839, 7894, 7953, 8015, 8090, 8152, 8228, 8296, 8312
// process-ui-message-stream.test.ts 8372, 8476, 8538, 8693, 8852, 8998, 9089, 9239, 9342, 9544, 9629, 9759
// process-ui-message-stream.test.ts 9841, 9975, 10037
func TestR84streamTSProcessUIMessageStreamReplay(t *testing.T) {
	runs := r84streamLoadProcessUIRuns(t)
	if len(runs) < 60 {
		t.Fatalf("fixture has %d runs", len(runs))
	}
	for _, run := range runs {
		run := run
		t.Run(run.Names[0], func(t *testing.T) {
			for _, f := range run.Flags {
				if f == "resetStateOnFirstMessageStart" {
					t.Skip("resetStateOnFirstMessageStart is a client chat option (chat.ts)")
				}
			}
			var original []UIMessageChunk
			if run.LastMessage != nil {
				original = []UIMessageChunk{UIMessageChunk(run.LastMessage)}
			}
			state := newUIMessageCallbackState(original, run.MessageID)
			var writes []interface{}
			var errs []string
			for _, chunk := range run.Chunks {
				write, err := state.applyForSnapshot(UIMessageChunk(chunk), nil)
				if err != nil {
					errs = append(errs, err.Error())
					// An error chunk is reported and processing continues;
					// any other failure errors the TS stream (ReadUIMessages).
					if stringValue(chunk["type"]) == "error" {
						continue
					}
					break
				}
				if !write {
					continue
				}
				snap, err := UIMessageFromChunk(state.responseMessage())
				if err != nil {
					t.Fatal(err)
				}
				writes = append(writes, r84streamJSON(t, snap))
			}
			wantWrites := r84streamJSON(t, run.Writes)
			if len(run.Writes) == 0 {
				wantWrites = r84streamJSON(t, []interface{}{})
			}
			gotWrites := r84streamJSON(t, writes)
			if writes == nil {
				gotWrites = r84streamJSON(t, []interface{}{})
			}
			if !reflect.DeepEqual(gotWrites, wantWrites) {
				gw, _ := json.MarshalIndent(gotWrites, "", " ")
				ww, _ := json.MarshalIndent(wantWrites, "", " ")
				t.Errorf("writes differ (%v)\n got: %s\nwant: %s", run.Names, gw, ww)
			}
			final, err := UIMessageFromChunk(state.responseMessage())
			if err != nil {
				t.Fatal(err)
			}
			if run.Final != nil && !reflect.DeepEqual(r84streamJSON(t, final), run.Final) {
				gf, _ := json.MarshalIndent(r84streamJSON(t, final), "", " ")
				wf, _ := json.MarshalIndent(run.Final, "", " ")
				t.Errorf("final message differs (%v)\n got: %s\nwant: %s", run.Names, gf, wf)
			}
			if len(errs) != len(run.Errors) {
				t.Errorf("errors: got %q, want %q", errs, run.Errors)
			} else {
				for i := range errs {
					if errs[i] != run.Errors[i] {
						t.Errorf("error %d: got %q, want %q", i, errs[i], run.Errors[i])
					}
				}
			}
		})
	}
}

// TestR84streamReadUIMessageStreamTSFraming compares ReadUIMessageStream
// with TS DefaultChatTransport's parseJsonEventStream (TextDecoderStream,
// then eventsource-parser 3.0.8) on the same bytes, split across two reads.
// Each want was produced by running provider-utils parse-json-event-stream.ts
// at e64ed82f27 under bun on these inputs.
func TestR84streamReadUIMessageStreamTSFraming(t *testing.T) {
	cat := func(parts ...string) string { return strings.Join(parts, "") }
	cases := []struct {
		name, in, want string
	}{
		{"one BOM", "\xEF\xBB\xBFdata: {\"type\":\"a\"}\n\n", `[{"type":"a"}]`},
		{"two BOMs: the first line is no data field", "\xEF\xBB\xBF\xEF\xBB\xBFdata: {\"type\":\"a\"}\n\n", `[]`},
		{"bare CR and CRLF", "data: {\"type\":\"a\"}\r\rdata: {\"type\":\"b\"}\r\n\r\n", `[{"type":"a"},{"type":"b"}]`},
		{"multi-line data", "data: {\"type\":\ndata: \"a\"}\n\n", `[{"type":"a"}]`},
		{"unterminated last event", "data: {\"type\":\"a\"}\n\ndata: {\"type\":\"b\"}\n", `[{"type":"a"}]`},
		{"no space after colon", "data:{\"type\":\"a\"}\n\n", `[{"type":"a"}]`},
		{"truncated sequence is one U+FFFD", cat("data: {\"type\":\"a\",\"v\":\"x", "\xF0\x9F\x98", "y\"}\n\n"), `[{"type":"a","v":"x\ufffdy"}]`},
		{"BOM inside data kept", cat("data: {\"type\":\"a\"}\n\ndata: {\"type\":\"\xEF\xBB\xBFb\",\"v\":\"", "\xFF", "\"}\n\n"), `[{"type":"a"},{"type":"\ufeffb","v":"\ufffd"}]`},
		{"event and id fields, comment, DONE", "event: x\nid: 1\ndata: {\"type\":\"a\"}\n\n: comment\ndata: [DONE]\n\n", `[{"type":"a"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := io.MultiReader(strings.NewReader(tc.in[:7]), strings.NewReader(tc.in[7:]))
			chunks, err := ReadUIMessageStream(r)
			if err != nil {
				t.Fatal(err)
			}
			var want interface{}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if got := r84streamJSON(t, chunks); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
	if _, err := ReadUIMessageStream(strings.NewReader("data: {nope\n\n")); err == nil {
		t.Fatal("invalid JSON data: want an error (TS transport throws the parse failure)")
	}
}

func r84streamUIParts(t *testing.T, opts UIMessageStreamResultOptions, chunks ...provider.StreamChunk) interface{} {
	t.Helper()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: uiModel(chunks...), Prompt: "prompt", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	return r84streamJSON(t, tsUIParts(t, r, opts))
}

func r84streamWant(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// stream-text.test.ts 6568: document sources (with and without filename)
// keep their own providerMetadata in the UI stream when sendSources is set.
func TestR84streamToUIMessageStreamDocumentSources(t *testing.T) {
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)}
	got := r84streamUIParts(t, UIMessageStreamResultOptions{SendSources: ptrBool(true)},
		provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "document", ID: "doc-123", MediaType: "application/pdf", Title: "Document Example", Filename: "example.pdf", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"doc-value"}}`)}},
		provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello from document!"},
		provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "document", ID: "doc-456", MediaType: "text/plain", Title: "Text Document", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"doc-value2"}}`)}},
		finish)
	want := r84streamWant(t, `[
		{"type":"start"},{"type":"start-step"},
		{"filename":"example.pdf","mediaType":"application/pdf","providerMetadata":{"provider":{"custom":"doc-value"}},"sourceId":"doc-123","title":"Document Example","type":"source-document"},
		{"id":"1","type":"text-start"},{"delta":"Hello from document!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},
		{"mediaType":"text/plain","providerMetadata":{"provider":{"custom":"doc-value2"}},"sourceId":"doc-456","title":"Text Document","type":"source-document"},
		{"type":"finish-step"},{"finishReason":"stop","type":"finish"}]`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

// stream-text.test.ts 6734: file parts carry their own providerMetadata; a
// base64 string is sent as given ("Hello World" is not re-encoded).
func TestR84streamToUIMessageStreamFileProviderMetadata(t *testing.T) {
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)}
	got := r84streamUIParts(t, UIMessageStreamResultOptions{},
		provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "Hello World"}, ProviderMetadata: json.RawMessage(`{"testProvider":{"signature":"sig-1"}}`)}},
		provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "QkFVRw=="}}},
		finish)
	var files []interface{}
	for _, p := range got.([]interface{}) {
		if p.(map[string]interface{})["type"] == "file" {
			files = append(files, p)
		}
	}
	want := r84streamWant(t, `[
		{"mediaType":"text/plain","providerMetadata":{"testProvider":{"signature":"sig-1"}},"type":"file","url":"data:text/plain;base64,Hello World"},
		{"mediaType":"image/jpeg","type":"file","url":"data:image/jpeg;base64,QkFVRw=="}]`)
	if !reflect.DeepEqual(r84streamJSON(t, files), want) {
		t.Fatalf("got %v\nwant %v", files, want)
	}
}

// stream-text.test.ts 11440: onToolExecutionStart gets the call id, the
// step messages, the tool call and the validated tool context.
func TestR84streamOnToolExecutionStartContext(t *testing.T) {
	tool := r71streamTool1(true, nil, nil)
	tool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []interface{}{"context"}})
	var mu sync.Mutex
	var events []OnToolCallStartEvent
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"})), Prompt: "prompt",
		Tools: []types.Tool{tool}, ToolsContext: map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}},
		Internal: r71streamInternal,
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	e := events[0]
	if e.CallID != "test-telemetry-call-id" {
		t.Errorf("callId = %q", e.CallID)
	}
	if !reflect.DeepEqual(r84streamJSON(t, e.ToolContext), map[string]interface{}{"context": "test"}) {
		t.Errorf("toolContext = %#v", e.ToolContext)
	}
	if !reflect.DeepEqual(e.Messages, r71streamUserMessages("prompt")) {
		t.Errorf("messages = %#v", e.Messages)
	}
	if e.ToolCall.ID != "call-1" || e.ToolCall.ToolName != "tool1" || !reflect.DeepEqual(e.ToolCall.Arguments, map[string]interface{}{"value": "test"}) || e.ToolCall.ProviderExecuted || e.ToolCall.Dynamic {
		t.Errorf("toolCall = %#v", e.ToolCall)
	}
}

// stream-text.test.ts 9741 / 10623: the onStart and onStepStart events carry
// no telemetry settings (TS: no isEnabled / functionId property).
func TestR84streamCallbackEventsHaveNoTelemetryFields(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(OnStartEvent{}), reflect.TypeOf(OnStepStartEvent{})} {
		for i := 0; i < typ.NumField(); i++ {
			switch name := typ.Field(i).Name; name {
			case "IsEnabled", "FunctionID", "FunctionId", "Telemetry", "Metadata":
				t.Errorf("%s has telemetry field %s", typ.Name(), name)
			}
		}
	}
	var start OnStartEvent
	var step OnStepStartEvent
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		Telemetry:   &TelemetrySettings{IsEnabled: ptrBool(true), FunctionID: "deprecated-fn"},
		OnStart:     func(_ context.Context, e OnStartEvent) { start = e },
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { step = e },
	})
	for _, ev := range []interface{}{start, step} {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "deprecated-fn") {
			t.Errorf("event exposes the telemetry function id: %s", b)
		}
	}
}

// create-sse-stream-with-keep-alive.test.ts 51: an idle source keeps one
// pending receive while many keep-alive comments go out, and stopping ends
// the writer without consuming the source.
func TestR84streamKeepAliveManyIdleComments(t *testing.T) {
	chunks := make(chan UIMessageChunk)
	stop := make(chan struct{})
	var events []string
	const n = 2500
	writeFailed, err := writeUISSEEvents(chunks, time.Microsecond, true, stop, nil, func(b []byte) error {
		events = append(events, string(b))
		if len(events) == n+1 {
			close(stop)
		}
		return nil
	})
	if writeFailed || err != nil {
		t.Fatalf("writeFailed = %v, err = %v", writeFailed, err)
	}
	if len(events) != n+1 || events[0] != sseStreamOpenComment {
		t.Fatalf("events = %d, first %q", len(events), events[0])
	}
	for i, e := range events[1:] {
		if e != sseKeepAliveComment {
			t.Fatalf("event %d = %q", i+1, e)
		}
	}
	if sseStreamOpenComment != ": stream-open\n\n" || sseKeepAliveComment != ": keep-alive\n\n" {
		t.Fatalf("comments = %q, %q", sseStreamOpenComment, sseKeepAliveComment)
	}
	select {
	case chunks <- UIMessageChunk{"type": "start"}:
		t.Fatal("the stopped writer still receives from the source")
	default:
	}
}

// r84streamProcessUIFixture is the gzip -9 + base64 JSON fixture (see
// r84streamProcessUIRun).
const r84streamProcessUIFixture = `
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
`
