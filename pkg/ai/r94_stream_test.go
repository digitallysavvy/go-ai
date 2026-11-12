package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// r94streamTranscribeEvents records the streamTranscribe start and end
// events a custom telemetry integration receives.
type r94streamTranscribeEvents struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	start *telemetry.TelemetryStartEvent
	end   *telemetry.TelemetryFinishEvent
}

func (r *r94streamTranscribeEvents) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = &e
	return ctx
}

func (r *r94streamTranscribeEvents) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.end = &e
}

func (r *r94streamTranscribeEvents) get() (*telemetry.TelemetryStartEvent, *telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.start, r.end
}

// r94streamTranscribeModel streams stream-start (with a warning), a
// response-metadata part (headers and a body) and a finish part with
// segments, language and duration.
func r94streamTranscribeModel(seen *map[string]interface{}) *mockTranscriptionStreamerModel {
	duration := 1.5
	return &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			if seen != nil {
				*seen = opts.ProviderOptions
			}
			for {
				if _, err := opts.Audio.Next(ctx); err != nil {
					break
				}
			}
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: []types.Warning{{Type: "other", Message: "test warning"}}},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeResponseMetadata, ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}, Body: map[string]interface{}{"raw": true}},
				provider.TranscriptionStreamPart{
					Type:              provider.TranscriptionStreamPartTypeFinish,
					FinishText:        "Hello",
					Segments:          []provider.TranscriptSegment{{Text: "Hello", StartSecond: 0, EndSecond: 1}},
					Language:          "en",
					DurationInSeconds: &duration,
				},
			), nil
		},
	}
}

// TS stream-transcribe.ts 158-171 and 277-294: the start event carries
// inputAudioFormat and providerOptions (`providerOptions = {}` when none);
// the end event carries segments, language, durationInSeconds, the
// stream-start warnings and the current response metadata.
func TestR94streamTranscribeTelemetryEventFields(t *testing.T) {
	for _, tc := range []struct {
		name            string
		providerOptions map[string]interface{}
		want            map[string]interface{}
	}{
		{name: "no provider options", providerOptions: nil, want: map[string]interface{}{}},
		{name: "provider options", providerOptions: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}}, want: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r94streamTranscribeEvents{}
			var seen map[string]interface{}
			rate := 16000
			result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
				Model:            r94streamTranscribeModel(&seen),
				Audio:            newMockChanAudioStream([]byte("abc")),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
				ProviderOptions:  tc.providerOptions,
				Telemetry:        &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := result.Text(); err != nil {
				t.Fatal(err)
			}
			var start *telemetry.TelemetryStartEvent
			var end *telemetry.TelemetryFinishEvent
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if start, end = rec.get(); start != nil && end != nil {
					break
				}
			}
			if start == nil || end == nil {
				t.Fatalf("start = %v, end = %v; want both events", start, end)
			}
			if start.InputAudioFormat == nil || start.InputAudioFormat.Type != "audio/pcm" || start.InputAudioFormat.Rate == nil || *start.InputAudioFormat.Rate != 16000 {
				t.Errorf("start InputAudioFormat = %#v, want audio/pcm at 16000", start.InputAudioFormat)
			}
			if start.ProviderOptions == nil || !reflect.DeepEqual(start.ProviderOptions, tc.want) {
				t.Errorf("start ProviderOptions = %#v, want %#v", start.ProviderOptions, tc.want)
			}
			if seen == nil || !reflect.DeepEqual(seen, tc.want) {
				t.Errorf("DoStream ProviderOptions = %#v, want %#v", seen, tc.want)
			}
			wantSegments := []telemetry.TelemetryTranscriptTiming{{Text: "Hello", StartSecond: 0, EndSecond: 1}}
			if !reflect.DeepEqual(end.Segments, wantSegments) {
				t.Errorf("end Segments = %#v, want %#v", end.Segments, wantSegments)
			}
			if end.Language != "en" || end.DurationInSeconds == nil || *end.DurationInSeconds != 1.5 {
				t.Errorf("end Language = %q, DurationInSeconds = %v; want en, 1.5", end.Language, end.DurationInSeconds)
			}
			wantWarnings := []types.Warning{{Type: "other", Message: "test warning"}}
			if !reflect.DeepEqual(end.Warnings, wantWarnings) {
				t.Errorf("end Warnings = %#v, want %#v", end.Warnings, wantWarnings)
			}
			wantResponse := &telemetry.TelemetryAudioResponse{Timestamp: time.Unix(0, 0), ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}}
			if !reflect.DeepEqual(end.Response, wantResponse) {
				t.Errorf("end Response = %#v, want %#v", end.Response, wantResponse)
			}
		})
	}
}

