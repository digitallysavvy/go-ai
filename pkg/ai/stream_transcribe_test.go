package ai

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockChanAudioStream is a test-only provider.AudioStream backed by a
// channel, recording whether/why it was cancelled.
type mockChanAudioStream struct {
	chunks       chan []byte
	mu           sync.Mutex
	cancelled    bool
	cancelReason error
}

func newMockChanAudioStream(data ...[]byte) *mockChanAudioStream {
	ch := make(chan []byte, len(data)+1)
	for _, d := range data {
		ch <- d
	}
	close(ch)
	return &mockChanAudioStream{chunks: ch}
}

func (m *mockChanAudioStream) Next(ctx context.Context) ([]byte, error) {
	select {
	case chunk, ok := <-m.chunks:
		if !ok {
			return nil, io.EOF
		}
		return chunk, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockChanAudioStream) Cancel(reason error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelled = true
	m.cancelReason = reason
}

func (m *mockChanAudioStream) wasCancelled() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancelled, m.cancelReason
}

// sliceProviderTranscriptionStream replays a fixed slice of parts.
type sliceProviderTranscriptionStream struct {
	parts []provider.TranscriptionStreamPart
	idx   int
}

func (s *sliceProviderTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
	if s.idx >= len(s.parts) {
		return nil, io.EOF
	}
	p := s.parts[s.idx]
	s.idx++
	return &p, nil
}
func (s *sliceProviderTranscriptionStream) Err() error   { return nil }
func (s *sliceProviderTranscriptionStream) Close() error { return nil }

// liveProviderTranscriptionStream serves parts pushed on a channel and
// observes ctx cancellation on Next(), like a real provider stream watching
// the abort signal it was constructed with.
type liveProviderTranscriptionStream struct {
	ctx     context.Context
	parts   chan provider.TranscriptionStreamPart
	onClose func()
	mu      sync.Mutex
	closed  bool
}

func (s *liveProviderTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
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
func (s *liveProviderTranscriptionStream) Err() error { return nil }
func (s *liveProviderTranscriptionStream) Close() error {
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

// mockTranscriptionStreamerModel implements provider.TranscriptionModel and
// provider.TranscriptionStreamer.
type mockTranscriptionStreamerModel struct {
	doStreamFn func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error)
}

func (m *mockTranscriptionStreamerModel) SpecificationVersion() string { return "v4" }
func (m *mockTranscriptionStreamerModel) Provider() string             { return "mock-provider" }
func (m *mockTranscriptionStreamerModel) ModelID() string              { return "mock-model-id" }
func (m *mockTranscriptionStreamerModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTranscriptionStreamerModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	return m.doStreamFn(ctx, opts)
}

func sliceStreamResult(parts ...provider.TranscriptionStreamPart) *provider.TranscriptionStreamResult {
	return &provider.TranscriptionStreamResult{
		Stream:   &sliceProviderTranscriptionStream{parts: parts},
		Response: &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Unix(0, 0), ModelID: "test-model-id"},
	}
}

// TestStreamTranscribe_ThrowsWhenDoStreamUnavailable mirrors TS
// "should throw UnsupportedFunctionalityError when doStream is unavailable".
func TestStreamTranscribe_ThrowsWhenDoStreamUnavailable(t *testing.T) {
	model := &mockTranscriptionModelNoStream{}
	_, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream([]byte{1, 2, 3}),
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

type mockTranscriptionModelNoStream struct{}

func (m *mockTranscriptionModelNoStream) SpecificationVersion() string { return "v4" }
func (m *mockTranscriptionModelNoStream) Provider() string             { return "mock-provider" }
func (m *mockTranscriptionModelNoStream) ModelID() string              { return "mock-model-id" }
func (m *mockTranscriptionModelNoStream) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	return nil, nil
}

// TestStreamTranscribe_StreamsPartsAndResolvesMetadata mirrors TS
// "should stream transcript parts and resolve final metadata".
func TestStreamTranscribe_StreamsPartsAndResolvesMetadata(t *testing.T) {
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hel"},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "lo"},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: "item-1", Text: "Hello"},
				provider.TranscriptionStreamPart{
					Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello",
					Segments: []provider.TranscriptSegment{{Text: "Hello", StartSecond: 0, EndSecond: 1}},
					Language: "en",
				},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream([]byte{1, 2, 3}),
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}

	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	var parts []*TranscriptionStreamPart
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
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}

	text, err := result.Text()
	if err != nil || text != "Hello" {
		t.Fatalf("Text() = %q, %v", text, err)
	}
	segments, err := result.Segments()
	if err != nil || len(segments) != 1 || segments[0].Text != "Hello" {
		t.Fatalf("Segments() = %+v, %v", segments, err)
	}
	language, _ := result.Language()
	if language != "en" {
		t.Fatalf("Language() = %q", language)
	}
}

