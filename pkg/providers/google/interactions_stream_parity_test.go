package google

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func runInteractionsEvents(t *testing.T, events ...string) []*provider.StreamChunk {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: " + e + "\n\n")
	}
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(b.String())), "", nil, nil, nil, 0)
	defer func() { _ = stream.Close() }()
	var out []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, c)
	}
}

func chunkTypes(chunks []*provider.StreamChunk) []provider.ChunkType {
	var out []provider.ChunkType
	for _, c := range chunks {
		out = append(out, c.Type)
	}
	return out
}

func countType(chunks []*provider.StreamChunk, ct provider.ChunkType) int {
	n := 0
	for _, c := range chunks {
		if c.Type == ct {
			n++
		}
	}
	return n
}

const icreated = `{"event_type":"interaction.created","interaction":{"id":"i1","model":"m"}}`

// TS: image deltas are emitted inline and a text block stays open across them.
func TestInteractionsStreamImageDeltaInterleavedWithText(t *testing.T) {
	t.Parallel()
	chunks := runInteractionsEvents(t, icreated,
		`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"a"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"image","data":"AAEC","mime_type":"image/jpeg"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"b"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.completed","interaction":{"id":"i1","status":"completed"}}`)
	var seq []string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			seq = append(seq, c.Text)
		case provider.ChunkTypeFile:
			seq = append(seq, "file:"+c.GeneratedFileContent.MediaType)
		}
	}
	if got := strings.Join(seq, ","); got != "a,file:image/jpeg,b" {
		t.Fatalf("sequence = %s", got)
	}
}

// TS: image first then text in one pending model_output step.
func TestInteractionsStreamPendingImageThenText(t *testing.T) {
	t.Parallel()
	chunks := runInteractionsEvents(t, icreated,
		`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"image","data":"AAEC"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"video","uri":"https://x/v.mp4"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hi"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.completed","interaction":{"id":"i1","status":"completed"}}`)
	if countType(chunks, provider.ChunkTypeFile) != 2 || countType(chunks, provider.ChunkTypeText) != 1 || countType(chunks, provider.ChunkTypeTextStart) != 1 {
		t.Fatalf("chunks = %v", chunkTypes(chunks))
	}
}

// TS: sources are de-duplicated across the whole stream, and
// text_annotation_delta is an accepted alias.
func TestInteractionsStreamSourcesDedupedAcrossStream(t *testing.T) {
	t.Parallel()
	ann := `"annotations":[{"type":"url_citation","url":"https://a.example","title":"A"}]`
	chunks := runInteractionsEvents(t, icreated,
		`{"event_type":"step.start","index":0,"step":{"type":"model_output","content":[{"type":"text","text":"x",`+ann+`}]}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text_annotation_delta",`+ann+`}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"step.start","index":1,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"text_annotation","annotations":[{"type":"url_citation","url":"https://a.example"},{"type":"url_citation","url":"https://b.example"}]}}`,
		`{"event_type":"step.stop","index":1}`,
		`{"event_type":"interaction.completed","interaction":{"id":"i1","status":"completed"}}`)
	var urls []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSource {
			urls = append(urls, c.SourceContent.URL)
		}
	}
	if got := strings.Join(urls, ","); got != "https://a.example,https://b.example" {
		t.Fatalf("sources = %s", got)
	}
	if countType(chunks, provider.ChunkTypeTextStart) != 2 {
		t.Fatalf("text-start count; chunks = %v", chunkTypes(chunks))
	}
}

// TS: built-in tool steps never set hasFunctionCall (finish stays stop), the
// call takes arguments from step.start and keeps their wire order.
func TestInteractionsStreamBuiltinCallDoesNotMakeToolCallsFinish(t *testing.T) {
	t.Parallel()
	chunks := runInteractionsEvents(t, icreated,
		`{"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"c1","arguments":{"z":1,"a":2}}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.completed","interaction":{"id":"i1","status":"completed"}}`)
	var call, finish *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolCall:
			call = c
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if call == nil || call.ToolCall.RawArguments != `{"z":1,"a":2}` || call.ToolCall.ToolName != "google_search" {
		t.Fatalf("call = %#v", call)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish = %#v", finish)
	}
}

// TS: a nameless function_call is 'unknown' and tool-input-start is always
// sent at step.start; a status-less requires_action event sets that status.
func TestInteractionsStreamFunctionCallUnknownNameAndStatusDefaults(t *testing.T) {
	t.Parallel()
	chunks := runInteractionsEvents(t, icreated,
		`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"f1"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"{\"q\":","id":"f2"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"1}"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.requires_action"}`)
	got := chunkTypes(chunks)
	want := []provider.ChunkType{provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata, provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeFinish}
	if len(got) != len(want) {
		t.Fatalf("types = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("types = %v", got)
		}
	}
	if chunks[2].ToolCall.ToolName != "unknown" || chunks[2].ToolCall.ID != "f1" {
		t.Fatalf("start = %#v", chunks[2].ToolCall)
	}
	last := chunks[len(chunks)-1]
	if last.FinishReason != types.FinishReasonToolCalls || last.RawFinishReason != "requires_action" {
		t.Fatalf("finish = %#v", last)
	}
	if call := chunks[6].ToolCall; call.ID != "f2" || call.ToolName != "unknown" || call.RawArguments != `{"q":1}` {
		t.Fatalf("call = %#v", call)
	}
}

