package langchain

import (
	"context"
	"encoding/base64"
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
	starts := chunksOfType(chunks, "tool-input-start")
	if len(starts) != 1 || starts[0]["toolCallId"] != "call_abc123" {
		t.Fatalf("tool-input-start = %#v, want exactly one for call_abc123", starts)
	}
	// The tool call was only ever streamed as tool_call_chunks (which never
	// carry a complete input), so its input becomes "available" once the
	// values event finalizes the message - matching the TypeScript adapter's
	// values-event finalization of `seen.tool` bookkeeping.
	available := chunksOfType(chunks, "tool-input-available")
	if len(available) != 1 || available[0]["toolCallId"] != "call_abc123" {
		t.Fatalf("tool-input-available = %#v, want exactly one for call_abc123", available)
	}
	if name := available[0]["toolName"]; name != "delete_file" {
		t.Fatalf("tool-input-available toolName = %#v, want delete_file", name)
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

	out, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
		},
	})
	chunks := collectOutputChunks(t, out, errs)
	if len(gotMessages) != 1 || gotMessages[0]["type"] != "human" {
		t.Fatalf("stream messages = %#v", gotMessages)
	}
	if len(chunksOfType(chunks, "data-progress")) != 1 {
		t.Fatalf("missing data-progress chunk: %#v", chunks)
	}

	_, reconnectErrs := transport.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "chat-1"})
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
	out, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{})
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

// mirrors TS test "should preserve interleaved subgraph namespaces without
// splitting root reasoning" (adapter.test.ts)
func TestToUIMessageStreamPreservesInterleavedSubgraphNamespaces(t *testing.T) {
	rootMessageID := "root-message"
	subgraphMessageID := "subgraph-message"
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Mode: "messages", Namespace: []string{}, HasNamespace: true, Data: []interface{}{
		map[string]interface{}{"id": rootMessageID, "type": "ai", "content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "root-before"},
		}},
		map[string]interface{}{"langgraph_step": 5},
	}}
	events <- StreamEvent{Mode: "messages", Namespace: []string{"tools:subgraph-call"}, HasNamespace: true, Data: []interface{}{
		map[string]interface{}{"id": subgraphMessageID, "type": "ai", "content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "subgraph"},
		}},
		map[string]interface{}{"langgraph_step": 1},
	}}
	events <- StreamEvent{Mode: "messages", Namespace: []string{}, HasNamespace: true, Data: []interface{}{
		map[string]interface{}{"id": rootMessageID, "type": "ai", "content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "root-after"},
		}},
		map[string]interface{}{"langgraph_step": 5},
	}}
	close(events)

	chunks := collectChunks(t, events)

	rootReasoning := chunksForID(chunks, rootMessageID, "reasoning-start", "reasoning-delta", "reasoning-end")
	if len(rootReasoning) != 4 {
		t.Fatalf("root reasoning chunks = %#v, want start + 2 deltas + end", rootReasoning)
	}
	for _, chunk := range rootReasoning {
		if ns := namespaceFromChunk(t, chunk); len(ns) != 0 {
			t.Fatalf("root reasoning namespace = %#v, want []", ns)
		}
	}

	subgraphReasoning := chunksForID(chunks, subgraphMessageID, "reasoning-start", "reasoning-delta", "reasoning-end")
	if len(subgraphReasoning) != 3 {
		t.Fatalf("subgraph reasoning chunks = %#v, want start/delta/end", subgraphReasoning)
	}
	for _, chunk := range subgraphReasoning {
		ns := namespaceFromChunk(t, chunk)
		if len(ns) != 1 || ns[0] != "tools:subgraph-call" {
			t.Fatalf("subgraph reasoning namespace = %#v, want [tools:subgraph-call]", ns)
		}
	}

	// The root reasoning delta for "root-after" must arrive without a second
	// reasoning-start: interleaving the subgraph's own step-1 message must not
	// close or restart the root's still-open reasoning part.
	rootDeltas := chunksForID(chunks, rootMessageID, "reasoning-delta")
	if len(rootDeltas) != 2 || rootDeltas[0]["delta"] != "root-before" || rootDeltas[1]["delta"] != "root-after" {
		t.Fatalf("root reasoning deltas = %#v", rootDeltas)
	}
}

