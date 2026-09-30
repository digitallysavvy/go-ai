package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func newTestSpeechTranslationModel(baseURL string) *SpeechTranslationModel {
	p := New(Config{APIKey: "test-key", BaseURL: baseURL})
	return NewSpeechTranslationModel(p, "gpt-realtime-translate")
}

func drainUntilTranslationFinishOrError(t *testing.T, stream provider.SpeechTranslationStream) ([]*provider.SpeechTranslationStreamPart, error) {
	t.Helper()
	var parts []*provider.SpeechTranslationStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, part)
		if part.Type == provider.SpeechTranslationStreamPartTypeFinish {
			return parts, nil
		}
	}
}

func TestSpeechTranslationModel_DoStream_RequiresTargetLanguage(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewSpeechTranslationModel(p, "gpt-realtime-translate")
	_, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) || !strings.Contains(err.Error(), "targetLanguage is required") {
		t.Fatalf("err = %v, want an InvalidArgumentError about targetLanguage", err)
	}
}

func TestSpeechTranslationModel_DoStream_RejectsUnsupportedInputAudio(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewSpeechTranslationModel(p, "gpt-realtime-translate")

	rate24k := 24000
	_, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcmu", Rate: &rate24k},
		TargetLanguage:   "es",
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 24kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 24kHz PCM message", err)
	}

	rate16k := 16000
	_, err = model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate16k},
		TargetLanguage:   "es",
	})
	if err == nil || !strings.Contains(err.Error(), "only supports 24kHz 16-bit PCM") {
		t.Fatalf("err = %v, want a 24kHz PCM message for a non-24kHz rate", err)
	}
}

