package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestReadUIMessageStream_RoundTrip(t *testing.T) {
	input := []byte("data: {\"type\":\"text\",\"value\":\"hi\"}\n\ndata: {\"type\":\"finish\"}\n\ndata: [DONE]\n\n")
	chunks, err := ReadUIMessageStream(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("ReadUIMessageStream() error = %v", err)
	}
	if len(chunks) != 2 || chunks[0]["type"] != "text" || chunks[1]["type"] != "finish" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestCreateUIMessageStream_OnStepEndTakesPrecedenceOverDeprecatedOnStepFinish(t *testing.T) {
	var stepEndCalls int
	var stepFinishCalls int
	stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{"type": "finish-step"})
		},
		OnStepEnd: func(event map[string]interface{}) {
			stepEndCalls++
		},
		OnStepFinish: func(event map[string]interface{}) {
			stepFinishCalls++
		},
	})
	for range stream {
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if stepEndCalls != 1 || stepFinishCalls != 0 {
		t.Fatalf("callbacks: OnStepEnd=%d OnStepFinish=%d", stepEndCalls, stepFinishCalls)
	}
}

func TestPipeUIMessageStreamToResponse_AppendsDoneSentinel(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	buf := &bytes.Buffer{}

	if err := PipeUIMessageStreamToResponse(context.Background(), res, buf); err != nil {
		t.Fatalf("PipeUIMessageStreamToResponse() error = %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("data: [DONE]\n\n")) {
		t.Fatalf("missing DONE sentinel in %q", buf.String())
	}
}

func TestToUIMessageChunk_StandaloneParity(t *testing.T) {
	meta := json.RawMessage(`{"testProvider":{"signature":"sig-1"}}`)
	reasoning, ok := ToUIMessageChunk(provider.StreamChunk{
		Type:             provider.ChunkTypeReasoning,
		ID:               "reasoning-1",
		Reasoning:        "thinking",
		ProviderMetadata: meta,
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("reasoning chunk was suppressed")
	}
	if reasoning["type"] != "reasoning-delta" || reasoning["delta"] != "thinking" {
		t.Fatalf("reasoning chunk = %#v", reasoning)
	}
	if _, ok := reasoning["providerMetadata"].(map[string]interface{}); !ok {
		t.Fatalf("provider metadata missing: %#v", reasoning)
	}

	sendReasoning := false
	if chunk, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeReasoning,
		ID:   "reasoning-1",
		Text: "hidden",
	}, UIMessageStreamResultOptions{SendReasoning: &sendReasoning}); ok || chunk != nil {
		t.Fatalf("reasoning disabled chunk = %#v, %v", chunk, ok)
	}

	sendSources := true
	source, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeSource,
		SourceContent: &types.SourceContent{
			SourceType: "url",
			ID:         "source-1",
			URL:        "https://example.com",
			Title:      "Example",
		},
	}, UIMessageStreamResultOptions{SendSources: &sendSources})
	if !ok || source["type"] != "source-url" || source["sourceId"] != "source-1" {
		t.Fatalf("source chunk = %#v, %v", source, ok)
	}

	file, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeFile,
		GeneratedFileContent: &types.GeneratedFileContent{
			MediaType: "text/plain",
			Data:      []byte("Hello"),
		},
	}, UIMessageStreamResultOptions{})
	if !ok || file["type"] != "file" || file["url"] != "data:text/plain;base64,SGVsbG8=" {
		t.Fatalf("file chunk = %#v, %v", file, ok)
	}

	urlFile, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeFile,
		GeneratedFileContent: &types.GeneratedFileContent{
			MediaType: "text/plain",
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/file.txt"},
		},
	}, UIMessageStreamResultOptions{})
	if !ok || urlFile["url"] != "data:text/plain;base64,https://example.com/file.txt" {
		t.Fatalf("url file chunk = %#v, %v", urlFile, ok)
	}

	emptyToolDelta, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolInputDelta,
		ID:   "call-empty",
		Text: "",
	}, UIMessageStreamResultOptions{})
	if !ok || emptyToolDelta["type"] != "tool-input-delta" || emptyToolDelta["inputTextDelta"] != "" {
		t.Fatalf("empty tool delta chunk = %#v, %v", emptyToolDelta, ok)
	}

	tool, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolCall,
		ToolCall: &types.ToolCall{
			ID:               "call-1",
			ToolName:         "lookup",
			Arguments:        map[string]interface{}{"q": "x"},
			ProviderExecuted: true,
			Dynamic:          true,
			ToolMetadata:     map[string]interface{}{"clientName": "test-client"},
		},
	}, UIMessageStreamResultOptions{})
	if !ok || tool["type"] != "tool-input-available" || tool["providerExecuted"] != true || tool["dynamic"] != true {
		t.Fatalf("tool chunk = %#v, %v", tool, ok)
	}

	dynamicFromTools, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolCall,
		ToolCall: &types.ToolCall{
			ID:        "call-dynamic",
			ToolName:  "runtime",
			Arguments: map[string]interface{}{"q": "x"},
		},
	}, UIMessageStreamResultOptions{
		Tools: []types.Tool{{Name: "runtime", Type: types.ToolTypeDynamic}},
	})
	if !ok || dynamicFromTools["dynamic"] != true {
		t.Fatalf("dynamic tool chunk = %#v, %v", dynamicFromTools, ok)
	}

	providerExecutedError, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID:       "call-provider-error",
			ToolName:         "providerTool",
			Error:            errors.New("provider failed"),
			ProviderExecuted: true,
		},
	}, UIMessageStreamResultOptions{
		OnError: func(error) string { return "should not be used" },
	})
	if !ok || providerExecutedError["errorText"] != "provider failed" {
		t.Fatalf("provider executed error chunk = %#v, %v", providerExecutedError, ok)
	}

	abort, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user"}, UIMessageStreamResultOptions{})
	if !ok || abort["type"] != "abort" || abort["reason"] != "user" {
		t.Fatalf("abort chunk = %#v, %v", abort, ok)
	}
}