// mirrors TS test "should preserve repeated root tool lifecycles while child
// reasoning remains active" (adapter.test.ts), simplified to the root
// namespace only to exercise the core per-step tool call ID reuse guarantee.
func TestToUIMessageStreamPreservesReusedToolCallIDAcrossSteps(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"id": "root-message-1", "type": "ai", "content": "",
			"tool_call_chunks": []interface{}{
				map[string]interface{}{"id": "call-reused", "name": "write_column", "args": `{"column":"first"}`, "index": 0},
			},
		},
		map[string]interface{}{"langgraph_step": 1},
	}}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"id": "root-message-2", "type": "ai", "content": "",
			"tool_call_chunks": []interface{}{
				map[string]interface{}{"id": "call-reused", "name": "deploy_creatives", "args": `{"column":"second"}`, "index": 0},
			},
		},
		map[string]interface{}{"langgraph_step": 2},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	if len(starts) != 2 {
		t.Fatalf("tool-input-start = %#v, want 2 (one per step)", starts)
	}
	if starts[0]["toolCallId"] != "call-reused" || starts[0]["toolName"] != "write_column" {
		t.Fatalf("first tool-input-start = %#v", starts[0])
	}
	if starts[1]["toolCallId"] != "call-reused" || starts[1]["toolName"] != "deploy_creatives" {
		t.Fatalf("second tool-input-start = %#v", starts[1])
	}
}

// TestToUIMessageStreamSynthesizesStartForBareToolMessage ports TS "should
// preserve bare tool lifecycles when tool call ids repeat across steps"
// (adapter.test.ts, TS #21552 "preserve ordered bare LangChain tool
// lifecycles when tool-call IDs are reused"): a bare messages-mode
// ToolMessage with no preceding tool-call lifecycle ever observed for its
// id must still get a synthesized tool-input-start before its output.
func TestToUIMessageStreamSynthesizesStartForBareToolMessage(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"id": "tool-message-1", "type": "tool", "tool_call_id": "call-1",
			"content": "Sunny, 72°F", "name": "get_weather",
		},
		map[string]interface{}{"langgraph_step": 1},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	outputs := chunksOfType(chunks, "tool-output-available")
	if len(starts) != 1 || starts[0]["toolCallId"] != "call-1" || starts[0]["toolName"] != "get_weather" {
		t.Fatalf("tool-input-start = %#v, want one synthesized start for call-1/get_weather", starts)
	}
	if len(outputs) != 1 || outputs[0]["toolCallId"] != "call-1" || outputs[0]["output"] != "Sunny, 72°F" {
		t.Fatalf("tool-output-available = %#v", outputs)
	}
	startIdx, outputIdx := -1, -1
	for i, c := range chunks {
		if c["type"] == "tool-input-start" {
			startIdx = i
		}
		if c["type"] == "tool-output-available" {
			outputIdx = i
		}
	}
	if startIdx == -1 || outputIdx == -1 || startIdx >= outputIdx {
		t.Fatalf("tool-input-start (%d) must precede tool-output-available (%d); chunks=%#v", startIdx, outputIdx, chunks)
	}
}