// TS stream-transcribe.ts 237-243: a response-metadata part updates the
// timestamp, model ID and headers of the transcription response; its body
// is not kept (TranscriptionModelResponseMetadata has no body).
func TestR94streamTranscribeResponseMetadataDropsBody(t *testing.T) {
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            r94streamTranscribeModel(nil),
		Audio:            newMockChanAudioStream([]byte("abc")),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := result.Responses()
	if err != nil {
		t.Fatal(err)
	}
	want := []TranscriptionModelResponseMetadata{{Timestamp: time.Unix(0, 0), ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}}}
	if !reflect.DeepEqual(responses, want) {
		t.Fatalf("Responses() = %#v, want %#v", responses, want)
	}
}

// r94streamFinishModel streams the given parts after draining the audio.
func r94streamFinishModel(capture func(*provider.TranscriptionStreamOptions), parts ...provider.TranscriptionStreamPart) *mockTranscriptionStreamerModel {
	return &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			if capture != nil {
				capture(opts)
			}
			for {
				if _, err := opts.Audio.Next(ctx); err != nil {
					break
				}
			}
			return sliceStreamResult(parts...), nil
		},
	}
}

// r94streamDrain reads stream to its end and returns the parts and the
// terminal error (nil at io.EOF).
func r94streamDrain(stream TranscriptionStream) ([]TranscriptionStreamPart, error) {
	var parts []TranscriptionStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err.Error() == "EOF" {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, *part)
	}
}

// TS stream-transcribe.test.ts 59 "should send args to doStream".
func TestR94streamTranscribeSendsArgsToDoStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var captured *provider.TranscriptionStreamOptions
	rate := 16000
	format := provider.AudioFormat{Type: "audio/pcm", Rate: &rate}
	result, err := ExperimentalStreamTranscribe(ctx, StreamTranscribeOptions{
		Model: r94streamFinishModel(func(o *provider.TranscriptionStreamOptions) { captured = o },
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello world"},
		),
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: format,
		ProviderOptions:  map[string]interface{}{"mock": map[string]interface{}{"option": "value"}},
		Headers:          map[string]string{"custom-request-header": "request-header-value"},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r94streamDrain(stream); err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("DoStream not called")
	}
	if !reflect.DeepEqual(captured.InputAudioFormat, format) {
		t.Errorf("InputAudioFormat = %#v, want %#v", captured.InputAudioFormat, format)
	}
	if want := map[string]interface{}{"mock": map[string]interface{}{"option": "value"}}; !reflect.DeepEqual(captured.ProviderOptions, want) {
		t.Errorf("ProviderOptions = %#v, want %#v", captured.ProviderOptions, want)
	}
	if want := map[string]string{"custom-request-header": "request-header-value", "user-agent": "ai/" + version.Version}; !reflect.DeepEqual(captured.Headers, want) {
		t.Errorf("Headers = %#v, want %#v", captured.Headers, want)
	}
	if !captured.IncludeRawChunks {
		t.Error("IncludeRawChunks = false, want true")
	}
	if captured.Audio == nil {
		t.Error("Audio = nil")
	}
	// The model's signal follows the caller's.
	if captured.AbortSignal == nil || captured.AbortSignal.Err() != nil {
		t.Fatalf("AbortSignal before cancel = %v", captured.AbortSignal)
	}
	cancel()
	select {
	case <-captured.AbortSignal.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("AbortSignal did not follow the caller's cancel")
	}
}

