package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 77 stream reviewer tests. Every package-level identifier starts with
// r77stream.

// TS WorkflowAgent invokeToolInputLifecycleCallbacks passes `messages:
// stepInputMessages`, the conversationPrompt the model step was called with
// (system message first). Go replayed the deferred callbacks with the step's
// input messages (no system message). Without ToolMessagesAsConversationPrompt
// the replay keeps the input messages (TS streamText).
func TestR77StreamDeferredToolInputCallbacksGetConversationPrompt(t *testing.T) {
	run := func(t *testing.T, internal *InternalOptions) map[string][]types.Message {
		var mu sync.Mutex
		got := map[string][]types.Message{}
		record := func(name string, msgs []types.Message) {
			mu.Lock()
			defer mu.Unlock()
			got[name] = msgs
		}
		tool := types.Tool{
			Name:       "search",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}},
			OnInputStart: func(_ context.Context, o types.OnInputStartOptions) error {
				record("start", o.Messages)
				return nil
			},
			OnInputDelta: func(_ context.Context, o types.OnInputDeltaOptions) error {
				record("delta", o.Messages)
				return nil
			},
			OnInputAvailable: func(_ context.Context, o types.OnInputAvailableOptions) error {
				record("available", o.Messages)
				return nil
			},
		}
		step := []provider.StreamChunk{
			{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "search"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{"query":"docs"}`},
			{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{"query": "docs"}, RawArguments: `{"query":"docs"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}
		r, err := StreamText(context.Background(), StreamTextOptions{
			Model: r71streamModel(nil, step), System: "sys", Prompt: "p", Tools: []types.Tool{tool},
			Internal: internal, OnError: func(context.Context, error) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	withSystem := `[{"role":"system","content":[{"type":"text","text":"sys"}]},{"role":"user","content":[{"type":"text","text":"p"}]}]`
	userOnly := `[{"role":"user","content":[{"type":"text","text":"p"}]}]`
	for _, c := range []struct {
		name     string
		internal *InternalOptions
		want     string
	}{
		{"workflow", &InternalOptions{DeferToolInputCallbacks: true, ToolMessagesAsConversationPrompt: true}, withSystem},
		{"deferred only", &InternalOptions{DeferToolInputCallbacks: true}, userOnly},
		{"inline", nil, userOnly},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := run(t, c.internal)
			for _, name := range []string{"start", "delta", "available"} {
				msgs, ok := got[name]
				if !ok {
					t.Fatalf("%s callback not called", name)
				}
				r75streamAssertJSON(t, name+" messages", r76streamRoleText(msgs), c.want)
			}
		})
	}
}

// TS prepareHeaders(headers, UI_MESSAGE_STREAM_HEADERS): a header the
// caller sets replaces the default of the same name. Go added Header values
// next to the default (two Cache-Control values).
func TestR77StreamUIResponseHeaderReplacesDefault(t *testing.T) {
	init := &UIMessageStreamResponseInit{
		Headers: map[string]string{"custom-header": "test"},
		Header:  http.Header{"Cache-Control": {"no-store"}, "Set-Cookie": {"a=1", "b=2"}},
	}
	resp, err := CreateUIMessageChunksResponse(r76streamChan(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "x"}), init)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	want := http.Header{
		"Cache-Control":                 {"no-store"},
		"Connection":                    {"keep-alive"},
		"Content-Type":                  {"text/event-stream"},
		"Custom-Header":                 {"test"},
		"Set-Cookie":                    {"a=1", "b=2"},
		"X-Accel-Buffering":             {"no"},
		"X-Vercel-Ai-Ui-Message-Stream": {"v1"},
	}
	if !reflect.DeepEqual(resp.Header, want) {
		t.Fatalf("headers = %v\nwant      %v", resp.Header, want)
	}
	// Headers (single value) also replaces a default, as before.
	_, _, h := uiMessageStreamResponseHead(&UIMessageStreamResponseInit{Headers: map[string]string{"content-type": "text/plain"}})
	if got := h.Values("Content-Type"); !reflect.DeepEqual(got, []string{"text/plain"}) {
		t.Fatalf("content-type = %q", got)
	}
}