// TestToUIMessageStreamSynthesizesStartForBareToolMessageAcrossSteps ports
// TS "should preserve bare tool lifecycles when tool call ids repeat
// across steps" for the bare-ToolMessage (not tool_call_chunks) path: the
// same provider-scoped id reused by two unrelated bare ToolMessages in two
// different steps must get two independent synthesized lifecycles.
func TestToUIMessageStreamSynthesizesStartForBareToolMessageAcrossSteps(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "tool-message-1", "type": "tool", "tool_call_id": "call-reused", "content": "first result", "name": "first_tool"},
		map[string]interface{}{"langgraph_step": 1},
	}}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "tool-message-2", "type": "tool", "tool_call_id": "call-reused", "content": "second result", "name": "second_tool"},
		map[string]interface{}{"langgraph_step": 2},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	outputs := chunksOfType(chunks, "tool-output-available")
	if len(starts) != 2 {
		t.Fatalf("tool-input-start = %#v, want 2 (one per step)", starts)
	}
	if starts[0]["toolCallId"] != "call-reused" || starts[0]["toolName"] != "first_tool" {
		t.Fatalf("first start = %#v", starts[0])
	}
	if starts[1]["toolCallId"] != "call-reused" || starts[1]["toolName"] != "second_tool" {
		t.Fatalf("second start = %#v", starts[1])
	}
	if len(outputs) != 2 || outputs[0]["output"] != "first result" || outputs[1]["output"] != "second result" {
		t.Fatalf("outputs = %#v", outputs)
	}
}

// TestToUIMessageStreamKeepsUnfinishedToolCallSeparateAcrossNamespaces ports
// TS "should keep an unfinished tool call separate from a bare same-id
// lifecycle in another namespace" (adapter.test.ts, TS #21552): a tool call
// started (via tool_call_chunks) but never completed in one namespace must
// not block a bare ToolMessage reusing the same provider-scoped id in a
// DIFFERENT namespace from getting its own synthesized lifecycle. This is
// the regression a namespace-unaware "has this id ever been started"
// check (rather than a namespace-scoped unfinished-call check) would fail.
func TestToUIMessageStreamKeepsUnfinishedToolCallSeparateAcrossNamespaces(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Namespace: []string{"tools:first"}, HasNamespace: true, Data: []interface{}{
		map[string]interface{}{
			"id": "message-1", "type": "ai", "content": "",
			"tool_call_chunks": []interface{}{
				map[string]interface{}{"id": "call-reused", "name": "first_tool", "args": `{"query":"first"}`, "index": 0},
			},
		},
		map[string]interface{}{"langgraph_step": 1},
	}}
	events <- StreamEvent{Mode: "messages", Namespace: []string{"tools:second"}, HasNamespace: true, Data: []interface{}{
		map[string]interface{}{"id": "tool-message-2", "type": "tool", "tool_call_id": "call-reused", "content": "second result", "name": "second_tool"},
		map[string]interface{}{"langgraph_step": 1},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	outputs := chunksOfType(chunks, "tool-output-available")
	if len(starts) != 2 {
		t.Fatalf("tool-input-start = %#v, want 2 (the first namespace's never-finished call, plus the second namespace's own)", starts)
	}
	if starts[0]["toolCallId"] != "call-reused" || starts[0]["toolName"] != "first_tool" {
		t.Fatalf("first namespace start = %#v", starts[0])
	}
	if starts[1]["toolCallId"] != "call-reused" || starts[1]["toolName"] != "second_tool" {
		t.Fatalf("second namespace start = %#v", starts[1])
	}
	if len(outputs) != 1 || outputs[0]["toolCallId"] != "call-reused" || outputs[0]["output"] != "second result" {
		t.Fatalf("outputs = %#v, want exactly the second namespace's output", outputs)
	}
}

// TestToUIMessageStreamAttachesDelayedBareToolMessageToUnfinishedLifecycle
// ports TS "should attach a delayed bare tool message to an unfinished
// lifecycle in the same namespace" (utils.test.ts, TS #21552): a tool call
// started via tool_call_chunks in one step, still unfinished when the step
// advances, must have its later bare ToolMessage output recognized as a
// delayed completion of that SAME lifecycle — no second (duplicate)
// synthesized start.
func TestToUIMessageStreamAttachesDelayedBareToolMessageToUnfinishedLifecycle(t *testing.T) {
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{
			"id": "msg-1", "type": "ai", "content": "",
			"tool_call_chunks": []interface{}{
				map[string]interface{}{"id": "call-1", "name": "get_weather", "args": `{"city":"SF"}`, "index": 0},
			},
		},
		map[string]interface{}{"langgraph_step": 1},
	}}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-2", "type": "ai", "content": "Waiting"},
		map[string]interface{}{"langgraph_step": 2},
	}}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "tool-message-1", "type": "tool", "tool_call_id": "call-1", "content": "Sunny", "name": "get_weather"},
		map[string]interface{}{"langgraph_step": 2},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	outputs := chunksOfType(chunks, "tool-output-available")
	if len(starts) != 1 || starts[0]["toolCallId"] != "call-1" || starts[0]["toolName"] != "get_weather" {
		t.Fatalf("tool-input-start = %#v, want exactly one (step 1's, not a duplicate)", starts)
	}
	if len(outputs) != 1 || outputs[0]["toolCallId"] != "call-1" || outputs[0]["output"] != "Sunny" {
		t.Fatalf("tool-output-available = %#v", outputs)
	}
}