func TestToUIMessageStream_Standalone(t *testing.T) {
	sendSources := true
	sendStart := true
	sendFinish := true
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "s1", URL: "https://example.com"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		ResponseMessageID: "msg-1",
		SendSources:       &sendSources,
		SendStart:         &sendStart,
		SendFinish:        &sendFinish,
	})
	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	var hasStart, hasText, hasSource, hasFinish bool
	for _, chunk := range got {
		switch chunk["type"] {
		case "start":
			hasStart = chunk["messageId"] == "msg-1"
		case "text-delta":
			hasText = chunk["delta"] == "hello"
		case "source-url":
			hasSource = true
		case "finish":
			hasFinish = chunk["finishReason"] == string(types.FinishReasonStop)
		}
	}
	if !hasStart || !hasText || !hasSource || !hasFinish {
		t.Fatalf("chunks = %#v", got)
	}
}

func TestToUIMessageStream_MessageIDParity(t *testing.T) {
	t.Run("no message ID by default", func(t *testing.T) {
		stream := testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		chunks, errs := ToUIMessageStream(context.Background(), stream)
		var start UIMessageChunk
		for chunk := range chunks {
			if chunk["type"] == "start" {
				start = chunk
			}
		}
		if err, ok := <-errs; ok && err != nil {
			t.Fatalf("err = %v", err)
		}
		if start == nil {
			t.Fatal("missing start chunk")
		}
		if _, ok := start["messageId"]; ok {
			t.Fatalf("default start chunk should not include messageId: %#v", start)
		}
	})

	t.Run("generate message ID without original messages is callback-only", func(t *testing.T) {
		stream := testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		finishEvent := make(chan map[string]interface{}, 1)
		chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
			GenerateMessageID: func() string { return "msg-generated" },
			OnFinish: func(event map[string]interface{}) {
				finishEvent <- event
			},
		})
		var start UIMessageChunk
		for chunk := range chunks {
			if chunk["type"] == "start" {
				start = chunk
			}
		}
		if err, ok := <-errs; ok && err != nil {
			t.Fatalf("err = %v", err)
		}
		if _, ok := start["messageId"]; ok {
			t.Fatalf("start chunk should omit generated messageId without original messages: %#v", start)
		}
		finish := <-finishEvent
		responseMessage, ok := finish["responseMessage"].(UIMessageChunk)
		if !ok || responseMessage["id"] != "msg-generated" {
			t.Fatalf("finish event = %#v", finish)
		}
		if _, hasContent := responseMessage["content"]; hasContent {
			t.Fatalf("responseMessage should use TS UIMessage parts, not content: %#v", responseMessage)
		}
		if parts, ok := responseMessage["parts"].([]interface{}); !ok || len(parts) != 1 {
			t.Fatalf("responseMessage parts = %#v", responseMessage["parts"])
		}
	})

	t.Run("generate message ID with original messages", func(t *testing.T) {
		stream := testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: func() string { return "msg-generated" },
		})
		var start UIMessageChunk
		for chunk := range chunks {
			if chunk["type"] == "start" {
				start = chunk
			}
		}
		if err, ok := <-errs; ok && err != nil {
			t.Fatalf("err = %v", err)
		}
		if start["messageId"] != "msg-generated" {
			t.Fatalf("start chunk = %#v", start)
		}
	})

	t.Run("reuse last assistant message ID", func(t *testing.T) {
		stream := testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "text-1", Text: "continued"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
			OriginalMessages: []UIMessageChunk{
				{"id": "msg-existing", "role": "assistant"},
			},
			GenerateMessageID: func() string { return "msg-new" },
		})
		var start UIMessageChunk
		for chunk := range chunks {
			if chunk["type"] == "start" {
				start = chunk
			}
		}
		if err, ok := <-errs; ok && err != nil {
			t.Fatalf("err = %v", err)
		}
		if start["messageId"] != "msg-existing" {
			t.Fatalf("start chunk = %#v", start)
		}
	})
}