// create-ui-message-stream-response.test.ts 19: the default headers plus the
// caller's, status 200, the chunk and [DONE] (keys sorted: see known
// differences, UI message stream JSON key order).
func TestR77StreamCreateUIMessageChunksResponseHeadersAndBody(t *testing.T) {
	resp, err := CreateUIMessageChunksResponse(r76streamChan(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "test-data"}),
		&UIMessageStreamResponseInit{Status: 200, StatusText: "OK", Headers: map[string]string{"Custom-Header": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.Status != "OK" {
		t.Fatalf("status = %d %q", resp.StatusCode, resp.Status)
	}
	if got, want := len(resp.Header), 6; got != want {
		t.Fatalf("headers = %v", resp.Header)
	}
	if resp.Header.Get("Custom-Header") != "test" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers = %v", resp.Header)
	}
	if got, want := string(body), "data: {\"delta\":\"test-data\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("body = %q\nwant %q", got, want)
	}
}

// create-ui-message-stream-response.test.ts 129 / 305: an error chunk is
// written as is, and a consumeSseStream callback that fails does not affect
// the response body (it is still called once and sees the same events).
func TestR77StreamCreateUIMessageChunksResponseErrorsAndConsumeFailure(t *testing.T) {
	resp, err := CreateUIMessageChunksResponse(r76streamChan(UIMessageChunk{"type": "error", "errorText": "Custom error message"}), &UIMessageStreamResponseInit{Status: 200})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got, want := string(body), "data: {\"errorText\":\"Custom error message\",\"type\":\"error\"}\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("error body = %q\nwant %q", got, want)
	}

	var mu sync.Mutex
	calls := 0
	resp, err = CreateUIMessageChunksResponse(r76streamChan(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "error-test"}), &UIMessageStreamResponseInit{
		Status: 200,
		ConsumeSSEStream: func(io.Reader) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return errors.New("consumeSseStream error")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("body read error = %v", err)
	}
	if got, want := string(body), "data: {\"delta\":\"error-test\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("body = %q\nwant %q", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("consumeSseStream calls = %d", calls)
	}
}

// TS updateMessageMetadata skips `metadata == null`. A Go value that encodes
// as JSON null (a nil pointer, a nil map, json.RawMessage("null")) replaced
// the message metadata with null.
func TestR77StreamNullMetadataIsSkipped(t *testing.T) {
	var nilMeta *r76streamMeta
	for _, c := range []struct {
		name string
		meta interface{}
	}{
		{"nil pointer", nilMeta},
		{"nil map", map[string]interface{}(nil)},
		{"raw null", json.RawMessage("null")},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var end map[string]interface{}
			_, err := r71streamCreate(t, []UIMessageChunk{
				{"type": "start", "messageId": "m1", "messageMetadata": map[string]interface{}{"a": 1.0}},
				{"type": "message-metadata", "messageMetadata": c.meta},
				{"type": "finish", "messageMetadata": c.meta},
			}, UIMessageStreamOptions{OnEnd: func(e map[string]interface{}) {
				mu.Lock()
				defer mu.Unlock()
				end = e
			}})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			r75streamAssertJSON(t, "metadata", end["responseMessage"].(UIMessageChunk)["metadata"], `{"a":1}`)

			// No metadata before: a null value leaves none (TS keeps undefined).
			var end2 map[string]interface{}
			_, err = r71streamCreate(t, []UIMessageChunk{
				{"type": "start", "messageId": "m1"},
				{"type": "message-metadata", "messageMetadata": c.meta},
			}, UIMessageStreamOptions{OnEnd: func(e map[string]interface{}) { end2 = e }})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := end2["responseMessage"].(UIMessageChunk)["metadata"]; ok {
				t.Fatalf("metadata = %#v, want none", end2["responseMessage"].(UIMessageChunk)["metadata"])
			}
		})
	}
}