// mirrors TS test "should emit completed tool lifecycles from values-only
// streams" (adapter.test.ts)
func TestToUIMessageStreamEmitsCompletedToolLifecycleFromValuesOnlyStream(t *testing.T) {
	toolCallID := "call-completed"
	baseMessages := []interface{}{
		map[string]interface{}{"id": "human-message", "type": "human", "content": "list products"},
		map[string]interface{}{
			"id": "ai-message", "type": "ai", "content": "",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": toolCallID, "name": "searchProducts", "args": map[string]interface{}{"query": "list"}},
			},
		},
	}
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": baseMessages}}
	events <- StreamEvent{Mode: "values", Data: map[string]interface{}{
		"messages": append(append([]interface{}{}, baseMessages...), map[string]interface{}{
			"id": "tool-message", "type": "tool", "tool_call_id": toolCallID, "content": `{"products":["p1","p2","p3"]}`,
		}),
	}}
	close(events)

	chunks := collectChunks(t, events)
	wantTypes := []string{"start", "tool-input-start", "tool-input-available", "tool-output-available", "finish"}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunks = %#v, want types %v", chunks, wantTypes)
	}
	for i, typ := range wantTypes {
		if chunks[i]["type"] != typ {
			t.Fatalf("chunk[%d] type = %v, want %v (chunks=%#v)", i, chunks[i]["type"], typ, chunks)
		}
	}
	if chunks[1]["toolCallId"] != toolCallID || chunks[1]["toolName"] != "searchProducts" {
		t.Fatalf("tool-input-start = %#v", chunks[1])
	}
	if chunks[3]["toolCallId"] != toolCallID || chunks[3]["output"] != `{"products":["p1","p2","p3"]}` {
		t.Fatalf("tool-output-available = %#v", chunks[3])
	}
}

// mirrors TS tests "should handle tools stream on_tool_start with input
// chunks", "...preliminary on_tool_event", and "...final on_tool_end without
// preliminary chunks" (utils.test.ts)
func TestToUIMessageStreamToolsStreamModeLifecycle(t *testing.T) {
	events := make(chan StreamEvent, 3)
	events <- StreamEvent{Mode: "tools", Data: map[string]interface{}{
		"event": "on_tool_start", "toolCallId": "call-weather", "name": "get_weather", "input": map[string]interface{}{"city": "SF"},
	}}
	events <- StreamEvent{Mode: "tools", Data: map[string]interface{}{
		"event": "on_tool_event", "toolCallId": "call-weather", "name": "get_weather", "data": map[string]interface{}{"status": "loading"},
	}}
	events <- StreamEvent{Mode: "tools", Data: map[string]interface{}{
		"event": "on_tool_end", "toolCallId": "call-weather", "name": "get_weather", "output": map[string]interface{}{"temperature": 72},
	}}
	close(events)

	chunks := collectChunks(t, events)
	starts := chunksOfType(chunks, "tool-input-start")
	availables := chunksOfType(chunks, "tool-input-available")
	outputs := chunksOfType(chunks, "tool-output-available")
	if len(starts) != 1 || len(availables) != 1 {
		t.Fatalf("expected exactly one input-start/available, got starts=%#v availables=%#v", starts, availables)
	}
	if len(outputs) != 2 {
		t.Fatalf("expected preliminary + final output, got %#v", outputs)
	}
	if outputs[0]["preliminary"] != true {
		t.Fatalf("first output = %#v, want preliminary", outputs[0])
	}
	if _, ok := outputs[1]["preliminary"]; ok {
		t.Fatalf("final output = %#v, want no preliminary key", outputs[1])
	}
}