// TS stream-transcribe.test.ts 261 "should keep already-resolved promises
// resolved when the stream errors later".
func TestR94streamTranscribeKeepsResolvedPromisesOnLaterError(t *testing.T) {
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: r94streamFinishModel(nil,
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: []types.Warning{{Type: "other", Message: "test warning"}}},
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: ""},
		),
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r94streamDrain(stream); !IsNoTranscriptGeneratedError(err) {
		t.Fatalf("FullStream error = %v, want NoTranscriptGeneratedError", err)
	}
	warnings, err := result.Warnings()
	if err != nil || !reflect.DeepEqual(warnings, []types.Warning{{Type: "other", Message: "test warning"}}) {
		t.Errorf("Warnings() = %#v, %v; want the stream-start warning", warnings, err)
	}
	if _, err := result.Text(); !IsNoTranscriptGeneratedError(err) {
		t.Errorf("Text() error = %v, want NoTranscriptGeneratedError", err)
	}
}

// r94streamTranscribeAllEvents records every streamTranscribe event a custom
// integration receives, in order.
type r94streamTranscribeAllEvents struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []telemetry.TelemetryStartEvent
	ends   []telemetry.TelemetryFinishEvent
	errs   []telemetry.TelemetryErrorEvent
}

func (r *r94streamTranscribeAllEvents) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, e)
	return ctx
}

func (r *r94streamTranscribeAllEvents) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}

func (r *r94streamTranscribeAllEvents) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, e)
}

// TS stream-transcribe.test.ts 357 "should emit one correlated telemetry
// error for a provider stream failure".
func TestR94streamTranscribeOneCorrelatedTelemetryError(t *testing.T) {
	streamErr := errors.New("connection lost")
	rec := &r94streamTranscribeAllEvents{}
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: &mockTranscriptionStreamerModel{
			doStreamFn: func(context.Context, *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
				return &provider.TranscriptionStreamResult{Stream: &erroringTranscriptionStream{
					parts: []provider.TranscriptionStreamPart{{Type: provider.TranscriptionStreamPartTypeStreamStart}},
					err:   streamErr,
				}}, nil
			},
		},
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Telemetry:        &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r94streamDrain(stream); err != streamErr { //nolint:errorlint // TS rejects with the same error object
		t.Fatalf("FullStream error = %v, want the provider error itself", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 1 || len(rec.ends) != 0 || len(rec.errs) != 1 {
		t.Fatalf("events: %d start, %d end, %d error; want 1, 0, 1", len(rec.starts), len(rec.ends), len(rec.errs))
	}
	if rec.errs[0].CallID == "" || rec.errs[0].CallID != rec.starts[0].CallID {
		t.Errorf("error CallID = %q, start CallID = %q; want the same non-empty ID", rec.errs[0].CallID, rec.starts[0].CallID)
	}
	if rec.errs[0].Error != streamErr { //nolint:errorlint // TS passes the same error object
		t.Errorf("error event Error = %v, want the provider error itself", rec.errs[0].Error)
	}
	if rec.errs[0].AIVersion == "" {
		t.Error("error event AIVersion is empty")
	}
}

// TS stream-transcribe.test.ts 507 "should reject the result promises
// without consuming fullStream when no transcript is produced".
func TestR94streamTranscribeRejectsWithoutFullStreamWhenNoTranscript(t *testing.T) {
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            r94streamFinishModel(nil, provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart}),
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.Text(); err == nil || err.Error() != "No transcript generated." {
		t.Fatalf("Text() error = %v, want No transcript generated.", err)
	}
}

// TS stream-transcribe.test.ts 540 "should support iterating fullStream
// before awaiting promises".
func TestR94streamTranscribeIteratesFullStreamBeforePromises(t *testing.T) {
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: r94streamFinishModel(nil,
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hello"},
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello"},
		),
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r94streamDrain(stream)
	if err != nil {
		t.Fatal(err)
	}
	if want := []TranscriptionStreamPart{{Type: "transcript-delta", ID: "item-1", Delta: "Hello"}}; !reflect.DeepEqual(parts, want) {
		t.Errorf("parts = %#v, want %#v", parts, want)
	}
	if text, err := result.Text(); err != nil || text != "Hello" {
		t.Errorf("Text() = %q, %v; want Hello", text, err)
	}
}

// r94streamLiveModel returns a model whose stream serves the parts pushed on
// the returned channel.
func r94streamLiveModel() (*mockTranscriptionStreamerModel, chan provider.TranscriptionStreamPart) {
	parts := make(chan provider.TranscriptionStreamPart)
	return &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, _ *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return &provider.TranscriptionStreamResult{Stream: &liveProviderTranscriptionStream{ctx: ctx, parts: parts}}, nil
		},
	}, parts
}