// Known difference (reviewer item 3): the end event of a stream without a
// finish chunk has finishReason nil from CreateUIMessageStreamWithOptions and
// types.FinishReason("") from ToUIMessageStream (both v0.5 shapes; TS
// undefined). With a finish chunk, CreateUIMessageStreamWithOptions reports
// the chunk's own value (a string) and ToUIMessageStream a types.FinishReason.
func TestR77StreamEndFinishReasonShapes(t *testing.T) {
	var end map[string]interface{}
	if _, err := r71streamCreate(t, []UIMessageChunk{{"type": "start"}}, UIMessageStreamOptions{OnEnd: func(e map[string]interface{}) { end = e }}); err != nil {
		t.Fatal(err)
	}
	if v, ok := end["finishReason"]; !ok || v != nil {
		t.Fatalf("CreateUIMessageStreamWithOptions finishReason = %#v (present %v), want nil", v, ok)
	}
	if _, err := r71streamCreate(t, []UIMessageChunk{{"type": "start"}, {"type": "finish", "finishReason": "stop"}}, UIMessageStreamOptions{OnEnd: func(e map[string]interface{}) { end = e }}); err != nil {
		t.Fatal(err)
	}
	if v := end["finishReason"]; v != "stop" {
		t.Fatalf("CreateUIMessageStreamWithOptions finishReason = %#v, want string stop", v)
	}

	var mu sync.Mutex
	var toEnd map[string]interface{}
	record := func(e map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		toEnd = e
	}
	if _, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart}, UIMessageStreamResultOptions{OnEnd: record}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if v := toEnd["finishReason"]; v != types.FinishReason("") {
		t.Fatalf("ToUIMessageStream finishReason = %#v, want FinishReason(\"\")", v)
	}
	mu.Unlock()
	if _, err := r71streamCollect(t, []provider.StreamChunk{r71streamStart, r71streamFinish()}, UIMessageStreamResultOptions{OnEnd: record}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if v := toEnd["finishReason"]; v != types.FinishReasonStop {
		t.Fatalf("ToUIMessageStream finishReason = %#v, want FinishReasonStop", v)
	}
}

