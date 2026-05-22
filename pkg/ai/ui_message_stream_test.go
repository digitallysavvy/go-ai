package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestReadUIMessageStream_RoundTrip(t *testing.T) {
	input := []byte("data: {\"type\":\"text\",\"value\":\"hi\"}\n\ndata: {\"type\":\"finish\"}\n\n")
	chunks, err := ReadUIMessageStream(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("ReadUIMessageStream() error = %v", err)
	}
	if len(chunks) != 2 || chunks[0]["type"] != "text" || chunks[1]["type"] != "finish" {
		t.Fatalf("chunks = %#v", chunks)
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
	finishCalled := make(chan struct{}, 1)
	stepCalled := make(chan struct{}, 1)

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
		OnStepFinish: func(map[string]interface{}) {
			stepCalled <- struct{}{}
		},
		OnFinish: func(map[string]interface{}) {
			finishCalled <- struct{}{}
		},
	})

	var got []UIMessageChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if !sendStart {
		// keep test intent clear
	}
	hasStart := false
	hasMetadata := false
	hasSource := false
	for _, c := range got {
		switch c["type"] {
		case "start":
			hasStart = true
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
	if len(finishCalled) == 0 || len(stepCalled) == 0 {
		t.Fatalf("expected callbacks")
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected err = %v", err)
		}
	default:
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
		if err == nil {
			t.Fatalf("expected err")
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
			writer.Write(UIMessageChunk{"type": "end"})
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
