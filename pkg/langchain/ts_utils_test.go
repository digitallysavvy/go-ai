package langchain

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the pure-helper describes of utils.test.ts (convertToolResultPart,
// convertAssistantContent, isAIMessageChunk, isToolMessageType, getMessageText,
// extractImageOutputs, citations, emitSourceChunks). The class-instance and
// isInstance cases have no Go counterpart (messages are JSON maps).
func TestTSConvertToolResultPart(t *testing.T) {
	out := func(typ types.ToolResultOutputType, v interface{}) types.ToolResultContent {
		return types.ToolResultContent{ToolCallID: "call-1", ToolName: "t", Output: &types.ToolResultOutput{Type: typ, Value: v}}
	}
	cases := []struct {
		name string
		in   types.ToolResultContent
		want string
	}{
		{"text", out(types.ToolResultOutputText, "Sunny, 72°F"), "Sunny, 72°F"},
		{"error-text", out(types.ToolResultOutputErrorText, "Something went wrong"), "Something went wrong"},
		{"json", out(types.ToolResultOutputJSON, map[string]interface{}{"temperature": 72}), `{"temperature":72}`},
		{"error-json", out(types.ToolResultOutputErrorJSON, map[string]interface{}{"error": "Failed"}), `{"error":"Failed"}`},
		{"content text blocks", types.ToolResultContent{ToolCallID: "call-1", Output: &types.ToolResultOutput{
			Type: types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{
				types.TextContentBlock{Text: "First part "}, types.TextContentBlock{Text: "Second part"},
			}}}, "First part Second part"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := convertToolResultPart(c.in)
			if got["tool_call_id"] != "call-1" || got["content"] != c.want || got["type"] != "tool" {
				t.Fatalf("got %#v, want content %q", got, c.want)
			}
		})
	}
}

func TestTSConvertAssistantContent(t *testing.T) {
	got := convertAssistantContent([]types.ContentPart{types.TextContent{Text: "Hello "}, types.TextContent{Text: "World"}})
	if got["content"] != "Hello World" || got["type"] != "ai" {
		t.Fatalf("text parts: %#v", got)
	}
	call := types.ToolCallContent{ToolCallID: "call-1", ToolName: "get_weather", Arguments: map[string]interface{}{"location": "NYC"}}
	got = convertAssistantContent([]types.ContentPart{types.TextContent{Text: "I'll check the weather"}, call})
	calls, _ := got["tool_calls"].([]interface{})
	want := map[string]interface{}{"id": "call-1", "name": "get_weather", "args": map[string]interface{}{"location": "NYC"}}
	if got["content"] != "I'll check the weather" || len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("mixed: %#v", got)
	}
	got = convertAssistantContent([]types.ContentPart{types.TextContent{Text: "Just text"}})
	// langchain-core AIMessage.toDict().data always has both arrays.
	c, ok := got["tool_calls"].([]interface{})
	inv, ok2 := got["invalid_tool_calls"].([]interface{})
	if !ok || len(c) != 0 || !ok2 || len(inv) != 0 {
		t.Fatalf("want empty tool_calls and invalid_tool_calls: %#v", got)
	}
}

