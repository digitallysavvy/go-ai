package ai

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// sliceSpeechStream replays a fixed slice of parts, then io.EOF.
type sliceSpeechStream struct {
	parts []provider.SpeechStreamPart
	i     int
}

func (s *sliceSpeechStream) Next() (*provider.SpeechStreamPart, error) {
	if s.i >= len(s.parts) {
		return nil, io.EOF
	}
	p := s.parts[s.i]
	s.i++
	return &p, nil
}
func (s *sliceSpeechStream) Err() error   { return nil }
func (s *sliceSpeechStream) Close() error { return nil }

// blockingSpeechStream blocks in Next until Close, like a provider stream
// waiting on a socket.
type blockingSpeechStream struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingSpeechStream() *blockingSpeechStream {
	return &blockingSpeechStream{closed: make(chan struct{})}
}
func (s *blockingSpeechStream) Next() (*provider.SpeechStreamPart, error) {
	<-s.closed
	return nil, errors.New("closed")
}
func (s *blockingSpeechStream) Err() error { return nil }
func (s *blockingSpeechStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

type mockStreamingSpeechModel struct {
	doStream func(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error)
}

func (m *mockStreamingSpeechModel) SpecificationVersion() string { return "v4" }
func (m *mockStreamingSpeechModel) Provider() string             { return "mock-provider" }
func (m *mockStreamingSpeechModel) ModelID() string              { return "mock-model-id" }
func (m *mockStreamingSpeechModel) DoGenerate(context.Context, *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	return nil, errors.New("not implemented")
}
func (m *mockStreamingSpeechModel) DoStreamSpeech(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
	return m.doStream(ctx, opts)
}

func speechParts(parts ...provider.SpeechStreamPart) *provider.SpeechStreamResult {
	return &provider.SpeechStreamResult{Stream: &sliceSpeechStream{parts: parts}}
}

var mpeg = provider.AudioFormat{Type: "audio/mpeg"}

func audioPart(b string) provider.SpeechStreamPart {
	return provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeAudio, Audio: []byte(b), AudioFormat: mpeg}
}

func finishPart() provider.SpeechStreamPart {
	return provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeFinish}
}

func TestStreamSpeech_SendsArgsToDoStream(t *testing.T) {
	var got *provider.SpeechStreamOptions
	var text []provider.SpeechStreamTextPart
	model := &mockStreamingSpeechModel{doStream: func(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		got = opts
		for {
			p, err := opts.Text.Next(ctx)
			if err != nil {
				break
			}
			text = append(text, *p)
		}
		return speechParts(audioPart("a"), finishPart()), nil
	}}
	speed := 1.5
	rate := 24000
	res, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
		Model: model, Text: "Hello", Voice: "v1", Speed: &speed, Language: "en", Instructions: "slow",
		OutputFormat:    &provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		ProviderOptions: map[string]interface{}{"x": map[string]interface{}{"a": 1}},
		Headers:         map[string]string{"h": "1"}, IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.Usage(); err != nil {
		t.Fatal(err)
	}
	if got.Voice != "v1" || got.Language != "en" || got.Instructions != "slow" || *got.Speed != 1.5 || !got.IncludeRawChunks {
		t.Fatalf("options not forwarded: %+v", got)
	}
	if got.OutputFormat.Type != "audio/pcm" || *got.OutputFormat.Rate != 24000 {
		t.Fatalf("outputFormat = %+v", got.OutputFormat)
	}
	if got.Headers["h"] != "1" || !strings.Contains(got.Headers["user-agent"], "ai/") {
		t.Fatalf("headers = %v", got.Headers)
	}
	if len(text) != 1 || text[0].Text != "Hello" {
		t.Fatalf("text parts = %+v", text)
	}
}

func TestStreamSpeech_AudioStreamDropsEmptyAndErrorParts(t *testing.T) {
	var onErrMu sync.Mutex
	var onErrs []error
	errSeen := make(chan struct{}, 1)
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return speechParts(
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeStreamStart},
			audioPart("one"),
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeAudio, AudioFormat: mpeg}, // empty, no metadata: dropped
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeError, Err: "boom"},
			audioPart("two"),
			finishPart(),
		), nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
		Model: model, Text: "x",
		OnError: func(err error) {
			onErrMu.Lock()
			onErrs = append(onErrs, err)
			onErrMu.Unlock()
			errSeen <- struct{}{}
		},
	})
	audio, err := res.AudioStream()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for {
		b, err := audio.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(b))
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("audio = %v", got)
	}
	<-errSeen
	onErrMu.Lock()
	defer onErrMu.Unlock()
	if len(onErrs) != 1 || onErrs[0].Error() != "boom" {
		t.Fatalf("onError = %v", onErrs)
	}
}

