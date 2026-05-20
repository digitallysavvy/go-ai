package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateImage_Basic(t *testing.T) {
	m := &testutil.MockImageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			if opts.Prompt != "cat" {
				t.Fatalf("prompt = %q, want cat", opts.Prompt)
			}
			return &types.ImageResult{
				Image:    []byte("img"),
				MimeType: "image/png",
			}, nil
		},
	}
	got, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:  m,
		Prompt: "cat",
	})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if len(got.Images) != 1 || string(got.Image.Data) != "img" {
		t.Fatalf("unexpected image result: %+v", got)
	}
}

func TestGenerateSpeechAndTranscribe_Basic(t *testing.T) {
	speechModel := &testutil.MockSpeechModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			if opts.Text != "hello" {
				t.Fatalf("text = %q, want hello", opts.Text)
			}
			return &types.SpeechResult{Audio: []byte("audio"), MimeType: "audio/mpeg"}, nil
		},
	}
	speech, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: speechModel,
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech() error = %v", err)
	}
	if string(speech.Audio.Data) != "audio" {
		t.Fatalf("audio = %q, want audio", string(speech.Audio.Data))
	}

	transcribeModel := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{Text: "hello world"}, nil
		},
	}
	transcript, err := Transcribe(context.Background(), TranscribeOptions{
		Model:    transcribeModel,
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if transcript.Text != "hello world" {
		t.Fatalf("text = %q, want hello world", transcript.Text)
	}
}

func TestGenerateAndObjectResult_JSONKeysCamelCase(t *testing.T) {
	g := GenerateTextResult{
		Text:         "ok",
		FinishReason: types.FinishReasonStop,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "x"}},
	}
	gb, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal generate result: %v", err)
	}
	gs := string(gb)
	if !strings.Contains(gs, `"finishReason"`) || !strings.Contains(gs, `"toolCalls"`) {
		t.Fatalf("json keys not camelCase: %s", gs)
	}
	if strings.Contains(gs, `"FinishReason"`) || strings.Contains(gs, `"ToolCalls"`) {
		t.Fatalf("json contains PascalCase keys: %s", gs)
	}

	o := GenerateObjectResult{
		EnumValue:    "choice",
		FinishReason: types.FinishReasonStop,
	}
	ob, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal object result: %v", err)
	}
	os := string(ob)
	if !strings.Contains(os, `"enumValue"`) || strings.Contains(os, `"EnumValue"`) {
		t.Fatalf("object json key mismatch: %s", os)
	}
}

func TestPipeTextStreamToResponse_WritesText(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	res := &StreamTextResult{}
	// set unexported stream from same package
	res.stream = stream

	var buf bytes.Buffer
	if err := PipeTextStreamToResponse(context.Background(), res, &buf); err != nil {
		t.Fatalf("PipeTextStreamToResponse() error = %v", err)
	}
	out := buf.String()
	if out != "a" {
		t.Fatalf("unexpected text output: %q", out)
	}
}

func TestCreateTextStreamResponse_ContentType(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateTextStreamResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("CreateTextStreamResponse() error = %v", err)
	}
	if got := httpRes.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	body, err := io.ReadAll(httpRes.Body)
	if err != nil {
		t.Fatalf("ReadAll(body) error = %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", string(body))
	}
}
