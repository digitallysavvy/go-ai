package langchain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func annotatedMsg(annotation map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"contentBlocks": []interface{}{map[string]interface{}{"type": "text", "text": "x", "annotations": []interface{}{annotation}}},
	}
}

// TS `citation.title ?? citation.source`: an empty title is present, so a
// URL-less citation with title "" and a source is skipped, not labelled by
// the source.
func TestR34CitationEmptyTitleDoesNotFallBackToSource(t *testing.T) {
	cites := extractCitations(annotatedMsg(map[string]interface{}{"type": "citation", "title": "", "source": "web"}))
	if got := emitSourceChunks(newEventState(), "m", cites); len(got) != 0 {
		t.Fatalf("empty title fell back to source: %#v", got)
	}
	cites = extractCitations(annotatedMsg(map[string]interface{}{"type": "citation", "source": "web"}))
	if got := emitSourceChunks(newEventState(), "m", cites); len(got) != 1 || got[0]["title"] != "web" {
		t.Fatalf("absent title must use the source: %#v", got)
	}
}

// buildSourceProviderMetadata tests `typeof === 'string'`, so empty citedText
// and source strings are kept.
func TestR34CitationEmptyStringsStayInProviderMetadata(t *testing.T) {
	cites := extractCitations(annotatedMsg(map[string]interface{}{"type": "citation", "url": "https://a.example", "citedText": "", "source": ""}))
	got := emitSourceChunks(newEventState(), "m", cites)
	want := map[string]interface{}{"langchain": map[string]interface{}{"citedText": "", "source": ""}}
	if len(got) != 1 || !reflect.DeepEqual(got[0]["providerMetadata"], want) {
		t.Fatalf("got %#v, want metadata %#v", got, want)
	}
}

// A reasoning block with an empty string still counts as found: TS does not
// fall back to additional_kwargs.reasoning.summary.
func TestR34EmptyReasoningBlockSkipsSummaryFallback(t *testing.T) {
	msg := map[string]interface{}{
		"contentBlocks":     []interface{}{map[string]interface{}{"type": "reasoning", "reasoning": ""}},
		"additional_kwargs": map[string]interface{}{"reasoning": map[string]interface{}{"summary": []interface{}{map[string]interface{}{"text": "from summary"}}}},
	}
	if got := messageReasoning(msg); got != "" {
		t.Fatalf("reasoning = %q, want empty", got)
	}
	delete(msg, "contentBlocks")
	if got := messageReasoning(msg); got != "from summary" {
		t.Fatalf("no blocks: reasoning = %q", got)
	}
}

// JSON.stringify drops an undefined output member; an explicit null stays.
func TestR34ToolOutputAbsentStaysOffTheWire(t *testing.T) {
	encode := func(ch map[string]interface{}) string { b, _ := json.Marshal(ch); return string(b) }
	state := newEventState()
	got := processToolsEvent(state, StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_end", "toolCallId": "c1", "name": "t"}})
	last := got[len(got)-1]
	if s := encode(last); s != `{"toolCallId":"c1","type":"tool-output-available"}` {
		t.Fatalf("absent output: %s", s)
	}
	state = newEventState()
	got = processToolsEvent(state, StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_end", "toolCallId": "c1", "name": "t", "output": nil}})
	if s := encode(got[len(got)-1]); s != `{"output":null,"toolCallId":"c1","type":"tool-output-available"}` {
		t.Fatalf("null output: %s", s)
	}
	chunks := processStreamEventsEvent(newEventState(), map[string]interface{}{"event": "on_tool_end", "run_id": "r1", "data": map[string]interface{}{}})
	if s := encode(chunks[0]); s != `{"toolCallId":"r1","type":"tool-output-available"}` {
		t.Fatalf("streamEvents absent output: %s", s)
	}
}

// `typeof name === 'string' ? name : 'unknown'` keeps an empty name.
func TestR34EmptyToolNameIsKept(t *testing.T) {
	got := processToolsEvent(newEventState(), StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_start", "toolCallId": "c1", "name": ""}})
	if got[0]["toolName"] != "" {
		t.Fatalf("tools toolName = %#v, want empty string", got[0]["toolName"])
	}
	got = processToolsEvent(newEventState(), StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_start", "toolCallId": "c1", "name": 5.0}})
	if got[0]["toolName"] != "unknown" {
		t.Fatalf("non-string toolName = %#v, want unknown", got[0]["toolName"])
	}
}

// getMessageId needs typeof id === 'string': a numeric id is no id, and an
// empty string id is an id that is not replaced by the kwargs one.
func TestR34MessageIDMustBeString(t *testing.T) {
	if id, ok := messageIDOpt(map[string]interface{}{"id": 5.0}); ok || id != "" {
		t.Fatalf("numeric id = %q, %v", id, ok)
	}
	if id, ok := messageIDOpt(map[string]interface{}{"id": "", "type": "constructor", "kwargs": map[string]interface{}{"id": "k"}}); !ok || id != "" {
		t.Fatalf("empty id = %q, %v", id, ok)
	}
	if id, ok := messageIDOpt(map[string]interface{}{"type": "constructor", "id": []interface{}{"a"}, "kwargs": map[string]interface{}{"id": "k"}}); !ok || id != "k" {
		t.Fatalf("kwargs id = %q, %v", id, ok)
	}
}