func TestStreamSpeech_FullStreamKeepsMetadataOnlyAudioButDoesNotCountIt(t *testing.T) {
	marker := provider.SpeechStreamPart{
		Type: provider.SpeechStreamPartTypeAudio, AudioFormat: mpeg,
		ProviderMetadata: map[string]interface{}{"elevenlabs": map[string]interface{}{"isFinalAudioForTurn": true}},
	}
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return speechParts(marker, finishPart()), nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x"})
	full, _ := res.FullStream()
	part, err := full.Next()
	if err != nil || part.Type != "audio" || len(part.Audio) != 0 || part.ProviderMetadata == nil {
		t.Fatalf("part = %+v err=%v", part, err)
	}
	// The marker is not audio, so finish without audio fails with no-speech.
	_, err = full.Next()
	if !IsNoSpeechGeneratedError(err) {
		t.Fatalf("err = %v, want NoSpeechGeneratedError", err)
	}
}

func TestStreamSpeech_ResolvesResultPromises(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	chars := 12
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		r := speechParts(
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeStreamStart, Warnings: []types.Warning{{Type: "other", Message: "w"}}},
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeResponseMetadata, ModelID: "resp-model", Headers: map[string]string{"a": "b"}, Timestamp: ts},
			audioPart("a"),
			provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeFinish, Usage: &provider.SpeechStreamUsage{InputCharacters: &chars}, ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"k": "v"}}},
		)
		return r, nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x"})
	usage, err := res.Usage()
	if err != nil || usage.InputCharacters == nil || *usage.InputCharacters != 12 {
		t.Fatalf("usage = %+v err=%v", usage, err)
	}
	warnings, _ := res.Warnings()
	if len(warnings) != 1 || warnings[0].Message != "w" {
		t.Fatalf("warnings = %+v", warnings)
	}
	responses, _ := res.Responses()
	if len(responses) != 1 || responses[0].ModelID != "resp-model" || !responses[0].Timestamp.Equal(ts) || responses[0].Headers["a"] != "b" {
		t.Fatalf("responses = %+v", responses)
	}
	pm, _ := res.ProviderMetadata()
	if pm["p"].(map[string]interface{})["k"] != "v" {
		t.Fatalf("providerMetadata = %v", pm)
	}
}

func TestStreamSpeech_NoAudioFailures(t *testing.T) {
	cases := map[string][]provider.SpeechStreamPart{
		"finish without audio":     {finishPart()},
		"ends without finish":      {},
		"error then no audio":      {{Type: provider.SpeechStreamPartTypeError, Err: "upstream"}},
		"error then finish":        {{Type: provider.SpeechStreamPartTypeError, Err: "upstream"}, finishPart()},
		"stream-start only closes": {{Type: provider.SpeechStreamPartTypeStreamStart}},
	}
	for name, parts := range cases {
		t.Run(name, func(t *testing.T) {
			model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
				return speechParts(parts...), nil
			}}
			res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x", OnError: func(error) {}})
			_, err := res.Usage()
			var ns *NoSpeechGeneratedError
			if !errors.As(err, &ns) {
				t.Fatalf("err = %v, want NoSpeechGeneratedError", err)
			}
			if strings.HasPrefix(name, "error then") {
				if ns.Cause == nil || ns.Cause.Error() != "upstream" || !errors.Is(err, ns.Cause) {
					t.Fatalf("cause = %v", ns.Cause)
				}
			}
		})
	}
}

func TestStreamSpeech_EndsAfterAudioWithoutFinishSurfacesProviderError(t *testing.T) {
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return speechParts(audioPart("a"), provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeError, Err: "limit hit"}), nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x", OnError: func(error) {}})
	if _, err := res.Usage(); err == nil || err.Error() != "limit hit" {
		t.Fatalf("err = %v", err)
	}

	model2 := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return speechParts(audioPart("a")), nil
	}}
	res2, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model2, Text: "x"})
	_, err := res2.Usage()
	var spe *providererrors.StreamProviderError
	if !errors.As(err, &spe) || spe.Message != "The speech stream ended before it finished." {
		t.Fatalf("err = %v", err)
	}
	if IsNoSpeechGeneratedError(err) {
		t.Fatal("audio was heard; must not be a no-speech error")
	}
}

