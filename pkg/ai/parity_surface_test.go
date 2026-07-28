package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

func TestGenerateImage_Base64ImagesDecodeToGeneratedFiles(t *testing.T) {
	m := &testutil.MockImageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			return &types.ImageResult{
				Image:        []byte("Zmlyc3Q="),
				Images:       [][]byte{[]byte("Zmlyc3Q="), []byte("c2Vjb25k")},
				Base64Image:  "Zmlyc3Q=",
				Base64Images: []string{"Zmlyc3Q=", "c2Vjb25k"},
				MimeType:     "image/png",
				Usage:        types.ImageUsage{ImageCount: 2},
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
	if len(got.Images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(got.Images))
	}
	if string(got.Image.Data) != "first" || string(got.Images[1].Data) != "second" {
		t.Fatalf("decoded images = %q/%q, want first/second", string(got.Image.Data), string(got.Images[1].Data))
	}
	if got.Usage.ImageCount != 2 {
		t.Fatalf("image count = %d, want 2", got.Usage.ImageCount)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"data":"Zmlyc3Q="`) {
		t.Fatalf("encoded result = %s, want first image JSON data to preserve provider base64", encoded)
	}
}

func TestGenerateImage_BatchesByMaxImagesPerCallAndAggregatesResults(t *testing.T) {
	var callNs []int
	m := &testutil.MockImageModel{
		MaxImages: 2,
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			if opts.N == nil {
				t.Fatal("call N is nil")
			}
			callNs = append(callNs, *opts.N)
			callIndex := len(callNs)
			images := make([][]byte, 0, *opts.N)
			metadata := make([]map[string]interface{}, 0, *opts.N)
			for i := 0; i < *opts.N; i++ {
				images = append(images, []byte(strings.Repeat(fmt.Sprintf("%d", callIndex), i+1)))
				metadata = append(metadata, map[string]interface{}{
					"call":  callIndex,
					"index": i,
				})
			}
			return &types.ImageResult{
				Image:    images[0],
				Images:   images,
				MimeType: "image/png",
				Warnings: []types.Warning{{
					Type:    "other",
					Message: fmt.Sprintf("call %d warning", callIndex),
				}},
				Response: &types.ResponseMetadata{
					ID:      fmt.Sprintf("resp-%d", callIndex),
					ModelID: "mock-image",
				},
				ProviderMetadata: map[string]interface{}{
					"mock": map[string]interface{}{"images": metadata},
				},
				Usage: types.ImageUsage{ImageCount: len(images)},
			}, nil
		},
	}
	n := 5

	got, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:  m,
		Prompt: "cat",
		N:      &n,
	})

	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if !reflect.DeepEqual(callNs, []int{2, 2, 1}) {
		t.Fatalf("call Ns = %+v, want [2 2 1]", callNs)
	}
	if len(got.Images) != 5 {
		t.Fatalf("len(images) = %d, want 5", len(got.Images))
	}
	if len(got.Warnings) != 3 {
		t.Fatalf("len(warnings) = %d, want 3", len(got.Warnings))
	}
	if len(got.Responses) != 3 || got.Responses[2].ID != "resp-3" {
		t.Fatalf("responses = %+v, want three aggregated responses", got.Responses)
	}
	if got.Usage.ImageCount != 5 {
		t.Fatalf("image count = %d, want 5", got.Usage.ImageCount)
	}
	mockMeta := got.ProviderMetadata["mock"].(map[string]interface{})
	imagesMeta := mockMeta["images"].([]interface{})
	if len(imagesMeta) != 5 {
		t.Fatalf("metadata images = %+v, want 5 entries", imagesMeta)
	}
}

func TestGenerateImage_DetectsMediaTypeWhenProviderOmitsIt(t *testing.T) {
	m := &testutil.MockImageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			return &types.ImageResult{
				Image: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
				Images: [][]byte{
					{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
					[]byte("unknown"),
				},
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
	if got.Images[0].MediaType != "image/png" {
		t.Fatalf("first media type = %q, want image/png", got.Images[0].MediaType)
	}
	if got.Images[1].MediaType != "image/png" {
		t.Fatalf("fallback media type = %q, want image/png", got.Images[1].MediaType)
	}
}

func TestGenerateImage_MaxRetriesRetriesEachCall(t *testing.T) {
	attempts := 0
	m := &testutil.MockImageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			attempts++
			if attempts == 1 {
				return nil, &providererrors.ProviderError{
					Provider:        "mock",
					StatusCode:      500,
					Message:         "temporary",
					ResponseHeaders: map[string]string{"retry-after-ms": "0"},
				}
			}
			return &types.ImageResult{
				Image:    []byte("img"),
				MimeType: "image/png",
				Usage:    types.ImageUsage{ImageCount: 1},
			}, nil
		},
	}
	maxRetries := 1

	got, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:      m,
		Prompt:     "cat",
		MaxRetries: &maxRetries,
	})

	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if string(got.Image.Data) != "img" {
		t.Fatalf("image = %q, want img", string(got.Image.Data))
	}
}

func TestGenerateImage_MaxRetriesDoesNotRetryNonRetryableError(t *testing.T) {
	attempts := 0
	m := &testutil.MockImageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			attempts++
			return nil, errors.New("not retryable")
		},
	}
	maxRetries := 2

	_, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:      m,
		Prompt:     "cat",
		MaxRetries: &maxRetries,
	})

	if err == nil || !strings.Contains(err.Error(), "not retryable") {
		t.Fatalf("GenerateImage() error = %v, want original non-retryable error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestGenerateImage_MaxRetriesRejectsNegative(t *testing.T) {
	maxRetries := -1

	_, err := GenerateImage(context.Background(), GenerateImageOptions{
		Model:      &testutil.MockImageModel{},
		Prompt:     "cat",
		MaxRetries: &maxRetries,
	})

	if err == nil || !strings.Contains(err.Error(), "maxRetries must be >= 0") {
		t.Fatalf("GenerateImage() error = %v, want maxRetries validation error", err)
	}
}

func TestGenerateSpeechAndTranscribe_Basic(t *testing.T) {
	speechModel := &testutil.MockSpeechModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			if opts.Text != "hello" {
				t.Fatalf("text = %q, want hello", opts.Text)
			}
			return &types.SpeechResult{Audio: []byte("audio")}, nil
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
	if len(speech.Responses) != 1 || speech.Responses[0].ModelID != speechModel.ModelID() || speech.Responses[0].Timestamp.IsZero() {
		t.Fatalf("speech responses = %#v, want fallback model/timestamp metadata", speech.Responses)
	}

	transcribeModel := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			if opts.MimeType != "audio/wav" {
				t.Fatalf("transcription media type = %q, want audio/wav", opts.MimeType)
			}
			if opts.Headers["user-agent"] != "go-ai/0.5.0" {
				t.Fatalf("transcription user-agent = %q, want go-ai/0.5.0", opts.Headers["user-agent"])
			}
			if opts.ProviderOptions == nil || len(opts.ProviderOptions) != 0 {
				t.Fatalf("provider options = %#v, want empty map", opts.ProviderOptions)
			}
			return &types.TranscriptionResult{
				Text:     "hello world",
				Warnings: []types.Warning{{Type: "other", Message: "Setting is not supported"}},
				Response: &types.ResponseMetadata{
					ModelID: "test-model-id",
					Headers: map[string]string{
						"X-Request-Id": "req-1",
					},
				},
				ProviderMetadata: map[string]interface{}{
					"test-provider": map[string]interface{}{"test-key": "test-value"},
				},
			}, nil
		},
	}
	transcript, err := Transcribe(context.Background(), TranscribeOptions{
		Model: transcribeModel,
		Audio: []byte("audio"),
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if transcript.Text != "hello world" {
		t.Fatalf("text = %q, want hello world", transcript.Text)
	}
	if len(transcript.Responses) != 1 || transcript.Responses[0].Headers["X-Request-Id"] != "req-1" {
		t.Fatalf("responses = %#v", transcript.Responses)
	}
	if len(transcript.Warnings) != 1 || transcript.Warnings[0].Message != "Setting is not supported" {
		t.Fatalf("warnings = %#v", transcript.Warnings)
	}
	if transcript.ProviderMetadata["test-provider"] == nil {
		t.Fatalf("provider metadata = %#v", transcript.ProviderMetadata)
	}
}

func TestSpeechAndTranscribeDeprecatedExperimentalAliases(t *testing.T) {
	speechModel := &testutil.MockSpeechModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			if opts.Text != "hello" {
				t.Fatalf("speech text = %q, want hello", opts.Text)
			}
			return &types.SpeechResult{Audio: []byte("audio")}, nil
		},
	}
	speech, err := ExperimentalGenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: speechModel,
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("ExperimentalGenerateSpeech() error = %v", err)
	}
	var speechAlias *Experimental_SpeechResult = speech
	var audioAlias GeneratedAudioFile = speechAlias.Audio
	if string(audioAlias.Data) != "audio" {
		t.Fatalf("audio = %q, want audio", string(audioAlias.Data))
	}
	if audioAlias.Format != "mp3" {
		t.Fatalf("audio format = %q, want mp3", audioAlias.Format)
	}
	if audioAlias.Base64() != "YXVkaW8=" {
		t.Fatalf("audio base64 = %q, want YXVkaW8=", audioAlias.Base64())
	}
	backingAudio := audioAlias.Uint8Array()
	backingAudio[0] = 'A'
	if string(audioAlias.Data) != "Audio" {
		t.Fatalf("Uint8Array should return backing data like TypeScript: %q", string(audioAlias.Data))
	}

	transcribeModel := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			if opts.MimeType != "audio/wav" {
				t.Fatalf("transcription media type = %q, want audio/wav", opts.MimeType)
			}
			return &types.TranscriptionResult{Text: "hello world"}, nil
		},
	}
	transcript, err := ExperimentalTranscribe(context.Background(), TranscribeOptions{
		Model: transcribeModel,
		Audio: []byte("audio"),
	})
	if err != nil {
		t.Fatalf("ExperimentalTranscribe() error = %v", err)
	}
	var transcriptAlias *Experimental_TranscriptionResult = transcript
	if transcriptAlias.Text != "hello world" {
		t.Fatalf("text = %q, want hello world", transcriptAlias.Text)
	}
}

func TestTranscribe_AudioURLCustomDownloadMatchesTypeScript(t *testing.T) {
	audioBytes := []byte{
		'R', 'I', 'F', 'F',
		0x24, 0, 0, 0,
		'W', 'A', 'V', 'E',
	}
	var downloadedURL string
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			if !bytes.Equal(opts.Audio, audioBytes) {
				t.Fatalf("audio = %v, want downloaded bytes", opts.Audio)
			}
			if opts.MimeType != "audio/wav" {
				t.Fatalf("media type = %q, want detected audio/wav", opts.MimeType)
			}
			if opts.Headers["user-agent"] != "custom-agent go-ai/0.5.0" {
				t.Fatalf("user-agent = %q", opts.Headers["user-agent"])
			}
			return &types.TranscriptionResult{
				Text:     "downloaded transcript",
				Response: &types.ResponseMetadata{ModelID: "mock-transcription"},
			}, nil
		},
	}

	got, err := Transcribe(context.Background(), TranscribeOptions{
		Model:    model,
		AudioURL: "https://example.com/audio.wav",
		Headers:  map[string]string{"User-Agent": "custom-agent"},
		Download: func(ctx context.Context, rawURL string) ([]byte, error) {
			downloadedURL = rawURL
			return audioBytes, nil
		},
	})
	if err != nil {
		t.Fatalf("Transcribe(AudioURL) error = %v", err)
	}
	if got.Text != "downloaded transcript" || downloadedURL != "https://example.com/audio.wav" {
		t.Fatalf("result/downloadedURL = %#v/%q", got, downloadedURL)
	}
}

func TestTranscribeAudioBase64AndMaxRetriesMatchTypeScript(t *testing.T) {
	attempts := 0
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			attempts++
			if attempts == 1 {
				return nil, &providererrors.ProviderError{
					Provider:        "mock",
					StatusCode:      429,
					Message:         "temporary",
					ResponseHeaders: map[string]string{"retry-after-ms": "0"},
				}
			}
			if string(opts.Audio) != "audio" {
				t.Fatalf("audio = %q, want decoded base64", string(opts.Audio))
			}
			if opts.MimeType != "audio/wav" {
				t.Fatalf("media type = %q, want fallback audio/wav", opts.MimeType)
			}
			return &types.TranscriptionResult{
				Text:     "decoded transcript",
				Response: &types.ResponseMetadata{ModelID: "mock-transcription"},
			}, nil
		},
	}
	maxRetries := 1

	got, err := Transcribe(context.Background(), TranscribeOptions{
		Model:       model,
		AudioBase64: "YXVkaW8",
		MaxRetries:  &maxRetries,
	})
	if err != nil {
		t.Fatalf("Transcribe(AudioBase64) error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got.Text != "decoded transcript" {
		t.Fatalf("text = %q, want decoded transcript", got.Text)
	}
}

func TestTranscribeInvalidAudioBase64AndNegativeMaxRetries(t *testing.T) {
	model := &testutil.MockTranscriptionModel{}

	_, err := Transcribe(context.Background(), TranscribeOptions{
		Model:       model,
		AudioBase64: "not base64!!!",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid base64 audio data") {
		t.Fatalf("invalid base64 error = %v", err)
	}

	maxRetries := -1
	_, err = Transcribe(context.Background(), TranscribeOptions{
		Model:      model,
		Audio:      []byte("audio"),
		MaxRetries: &maxRetries,
	})
	if err == nil || !strings.Contains(err.Error(), "maxRetries must be >= 0") {
		t.Fatalf("negative maxRetries error = %v", err)
	}
}

func TestTranscribeNoTranscriptGeneratedErrorWithResponses(t *testing.T) {
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{
				Text: "",
				Response: &types.ResponseMetadata{
					ModelID: "test-model-id",
					Headers: map[string]string{
						"custom-response-header": "response-header-value",
					},
				},
			}, nil
		},
	}

	_, err := Transcribe(context.Background(), TranscribeOptions{
		Model: model,
		Audio: []byte("audio"),
	})
	var noTranscript *NoTranscriptGeneratedError
	if !errors.As(err, &noTranscript) {
		t.Fatalf("Transcribe error = %T %v, want NoTranscriptGeneratedError", err, err)
	}
	if noTranscript.Error() != "No transcript generated." {
		t.Fatalf("error message = %q", noTranscript.Error())
	}
	if len(noTranscript.Responses) != 1 || noTranscript.Responses[0].Headers["custom-response-header"] != "response-header-value" {
		t.Fatalf("responses = %#v", noTranscript.Responses)
	}

	fallbackModel := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{Text: ""}, nil
		},
	}
	_, err = Transcribe(context.Background(), TranscribeOptions{
		Model: fallbackModel,
		Audio: []byte("audio"),
	})
	if !errors.As(err, &noTranscript) {
		t.Fatalf("Transcribe fallback error = %T %v, want NoTranscriptGeneratedError", err, err)
	}
	if len(noTranscript.Responses) != 1 || noTranscript.Responses[0].ModelID != fallbackModel.ModelID() || noTranscript.Responses[0].Timestamp.IsZero() {
		t.Fatalf("fallback responses = %#v, want model/timestamp metadata", noTranscript.Responses)
	}
}

func TestTranscribeResultJSONShapeMatchesTypeScript(t *testing.T) {
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{
				Text: "hello",
				Segments: []types.TranscriptionTimestamp{{
					Text:  "hello",
					Start: 0,
					End:   1,
				}},
				Timestamps: []types.TranscriptionTimestamp{{
					Text:  "provider-only timestamp",
					Start: 2,
					End:   3,
				}},
				Usage: types.TranscriptionUsage{DurationSeconds: 3},
				Response: &types.ResponseMetadata{
					ID:      "provider-only-id",
					ModelID: "test-model-id",
					Body:    []byte(`{"provider":true}`),
				},
			}, nil
		},
	}

	result, err := Transcribe(context.Background(), TranscribeOptions{
		Model: model,
		Audio: []byte("audio"),
	})
	if err != nil {
		t.Fatalf("Transcribe error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	s := string(encoded)
	for _, absent := range []string{`"usage"`, `"timestamps"`, `"id"`} {
		if strings.Contains(s, absent) {
			t.Fatalf("result JSON = %s, must not include %s", s, absent)
		}
	}
	for _, present := range []string{`"text"`, `"segments"`, `"responses"`, `"providerMetadata"`, `"body"`} {
		if !strings.Contains(s, present) {
			t.Fatalf("result JSON = %s, missing %s", s, present)
		}
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