// mirrors TS test "should handle tools stream on_tool_error" (utils.test.ts)
func TestToUIMessageStreamToolsStreamModeError(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "tools", Data: map[string]interface{}{
		"event": "on_tool_error", "toolCallId": "call-weather", "name": "get_weather", "error": errors.New("Weather API failed"),
	}}
	close(events)

	chunks := collectChunks(t, events)
	errChunks := chunksOfType(chunks, "tool-output-error")
	if len(errChunks) != 1 || errChunks[0]["toolCallId"] != "call-weather" || errChunks[0]["errorText"] != "Weather API failed" {
		t.Fatalf("tool-output-error = %#v", errChunks)
	}
}

// mirrors TS test "should close reasoning when text starts" (utils.test.ts,
// processModelChunk), ported to the LangGraph "messages" stream mode
// (processMessagesEvent), which previously only closed reasoning in the
// direct-model-chunk code path.
func TestProcessMessagesEventClosesReasoningBeforeText(t *testing.T) {
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": []interface{}{
			map[string]interface{}{"type": "reasoning", "reasoning": "thinking..."},
		}},
	}}
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "answer"},
		}},
	}}
	close(events)

	chunks := collectChunks(t, events)
	var order []string
	for _, chunk := range chunks {
		switch chunk["type"] {
		case "reasoning-start", "reasoning-delta", "reasoning-end", "text-start", "text-delta", "text-end":
			order = append(order, chunk["type"].(string))
		}
	}
	want := []string{"reasoning-start", "reasoning-delta", "reasoning-end", "text-start", "text-delta", "text-end"}
	if len(order) != len(want) {
		t.Fatalf("order = %#v, want %#v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %#v, want %#v", order, want)
		}
	}
}

// mirrors TS test "emits the requested outer chunks with sendStart=$sendStart
// and sendFinish=$sendFinish" (adapter.test.ts)
func TestToUIMessageStreamCallbacksSendStartSendFinish(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	tests := []struct {
		name         string
		sendStart    *bool
		sendFinish   *bool
		expectStart  bool
		expectFinish bool
	}{
		{name: "defaults", sendStart: nil, sendFinish: nil, expectStart: true, expectFinish: true},
		{name: "suppress start", sendStart: boolPtr(false), sendFinish: nil, expectStart: false, expectFinish: true},
		{name: "suppress finish", sendStart: nil, sendFinish: boolPtr(false), expectStart: true, expectFinish: false},
		{name: "suppress both", sendStart: boolPtr(false), sendFinish: boolPtr(false), expectStart: false, expectFinish: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan StreamEvent, 1)
			events <- StreamEvent{Mode: "values", Data: map[string]interface{}{}}
			close(events)

			out, errs := ToUIMessageStreamWithCallbacks(context.Background(), events, &StreamCallbacks{SendStart: tt.sendStart, SendFinish: tt.sendFinish})
			chunks := collectOutputChunks(t, out, errs)

			hasStart := len(chunksOfType(chunks, "start")) == 1
			hasFinish := len(chunksOfType(chunks, "finish")) == 1
			if hasStart != tt.expectStart {
				t.Fatalf("start present = %v, want %v; chunks=%#v", hasStart, tt.expectStart, chunks)
			}
			if hasFinish != tt.expectFinish {
				t.Fatalf("finish present = %v, want %v; chunks=%#v", hasFinish, tt.expectFinish, chunks)
			}
		})
	}
}