func TestStreamSpeech_DoStreamErrorRejectsAndCancelsText(t *testing.T) {
	boom := errors.New("setup failed")
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return nil, boom
	}}
	var mu sync.Mutex
	var reason error
	cancelled := make(chan struct{})
	deltas := make(chan string)
	text := NewChannelStringSpeechTextStream(deltas, func(r error) {
		mu.Lock()
		reason = r
		mu.Unlock()
		close(cancelled)
	})
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, TextStream: text})
	if _, err := res.Warnings(); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	<-cancelled
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(reason, boom) {
		t.Fatalf("cancel reason = %v", reason)
	}
}

func TestStreamSpeech_CancelWhileDoStreamPending(t *testing.T) {
	started := make(chan struct{})
	model := &mockStreamingSpeechModel{doStream: func(ctx context.Context, _ *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	res, _ := ExperimentalStreamSpeech(ctx, StreamSpeechOptions{Model: model, Text: "x"})
	full, _ := res.FullStream()
	<-started
	cancel()
	if _, err := full.Next(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next err = %v", err)
	}
}

func TestStreamSpeech_ClosingFullStreamAbortsProviderStream(t *testing.T) {
	bs := newBlockingSpeechStream()
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return &provider.SpeechStreamResult{Stream: bs}, nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x"})
	full, _ := res.FullStream()
	_ = full.Close()
	select {
	case <-bs.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("provider stream was not closed after cancelling the output")
	}
	if _, err := res.Usage(); err == nil || err.Error() != "Speech stream was cancelled." {
		t.Fatalf("Usage err = %v", err)
	}
}

func TestStreamSpeech_StreamOwnership(t *testing.T) {
	model := &mockStreamingSpeechModel{doStream: func(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		return speechParts(audioPart("a"), finishPart()), nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x"})
	if _, err := res.AudioStream(); err != nil {
		t.Fatal(err)
	}
	if _, err := res.FullStream(); err == nil || err.Error() != "Only one of audioStream or fullStream can be accessed, and only once." {
		t.Fatalf("err = %v", err)
	}
	res2, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, Text: "x"})
	if _, err := res2.Usage(); err != nil {
		t.Fatal(err)
	}
	if _, err := res2.FullStream(); err == nil || err.Error() != "fullStream cannot be accessed after a result promise." {
		t.Fatalf("err = %v", err)
	}
	if _, err := res2.AudioStream(); err == nil || err.Error() != "audioStream cannot be accessed after a result promise." {
		t.Fatalf("err = %v", err)
	}
}

type speechModelNoStream struct{}

func (speechModelNoStream) SpecificationVersion() string { return "v4" }
func (speechModelNoStream) Provider() string             { return "mock-provider" }
func (speechModelNoStream) ModelID() string              { return "mock-model-id" }
func (speechModelNoStream) DoGenerate(context.Context, *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	return nil, nil
}

func TestStreamSpeech_UnsupportedModel(t *testing.T) {
	_, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: speechModelNoStream{}, Text: "x"})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "streaming speech" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), `The mock-provider model "mock-model-id" does not support streaming speech.`) {
		t.Fatalf("message = %v", err)
	}
}

func TestStreamSpeech_TextAndTextStreamAreExclusive(t *testing.T) {
	_, err := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{
		Model: &mockStreamingSpeechModel{}, Text: "x",
		TextStream: NewChannelStringSpeechTextStream(make(chan string), nil),
	})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamSpeech_TextPartsCarryVoiceAndProviderOptions(t *testing.T) {
	parts := make(chan SpeechTextPart, 2)
	parts <- SpeechTextPart{Text: "Hi", Voice: "a"}
	parts <- SpeechTextPart{Text: "Yo", Voice: "b", ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{"newTurn": true}}}
	close(parts)
	var seen []provider.SpeechStreamTextPart
	model := &mockStreamingSpeechModel{doStream: func(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
		for {
			p, err := opts.Text.Next(ctx)
			if err != nil {
				break
			}
			seen = append(seen, *p)
		}
		return speechParts(audioPart("a"), finishPart()), nil
	}}
	res, _ := ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: model, TextStream: NewChannelSpeechTextStream(parts, nil)})
	if _, err := res.Usage(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1].Voice != "b" || seen[1].ProviderOptions == nil {
		t.Fatalf("seen = %+v", seen)
	}
}