// TestStreamTranscribe_RejectsWhenNoTranscript mirrors TS
// "should reject final promises when no transcript is returned".
func TestStreamTranscribe_RejectsWhenNoTranscript(t *testing.T) {
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: ""},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream(),
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	_, err = result.Text()
	if !IsNoTranscriptGeneratedError(err) {
		t.Fatalf("Text() err = %v, want NoTranscriptGeneratedError", err)
	}
}

// TestStreamTranscribe_CancelsAudioWhenDoStreamRejects mirrors TS
// "should cancel the audio stream when doStream rejects".
func TestStreamTranscribe_CancelsAudioWhenDoStreamRejects(t *testing.T) {
	audio := newMockChanAudioStream()
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return nil, errors.New("authentication failed")
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{Model: model, Audio: audio})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	_, err = result.Text()
	if err == nil || err.Error() != "authentication failed" {
		t.Fatalf("Text() err = %v, want authentication failed", err)
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

// TestStreamTranscribe_AbortsPendingDoStreamOnCancel mirrors TS
// "should abort a still-pending doStream when fullStream is cancelled".
func TestStreamTranscribe_AbortsPendingDoStreamOnCancel(t *testing.T) {
	observedDone := make(chan struct{}, 1)
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			<-ctx.Done()
			observedDone <- struct{}{}
			return nil, ctx.Err()
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream(),
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
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

// TestStreamTranscribe_CancelsModelStreamOnEarlyCancel mirrors TS
// "should cancel the model stream when fullStream is cancelled early".
func TestStreamTranscribe_CancelsModelStreamOnEarlyCancel(t *testing.T) {
	var closedMu sync.Mutex
	closed := false
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			parts := make(chan provider.TranscriptionStreamPart, 2)
			parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart}
			parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hel"}
			return &provider.TranscriptionStreamResult{
				Stream: &liveProviderTranscriptionStream{ctx: ctx, parts: parts, onClose: func() {
					closedMu.Lock()
					closed = true
					closedMu.Unlock()
				}},
			}, nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream(),
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}
	part, err := stream.Next()
	if err != nil || part.Type != provider.TranscriptionStreamPartTypeDelta {
		t.Fatalf("stream.Next() = %+v, %v", part, err)
	}
	_ = stream.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		closedMu.Lock()
		c := closed
		closedMu.Unlock()
		if c {
			break
		}
		time.Sleep(time.Millisecond)
	}
	closedMu.Lock()
	c := closed
	closedMu.Unlock()
	if !c {
		t.Fatal("expected the model stream to be closed on early cancel")
	}
}

// TestStreamTranscribe_ResolvesWithoutConsumingFullStream mirrors TS
// "should resolve the result promises without consuming fullStream".
func TestStreamTranscribe_ResolvesWithoutConsumingFullStream(t *testing.T) {
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hello"},
				provider.TranscriptionStreamPart{
					Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello",
					Segments: []provider.TranscriptSegment{{Text: "Hello", StartSecond: 0, EndSecond: 1}},
				},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{Model: model, Audio: newMockChanAudioStream()})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	text, err := result.Text()
	if err != nil || text != "Hello" {
		t.Fatalf("Text() = %q, %v", text, err)
	}
	warnings, err := result.Warnings()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("Warnings() = %+v, %v", warnings, err)
	}
}

// TestStreamTranscribe_RejectsFullStreamAfterResultPromise mirrors TS
// "should reject fullStream access after a result promise claimed the stream".
func TestStreamTranscribe_RejectsFullStreamAfterResultPromise(t *testing.T) {
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello"},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{Model: model, Audio: newMockChanAudioStream()})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	if _, err := result.Text(); err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if _, err := result.FullStream(); err == nil {
		t.Fatal("expected FullStream() to error after a result accessor claimed the stream")
	}
}

// TestStreamTranscribe_MP4DetectionMatchesTypeScript verifies the ftyp-based
// MP4/M4A media type detection fix (TS 76cb673) used by both batch Transcribe
// and streaming setups that infer media type from raw bytes.
func TestStreamTranscribe_MP4DetectionMatchesTypeScript(t *testing.T) {
	m4a := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	if got := detectTranscriptionMediaType(m4a); got != "audio/mp4" {
		t.Fatalf("detectTranscriptionMediaType(M4A) = %q, want audio/mp4", got)
	}
}