// get-response-ui-message-id.test.ts 8 / 16 / 28 / 40 / 48 (through
// responseUIMessageIDOK, the Go form of getResponseUIMessageId as
// toUIMessageStream calls it; ResponseMessageID is the string form).
func TestR77StreamGetResponseUIMessageID(t *testing.T) {
	gen := func() string { return "new-id" }
	for _, c := range []struct {
		name   string
		opts   UIMessageStreamResultOptions
		want   string
		wantOK bool
	}{
		{"no original messages", UIMessageStreamResultOptions{GenerateMessageID: gen}, "", false},
		{"last assistant", UIMessageStreamResultOptions{GenerateMessageID: gen, OriginalMessages: []UIMessageChunk{{"id": "msg-1", "role": "user", "parts": []interface{}{}}, {"id": "msg-2", "role": "assistant", "parts": []interface{}{}}}}, "msg-2", true},
		{"last not assistant", UIMessageStreamResultOptions{GenerateMessageID: gen, OriginalMessages: []UIMessageChunk{{"id": "msg-1", "role": "assistant", "parts": []interface{}{}}, {"id": "msg-2", "role": "user", "parts": []interface{}{}}}}, "new-id", true},
		{"empty messages", UIMessageStreamResultOptions{GenerateMessageID: gen, OriginalMessages: []UIMessageChunk{}}, "new-id", true},
		{"string id", UIMessageStreamResultOptions{ResponseMessageID: "response-id", OriginalMessages: []UIMessageChunk{}}, "response-id", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := responseUIMessageIDOK(c.opts)
			if got != c.want || ok != c.wantOK {
				t.Fatalf("id = %q, %v; want %q, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}

// read-ui-message-stream.test.ts 259: a hydrated input-streaming static tool
// part without rawInput accepts a resumed delta and the final input.
func TestR77StreamReadUIMessagesHydratedPartialToolWithoutRawInput(t *testing.T) {
	var message UIMessage
	if err := json.Unmarshal([]byte(`{"id":"msg-123","role":"assistant","parts":[{"type":"tool-createDocument","toolCallId":"tool-1","state":"input-streaming","input":{"title":"Hel"}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	msgs, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{
		Message: &message,
		Stream: r76streamChan(
			UIMessageChunk{"type": "tool-input-delta", "toolCallId": "tool-1", "inputTextDelta": `lo"}`},
			UIMessageChunk{"type": "tool-input-available", "toolCallId": "tool-1", "toolName": "createDocument", "input": map[string]interface{}{"title": "Hello"}},
		),
		TerminateOnError: true,
	})
	var last UIMessage
	n := 0
	for m := range msgs {
		last = m
		n++
	}
	for err := range errs {
		t.Fatalf("error = %v", err)
	}
	if n == 0 || len(last.Parts) != 1 {
		t.Fatalf("snapshots = %d, last = %+v", n, last)
	}
	b, _ := json.Marshal(last.Parts[0])
	var part map[string]interface{}
	_ = json.Unmarshal(b, &part)
	if part["type"] != "tool-createDocument" || part["toolCallId"] != "tool-1" || part["state"] != "input-available" || !reflect.DeepEqual(part["input"], map[string]interface{}{"title": "Hello"}) {
		t.Fatalf("part = %s", b)
	}
}

// r77streamPartsJSON reduces full stream parts to the TS snapshot fields the
// stream-text.test.ts result.stream ports compare.
func r77streamPartsJSON(parts []provider.StreamChunk, withTimestamp bool) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, p := range parts {
		name := tsPartName(&p)
		if i := strings.IndexByte(name, ':'); i >= 0 {
			name = name[:i]
		}
		m := map[string]interface{}{"type": name}
		switch p.Type {
		case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd:
			m["id"] = p.ID
		case provider.ChunkTypeText:
			m["id"], m["text"] = p.ID, p.Text
		case provider.ChunkTypeSource:
			s := p.SourceContent
			m["id"], m["sourceType"], m["url"], m["title"] = s.ID, s.SourceType, s.URL, s.Title
			m["providerMetadata"] = json.RawMessage(s.ProviderMetadata)
		case provider.ChunkTypeCustom:
			m["kind"] = p.CustomContent.Kind
			m["providerMetadata"] = json.RawMessage(p.CustomContent.ProviderMetadata)
		case provider.ChunkTypeFinishStep:
			m["finishReason"], m["rawFinishReason"] = string(p.FinishReason), p.RawFinishReason
			response := map[string]interface{}{"id": p.Response.ID, "modelId": p.Response.ModelID}
			if withTimestamp {
				response["timestamp"] = p.Response.Timestamp.UTC().Format(time.RFC3339Nano)
			}
			m["response"] = response
			m["usage"] = map[string]interface{}{"inputTokens": p.Usage.GetInputTokens(), "outputTokens": p.Usage.GetOutputTokens(), "totalTokens": p.Usage.GetTotalTokens()}
			m["hasTimeBetweenOutputChunks"] = p.Performance != nil && p.Performance.TimeBetweenOutputChunksMs != nil
		case provider.ChunkTypeFinish:
			m["finishReason"], m["rawFinishReason"] = string(p.FinishReason), p.RawFinishReason
			m["totalUsage"] = map[string]interface{}{"inputTokens": p.Usage.GetInputTokens(), "outputTokens": p.Usage.GetOutputTokens(), "totalTokens": p.Usage.GetTotalTokens()}
		}
		out = append(out, m)
	}
	return out
}

func r77streamFinishStop() provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}
}

// stream-text.test.ts 669: result.stream > should send text deltas (the
// provider's response metadata id, model id and timestamp on finish-step;
// timeBetweenOutputChunksMs set for three deltas; the prompt sent).
func TestR77StreamFullStreamTextDeltas(t *testing.T) {
	var prompt types.Prompt
	model := r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "response-id", ModelID: "response-model-id", Timestamp: time.UnixMilli(5000)}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
		{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		r77streamFinishStop(),
	})
	inner := model.DoStreamFunc
	model.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		prompt = o.Prompt
		return inner(ctx, o)
	}
	_, parts := r73streamParts(t, StreamTextOptions{Model: model, Prompt: "test-input"})
	r75streamAssertJSON(t, "prompt", r76streamRoleText(prompt.Messages), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
	if prompt.System != "" {
		t.Fatalf("system = %q", prompt.System)
	}
	r75streamAssertJSON(t, "parts", r77streamPartsJSON(parts, true), `[
		{"type":"start"},{"type":"start-step"},
		{"type":"text-start","id":"1"},
		{"type":"text-delta","id":"1","text":"Hello"},
		{"type":"text-delta","id":"1","text":", "},
		{"type":"text-delta","id":"1","text":"world!"},
		{"type":"text-end","id":"1"},
		{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"response-id","modelId":"response-model-id","timestamp":"1970-01-01T00:00:05Z"},"usage":{"inputTokens":3,"outputTokens":10,"totalTokens":13},"hasTimeBetweenOutputChunks":true},
		{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokens":3,"outputTokens":10,"totalTokens":13}}]`)
}