func TestToUIMessageStream_SynthesizesContentBoundariesForGoProviderChunks(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeReasoning, Reasoning: "thinking"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	chunks, errs := ToUIMessageStream(context.Background(), stream)
	var got []string
	for chunk := range chunks {
		if typ, _ := chunk["type"].(string); typ != "" {
			got = append(got, typ)
		}
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	wantOrder := []string{"start", "text-start", "text-delta", "reasoning-start", "reasoning-delta", "text-end", "reasoning-end", "finish-step", "finish"}
	if len(got) != len(wantOrder) {
		t.Fatalf("chunks = %#v, want %#v", got, wantOrder)
	}
	for i, want := range wantOrder {
		if got[i] != want {
			t.Fatalf("chunks = %#v, want %#v", got, wantOrder)
		}
	}
}

func TestToUIMessageStream_SuppressesSyntheticReasoningWhenDisabled(t *testing.T) {
	sendReasoning := false
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-1"},
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "hidden"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		SendReasoning: &sendReasoning,
	})
	for chunk := range chunks {
		if typ, _ := chunk["type"].(string); strings.HasPrefix(typ, "reasoning") {
			t.Fatalf("reasoning chunk leaked with sendReasoning=false: %#v", chunk)
		}
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateUIMessageStream_FromStreamTextResult(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	chunks, errs := CreateUIMessageStream(context.Background(), res)

	var got []UIMessageChunk
	for ch := range chunks {
		got = append(got, ch)
	}
	if len(got) == 0 {
		t.Fatalf("expected chunks, got none")
	}
	hasStart := false
	hasText := false
	for _, c := range got {
		switch c["type"] {
		case "start":
			hasStart = true
		case "text-delta":
			hasText = true
		}
	}
	if !hasStart || !hasText {
		t.Fatalf("unexpected chunks: %#v", got)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
}

func TestCreateUIMessageStreamResponse_Headers(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateUIMessageStreamResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponse() error = %v", err)
	}
	if got := httpRes.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q", got)
	}
}

func TestCreateUIMessageStreamResponse_WithInit(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, &UIMessageStreamResponseInit{
		Status:     http.StatusCreated,
		StatusText: "Created",
		Headers:    map[string]string{"X-Test": "go"},
	})
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponseWithInit() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := httpRes.Status; got != "Created" {
		t.Fatalf("status text = %q, want %q", got, "Created")
	}
	if got := httpRes.Header.Get("X-Test"); got != "go" {
		t.Fatalf("header X-Test = %q, want go", got)
	}
}