// TS JSON.stringify(call.arguments) keeps the wire key order.
func TestInteractionsParseOutputsKeepsArgumentKeyOrder(t *testing.T) {
	t.Parallel()
	steps := []interactionsContentBlock{
		{Type: "function_call", ID: "f", Name: "n", Arguments: []byte(`{"z":1,"a":{"y":1,"b":2}}`)},
		{Type: "google_search_call", ID: "g", Arguments: []byte(`{"q":"x","a":1}`)},
	}
	content, _, _ := (&InteractionsLanguageModel{}).parseOutputs(steps, "i")
	fc := content[0].(types.ToolCallContent)
	sc := content[1].(types.ToolCallContent)
	if fc.Input != `{"z":1,"a":{"y":1,"b":2}}` || sc.Input != `{"q":"x","a":1}` {
		t.Fatalf("inputs = %s | %s", fc.Input, sc.Input)
	}
}

func TestInteractionsConvertToolCallInputLikeSafeParseToolArgs(t *testing.T) {
	t.Parallel()
	m := &InteractionsLanguageModel{}
	cases := map[string]string{
		`{"b":1,"a":2}`: `{"b":1,"a":2}`,
		`[1,2]`:         `{"value":[1,2]}`,
		`not json`:      `{"value":"not json"}`,
		`5`:             `{"value":5}`,
	}
	for input, want := range cases {
		steps, _, _, err := m.convertPrompt(types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c", ToolName: "n", Input: input},
		}}}}, GoogleInteractionsProviderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got := jsonStringify(steps[0]["arguments"])
		if got != want {
			t.Errorf("input %q: arguments = %s, want %s", input, got, want)
		}
	}
}

func TestInteractionsConvertBareTopLevelMediaTypeSniffsBytes(t *testing.T) {
	t.Parallel()
	m := &InteractionsLanguageModel{}
	png := []byte("\x89PNG\r\n\x1a\n0000")
	steps, _, _, err := m.convertPrompt(types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{Data: png, MediaType: "image/*"},
	}}}}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	block := steps[0]["content"].([]map[string]interface{})[0]
	if block["mime_type"] != "image/png" {
		t.Fatalf("mime_type = %v", block["mime_type"])
	}
	_, _, _, err = m.convertPrompt(types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{Data: []byte("zzzz"), MediaType: "image/*"},
	}}}}, GoogleInteractionsProviderOptions{})
	if err == nil || !strings.Contains(err.Error(), "must specify subtype since it could not be auto-detected") {
		t.Fatalf("err = %v", err)
	}
}

// TS only knows model_output/thought/function_call/processing/built-in steps:
// a top-level text/image/video step is an unknown block that emits nothing,
// in streaming and in parseOutputs.
func TestInteractionsTopLevelContentStepsAreIgnored(t *testing.T) {
	chunks := runInteractionsEvents(t,
		`{"event_type":"step.start","index":0,"step":{"type":"text","text":"x"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hi"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"step.start","index":1,"step":{"type":"image"}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"image","data":"AAEC"}}`,
		`{"event_type":"step.stop","index":1}`,
		`{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed"}}`,
	)
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeStreamStart, provider.ChunkTypeFinish:
		default:
			t.Fatalf("unexpected chunk %v", c.Type)
		}
	}
	content, _, _ := (&InteractionsLanguageModel{}).parseOutputs([]interactionsContentBlock{{Type: "text", Text: "x"}, {Type: "image", Data: "AAEC"}}, "v1")
	if len(content) != 0 {
		t.Fatalf("content = %#v", content)
	}
}

// TS generateId: every source gets its own id (no repeating "source-N").
func TestInteractionsSourceIDsAreUnique(t *testing.T) {
	chunks := runInteractionsEvents(t,
		`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text_annotation","annotations":[{"type":"url_citation","url":"https://a.example"}]}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text_annotation","annotations":[{"type":"url_citation","url":"https://b.example"}]}}`,
		`{"event_type":"step.stop","index":0}`,
	)
	seen := map[string]bool{}
	n := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSource {
			n++
			if c.SourceContent.ID == "" || seen[c.SourceContent.ID] {
				t.Fatalf("source id %q empty or repeated", c.SourceContent.ID)
			}
			seen[c.SourceContent.ID] = true
		}
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
	ids := 0
	gen := func() string { ids++; return "gen-" + string(rune('0'+ids)) }
	out := annotationsToSources([]interactionsAnnotation{{Type: "url_citation", URL: "https://a.example"}}, gen)
	if out[0].(types.SourceContent).ID != "gen-1" {
		t.Fatalf("configured generator unused: %#v", out)
	}
}

// TS flush emits the finish part once, at the end of the stream: an error
// event does not finish early or close open blocks.
func TestInteractionsErrorEventFinishesAtStreamEnd(t *testing.T) {
	chunks := runInteractionsEvents(t,
		`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"a"}}`,
		`{"event_type":"error","error":{"message":"boom","code":"X"}}`,
	)
	var types []provider.ChunkType
	for _, c := range chunks {
		types = append(types, c.Type)
	}
	want := []provider.ChunkType{provider.ChunkTypeStreamStart, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeError, provider.ChunkTypeFinish}
	if len(types) != len(want) {
		t.Fatalf("chunks = %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("chunks = %v", types)
		}
	}
}

// TS streamGoogleInteractionEvents stops after a terminal `error` event: the
// background stream must not reconnect when the connection then closes.
func TestInteractionsBackgroundStreamDoesNotReconnectAfterError(t *testing.T) {
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"event_type\":\"error\",\"error\":{\"message\":\"boom\"}}\n\n")
	}))
	defer server.Close()
	client := New(Config{APIKey: "k", BaseURL: server.URL}).client
	stream, err := newInteractionsStream(context.Background(), client, "v1", nil, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	chunks := drainInteractions(t, stream)
	if gets.Load() != 1 {
		t.Fatalf("GET count = %d, want 1", gets.Load())
	}
	if last := chunks[len(chunks)-1]; last.Type != provider.ChunkTypeFinish {
		t.Fatalf("last chunk = %v", last.Type)
	}
}