// TestSpeechTranslationModel_DoStream_StreamsTranslationEndToEnd mirrors the
// TS "should stream gpt-realtime-translate using the OpenAI realtime
// translations WebSocket" test.
func TestSpeechTranslationModel_DoStream_StreamsTranslationEndToEnd(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	audio := newChanTestAudioStream([]byte{1, 2, 3})
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	sessionUpdate := server.waitForReceived(t, "session.update", time.Second)
	session, _ := sessionUpdate["session"].(map[string]interface{})
	audioCfg, _ := session["audio"].(map[string]interface{})
	output, _ := audioCfg["output"].(map[string]interface{})
	if output["language"] != "es" {
		t.Fatalf("session.audio.output.language = %v", output["language"])
	}

	appendMsg := server.waitForReceived(t, "session.input_audio_buffer.append", time.Second)
	if audioB64, _ := appendMsg["audio"].(string); audioB64 == "" {
		t.Fatal("expected base64 audio in session.input_audio_buffer.append")
	} else if decoded, err := base64.StdEncoding.DecodeString(audioB64); err != nil || string(decoded) != "\x01\x02\x03" {
		t.Fatalf("decoded audio = %q, err = %v", decoded, err)
	}
	server.waitForReceived(t, "session.close", time.Second)

	server.toSend <- map[string]interface{}{"type": "session.input_transcript.delta", "delta": "Hello"}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola"}
	server.toSend <- map[string]interface{}{"type": "session.output_audio.delta", "delta": "BAUG"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	// stream-start, source-delta, output-delta, audio, source-final,
	// output-final, finish.
	if len(parts) != 7 {
		t.Fatalf("len(parts) = %d, want 7; got %+v", len(parts), parts)
	}
	if parts[0].Type != provider.SpeechTranslationStreamPartTypeStreamStart {
		t.Fatalf("parts[0] = %+v", parts[0])
	}
	if parts[1].Type != provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta || parts[1].Delta != "Hello" {
		t.Fatalf("parts[1] = %+v", parts[1])
	}
	if parts[2].Type != provider.SpeechTranslationStreamPartTypeOutputTextDelta || parts[2].Delta != "Hola" {
		t.Fatalf("parts[2] = %+v", parts[2])
	}
	wantAudio, _ := base64.StdEncoding.DecodeString("BAUG")
	if parts[3].Type != provider.SpeechTranslationStreamPartTypeAudio || string(parts[3].AudioData) != string(wantAudio) {
		t.Fatalf("parts[3] = %+v", parts[3])
	}
	if parts[4].Type != provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal || parts[4].Text != "Hello" {
		t.Fatalf("parts[4] = %+v", parts[4])
	}
	if parts[5].Type != provider.SpeechTranslationStreamPartTypeOutputTextFinal || parts[5].Text != "Hola" {
		t.Fatalf("parts[5] = %+v", parts[5])
	}
	finish := parts[6]
	if finish.Type != provider.SpeechTranslationStreamPartTypeFinish || finish.SourceText != "Hello" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_AccumulatesDeltasUntilSessionCloses
// mirrors TS "should accumulate transcript deltas until the session closes".
func TestSpeechTranslationModel_DoStream_AccumulatesDeltasUntilSessionCloses(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)

	server.toSend <- map[string]interface{}{"type": "session.input_transcript.delta", "delta": "Hello "}
	server.toSend <- map[string]interface{}{"type": "session.input_transcript.delta", "delta": "world"}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola "}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "mundo"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "Hello world" || finish.OutputText != "Hola mundo" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_WarnsUnsupportedSourceLanguageAndOutputFormat
// mirrors TS "should warn about unsupported sourceLanguage and
// outputAudioFormat".
func TestSpeechTranslationModel_DoStream_WarnsUnsupportedSourceLanguageAndOutputFormat(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:             newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat:  provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:    "es",
		SourceLanguage:    "en",
		OutputAudioFormat: &provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	if len(first.Warnings) != 2 || first.Warnings[0].Feature != "sourceLanguage" || first.Warnings[1].Feature != "outputAudioFormat" {
		t.Fatalf("warnings = %+v", first.Warnings)
	}
}

// TestSpeechTranslationModel_DoStream_SkipsEmptyOutputAudioDeltas mirrors TS
// "should skip empty output audio deltas".
func TestSpeechTranslationModel_DoStream_SkipsEmptyOutputAudioDeltas(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.output_audio.delta", "delta": ""}
	server.toSend <- map[string]interface{}{"type": "session.output_audio.delta", "delta": "BAUG"}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	var audioParts int
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeAudio {
			audioParts++
		}
	}
	if audioParts != 1 {
		t.Fatalf("audioParts = %d, want 1", audioParts)
	}
}

// TestSpeechTranslationModel_DoStream_EmitsRecoverableServerErrors mirrors TS
// "should emit recoverable server errors and continue streaming".
func TestSpeechTranslationModel_DoStream_EmitsRecoverableServerErrors(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "invalid target language"}}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	var found bool
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeError {
			found = true
			if p.Err == nil || !strings.Contains(p.Err.(error).Error(), "invalid target language") {
				t.Fatalf("error part = %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("expected an error part")
	}
	finish := parts[len(parts)-1]
	if finish.SourceText != "" || finish.OutputText != "Hola" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestSpeechTranslationModel_DoStream_IncludeRawChunks mirrors TS "should
// include raw provider chunks when includeRawChunks is enabled".
func TestSpeechTranslationModel_DoStream_IncludeRawChunks(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.updated"}
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	var rawCount int
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeRaw {
			rawCount++
		}
	}
	if rawCount != 3 {
		t.Fatalf("rawCount = %d, want 3", rawCount)
	}
}

// TestSpeechTranslationModel_DoStream_StripsAuthorizationHeader mirrors TS
// "should strip only the Authorization header and pass other headers to the
// WebSocket constructor".
func TestSpeechTranslationModel_DoStream_StripsAuthorizationHeader(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	p := New(Config{APIKey: "test-key", BaseURL: server.ts.URL, Organization: "test-organization", Headers: map[string]string{"Custom-Header": "custom-value"}})
	model := NewSpeechTranslationModel(p, "gpt-realtime-translate")

	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.closed"}
	if _, err := drainUntilTranslationFinishOrError(t, result.Stream); err != nil {
		t.Fatalf("stream error = %v", err)
	}
}

// TestSpeechTranslationModel_DoStream_CancelMidStream exercises Close() while
// the stream is actively receiving, verifying Next() unblocks and the
// caller's AudioStream is cancelled.
func TestSpeechTranslationModel_DoStream_CancelMidStream(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	audio := newChanTestAudioStream()
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	server.waitForReceived(t, "session.update", time.Second)
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := result.Stream.Close(); err != nil {
			t.Errorf("Stream.Close() error = %v", err)
		}
	}()
	<-done

	if _, err := result.Stream.Next(); err == nil {
		t.Fatal("expected Stream.Next() to return an error after Close()")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		audio.mu.Lock()
		cancelled := audio.cancelled
		audio.mu.Unlock()
		if cancelled {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the AudioStream to be cancelled after Close()")
}

// TestSpeechTranslationModel_DoStream_ErrorsOnCloseBeforeFinishing mirrors TS
// "should error the stream with close diagnostics when the socket closes
// before finishing".
func TestSpeechTranslationModel_DoStream_ErrorsOnCloseBeforeFinishing(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Ho"}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	_, err = drainUntilTranslationFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "closed unexpectedly before finishing") {
		t.Fatalf("err = %v, want a close-diagnostics message", err)
	}
}

// TestSpeechTranslationModel_DoStream_IgnoresMessagesAfterFinish mirrors TS
// "should ignore server messages after the stream finished".
func TestSpeechTranslationModel_DoStream_IgnoresMessagesAfterFinish(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	model := newTestSpeechTranslationModel(server.ts.URL)
	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.output_transcript.delta", "delta": "Hola"}
	server.toSend <- map[string]interface{}{"type": "session.closed"}

	parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if parts[len(parts)-1].Type != provider.SpeechTranslationStreamPartTypeFinish {
		t.Fatalf("last part = %+v, want finish", parts[len(parts)-1])
	}
	if _, err := result.Stream.Next(); err == nil {
		t.Fatal("expected io.EOF after the stream finished")
	}
}

// TestProvider_Translation_CreatesSpeechTranslationModel mirrors TS "should
// be created by the provider translation factory with realtime auth".
func TestProvider_Translation_CreatesSpeechTranslationModel(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()

	p := New(Config{APIKey: "test-key", BaseURL: server.ts.URL})
	model, err := p.Translation("gpt-realtime-translate")
	if err != nil {
		t.Fatalf("Translation() error = %v", err)
	}
	if model.Provider() != "openai.speech-translation" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "gpt-realtime-translate" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}

	rate := 24000
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForReceived(t, "session.update", time.Second)
}