func TestStreamTextResult_ToUIMessageStreamResponse(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := res.ToUIMessageStreamResponse(context.Background(), &UIMessageStreamResponseInit{
		Headers: map[string]string{"X-Method": "stream"},
	})
	if err != nil {
		t.Fatalf("ToUIMessageStreamResponse() error = %v", err)
	}
	if got := httpRes.Header.Get("X-Method"); got != "stream" {
		t.Fatalf("header X-Method = %q, want stream", got)
	}
}

func TestStreamTextResult_ToUIMessageStream(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	chunks, errs := res.ToUIMessageStream(context.Background())

	var got []UIMessageChunk
	for ch := range chunks {
		got = append(got, ch)
	}
	if len(got) == 0 {
		t.Fatalf("expected chunks, got none")
	}
	hasStart := false
	for _, c := range got {
		if c["type"] == "start" {
			hasStart = true
		}
	}
	if !hasStart {
		t.Fatalf("missing start chunk: %#v", got)
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamTextResult_ToUIMessageStream_OptionsAndCallbacks(t *testing.T) {
	sendReasoning := false
	sendSources := false
	sendFinish := false
	sendStart := true
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "one"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "s1", URL: "https://example.com"}},
	})
	res := &StreamTextResult{stream: stream}
	stepCalled := make(chan map[string]interface{}, 1)
	finishCalled := make(chan map[string]interface{}, 1)

	var metadata []map[string]interface{}
	chunks, errs := res.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
		SendReasoning: &sendReasoning,
		SendSources:   &sendSources,
		SendStart:     &sendStart,
		SendFinish:    &sendFinish,
		OriginalMessages: []UIMessageChunk{
			{"id": "msg-1", "role": "user", "content": []interface{}{}},
		},
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			metadata = append(metadata, part)
			return map[string]interface{}{"source": "test"}
		},
		OnStepFinish: func(event map[string]interface{}) {
			stepCalled <- event
		},
		OnFinish: func(event map[string]interface{}) {
			finishCalled <- event
		},
	})

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	var hasFinishStep bool
	hasStart := false
	hasMetadata := false
	hasSource := false
	for _, c := range got {
		switch c["type"] {
		case "start":
			hasStart = true
		case "finish-step":
			hasFinishStep = true
		case "message-metadata":
			hasMetadata = true
		case "source-url":
			hasSource = true
		}
	}
	if !hasStart {
		t.Fatalf("expected start chunk: %#v", got)
	}
	if hasSource {
		t.Fatalf("did not expect source-url chunk when sendSources=false: %#v", got)
	}
	if !hasMetadata || len(metadata) == 0 {
		t.Fatalf("expected message-metadata: %#v", got)
	}
	stepMessageReceived := <-stepCalled
	finishMessageReceived := <-finishCalled

	if len(stepCalled) != 0 || len(finishCalled) != 0 {
		t.Fatalf("callbacks should fire exactly once")
	}
	if responseMessage, ok := stepMessageReceived["responseMessage"].(UIMessageChunk); !ok {
		t.Fatalf("step callback responseMessage missing: %#v", stepMessageReceived["responseMessage"])
	} else if _, hasParts := responseMessage["parts"].([]interface{}); !hasParts {
		t.Fatalf("step callback responseMessage should contain parts: %#v", responseMessage)
	}
	if responseMessage, ok := finishMessageReceived["responseMessage"].(UIMessageChunk); !ok {
		t.Fatalf("finish callback responseMessage missing: %#v", finishMessageReceived["responseMessage"])
	} else if _, hasParts := responseMessage["parts"].([]interface{}); !hasParts {
		t.Fatalf("finish callback responseMessage should contain parts: %#v", responseMessage)
	}
	if isContinuation, ok := stepMessageReceived["isContinuation"].(bool); !ok || isContinuation {
		t.Fatalf("step callback should report continuation false for new message: %#v", stepMessageReceived["isContinuation"])
	}
	if isContinuation, ok := finishMessageReceived["isContinuation"].(bool); !ok || isContinuation {
		t.Fatalf("finish callback should report continuation false for new message: %#v", finishMessageReceived["isContinuation"])
	}
	if gotReason, ok := finishMessageReceived["finishReason"].(types.FinishReason); !ok || gotReason != types.FinishReasonStop {
		t.Fatalf("finish callback finishReason = %#v", finishMessageReceived["finishReason"])
	}
	if gotMessages, ok := stepMessageReceived["messages"].([]UIMessageChunk); !ok || len(gotMessages) != 2 {
		t.Fatalf("step callback messages shape = %#v", stepMessageReceived["messages"])
	}
	if gotMessages, ok := finishMessageReceived["messages"].([]UIMessageChunk); !ok || len(gotMessages) != 2 {
		t.Fatalf("finish callback messages shape = %#v", finishMessageReceived["messages"])
	}
	if !hasMetadata || len(metadata) == 0 {
		t.Fatalf("expected message-metadata: %#v", got)
	}
	if hasSource {
		t.Fatalf("did not expect source-url chunk when sendSources=false: %#v", got)
	}
	if !hasStart {
		t.Fatalf("expected start chunk: %#v", got)
	}
	if !hasFinishStep {
		t.Fatalf("expected finish-step from finish chunk: %#v", got)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
}

