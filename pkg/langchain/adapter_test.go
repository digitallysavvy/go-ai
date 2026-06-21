package langchain

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestToUIMessageStreamHITLMatchesToolCallEmittedViaMessagesMode(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"lc":   1,
			"type": "constructor",
			"id":   []interface{}{"langchain_core", "messages", "AIMessageChunk"},
			"kwargs": map[string]interface{}{
				"id":      "ai-msg-1",
				"content": []interface{}{},
				"tool_call_chunks": []interface{}{
					map[string]interface{}{
						"id":    "call_abc123",
						"name":  "delete_file",
						"args":  `{"filename":"report.pdf"}`,
						"index": 0,
					},
				},
				"tool_calls": []interface{}{},
			},
		},
		map[string]interface{}{"langgraph_step": 1, "langgraph_node": "agent"},
	}}
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"type":   "constructor",
				"id":     []interface{}{"langchain_core", "messages", "HumanMessage"},
				"kwargs": map[string]interface{}{"id": "human-1", "content": "Delete report.pdf"},
			},
			map[string]interface{}{
				"type": "constructor",
				"id":   []interface{}{"langchain_core", "messages", "AIMessage"},
				"kwargs": map[string]interface{}{
					"id":      "ai-msg-1",
					"content": "",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_abc123",
							"name": "delete_file",
							"args": map[string]interface{}{"filename": "report.pdf"},
							"type": "tool_call",
						},
					},
				},
			},
		},
		"__interrupt__": []interface{}{
			map[string]interface{}{
				"value": map[string]interface{}{
					"actionRequests": []interface{}{
						map[string]interface{}{
							"name": "delete_file",
							"args": map[string]interface{}{"filename": "report.pdf"},
						},
					},
				},
			},
		},
	}}
	close(events)

	chunks := collectChunks(t, events)
	approvalEvents := chunksOfType(chunks, "tool-approval-request")
	if len(approvalEvents) != 1 {
		t.Fatalf("approval events = %d, want 1; chunks=%#v", len(approvalEvents), chunks)
	}
	if approvalEvents[0]["approvalId"] != "call_abc123" || approvalEvents[0]["toolCallId"] != "call_abc123" {
		t.Fatalf("approval = %#v, want original call id", approvalEvents[0])
	}
	for _, chunk := range chunksOfType(chunks, "tool-input-start") {
		if chunk["toolCallId"] != "call_abc123" {
			t.Fatalf("unexpected duplicate tool-input-start: %#v", chunk)
		}
	}
	if available := chunksOfType(chunks, "tool-input-available"); len(available) != 0 {
		t.Fatalf("tool-input-available events = %d, want 0 for streamed plain RemoteGraph chunks; chunks=%#v", len(available), chunks)
	}
}

func TestLangSmithDeploymentTransportSendMessagesAndReconnect(t *testing.T) {
	var gotMessages []LangChainMessage
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{
		URL:     "https://test.langsmith.app",
		GraphID: "custom-agent",
		Stream: func(_ context.Context, messages []LangChainMessage) (<-chan StreamEvent, error) {
			gotMessages = messages
			events := make(chan StreamEvent, 1)
			events <- StreamEvent{Mode: "custom", Data: map[string]interface{}{"type": "progress", "value": 1}}
			close(events)
			return events, nil
		},
	})
	if transport.graphID != "custom-agent" {
		t.Fatalf("graphID = %q, want custom-agent", transport.graphID)
	}

	out, errs := transport.SendMessages(context.Background(), []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
	})
	chunks := collectOutputChunks(t, out, errs)
	if len(gotMessages) != 1 || gotMessages[0]["type"] != "human" {
		t.Fatalf("stream messages = %#v", gotMessages)
	}
	if len(chunksOfType(chunks, "data-progress")) != 1 {
		t.Fatalf("missing data-progress chunk: %#v", chunks)
	}

	_, reconnectErrs := transport.ReconnectToStream(context.Background(), "chat-1")
	if err := <-reconnectErrs; err == nil || err.Error() != "Method not implemented." {
		t.Fatalf("reconnect err = %v", err)
	}
}

func TestLangSmithDeploymentTransportDefaultsGraphID(t *testing.T) {
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: "https://test.langsmith.app"})
	if transport.graphID != "agent" {
		t.Fatalf("graphID = %q, want agent", transport.graphID)
	}
}

