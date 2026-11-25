package langchain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func typesOf(chunks []ai.UIMessageChunk) string {
	var out []string
	for _, c := range chunks {
		out = append(out, c["type"].(string))
	}
	return strings.Join(out, ",")
}

func messagesEvent(message map[string]interface{}) StreamEvent {
	return StreamEvent{Mode: "messages", Data: []interface{}{message, map[string]interface{}{}}}
}

func run(t *testing.T, events ...StreamEvent) []ai.UIMessageChunk {
	t.Helper()
	ch := make(chan StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return collectChunks(t, ch)
}

// TS emits reasoning before text for a chunk that has both, and closes the
// reasoning when the text starts.
func TestMessagesChunkEmitsReasoningBeforeText(t *testing.T) {
	got := run(t, messagesEvent(map[string]interface{}{
		"id": "m1", "type": "ai",
		"content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "think"},
			map[string]interface{}{"type": "text", "text": "answer"},
		},
	}))
	want := "start,reasoning-start,reasoning-delta,reasoning-end,text-start,text-delta,text-end,finish"
	if typesOf(got) != want {
		t.Fatalf("chunks = %s, want %s", typesOf(got), want)
	}
}

// A chunk with tool call chunks is handled as a tool call and nothing else.
func TestMessagesChunkWithToolCallChunksSkipsText(t *testing.T) {
	got := run(t, messagesEvent(map[string]interface{}{
		"id": "m1", "type": "ai", "content": "ignored",
		"tool_call_chunks": []interface{}{map[string]interface{}{"id": "c1", "name": "t", "args": "{", "index": 0}},
	}))
	if typesOf(got) != "start,tool-input-start,tool-input-delta,finish" {
		t.Fatalf("chunks = %s", typesOf(got))
	}
}

// Only AI and tool messages are streamed; a message without an id is dropped.
func TestMessagesEventIgnoresOtherMessageTypesAndMissingIDs(t *testing.T) {
	got := run(t,
		messagesEvent(map[string]interface{}{"id": "h1", "type": "human", "content": "hi"}),
		messagesEvent(map[string]interface{}{"type": "ai", "content": "no id"}),
	)
	if typesOf(got) != "start,finish" {
		t.Fatalf("chunks = %s", typesOf(got))
	}
}

func TestMessagesEventEmitsGeneratedImageOnce(t *testing.T) {
	msg := map[string]interface{}{
		"id": "m1", "type": "ai", "content": "",
		"additional_kwargs": map[string]interface{}{"tool_outputs": []interface{}{
			map[string]interface{}{"id": "img1", "type": "image_generation_call", "result": "QUJD", "output_format": "webp"},
		}},
	}
	got := run(t, messagesEvent(msg), messagesEvent(msg))
	files := chunksOfType(got, "file")
	if len(files) != 1 || files[0]["mediaType"] != "image/webp" || files[0]["url"] != "data:image/webp;base64,QUJD" {
		t.Fatalf("files = %#v", files)
	}
}

// Concurrent messages close in the order they were first seen.
func TestValuesClosesOpenMessagesInFirstSeenOrder(t *testing.T) {
	var events []StreamEvent
	for _, id := range []string{"zeta", "alpha", "mid"} {
		events = append(events, messagesEvent(map[string]interface{}{"id": id, "type": "ai", "content": "x"}))
	}
	events = append(events, StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{}}})
	var ends []string
	for _, c := range chunksOfType(run(t, events...), "text-end") {
		ends = append(ends, c["id"].(string))
	}
	if strings.Join(ends, ",") != "zeta,alpha,mid" {
		t.Fatalf("text-end order = %v", ends)
	}
}

// Reasoning that was only produced in the final state (GPT-5 format) is
// emitted once from the values event.
func TestValuesEmitsUnstreamedReasoningOnce(t *testing.T) {
	values := StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{
		map[string]interface{}{
			"id": "m1", "type": "ai", "content": "",
			"response_metadata": map[string]interface{}{"output": []interface{}{
				map[string]interface{}{"type": "reasoning", "id": "rs_1", "summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": "because"}}},
			}},
		},
	}}}
	got := run(t, values, values)
	if typesOf(got) != "start,reasoning-start,reasoning-delta,reasoning-end,finish" {
		t.Fatalf("chunks = %s", typesOf(got))
	}
	if got[2]["delta"] != "because" {
		t.Fatalf("delta = %v", got[2]["delta"])
	}
}

