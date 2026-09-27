package ai

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type sliceProviderTranslationStream struct {
	parts []provider.SpeechTranslationStreamPart
	idx   int
}

func (s *sliceProviderTranslationStream) Next() (*provider.SpeechTranslationStreamPart, error) {
	if s.idx >= len(s.parts) {
		return nil, io.EOF
	}
	p := s.parts[s.idx]
	s.idx++
	return &p, nil
}
func (s *sliceProviderTranslationStream) Err() error   { return nil }
func (s *sliceProviderTranslationStream) Close() error { return nil }

type mockSpeechTranslationModel struct {
	doStreamFn func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error)
}

func (m *mockSpeechTranslationModel) SpecificationVersion() string { return "v4" }
func (m *mockSpeechTranslationModel) Provider() string             { return "mock-provider" }
func (m *mockSpeechTranslationModel) ModelID() string              { return "mock-model-id" }
func (m *mockSpeechTranslationModel) DoStream(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
	return m.doStreamFn(ctx, opts)
}

func translationSliceResult(parts ...provider.SpeechTranslationStreamPart) *provider.SpeechTranslationStreamResult {
	return &provider.SpeechTranslationStreamResult{
		Stream:   &sliceProviderTranslationStream{parts: parts},
		Response: &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Unix(0, 0), ModelID: "test-model-id"},
	}
}

// TestStreamTranslate_StreamsAudioAndResolvesMetadata exercises the
// audio-output success path (TS stream-translate.test.ts basic flow).
func TestStreamTranslate_StreamsAudioAndResolvesMetadata(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			if opts.TargetLanguage != "es" {
				t.Fatalf("TargetLanguage = %q, want es", opts.TargetLanguage)
			}
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, ID: "a1", AudioData: []byte{1, 2, 3}},
				provider.SpeechTranslationStreamPart{
					Type: provider.SpeechTranslationStreamPartTypeFinish,
					SourceText: "hello", OutputText: "hola",
				},
			), nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream([]byte{1, 2, 3}), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}

	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	var parts []*TranslationStreamPart
	for {
		p, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		parts = append(parts, p)
	}
	if len(parts) != 1 || parts[0].Type != provider.SpeechTranslationStreamPartTypeAudio {
		t.Fatalf("parts = %+v", parts)
	}

	sourceText, err := result.SourceText()
	if err != nil || sourceText != "hello" {
		t.Fatalf("SourceText() = %q, %v", sourceText, err)
	}
	translationText, err := result.TranslationText()
	if err != nil || translationText != "hola" {
		t.Fatalf("TranslationText() = %q, %v", translationText, err)
	}
}

// TestStreamTranslate_AudioOnlyProviderSucceedsWithEmptyOutputText verifies
// that a stream producing only audio (no output text) is not treated as a
// failure, mirroring TS's `hasAudioOutput` success condition.
func TestStreamTranslate_AudioOnlyProviderSucceedsWithEmptyOutputText(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: []byte{1}},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeFinish, SourceText: "hi", OutputText: ""},
			), nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	translationText, err := result.TranslationText()
	if err != nil {
		t.Fatalf("TranslationText() error = %v, want success (audio output present)", err)
	}
	if translationText != "" {
		t.Fatalf("TranslationText() = %q, want empty", translationText)
	}
}

// TestStreamTranslate_RejectsWhenNeitherAudioNorText mirrors the
// NoTranslationGeneratedError path when a stream produces neither audio nor
// output text.
func TestStreamTranslate_RejectsWhenNeitherAudioNorText(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeFinish, SourceText: "hi", OutputText: ""},
			), nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	_, err = result.TranslationText()
	if !IsNoTranslationGeneratedError(err) {
		t.Fatalf("TranslationText() err = %v, want NoTranslationGeneratedError", err)
	}
}

// TestStreamTranslate_CancelsAudioWhenDoStreamRejects mirrors the transcribe
// equivalent for the translation pipeline.
func TestStreamTranslate_CancelsAudioWhenDoStreamRejects(t *testing.T) {
	audio := newMockChanAudioStream()
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return nil, errors.New("setup failed")
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{Model: model, Audio: audio, TargetLanguage: "es"})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	if _, err := result.TranslationText(); err == nil {
		t.Fatal("expected TranslationText() to fail")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cancelled, _ := audio.wasCancelled(); cancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected audio stream to be cancelled")
}