func TestLangSmithDeploymentTransportSendMessagesReportsStreamError(t *testing.T) {
	want := errors.New("boom")
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{
		Stream: func(context.Context, []LangChainMessage) (<-chan StreamEvent, error) {
			return nil, want
		},
	})
	out, errs := transport.SendMessages(context.Background(), nil)
	if _, ok := <-out; ok {
		t.Fatal("expected closed output")
	}
	if err := <-errs; !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestToUIMessageStreamHITLMatchesToolCallOnlyEmittedViaValuesMode(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"type":   "constructor",
				"id":     []interface{}{"langchain_core", "messages", "HumanMessage"},
				"kwargs": map[string]interface{}{"id": "human-1", "content": "Delete report.pdf"},
			},
			map[string]interface{}{
				"type": "constructor",
				"id":   []interface{}{"langchain_core", "messages", "AIMessage"},
				"kwargs": map[string]interface{}{
					"id":      "ai-msg-1",
					"content": "",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_values_only",
							"name": "delete_file",
							"args": map[string]interface{}{"filename": "report.pdf"},
							"type": "tool_call",
						},
					},
				},
			},
		},
		"__interrupt__": []interface{}{
			map[string]interface{}{
				"value": map[string]interface{}{
					"actionRequests": []interface{}{
						map[string]interface{}{
							"name": "delete_file",
							"args": map[string]interface{}{"filename": "report.pdf"},
						},
					},
				},
			},
		},
	}}
	close(events)

	chunks := collectChunks(t, events)
	approvalEvents := chunksOfType(chunks, "tool-approval-request")
	if len(approvalEvents) != 1 {
		t.Fatalf("approval events = %d, want 1; chunks=%#v", len(approvalEvents), chunks)
	}
	if approvalEvents[0]["toolCallId"] != "call_values_only" {
		t.Fatalf("approval = %#v, want values tool call id", approvalEvents[0])
	}
}

func TestParseLangGraphEventSupportsNamespacedTuple(t *testing.T) {
	event := ParseLangGraphEvent([]interface{}{"namespace", "values", map[string]interface{}{"ok": true}})
	if event.Mode != "values" {
		t.Fatalf("mode = %q, want values", event.Mode)
	}
	data, ok := event.Data.(map[string]interface{})
	if !ok || data["ok"] != true {
		t.Fatalf("data = %#v", event.Data)
	}
}

func TestToUIMessageStreamEmitsLangChainSourceParts(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"type": "constructor",
			"id":   []interface{}{"langchain_core", "messages", "AIMessageChunk"},
			"kwargs": map[string]interface{}{
				"id":      "ai-msg-2",
				"content": "answer",
				"content_blocks": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "answer",
						"annotations": []interface{}{
							map[string]interface{}{
								"type":        "citation",
								"url":         "https://example.com/doc",
								"title":       "Example Doc",
								"cited_text":  "quoted passage",
								"start_index": 2,
								"end_index":   15,
								"source":      "retriever",
							},
							map[string]interface{}{
								"type":       "citation",
								"title":      "Local Note",
								"cited_text": "local passage",
							},
						},
					},
				},
			},
		},
	}}
	close(events)

	chunks := collectChunks(t, events)
	urlSources := chunksOfType(chunks, "source-url")
	if len(urlSources) != 1 {
		t.Fatalf("source-url chunks = %d, want 1; chunks=%#v", len(urlSources), chunks)
	}
	if urlSources[0]["sourceId"] != "https://example.com/doc" || urlSources[0]["url"] != "https://example.com/doc" || urlSources[0]["title"] != "Example Doc" {
		t.Fatalf("source-url = %#v", urlSources[0])
	}
	metadata := urlSources[0]["providerMetadata"].(map[string]interface{})["langchain"].(map[string]interface{})
	if metadata["citedText"] != "quoted passage" || metadata["startIndex"] != 2 || metadata["endIndex"] != 15 || metadata["source"] != "retriever" {
		t.Fatalf("provider metadata = %#v", metadata)
	}
	docSources := chunksOfType(chunks, "source-document")
	if len(docSources) != 1 {
		t.Fatalf("source-document chunks = %d, want 1; chunks=%#v", len(docSources), chunks)
	}
	if docSources[0]["title"] != "Local Note" || docSources[0]["mediaType"] != "text/plain" {
		t.Fatalf("source-document = %#v", docSources[0])
	}
}

func TestToUIMessageStreamFromAnyHandlesDirectModelChunks(t *testing.T) {
	events := make(chan interface{}, 2)
	events <- map[string]interface{}{"id": "model-msg-1", "content": "Hello from model"}
	events <- map[string]interface{}{"id": "model-msg-1", "content": " again"}
	close(events)

	out, errs := ToUIMessageStreamFromAny(context.Background(), events)
	chunks := collectOutputChunks(t, out, errs)
	textStarts := chunksOfType(chunks, "text-start")
	textDeltas := chunksOfType(chunks, "text-delta")
	textEnds := chunksOfType(chunks, "text-end")
	if len(textStarts) != 1 || len(textDeltas) != 2 || len(textEnds) != 1 {
		t.Fatalf("unexpected model text lifecycle: %#v", chunks)
	}
	if textStarts[0]["id"] != "model-msg-1" || textEnds[0]["id"] != "model-msg-1" {
		t.Fatalf("text lifecycle ids: starts=%#v ends=%#v", textStarts, textEnds)
	}
}