func TestToUIMessageStream_OnFinishBuildsTSUIMessageParts(t *testing.T) {
	sendSources := true
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "text-1"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "text-1"},
		{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-1"},
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-1"},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "source-1", URL: "https://example.com", Title: "Example"}},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("file")}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "go"}}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "lookup", Result: map[string]interface{}{"ok": true}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	finishEvent := make(chan map[string]interface{}, 1)
	chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		OriginalMessages: []UIMessageChunk{},
		SendSources:      &sendSources,
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			if part["type"] == "finish" {
				return map[string]interface{}{"done": true}
			}
			return nil
		},
		OnFinish: func(event map[string]interface{}) {
			finishEvent <- event
		},
	})
	for range chunks {
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	event := <-finishEvent
	message, ok := event["responseMessage"].(UIMessageChunk)
	if !ok {
		t.Fatalf("responseMessage = %#v", event["responseMessage"])
	}
	if _, hasContent := message["content"]; hasContent {
		t.Fatalf("responseMessage should not contain legacy content field: %#v", message)
	}
	metadata, ok := message["metadata"].(UIMessageChunk)
	if !ok {
		if m, mapOK := message["metadata"].(map[string]interface{}); mapOK {
			metadata = UIMessageChunk(m)
			ok = true
		}
	}
	if !ok || metadata["done"] != true {
		t.Fatalf("metadata = %#v", message["metadata"])
	}
	parts, ok := message["parts"].([]interface{})
	if !ok {
		t.Fatalf("parts = %#v", message["parts"])
	}
	seen := map[string]bool{}
	for _, raw := range parts {
		part, ok := raw.(UIMessageChunk)
		if !ok {
			t.Fatalf("part = %#v", raw)
		}
		if typ, _ := part["type"].(string); typ != "" {
			seen[typ] = true
		}
	}
	for _, typ := range []string{"text", "reasoning", "source-url", "file", "tool-lookup"} {
		if !seen[typ] {
			t.Fatalf("missing %s part in %#v", typ, parts)
		}
	}
}