// Regression guard for the TypeScript adapter's lazy text buffering (only
// buffer completion text for OnFinal when OnFinal is actually configured):
// OnToken/OnText must keep working even when OnFinal is not set.
func TestToUIMessageStreamOnTokenWorksWithoutOnFinal(t *testing.T) {
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Mode: "messages", Data: []interface{}{
		map[string]interface{}{"id": "msg-1", "type": "ai", "content": "Hello"},
	}}
	close(events)

	var tokens []string
	out, errs := ToUIMessageStreamWithCallbacks(context.Background(), events, &StreamCallbacks{
		OnToken: func(token string) error {
			tokens = append(tokens, token)
			return nil
		},
	})
	_ = collectOutputChunks(t, out, errs)
	if len(tokens) != 1 || tokens[0] != "Hello" {
		t.Fatalf("tokens = %#v, want [Hello] even without OnFinal configured", tokens)
	}
}

// mirrors TS tests "should convert user messages with image content (URL)",
// "(base64)", "(data URL)", and "should convert image files to canonical
// image blocks" (adapter.test.ts)
func TestConvertUserContentCanonicalImageBlocks(t *testing.T) {
	tests := []struct {
		name    string
		content types.ContentPart
		want    map[string]interface{}
	}{
		{
			name:    "http URL",
			content: types.ImageContent{URL: "https://example.com/image.jpg"},
			want:    map[string]interface{}{"type": "image", "url": "https://example.com/image.jpg"},
		},
		{
			name:    "base64 bytes",
			content: types.ImageContent{Image: []byte("hello"), MimeType: "image/png"},
			want:    map[string]interface{}{"type": "image", "data": base64.StdEncoding.EncodeToString([]byte("hello")), "mimeType": "image/png"},
		},
		{
			name:    "data URL",
			content: types.ImageContent{URL: "data:image/png;base64,abc123"},
			want:    map[string]interface{}{"type": "image", "data": "abc123", "mimeType": "image/png"},
		},
		{
			name:    "image file with URL and image/jpeg mediaType",
			content: types.FileContent{URL: "https://example.com/photo.jpg", MediaType: "image/jpeg"},
			want:    map[string]interface{}{"type": "image", "url": "https://example.com/photo.jpg"},
		},
		{
			name:    "image file with base64 data",
			content: types.FileContent{Data: []byte("png-bytes"), MediaType: "image/png"},
			want:    map[string]interface{}{"type": "image", "data": base64.StdEncoding.EncodeToString([]byte("png-bytes")), "mimeType": "image/png"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := ConvertModelMessages([]types.Message{{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "x"}, tt.content},
			}})
			blocks, ok := messages[0]["content"].([]interface{})
			if !ok || len(blocks) != 2 {
				t.Fatalf("content = %#v", messages[0]["content"])
			}
			got, ok := blocks[1].(map[string]interface{})
			if !ok {
				t.Fatalf("block = %#v", blocks[1])
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("block[%q] = %#v, want %#v (block=%#v)", k, got[k], v, got)
				}
			}
			if _, hasImageURL := got["image_url"]; hasImageURL {
				t.Fatalf("block still uses legacy image_url shape: %#v", got)
			}
		})
	}
}

func chunksForID(chunks []ai.UIMessageChunk, id string, types ...string) []ai.UIMessageChunk {
	wanted := map[string]bool{}
	for _, typ := range types {
		wanted[typ] = true
	}
	var out []ai.UIMessageChunk
	for _, chunk := range chunks {
		typ, _ := chunk["type"].(string)
		if !wanted[typ] {
			continue
		}
		if chunk["id"] != id {
			continue
		}
		out = append(out, chunk)
	}
	return out
}

func namespaceFromChunk(t *testing.T, chunk ai.UIMessageChunk) []string {
	t.Helper()
	pm, ok := chunk["providerMetadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("chunk missing providerMetadata: %#v", chunk)
	}
	lc, ok := pm["langchain"].(map[string]interface{})
	if !ok {
		t.Fatalf("chunk missing providerMetadata.langchain: %#v", chunk)
	}
	ns, ok := lc["namespace"].([]string)
	if !ok {
		t.Fatalf("namespace not []string: %#v", lc["namespace"])
	}
	return ns
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