// TS stream-transcribe.test.ts 561 "should resolve a result promise while
// fullStream is actively consumed".
func TestR94streamTranscribeResolvesPromiseWhileFullStreamConsumed(t *testing.T) {
	model, parts := r94streamLiveModel()
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            model,
		Audio:            newMockChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart}
	go func() {
		parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hello"}
	}()
	part, err := stream.Next()
	if err != nil || !reflect.DeepEqual(*part, TranscriptionStreamPart{Type: "transcript-delta", ID: "item-1", Delta: "Hello"}) {
		t.Fatalf("first part = %#v, %v", part, err)
	}
	// Keep consuming fullStream while waiting on the result promise.
	streamDone := make(chan error, 1)
	go func() {
		_, err := stream.Next()
		streamDone <- err
	}()
	textDone := make(chan string, 1)
	go func() {
		text, _ := result.Text()
		textDone <- text
	}()
	parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello"}
	close(parts)
	select {
	case text := <-textDone:
		if text != "Hello" {
			t.Errorf("Text() = %q, want Hello", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Text() did not resolve")
	}
	select {
	case err := <-streamDone:
		if err == nil || err.Error() != "EOF" {
			t.Errorf("stream end = %v, want EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fullStream did not end")
	}
}

// TS stream-transcribe.test.ts 600 "should reject a second fullStream
// access": the second access fails, and cancelling the first rejects the
// pending result promise.
func TestR94streamTranscribeRejectsSecondFullStreamAccess(t *testing.T) {
	model, parts := r94streamLiveModel()
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            model,
		Audio:            newMockChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart}
	textErr := make(chan error, 1)
	go func() {
		_, err := result.Text()
		textErr <- err
	}()
	if _, err := result.FullStream(); err == nil || err.Error() != "fullStream can only be accessed once." {
		t.Fatalf("second FullStream() error = %v, want fullStream can only be accessed once.", err)
	}
	_ = stream.Close()
	select {
	case err := <-textErr:
		if err == nil {
			t.Error("Text() resolved, want a rejection after the cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Text() did not settle after the cancel")
	}
}

// TS stream-transcribe.test.ts 622 "should emit telemetry events with the
// consumed audio byte count" (Go audio chunks are bytes; TS's base64 string
// chunk 'BAU=' is the two bytes 4, 5).
func TestR94streamTranscribeTelemetryEventsConsumedByteCount(t *testing.T) {
	rec := &r94streamTranscribeAllEvents{}
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: r94streamFinishModel(nil,
			provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
			provider.TranscriptionStreamPart{
				Type:             provider.TranscriptionStreamPartTypeFinish,
				FinishText:       "Hello",
				ProviderMetadata: map[string]interface{}{"mock": map[string]interface{}{"traceId": "trace-1"}},
				Usage:            map[string]interface{}{"inputTokens": 3},
			},
		),
		Audio:            newMockChanAudioStream([]byte{1, 2, 3}, []byte{4, 5}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Telemetry: &TelemetrySettings{
			IsEnabled:    telemetry.Bool(true),
			FunctionID:   "stream-audio",
			Integrations: []telemetry.TelemetryIntegration{rec},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r94streamDrain(stream); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 1 || len(rec.ends) != 1 {
		t.Fatalf("events: %d start, %d end; want 1, 1", len(rec.starts), len(rec.ends))
	}
	start, end := rec.starts[0], rec.ends[0]
	if start.CallID == "" || start.CallID != end.CallID {
		t.Errorf("CallIDs = %q / %q, want the same non-empty ID", start.CallID, end.CallID)
	}
	if start.OperationType != "ai.streamTranscribe" || start.AudioByteLength != nil || start.AudioMediaType != "audio/pcm" || start.Settings == nil || start.Settings.FunctionID != "stream-audio" {
		t.Errorf("start = %s, size %v, %q, settings %+v", start.OperationType, start.AudioByteLength, start.AudioMediaType, start.Settings)
	}
	if end.OperationType != "ai.streamTranscribe" || end.AudioByteLength == nil || *end.AudioByteLength != 5 || end.AudioMediaType != "audio/pcm" || end.Text != "Hello" {
		t.Errorf("end = %s, size %v, %q, text %q", end.OperationType, end.AudioByteLength, end.AudioMediaType, end.Text)
	}
	if !reflect.DeepEqual(end.ProviderUsage, map[string]interface{}{"inputTokens": 3}) || !reflect.DeepEqual(end.ProviderMetadata, map[string]interface{}{"mock": map[string]interface{}{"traceId": "trace-1"}}) {
		t.Errorf("end usage = %#v, metadata = %#v", end.ProviderUsage, end.ProviderMetadata)
	}
	if end.Settings == nil || end.Settings.FunctionID != "stream-audio" {
		t.Errorf("end settings = %+v, want functionId stream-audio", end.Settings)
	}
}

// r94streamSettlesWithin reports whether fn returns within d, and its
// error.
func r94streamSettlesWithin(d time.Duration, fn func() error) (bool, error, chan error) {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return true, err, done
	case <-time.After(d):
		return false, nil, done
	}
}

// TS pipes the model stream through a TransformStream with a readable
// high-water mark of 0: once fullStream is claimed, a part it does not
// forward (here the finish part) is processed only when the reader pulls,
// so the result promises wait for the reader. Without a FullStream the
// accessors drain the stream themselves.
func TestR94streamAccessorsWaitForFullStreamReader(t *testing.T) {
	const quiet = 150 * time.Millisecond
	t.Run("transcribe", func(t *testing.T) {
		newResult := func() *StreamTranscriptionResult {
			r, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
				Model: r94streamFinishModel(nil,
					provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
					provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "Hello"},
				),
				Audio:            newMockChanAudioStream([]byte{1}),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		r := newResult()
		stream, err := r.FullStream()
		if err != nil {
			t.Fatal(err)
		}
		settled, _, done := r94streamSettlesWithin(quiet, func() error { _, err := r.Text(); return err })
		if settled {
			t.Fatal("Text() settled before the FullStream reader pulled")
		}
		if _, err := stream.Next(); err == nil || err.Error() != "EOF" {
			t.Fatalf("Next() = %v, want EOF", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Text() error = %v", err)
		}
		r = newResult()
		if text, err := r.Text(); err != nil || text != "Hello" {
			t.Fatalf("Text() without FullStream = %q, %v", text, err)
		}
	})
	t.Run("translate", func(t *testing.T) {
		newResult := func() *StreamTranslationResult {
			r, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{
				Model: &mockSpeechTranslationModel{doStreamFn: func(context.Context, *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
					return translationSliceResult(
						provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart},
						provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeFinish, OutputText: "hola"},
					), nil
				}},
				Audio: newMockChanAudioStream([]byte{1}), TargetLanguage: "es",
			})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		r := newResult()
		stream, err := r.FullStream()
		if err != nil {
			t.Fatal(err)
		}
		settled, _, done := r94streamSettlesWithin(quiet, func() error { _, err := r.TranslationText(); return err })
		if settled {
			t.Fatal("TranslationText() settled before the FullStream reader pulled")
		}
		if _, err := stream.Next(); err == nil || err.Error() != "EOF" {
			t.Fatalf("Next() = %v, want EOF", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("TranslationText() error = %v", err)
		}
		r = newResult()
		if text, err := r.TranslationText(); err != nil || text != "hola" {
			t.Fatalf("TranslationText() without FullStream = %q, %v", text, err)
		}
	})
	t.Run("speech", func(t *testing.T) {
		// No audio: the finish part fails the call with NoSpeechGeneratedError.
		newResult := func() *StreamSpeechResult {
			r, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
				Model: &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
					return speechParts(provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeStreamStart}, finishPart()), nil
				}},
				Text: "Hello",
			})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		r := newResult()
		stream, err := r.FullStream()
		if err != nil {
			t.Fatal(err)
		}
		settled, _, done := r94streamSettlesWithin(quiet, func() error { _, err := r.Usage(); return err })
		if settled {
			t.Fatal("Usage() settled before the FullStream reader pulled")
		}
		if _, err := stream.Next(); err == nil {
			t.Fatal("Next() = nil error, want the no-speech failure")
		}
		if err := <-done; err == nil {
			t.Fatal("Usage() resolved, want the no-speech failure")
		}
		r = newResult()
		if _, err := r.Usage(); err == nil {
			t.Fatal("Usage() without FullStream resolved, want the no-speech failure")
		}
	})
}