func TestTSMessagePredicatesAndText(t *testing.T) {
	m := func(kv ...interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	for _, c := range []struct {
		msg  map[string]interface{}
		want bool
	}{
		{m("type", "ai", "content", "Hello", "id", "msg-1"), true},
		{m("type", "AIMessageChunk", "content", "Hello", "id", "msg-1"), true},
		{m("type", "tool", "content", "Hello"), false},
		{m("type", "human", "content", "Hello"), false},
		{nil, false},
	} {
		if got := isAIMessageChunk(c.msg); got != c.want {
			t.Errorf("isAIMessageChunk(%v) = %v", c.msg, got)
		}
	}
	if !isToolMessage(m("type", "tool", "content", "Result", "tool_call_id", "call-1")) ||
		isToolMessage(m("type", "ai", "content", "Hello")) || isToolMessage(m("type", "human")) || isToolMessage(nil) {
		t.Error("isToolMessage")
	}
	for _, c := range []struct {
		msg  map[string]interface{}
		want string
	}{
		{m("content", "Plain text"), "Plain text"},
		{m("content", []interface{}{m("type", "text", "text", "Array")}), "Array"},
		{m("content", []interface{}{m("type", "image", "url", "http://example.com")}), ""},
		{nil, ""},
		{m("type", "ai"), ""},
	} {
		if got := messageText(c.msg); got != c.want {
			t.Errorf("messageText(%v) = %q, want %q", c.msg, got, c.want)
		}
	}
}

func TestTSImageOutputs(t *testing.T) {
	st := newEventState()
	src := map[string]interface{}{"additional_kwargs": map[string]interface{}{"tool_outputs": []interface{}{
		map[string]interface{}{"id": "img-1", "type": "image_generation_call", "status": "completed", "result": "base64data"},
		map[string]interface{}{"id": "tool-1", "type": "tool_call", "status": "completed"},
	}}}
	if got := imageOutputChunks(st, src); len(got) != 1 {
		t.Fatalf("want one image, got %v", got)
	}
	for _, s := range []map[string]interface{}{nil, {"additional_kwargs": map[string]interface{}{"tool_outputs": "not-array"}}, {"additional_kwargs": map[string]interface{}{}}} {
		if got := imageOutputChunks(newEventState(), s); len(got) != 0 {
			t.Fatalf("want none for %v, got %v", s, got)
		}
	}
}

func TestTSCitations(t *testing.T) {
	annotated := func(ann ...interface{}) map[string]interface{} {
		return map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "x", "annotations": ann}}}
	}
	got := extractCitations(annotated(map[string]interface{}{
		"type": "citation", "url": "https://example.com/sky", "title": "Why the sky is blue",
		"citedText": "Rayleigh scattering", "startIndex": float64(0), "endIndex": float64(16),
	}))
	if len(got) != 1 || got[0].URL != "https://example.com/sky" || got[0].Title != "Why the sky is blue" ||
		got[0].CitedText != "Rayleigh scattering" || got[0].Start != float64(0) || got[0].End != float64(16) {
		t.Fatalf("citations: %#v", got)
	}
	for name, msg := range map[string]map[string]interface{}{
		"empty annotations": annotated(),
		"string content":    {"content": "just text"},
		"non-citation":      annotated(map[string]interface{}{"type": "something-else", "value": 1}),
	} {
		if got := extractCitations(msg); len(got) != 0 {
			t.Errorf("%s: want none, got %#v", name, got)
		}
	}
	serialized := map[string]interface{}{"lc": 1, "type": "constructor", "id": []interface{}{"langchain_core", "messages", "AIMessageChunk"},
		"kwargs": annotated(map[string]interface{}{"type": "citation", "url": "https://example.com"})}
	if got := extractCitations(serialized); len(got) != 1 || got[0].URL != "https://example.com" {
		t.Fatalf("serialized: %#v", got)
	}
}

func TestTSEmitSourceChunks(t *testing.T) {
	st := newEventState()
	got := emitSourceChunks(st, "msg-1", []langChainCitation{{URL: "https://example.com/doc", Title: "Doc Title", CitedText: "excerpt", Start: 5, End: 12, Source: "web"}})
	if len(got) != 1 || got[0]["type"] != "source-url" || got[0]["sourceId"] != "https://example.com/doc" || got[0]["title"] != "Doc Title" {
		t.Fatalf("source-url: %#v", got)
	}
	wantMeta := map[string]interface{}{"langchain": map[string]interface{}{"citedText": "excerpt", "startIndex": 5, "endIndex": 12, "source": "web"}}
	if !reflect.DeepEqual(got[0]["providerMetadata"], wantMeta) {
		t.Fatalf("metadata: %#v", got[0]["providerMetadata"])
	}

	got = emitSourceChunks(newEventState(), "msg-1", []langChainCitation{{Title: "Local Doc", CitedText: "quoted text"}})
	if len(got) != 1 || got[0]["type"] != "source-document" || got[0]["sourceId"] != "msg-1:Local Doc:quoted text::" || got[0]["mediaType"] != "text/plain" {
		t.Fatalf("source-document: %#v", got)
	}
	if got := emitSourceChunks(newEventState(), "msg-1", []langChainCitation{{CitedText: "orphan excerpt"}}); len(got) != 0 {
		t.Fatalf("url-less without title or source: %#v", got)
	}

	st = newEventState()
	first := emitSourceChunks(st, "msg-1", []langChainCitation{{Title: "A"}, {Title: "B"}})
	second := emitSourceChunks(st, "msg-1", []langChainCitation{{Title: "B"}})
	if len(first) != 2 || len(second) != 0 {
		t.Fatalf("dedupe across calls: %v then %v", first, second)
	}

	got = emitSourceChunks(newEventState(), "msg-1", []langChainCitation{{URL: "https://example.com"}})
	if _, has := got[0]["providerMetadata"]; has || len(got) != 1 {
		t.Fatalf("providerMetadata should be omitted: %#v", got)
	}
	st = newEventState()
	c := langChainCitation{URL: "https://example.com"}
	if n := len(emitSourceChunks(st, "msg-1", []langChainCitation{c})) + len(emitSourceChunks(st, "msg-1", []langChainCitation{c})); n != 1 {
		t.Fatalf("url dedupe: %d", n)
	}
}