func TestCreateUIMessageStreamWithOptions_MetadataDeepMergeAndInvalidToolOutput(t *testing.T) {
	var errorsSeen []string
	finishEvent := make(chan map[string]interface{}, 1)

	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{
				"type": "start",
				"messageMetadata": UIMessageChunk{
					"nested": UIMessageChunk{"a": 1},
					"keep":   true,
				},
			})
			writer.Write(UIMessageChunk{
				"type": "message-metadata",
				"messageMetadata": map[string]interface{}{
					"nested": map[string]interface{}{"b": 2},
				},
			})
			writer.Write(UIMessageChunk{
				"type":       "tool-output-available",
				"toolCallId": "missing-call",
				"output":     "ignored",
			})
		},
		OnError: func(err error) string {
			errorsSeen = append(errorsSeen, err.Error())
			return err.Error()
		},
		OnFinish: func(event map[string]interface{}) {
			finishEvent <- event
		},
	})

	for range chunks {
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(errorsSeen) != 1 || errorsSeen[0] != `No tool invocation found for tool call ID "missing-call".` {
		t.Fatalf("errorsSeen = %#v", errorsSeen)
	}
	event := <-finishEvent
	message, ok := event["responseMessage"].(UIMessageChunk)
	if !ok {
		t.Fatalf("responseMessage = %#v", event["responseMessage"])
	}
	metadata, ok := message["metadata"].(map[string]interface{})
	if !ok {
		if typed, typedOK := message["metadata"].(UIMessageChunk); typedOK {
			metadata = map[string]interface{}(typed)
			ok = true
		}
	}
	if !ok {
		t.Fatalf("metadata = %#v", message["metadata"])
	}
	nested, ok := metadata["nested"].(map[string]interface{})
	if !ok {
		if typed, typedOK := metadata["nested"].(UIMessageChunk); typedOK {
			nested = map[string]interface{}(typed)
			ok = true
		}
	}
	if !ok || nested["a"] != 1 || nested["b"] != 2 || metadata["keep"] != true {
		t.Fatalf("merged metadata = %#v", metadata)
	}
}

func TestCreateUIMessageStreamWithOptions_DataPartsUpdateCallbackState(t *testing.T) {
	finishEvent := make(chan map[string]interface{}, 1)

	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{
				"type": "data-weather",
				"id":   "part-1",
				"data": map[string]interface{}{"city": "NYC"},
			})
			writer.Write(UIMessageChunk{
				"type": "data-weather",
				"id":   "part-1",
				"data": map[string]interface{}{"city": "Boston"},
			})
			writer.Write(UIMessageChunk{
				"type":      "data-weather",
				"id":        "transient",
				"data":      map[string]interface{}{"city": "Hidden"},
				"transient": true,
			})
		},
		OnFinish: func(event map[string]interface{}) {
			finishEvent <- event
		},
	})
	for range chunks {
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	event := <-finishEvent
	message, ok := event["responseMessage"].(UIMessageChunk)
	if !ok {
		t.Fatalf("responseMessage = %#v", event["responseMessage"])
	}
	parts, ok := message["parts"].([]interface{})
	if !ok || len(parts) != 1 {
		t.Fatalf("parts = %#v", message["parts"])
	}
	part, ok := parts[0].(UIMessageChunk)
	if !ok {
		t.Fatalf("part = %#v", parts[0])
	}
	data, ok := part["data"].(UIMessageChunk)
	if !ok {
		if mapped, mapOK := part["data"].(map[string]interface{}); mapOK {
			data = UIMessageChunk(mapped)
			ok = true
		}
	}
	if !ok || part["type"] != "data-weather" || part["id"] != "part-1" || data["city"] != "Boston" {
		t.Fatalf("data part = %#v", part)
	}
}

func TestToUIMessageStream_OnFinishReportsAbortChunk(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "before"},
		{Type: provider.ChunkTypeAbort, AbortReason: "manual abort"},
	})
	finishEvent := make(chan map[string]interface{}, 1)
	chunks, errs := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		OnFinish: func(event map[string]interface{}) {
			finishEvent <- event
		},
	})
	var sawAbort bool
	for chunk := range chunks {
		if chunk["type"] == "abort" && chunk["reason"] == "manual abort" {
			sawAbort = true
		}
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
	if !sawAbort {
		t.Fatal("missing abort chunk")
	}
	event := <-finishEvent
	if event["isAborted"] != true {
		t.Fatalf("isAborted = %#v, want true", event["isAborted"])
	}
}

func TestCreateUIMessageStreamWithOptions_AsyncErrorEmission(t *testing.T) {
	streamErr := errors.New("boom")
	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			panic(streamErr)
		},
		OnError: func(err error) string {
			return "ui-error-" + err.Error()
		},
	})
	var got []UIMessageChunk
	for c := range chunks {
		got = append(got, c)
	}
	if len(got) == 0 {
		t.Fatalf("expected error chunk")
	}
	hasError := false
	for _, c := range got {
		if c["type"] == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Fatalf("expected error chunk, got %#v", got)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("handled stream error should be emitted as chunk only, got err = %v", err)
		}
	default:
	}
}