func TestUserContentJoinsTextAndUsesTSFileBlocks(t *testing.T) {
	msgs := ConvertModelMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}, types.TextContent{Text: "b"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{URL: "https://x.test/d.pdf", MediaType: "application/pdf"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{Data: []byte("pdf"), MediaType: "application/pdf", Filename: "n.pdf"}}},
	})
	if msgs[0]["content"] != "ab" || msgs[0]["role"] != "human" {
		t.Fatalf("text = %#v", msgs[0])
	}
	url := msgs[1]["content"].([]interface{})[0].(map[string]interface{})
	if url["type"] != "file" || url["url"] != "https://x.test/d.pdf" || url["mimeType"] != "application/pdf" || url["filename"] != "file.pdf" {
		t.Fatalf("url block = %#v", url)
	}
	data := msgs[2]["content"].([]interface{})[0].(map[string]interface{})
	if data["data"] != "cGRm" || data["mimeType"] != "application/pdf" || data["filename"] != "n.pdf" {
		t.Fatalf("data block = %#v", data)
	}
}

func TestSourceURLCarriesOnlyTheCitationTitle(t *testing.T) {
	msg := map[string]interface{}{"id": "m1", "type": "ai", "content": []interface{}{
		map[string]interface{}{"type": "text", "text": "hi", "annotations": []interface{}{
			map[string]interface{}{"type": "citation", "url": "https://a.test", "source": "retriever"},
			map[string]interface{}{"type": "citation", "source": "doc-1"},
		}},
	}}
	chunks := run(t, messagesEvent(msg))
	urls := chunksOfType(chunks, "source-url")
	docs := chunksOfType(chunks, "source-document")
	if len(urls) != 1 || urls[0]["title"] != nil {
		t.Fatalf("urls = %#v", urls)
	}
	if len(docs) != 1 || docs[0]["title"] != "doc-1" {
		t.Fatalf("docs = %#v", docs)
	}
}

// A model stream emits reasoning, then text, and an empty stream emits no
// finish chunk.
func TestModelAndEmptyStreams(t *testing.T) {
	got := run(t,
		StreamEvent{Mode: "model", Data: map[string]interface{}{"id": "m1", "content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "r"},
			map[string]interface{}{"type": "text", "text": "t"},
		}}},
	)
	if typesOf(got) != "start,reasoning-start,reasoning-delta,reasoning-end,text-start,text-delta,text-end,finish" {
		t.Fatalf("chunks = %s", typesOf(got))
	}
	if got := run(t); typesOf(got) != "start" {
		t.Fatalf("empty stream chunks = %s", typesOf(got))
	}
}

func TestUpstreamErrorEndsStreamWithErrorChunk(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- messagesEvent(map[string]interface{}{"id": "m1", "type": "ai", "content": "partial"})
	events <- StreamEvent{Err: errors.New("boom")}
	close(events)
	var finalText string
	var seen error
	out, errs := ToUIMessageStreamWithCallbacks(context.Background(), events, &StreamCallbacks{
		OnFinal: func(s string) error { finalText = s; return nil },
		OnError: func(err error) error { seen = err; return nil },
	})
	var chunks []ai.UIMessageChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	if err := <-errs; err != nil {
		t.Fatalf("unexpected errs: %v", err)
	}
	last := chunks[len(chunks)-1]
	if last["type"] != "error" || last["errorText"] != "boom" || finalText != "partial" || seen == nil {
		t.Fatalf("chunks=%v final=%q seen=%v", typesOf(chunks), finalText, seen)
	}
}

// TS regex /^data:([^;]+);base64,(.+)$/ : `.` rejects line terminators, so a
// base64 payload with a line break stays a plain url block.
func TestImageDataURLWithLineBreakStaysURL(t *testing.T) {
	block := canonicalImageBlockFromString("data:image/png;base64,AAAA\nBBBB", "image/png")
	if block["url"] != "data:image/png;base64,AAAA\nBBBB" || block["data"] != nil {
		t.Fatalf("block = %v", block)
	}
	block = canonicalImageBlockFromString("data:image/png;base64,AAAA", "image/png")
	if block["data"] != "AAAA" || block["mimeType"] != "image/png" {
		t.Fatalf("block = %v", block)
	}
}

// TS: `actionRequest.args || actionRequest.arguments` and
// `actionRequests || action_requests` fall through on falsy values.
func TestInterruptFalsyArgsFallThrough(t *testing.T) {
	got := run(t, StreamEvent{Mode: "values", Data: map[string]interface{}{
		"__interrupt__": []interface{}{map[string]interface{}{"value": map[string]interface{}{
			"actionRequests":  nil,
			"action_requests": []interface{}{map[string]interface{}{"id": "c1", "name": "t", "args": nil, "arguments": map[string]interface{}{"a": 1.0}}},
		}}},
	}})
	var input interface{}
	for _, c := range got {
		if c["type"] == "tool-input-available" {
			input = c["input"]
		}
	}
	m, ok := input.(map[string]interface{})
	if !ok || m["a"] != 1.0 {
		t.Fatalf("chunks = %v", got)
	}
}