func TestToUIMessageStreamCallbacksMatchLifecycle(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": "Hello "},
	}}
	valuesData := map[string]interface{}{"messages": []interface{}{}}
	events <- StreamEvent{Mode: "values", Data: valuesData}
	close(events)

	var order []string
	var tokens []string
	var final string
	var finishState interface{}
	out, errs := ToUIMessageStreamWithCallbacks(context.Background(), events, &StreamCallbacks{
		OnStart: func() error {
			order = append(order, "start")
			return nil
		},
		OnToken: func(token string) error {
			tokens = append(tokens, token)
			return nil
		},
		OnText: func(text string) error {
			order = append(order, "text")
			return nil
		},
		OnFinal: func(completion string) error {
			order = append(order, "final")
			final = completion
			return nil
		},
		OnFinish: func(state interface{}) error {
			order = append(order, "finish")
			finishState = state
			return nil
		},
	})
	_ = collectOutputChunks(t, out, errs)

	if final != "Hello " {
		t.Fatalf("final completion = %q, want Hello ", final)
	}
	if len(tokens) != 1 || tokens[0] != "Hello " {
		t.Fatalf("tokens = %#v", tokens)
	}
	finishMap, ok := finishState.(map[string]interface{})
	if !ok || finishMap["messages"] == nil {
		t.Fatalf("finish state = %#v, want values data", finishState)
	}
	wantOrder := []string{"start", "text", "final", "finish"}
	if len(order) != len(wantOrder) {
		t.Fatalf("callback order = %#v, want %#v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("callback order = %#v, want %#v", order, wantOrder)
		}
	}
}

func TestToUIMessageStreamCallbacksCallAbortWithPartialFinal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": "partial"},
	}}
	cancel()

	var final string
	aborted := false
	out, errs := ToUIMessageStreamWithCallbacks(ctx, events, &StreamCallbacks{
		OnFinal: func(completion string) error {
			final = completion
			return nil
		},
		OnAbort: func() error {
			aborted = true
			return nil
		},
	})
	for range out {
	}
	if err := <-errs; err == nil {
		t.Fatal("expected context error")
	}
	if final != "" {
		t.Fatalf("final completion = %q, want empty before any chunk sends", final)
	}
	if !aborted {
		t.Fatal("expected OnAbort callback")
	}
}

func TestToUIMessageStreamStreamEventsResetTextBetweenModelInvocations(t *testing.T) {
	events := make(chan StreamEvent, 4)
	events <- StreamEvent{Mode: "streamEvents", Data: map[string]interface{}{"event": "on_chat_model_start", "run_id": "run-1"}}
	events <- StreamEvent{Mode: "streamEvents", Data: map[string]interface{}{
		"event": "on_chat_model_stream",
		"data":  map[string]interface{}{"chunk": map[string]interface{}{"id": "msg-1", "content": "first"}},
	}}
	events <- StreamEvent{Mode: "streamEvents", Data: map[string]interface{}{"event": "on_chat_model_start", "run_id": "run-2"}}
	events <- StreamEvent{Mode: "streamEvents", Data: map[string]interface{}{
		"event": "on_chat_model_stream",
		"data":  map[string]interface{}{"chunk": map[string]interface{}{"id": "msg-2", "content": "second"}},
	}}
	close(events)

	chunks := collectChunks(t, events)
	textStarts := chunksOfType(chunks, "text-start")
	textEnds := chunksOfType(chunks, "text-end")
	if len(textStarts) != 2 || len(textEnds) != 2 {
		t.Fatalf("text starts=%d ends=%d chunks=%#v", len(textStarts), len(textEnds), chunks)
	}
	if textStarts[0]["id"] != "msg-1" || textEnds[0]["id"] != "msg-1" || textStarts[1]["id"] != "msg-2" || textEnds[1]["id"] != "msg-2" {
		t.Fatalf("unexpected text lifecycle chunks: starts=%#v ends=%#v", textStarts, textEnds)
	}
}

func TestConvertModelMessages(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "system"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "use tool"},
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "docs"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "call-1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"ok": true}}},
		}},
	}
	got := ConvertModelMessages(messages)
	if len(got) != 4 {
		t.Fatalf("messages = %d, want 4: %#v", len(got), got)
	}
	if got[0]["type"] != "system" || got[0]["content"] != "system" {
		t.Fatalf("system = %#v", got[0])
	}
	if got[1]["type"] != "human" || got[1]["content"] != "hello" {
		t.Fatalf("human = %#v", got[1])
	}
	if got[2]["type"] != "ai" || got[2]["content"] != "use tool" {
		t.Fatalf("assistant = %#v", got[2])
	}
	if got[3]["type"] != "tool" || got[3]["tool_call_id"] != "call-1" || got[3]["content"] != `{"ok":true}` {
		t.Fatalf("tool = %#v", got[3])
	}
}

func collectChunks(t *testing.T, events <-chan StreamEvent) []ai.UIMessageChunk {
	t.Helper()
	out, errs := ToUIMessageStream(context.Background(), events)
	return collectOutputChunks(t, out, errs)
}

func collectOutputChunks(t *testing.T, out <-chan ai.UIMessageChunk, errs <-chan error) []ai.UIMessageChunk {
	t.Helper()
	var chunks []ai.UIMessageChunk
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	return chunks
}

func chunksOfType(chunks []ai.UIMessageChunk, typ string) []ai.UIMessageChunk {
	var out []ai.UIMessageChunk
	for _, chunk := range chunks {
		if chunk["type"] == typ {
			out = append(out, chunk)
		}
	}
	return out
}