func TestStreamTextResult_ToUIMessageStream_ToolInputAndOutputEventShapes(t *testing.T) {
	approvalReason := "policy-blocked"
	toolInputStartMeta, _ := json.Marshal(map[string]interface{}{"provider": "search"})
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{
			Type: provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{
				ID:               "call-1",
				ToolName:         "search",
				Title:            "Search tool",
				Arguments:        map[string]interface{}{"query": "go"},
				ProviderExecuted: true,
				ToolMetadata:     map[string]interface{}{"category": "docs"},
				Dynamic:          true,
			},
			ProviderMetadata: toolInputStartMeta,
		},
		{
			Type: provider.ChunkTypeToolInputDelta,
			ID:   "call-1",
			Text: `{"query":"g`,
		},
		{
			Type: provider.ChunkTypeToolInputEnd,
			ID:   "call-1",
		},
		{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               "call-1",
				ToolName:         "search",
				Title:            "Search tool",
				Arguments:        map[string]interface{}{"query": "go"},
				ProviderMetadata: map[string]interface{}{"provider": "search"},
				ToolMetadata:     map[string]interface{}{"category": "docs"},
				Dynamic:          true,
			},
		},
		{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID:       "call-1",
				Result:           map[string]interface{}{"text": "ok"},
				ProviderExecuted: false,
				ProviderMetadata: map[string]interface{}{"provider": "search"},
				ToolMetadata:     map[string]interface{}{"category": "docs"},
				Dynamic:          true,
				Preliminary:      true,
			},
		},
		{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID:     "call-denied",
				ApprovalStatus: types.ToolApprovalStatusDenied,
				ApprovalReason: &approvalReason,
			},
		},
		{
			Type: provider.ChunkTypeFinish,
			ID:   "ignored",
		},
	})
	res := &StreamTextResult{stream: stream}
	chunks, errs := res.ToUIMessageStream(context.Background())

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	var (
		toolInputStart      UIMessageChunk
		toolInputDelta      UIMessageChunk
		toolInputAvailable  UIMessageChunk
		toolOutputAvailable UIMessageChunk
		toolOutputDenied    UIMessageChunk
		finishStepCount     int
	)
	for _, chunk := range got {
		switch chunk["type"] {
		case "tool-input-start":
			toolInputStart = chunk
		case "tool-input-delta":
			toolInputDelta = chunk
		case "tool-input-available":
			toolInputAvailable = chunk
		case "tool-output-available":
			toolOutputAvailable = chunk
		case "tool-output-denied":
			toolOutputDenied = chunk
		case "finish-step":
			finishStepCount++
		}
	}
	if gotStep := toolInputStart["toolCallId"]; gotStep != "call-1" {
		t.Fatalf("tool-input-start.toolCallId = %v, want call-1", gotStep)
	}
	if gotStep := toolInputDelta["toolCallId"]; gotStep != "call-1" {
		t.Fatalf("tool-input-delta.toolCallId = %v, want call-1", gotStep)
	}
	if gotDelta := toolInputDelta["inputTextDelta"]; gotDelta != "{\"query\":\"g" {
		t.Fatalf("tool-input-delta.inputTextDelta = %v, want {\\\"query\\\":\\\"g", gotDelta)
	}
	if gotMeta, ok := toolInputStart["providerMetadata"].(map[string]interface{}); !ok || gotMeta["provider"] != "search" {
		t.Fatalf("tool-input-start.providerMetadata = %#v", gotMeta)
	}
	if gotMeta, ok := toolInputStart["toolMetadata"].(map[string]interface{}); !ok || gotMeta["category"] != "docs" {
		t.Fatalf("tool-input-start.toolMetadata = %#v", gotMeta)
	}
	if gotDynamic := toolInputStart["dynamic"]; gotDynamic != true {
		t.Fatalf("tool-input-start.dynamic = %v, want true", gotDynamic)
	}
	if gotTitle := toolInputStart["title"]; gotTitle != "Search tool" {
		t.Fatalf("tool-input-start.title = %v, want Search tool", gotTitle)
	}
	if gotInput := toolInputAvailable["input"]; gotInput == nil {
		t.Fatalf("tool-input-available.input = %#v", gotInput)
	}
	if gotMeta, ok := toolOutputAvailable["providerMetadata"].(map[string]interface{}); !ok || gotMeta["provider"] != "search" {
		t.Fatalf("tool-output-available.providerMetadata = %#v", gotMeta)
	}
	if gotMeta, ok := toolOutputAvailable["toolMetadata"].(map[string]interface{}); !ok || gotMeta["category"] != "docs" {
		t.Fatalf("tool-output-available.toolMetadata = %#v", gotMeta)
	}
	if gotOutput := toolOutputAvailable["output"]; gotOutput == nil {
		t.Fatalf("tool-output-available.output = %#v", gotOutput)
	}
	if gotPreliminary := toolOutputAvailable["preliminary"]; gotPreliminary != true {
		t.Fatalf("tool-output-available.preliminary = %v, want true", gotPreliminary)
	}
	if _, ok := toolOutputDenied["toolCallId"]; !ok {
		t.Fatalf("tool-output-denied.toolCallId missing: %#v", toolOutputDenied)
	}
	if finishStepCount != 1 {
		t.Fatalf("finish-step count = %d, want 1", finishStepCount)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
}

