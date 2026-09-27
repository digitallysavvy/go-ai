package ai

import (
	"context"
	"errors"
	"io"
	"sync"
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

// liveProviderTranslationStream serves parts pushed on a channel and observes
// ctx cancellation on Next(), mirroring liveProviderTranscriptionStream.
type liveProviderTranslationStream struct {
	ctx     context.Context
	parts   chan provider.SpeechTranslationStreamPart
	onClose func()
	mu      sync.Mutex
	closed  bool
}

func (s *liveProviderTranslationStream) Next() (*provider.SpeechTranslationStreamPart, error) {
	select {
	case p, ok := <-s.parts:
		if !ok {
			return nil, io.EOF
		}
		return &p, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
func (s *liveProviderTranslationStream) Err() error { return nil }
func (s *liveProviderTranslationStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.onClose != nil {
		s.onClose()
	}
	return nil
}

// erroringTranslationStream replays parts then fails with err.
type erroringTranslationStream struct {
	parts []provider.SpeechTranslationStreamPart
	idx   int
	err   error
}

func (s *erroringTranslationStream) Next() (*provider.SpeechTranslationStreamPart, error) {
	if s.idx >= len(s.parts) {
		return nil, s.err
	}
	p := s.parts[s.idx]
	s.idx++
	return &p, nil
}
func (s *erroringTranslationStream) Err() error   { return s.err }
func (s *erroringTranslationStream) Close() error { return nil }

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

// TestStreamTranslate_AbortsPendingDoStreamOnCancel mirrors
// TestStreamTranscribe_AbortsPendingDoStreamOnCancel: cancelling FullStream
// while DoStream setup is still pending must cancel that setup's ctx.
func TestStreamTranslate_AbortsPendingDoStreamOnCancel(t *testing.T) {
	observedDone := make(chan struct{}, 1)
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			<-ctx.Done()
			observedDone <- struct{}{}
			return nil, ctx.Err()
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	_ = stream.Close()

	select {
	case <-observedDone:
	case <-time.After(time.Second):
		t.Fatal("expected doStream's ctx to be cancelled")
	}
}

// TestStreamTranslate_CancelsModelStreamOnEarlyCancel mirrors
// TestStreamTranscribe_CancelsModelStreamOnEarlyCancel.
func TestStreamTranslate_CancelsModelStreamOnEarlyCancel(t *testing.T) {
	var closedMu sync.Mutex
	closed := false
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			parts := make(chan provider.SpeechTranslationStreamPart, 2)
			parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart}
			parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: []byte{1}}
			return &provider.SpeechTranslationStreamResult{
				Stream: &liveProviderTranslationStream{ctx: ctx, parts: parts, onClose: func() {
					closedMu.Lock()
					closed = true
					closedMu.Unlock()
				}},
			}, nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	part, err := stream.Next()
	if err != nil || part.Type != provider.SpeechTranslationStreamPartTypeAudio {
		t.Fatalf("stream.Next() = %+v, %v", part, err)
	}
	_ = stream.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		closedMu.Lock()
		c := closed
		closedMu.Unlock()
		if c {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected the model stream to be closed on early cancel")
}

// TestStreamTranslate_ResolvesWithoutConsumingFullStream mirrors
// TestStreamTranscribe_ResolvesWithoutConsumingFullStream.
func TestStreamTranslate_ResolvesWithoutConsumingFullStream(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, Delta: "hola"},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeFinish, SourceText: "hello", OutputText: "hola"},
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
	if err != nil || translationText != "hola" {
		t.Fatalf("TranslationText() = %q, %v", translationText, err)
	}
	warnings, err := result.Warnings()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("Warnings() = %+v, %v", warnings, err)
	}
}

// TestStreamTranslate_RejectsFullStreamAfterResultPromise mirrors
// TestStreamTranscribe_RejectsFullStreamAfterResultPromise.
func TestStreamTranslate_RejectsFullStreamAfterResultPromise(t *testing.T) {
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeFinish, SourceText: "hi", OutputText: "hola"},
			), nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: newMockChanAudioStream(), TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	if _, err := result.TranslationText(); err != nil {
		t.Fatalf("TranslationText() error = %v", err)
	}
	if _, err := result.FullStream(); err == nil {
		t.Fatal("expected FullStream() to error after a result accessor claimed the stream")
	}
}

// TestStreamTranslate_RejectsPendingPromisesOnMidSendCancel is the translate
// counterpart to TestStreamTranscribe_RejectsPendingPromisesOnMidSendCancel:
// a regression test for the deadlock fixed where closing FullStream while a
// part-send is blocked skipped rejecting the pending promises and cancelling
// audio.
func TestStreamTranslate_RejectsPendingPromisesOnMidSendCancel(t *testing.T) {
	audio := newMockChanAudioStream()
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			parts := make(chan provider.SpeechTranslationStreamPart, 2)
			parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart}
			parts <- provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: []byte{1}}
			return &provider.SpeechTranslationStreamResult{
				Stream: &liveProviderTranslationStream{ctx: ctx, parts: parts},
			}, nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: audio, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}

	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close() error = %v", err)
	}

	done := make(chan struct{})
	var translationText string
	var translationErr error
	go func() {
		translationText, translationErr = result.TranslationText()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TranslationText() deadlocked after FullStream was cancelled mid-send")
	}
	if translationErr == nil {
		t.Fatalf("TranslationText() = %q, want an error after cancellation", translationText)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cancelled, _ := audio.wasCancelled(); cancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected audio stream to be cancelled after mid-send cancellation")
}

// TestStreamTranslate_ProviderErrorMidStream_RejectsAndCancelsAudio mirrors
// TestStreamTranscribe_ProviderErrorMidStream_RejectsAndCancelsAudio.
func TestStreamTranslate_ProviderErrorMidStream_RejectsAndCancelsAudio(t *testing.T) {
	audio := newMockChanAudioStream()
	streamErr := errors.New("mid-stream provider failure")
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return &provider.SpeechTranslationStreamResult{
				Stream: &erroringTranslationStream{
					parts: []provider.SpeechTranslationStreamPart{
						{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
						{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: []byte{1}},
					},
					err: streamErr,
				},
			}, nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: audio, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}

	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	for {
		_, err := stream.Next()
		if err != nil {
			if !errors.Is(err, streamErr) {
				t.Fatalf("stream.Next() error = %v, want %v", err, streamErr)
			}
			break
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cancelled, _ := audio.wasCancelled(); cancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected audio stream to be cancelled after a mid-stream provider error")
}

// TestStreamTranslate_UnsupportedPartTypeFails mirrors TS
// stream-translate.ts's exhaustive-check default branch: an unrecognized
// part type from the provider is a hard error, not a silently dropped chunk.
func TestStreamTranslate_UnsupportedPartTypeFails(t *testing.T) {
	audio := newMockChanAudioStream()
	model := &mockSpeechTranslationModel{
		doStreamFn: func(ctx context.Context, opts *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return translationSliceResult(
				provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
				provider.SpeechTranslationStreamPart{Type: "not-a-real-type"},
			), nil
		},
	}

	result, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
		Model: model, Audio: audio, TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranslate error = %v", err)
	}
	if _, err := result.TranslationText(); err == nil {
		t.Fatal("expected TranslationText() to fail for an unsupported part type")
	}
}