// r94streamSpeechText is a caller text stream recording Cancel.
type r94streamSpeechText struct {
	mu        sync.Mutex
	cancelled bool
}

func (s *r94streamSpeechText) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *r94streamSpeechText) Cancel(error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

func (s *r94streamSpeechText) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// TS stream-speech.test.ts 587 "should reject result promises and cancel
// the text when doStream rejects", and stream-speech.ts's catch: once the
// model returned its stream, the model owns the text stream, so a later
// model stream failure does not cancel it from the pipe.
func TestR94streamSpeechCancelsTextOnlyBeforeTheModelOwnsIt(t *testing.T) {
	t.Run("doStream rejects", func(t *testing.T) {
		text := &r94streamSpeechText{}
		r, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
			Model: &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
				return nil, errors.New("connect failed")
			}},
			TextStream: text,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Usage(); err == nil || err.Error() != "connect failed" {
			t.Fatalf("Usage() error = %v, want connect failed", err)
		}
		if !text.wasCancelled() {
			t.Error("text stream not cancelled after doStream rejected")
		}
	})
	t.Run("model stream errors", func(t *testing.T) {
		text := &r94streamSpeechText{}
		r, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
			Model: &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
				return &provider.SpeechStreamResult{Stream: &r94streamErrSpeechStream{err: errors.New("connection lost")}}, nil
			}},
			TextStream: text,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Usage(); err == nil || err.Error() != "connection lost" {
			t.Fatalf("Usage() error = %v, want connection lost", err)
		}
		if text.wasCancelled() {
			t.Error("the pipe cancelled a text stream the model owns")
		}
	})
}

type r94streamErrSpeechStream struct{ err error }

func (s *r94streamErrSpeechStream) Next() (*provider.SpeechStreamPart, error) { return nil, s.err }
func (s *r94streamErrSpeechStream) Err() error                                { return s.err }
func (s *r94streamErrSpeechStream) Close() error                              { return nil }