// stream-text.test.ts 1061 / 1180: result.stream > should send sources /
// custom parts (TS modelWithSources / modelWithCustom), with the fallback
// response id id-0 and model id mock-model-id.
func TestR77StreamFullStreamSourcesAndCustomParts(t *testing.T) {
	source := func(id, url, title, custom string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: id, URL: url, Title: title, ProviderMetadata: json.RawMessage(`{"provider":{"custom":"` + custom + `"}}`)}}
	}
	text := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	}
	finishStep := `{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id"},"usage":{"inputTokens":3,"outputTokens":10,"totalTokens":13},"hasTimeBetweenOutputChunks":false}`
	finish := `{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokens":3,"outputTokens":10,"totalTokens":13}}`
	textJSON := `{"type":"text-start","id":"1"},{"type":"text-delta","id":"1","text":"Hello!"},{"type":"text-end","id":"1"}`
	opts := func(step []provider.StreamChunk) StreamTextOptions {
		return StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "prompt", Internal: &InternalOptions{
			GenerateID: func() string { return "id-0" },
		}}
	}

	step := append([]provider.StreamChunk{source("123", "https://example.com", "Example", "value")}, text...)
	step = append(step, source("456", "https://example.com/2", "Example 2", "value2"), r77streamFinishStop())
	_, parts := r73streamParts(t, opts(step))
	r75streamAssertJSON(t, "sources", r77streamPartsJSON(parts, false), `[{"type":"start"},{"type":"start-step"},
		{"type":"source","id":"123","sourceType":"url","url":"https://example.com","title":"Example","providerMetadata":{"provider":{"custom":"value"}}},
		`+textJSON+`,
		{"type":"source","id":"456","sourceType":"url","url":"https://example.com/2","title":"Example 2","providerMetadata":{"provider":{"custom":"value2"}}},
		`+finishStep+`,`+finish+`]`)

	step = append(append([]provider.StreamChunk(nil), text...),
		provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "openai.compaction", ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"cmp_123"}}`)}},
		r77streamFinishStop())
	_, parts = r73streamParts(t, opts(step))
	r75streamAssertJSON(t, "custom", r77streamPartsJSON(parts, false), `[{"type":"start"},{"type":"start-step"},`+textJSON+`,
		{"type":"custom","kind":"openai.compaction","providerMetadata":{"openai":{"itemId":"cmp_123"}}},
		`+finishStep+`,`+finish+`]`)
}

// handle-ui-message-stream-finish.test.ts 22: without onFinish/onStepEnd
// the chunks pass through unchanged (no processing: a delta without its
// start does not fail the stream).
func TestR77StreamHandleFinishPassThroughWithoutCallbacks(t *testing.T) {
	input := []UIMessageChunk{
		{"type": "start", "messageId": "msg-123"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "Hello, World!"},
		{"type": "text-end", "id": "text-1"},
		{"type": "text-delta", "id": "missing", "delta": "x"},
		{"type": "finish"},
	}
	got, err := r71streamCreate(t, input, UIMessageStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("chunks = %v\nwant %v", got, input)
	}
	if !strings.Contains(r71streamJSON(t, got[0]), `"messageId":"msg-123"`) {
		t.Fatalf("start = %v", got[0])
	}
}