func TestStreamTextResult_ToUIMessageStream_ContinuationCallbackState(t *testing.T) {
	genID := "msg-continuation"
	sendStart := true
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	res := &StreamTextResult{stream: stream}
	finishEvent := make(chan map[string]interface{}, 1)
	stepEvent := make(chan map[string]interface{}, 1)

	chunks, errs := res.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
		SendStart: &sendStart,
		GenerateMessageID: func() string {
			return genID
		},
		OriginalMessages: []UIMessageChunk{
			{
				"id":    genID,
				"role":  "assistant",
				"parts": []interface{}{UIMessageChunk{"type": "text", "text": "old", "state": "done"}},
			},
		},
		OnStepFinish: func(event map[string]interface{}) {
			stepEvent <- event
		},
		OnFinish: func(event map[string]interface{}) {
			finishEvent <- event
		},
	})

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) == 0 {
		t.Fatalf("expected chunks, got none")
	}
	events := 0
	if msg := <-stepEvent; msg["isContinuation"] != true {
		t.Fatalf("step.isContinuation = %v, want true", msg["isContinuation"])
	} else {
		events++
	}
	if msg := <-finishEvent; msg["isContinuation"] != true {
		t.Fatalf("finish.isContinuation = %v, want true", msg["isContinuation"])
	} else {
		events++
	}
	if events != 2 {
		t.Fatalf("expected both callbacks to fire, got %d", events)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
}

func TestCreateUIMessageStreamWithOptions_WriteAndCallbacks(t *testing.T) {
	ch1 := make(chan UIMessageChunk)
	go func() {
		ch1 <- UIMessageChunk{"type": "merged"}
		close(ch1)
	}()

	stepDone := make(chan struct{}, 1)
	finishDone := make(chan struct{}, 1)

	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{"type": "start"})
			writer.Merge(ch1)
			writer.Write(UIMessageChunk{"type": "finish-step"})
		},
		OnError: func(err error) string {
			return err.Error()
		},
		OnStepFinish: func(_ map[string]interface{}) {
			stepDone <- struct{}{}
		},
		OnFinish: func(_ map[string]interface{}) {
			finishDone <- struct{}{}
		},
	})

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(got))
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
	}
	<-stepDone
	<-finishDone
}

func TestPipeUIMessageStreamToResponse_WithConsumeSSEStream(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}

	buf := &bytes.Buffer{}
	var consumeBuf bytes.Buffer
	httpRes, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			_, err := io.Copy(&consumeBuf, r)
			return err
		},
		StatusText: "OK",
	})
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponseWithInit() error = %v", err)
	}
	if httpRes.Status != "OK" {
		t.Fatalf("status text = %q", httpRes.Status)
	}
	_, err = io.Copy(buf, httpRes.Body)
	if err != nil {
		t.Fatalf("copy body = %v", err)
	}
	if got := buf.Len(); got == 0 {
		t.Fatalf("response body is empty")
	}
	if got := consumeBuf.Len(); got == 0 {
		t.Fatalf("consume stream body is empty")
	}
}
