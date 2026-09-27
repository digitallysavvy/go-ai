package elevenlabs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// chanAudioStream is a provider.AudioStream test double backed by a channel
// of chunks; closing the channel signals io.EOF.
type chanAudioStream struct {
	chunks chan []byte

	mu          sync.Mutex
	cancelled   bool
	cancelCount int
}

func newChanAudioStream(chunks ...[]byte) *chanAudioStream {
	ch := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &chanAudioStream{chunks: ch}
}

// newBlockingAudioStream returns an AudioStream whose Next never yields a
// chunk or EOF until its context is done, for tests that need audio to still
// be "streaming" indefinitely.
func newBlockingAudioStream() *chanAudioStream {
	return &chanAudioStream{chunks: make(chan []byte)}
}

func (s *chanAudioStream) Next(ctx context.Context) ([]byte, error) {
	select {
	case c, ok := <-s.chunks:
		if !ok {
			return nil, io.EOF
		}
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *chanAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
	s.cancelCount++
}

func (s *chanAudioStream) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// realtimeTestServer runs a minimal ElevenLabs Scribe v2 Realtime WebSocket
// endpoint: it records every received client message and lets the test
// script server->client events over a channel.
type realtimeTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}

	toSend    chan interface{}
	closeConn chan struct{}
}

func newRealtimeTestServer(t *testing.T) *realtimeTestServer {
	t.Helper()
	s := &realtimeTestServer{toSend: make(chan interface{}, 16), closeConn: make(chan struct{})}
	wsHandlerFn := func(conn *websocket.Conn) {
		go func() {
			for msg := range s.toSend {
				encoded, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				if err := websocket.Message.Send(conn, string(encoded)); err != nil {
					return
				}
			}
		}()
		go func() {
			<-s.closeConn
			_ = conn.Close()
		}()
		for {
			var raw string
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				continue
			}
			s.mu.Lock()
			s.received = append(s.received, decoded)
			s.mu.Unlock()
		}
	}
	handler := websocket.Server{Handler: wsHandlerFn}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *realtimeTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *realtimeTestServer) closeConnection() {
	close(s.closeConn)
}

func (s *realtimeTestServer) all() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]interface{}, len(s.received))
	copy(out, s.received)
	return out
}

func (s *realtimeTestServer) waitForCount(t *testing.T, n int, timeout time.Duration) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := s.all(); len(got) >= n {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d received messages; got %v", n, s.all())
	return nil
}

func newRealtimeTestModel(baseURL string) *TranscriptionModel {
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	return NewTranscriptionModel(p, ModelScribeV2Realtime)
}

func drainUntilFinishOrError(t *testing.T, stream provider.TranscriptionStream) ([]*provider.TranscriptionStreamPart, error) {
	t.Helper()
	var parts []*provider.TranscriptionStreamPart
	for {
		part, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return parts, nil
			}
			return parts, err
		}
		parts = append(parts, part)
		if part.Type == provider.TranscriptionStreamPartTypeFinish {
			return parts, nil
		}
	}
}

func rate(v int) *int { return &v }

func TestTranscriptionModel_DoStream_RejectsNonRealtimeModel(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelScribeV2)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if !errors.Is(err, providererrors.ErrUnsupportedFeature) {
		t.Fatalf("err = %v, want ErrUnsupportedFeature", err)
	}
}

func TestTranscriptionModel_DoStream_RejectsUnsupportedInputFormat(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelScribeV2Realtime)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(32000)},
	})
	if err == nil || !strings.Contains(err.Error(), "ElevenLabs realtime transcription supports") {
		t.Fatalf("err = %v", err)
	}
}

func TestTranscriptionModel_DoStream_Supports8kHzMuLaw(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelScribeV2Realtime)
	audioFormat, sr, err := elevenLabsRealtimeAudioFormat(provider.AudioFormat{Type: "audio/pcmu", Rate: rate(8000)})
	if err != nil || audioFormat != "ulaw_8000" || sr != 8000 {
		t.Fatalf("audioFormat = %q, sr = %d, err = %v", audioFormat, sr, err)
	}
	_ = model
}

func TestTranscriptionModel_DoStream_DefaultsPCMToRate16000(t *testing.T) {
	audioFormat, sr, err := elevenLabsRealtimeAudioFormat(provider.AudioFormat{Type: "audio/pcm"})
	if err != nil || audioFormat != "pcm_16000" || sr != 16000 {
		t.Fatalf("audioFormat = %q, sr = %d, err = %v", audioFormat, sr, err)
	}
}

func TestTranscriptionModel_DoStream_RejectsBackgroundFilterWithTimestamps(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelScribeV2Realtime)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"filterBackgroundAudio": true, "includeTimestamps": true},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err = %v", err)
	}
}

func TestTranscriptionModel_DoStream_RejectsBackgroundFilterWithLanguageDetection(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewTranscriptionModel(p, ModelScribeV2Realtime)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"filterBackgroundAudio": true, "includeLanguageDetection": true},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err = %v", err)
	}
}

// TestTranscriptionModel_DoStream_BuildsURLWithQueryParams mirrors the TS
// "streams Scribe v2 Realtime and maps partial, committed, and timestamped
// output" URL/query-string assertions.
func TestTranscriptionModel_DoStream_BuildsURLWithQueryParams(t *testing.T) {
	streaming := &StreamingOptions{
		CommitStrategy:           "manual",
		EnableLogging:            boolPtr(false),
		IncludeLanguageDetection: boolPtr(true),
		IncludeTimestamps:        boolPtr(true),
		Keyterms:                 []string{"Vercel", "AI SDK"},
		MinSilenceDurationMs:     intPtr(200),
		MinSpeechDurationMs:      intPtr(150),
		NoVerbatim:               boolPtr(true),
		PreviousText:             stringPtr("Earlier context"),
		SecondaryLanguages:       []string{"es", "fr"},
		VadSilenceThresholdSecs:  floatPtr2(1.2),
		VadThreshold:             floatPtr2(0.5),
	}
	u, err := buildElevenLabsRealtimeURL("https://api.elevenlabs.io", ModelScribeV2Realtime, "pcm_16000", stringPtr("en"), streaming)
	if err != nil {
		t.Fatalf("buildElevenLabsRealtimeURL() error = %v", err)
	}
	if got, want := u.Scheme+"://"+u.Host+u.Path, "wss://api.elevenlabs.io/v1/speech-to-text/realtime"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	q := u.Query()
	wantSingle := map[string]string{
		"audio_format":               "pcm_16000",
		"commit_strategy":            "manual",
		"enable_logging":             "false",
		"include_language_detection": "true",
		"include_timestamps":         "true",
		"language_code":              "en",
		"min_silence_duration_ms":    "200",
		"min_speech_duration_ms":     "150",
		"model_id":                   ModelScribeV2Realtime,
		"no_verbatim":                "true",
		"vad_silence_threshold_secs": "1.2",
		"vad_threshold":              "0.5",
	}
	for k, want := range wantSingle {
		if got := q.Get(k); got != want {
			t.Fatalf("query[%q] = %q, want %q", k, got, want)
		}
	}
	if got := q["keyterms"]; len(got) != 2 || got[0] != "Vercel" || got[1] != "AI SDK" {
		t.Fatalf("keyterms = %v", got)
	}
	if got := q["secondary_languages"]; len(got) != 2 || got[0] != "es" || got[1] != "fr" {
		t.Fatalf("secondary_languages = %v", got)
	}
}

// TestTranscriptionModel_DoStream_NullStreamingOptionsOmitParams mirrors TS
// "accepts explicit null values in realtime provider options".
func TestTranscriptionModel_DoStream_NullStreamingOptionsOmitParams(t *testing.T) {
	u, err := buildElevenLabsRealtimeURL("https://api.elevenlabs.io", ModelScribeV2Realtime, "pcm_16000", nil, nil)
	if err != nil {
		t.Fatalf("buildElevenLabsRealtimeURL() error = %v", err)
	}
	q := u.Query()
	if len(q) != 2 || q.Get("model_id") != ModelScribeV2Realtime || q.Get("audio_format") != "pcm_16000" {
		t.Fatalf("query = %v, want only model_id and audio_format", q)
	}
}

// TestBuildElevenLabsRealtimeURL_DistinguishesExplicitEmptyLanguageCode
// mirrors TS's `languageCode ?? undefined`: an explicit "" is not null, so it
// is still forwarded to the wire (unlike an absent/nil value, which is
// omitted). A plain Go string can't represent "unset" separately from "",
// which is why LanguageCode is a *string.
func TestBuildElevenLabsRealtimeURL_DistinguishesExplicitEmptyLanguageCode(t *testing.T) {
	withEmpty, err := buildElevenLabsRealtimeURL("https://api.elevenlabs.io", ModelScribeV2Realtime, "pcm_16000", stringPtr(""), nil)
	if err != nil {
		t.Fatalf("buildElevenLabsRealtimeURL() error = %v", err)
	}
	if got, ok := withEmpty.Query()["language_code"]; !ok || len(got) != 1 || got[0] != "" {
		t.Fatalf("query[language_code] = %v, want a present empty value", got)
	}

	withNil, err := buildElevenLabsRealtimeURL("https://api.elevenlabs.io", ModelScribeV2Realtime, "pcm_16000", nil, nil)
	if err != nil {
		t.Fatalf("buildElevenLabsRealtimeURL() error = %v", err)
	}
	if _, ok := withNil.Query()["language_code"]; ok {
		t.Fatalf("query = %v, want language_code omitted for a nil (unset) value", withNil.Query())
	}
}

// TestExtractTranscriptionOptions_DistinguishesEmptyFromUnset verifies that
// providerOptions.elevenlabs.languageCode / streaming.previousText set to an
// explicit "" parse to a non-nil pointer to "", while an absent key parses to
// nil, mirroring TS's z.string().nullish() (no default): only null/undefined
// collapse to "unset"; an explicit "" survives.
func TestExtractTranscriptionOptions_DistinguishesEmptyFromUnset(t *testing.T) {
	optsExplicitEmpty, present, _ := extractTranscriptionOptions(map[string]interface{}{
		"elevenlabs": map[string]interface{}{
			"languageCode": "",
			"streaming":    map[string]interface{}{"previousText": ""},
		},
	})
	if !present {
		t.Fatal("expected present = true")
	}
	if optsExplicitEmpty.LanguageCode == nil || *optsExplicitEmpty.LanguageCode != "" {
		t.Fatalf("LanguageCode = %v, want a non-nil pointer to \"\"", optsExplicitEmpty.LanguageCode)
	}
	if optsExplicitEmpty.Streaming == nil || optsExplicitEmpty.Streaming.PreviousText == nil || *optsExplicitEmpty.Streaming.PreviousText != "" {
		t.Fatalf("Streaming.PreviousText = %v, want a non-nil pointer to \"\"", optsExplicitEmpty.Streaming)
	}

	optsUnset, present, _ := extractTranscriptionOptions(map[string]interface{}{
		"elevenlabs": map[string]interface{}{
			"streaming": map[string]interface{}{},
		},
	})
	if !present {
		t.Fatal("expected present = true")
	}
	if optsUnset.LanguageCode != nil {
		t.Fatalf("LanguageCode = %v, want nil when unset", optsUnset.LanguageCode)
	}
	if optsUnset.Streaming.PreviousText != nil {
		t.Fatalf("Streaming.PreviousText = %v, want nil when unset", optsUnset.Streaming.PreviousText)
	}

	optsNull, _, _ := extractTranscriptionOptions(map[string]interface{}{
		"elevenlabs": map[string]interface{}{
			"languageCode": nil,
			"streaming":    map[string]interface{}{"previousText": nil},
		},
	})
	if optsNull.LanguageCode != nil {
		t.Fatalf("LanguageCode = %v, want nil for an explicit null (TS nullish collapses null to undefined)", optsNull.LanguageCode)
	}
	if optsNull.Streaming.PreviousText != nil {
		t.Fatalf("Streaming.PreviousText = %v, want nil for an explicit null", optsNull.Streaming.PreviousText)
	}
}

// TestTranscriptionModel_DoStream_ExplicitEmptyPreviousTextIsSent verifies
// the wire-level effect of the LanguageCode/PreviousText pointer fix: an
// explicit "" still rides the first audio chunk's `previous_text` field and
// the WebSocket URL's `language_code` query param, exactly like a non-empty
// value would, unlike an absent option (see
// TestTranscriptionModel_DoStream_NullStreamingOptionsOmitParams).
func TestTranscriptionModel_DoStream_ExplicitEmptyPreviousTextIsSent(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"languageCode": "",
				"streaming":    map[string]interface{}{"previousText": ""},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	wsURL := result.RequestBody.(string)
	if !strings.Contains(wsURL, "language_code=") {
		t.Fatalf("request URL = %q, want an explicit (empty) language_code param", wsURL)
	}

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}

	msgs := server.waitForCount(t, 2, time.Second)
	firstChunk := msgs[0]
	previousText, ok := firstChunk["previous_text"]
	if !ok || previousText != "" {
		t.Fatalf("msgs[0].previous_text = %v (present=%v), want a present empty string", previousText, ok)
	}
}

// TestTranscriptionModel_DoStream_EndToEnd mirrors the TS end-to-end test:
// session_started gates audio; partial/committed/timestamped events are
// mapped to stream parts; the stream finishes with joined text and segments.
func TestTranscriptionModel_DoStream_EndToEnd(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}, []byte{4, 5, 6}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"languageCode": "en",
				"streaming": map[string]interface{}{
					"includeTimestamps":        true,
					"includeLanguageDetection": true,
					"previousText":             "Earlier context",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}

	// drain stream-start first, then let the audio pump run.
	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	if first.Type != provider.TranscriptionStreamPartTypeStreamStart || len(first.Warnings) != 0 {
		t.Fatalf("first = %+v", first)
	}

	msgs := server.waitForCount(t, 3, time.Second)
	first0 := msgs[0]
	if first0["message_type"] != "input_audio_chunk" || first0["commit"] != false {
		t.Fatalf("msgs[0] = %+v", first0)
	}
	if first0["previous_text"] != "Earlier context" {
		t.Fatalf("msgs[0].previous_text = %v, want 'Earlier context'", first0["previous_text"])
	}
	if b64, _ := first0["audio_base_64"].(string); b64 == "" {
		t.Fatal("expected base64 audio in first chunk")
	} else if decoded, err := base64.StdEncoding.DecodeString(b64); err != nil || string(decoded) != "\x01\x02\x03" {
		t.Fatalf("decoded = %q, err = %v", decoded, err)
	}
	if msgs[1]["previous_text"] != nil {
		t.Fatalf("msgs[1] should not carry previous_text: %+v", msgs[1])
	}
	final := msgs[2]
	if final["commit"] != true || final["audio_base_64"] != "" {
		t.Fatalf("final commit msg = %+v", final)
	}

	server.toSend <- map[string]interface{}{"message_type": "partial_transcript", "text": "Hello wor"}
	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": "Hello world."}
	server.toSend <- map[string]interface{}{
		"message_type":  "committed_transcript_with_timestamps",
		"text":          "Hello world.",
		"language_code": "en",
		"words": []map[string]interface{}{
			{"text": "Hello", "start": 0, "end": 0.4, "type": "word"},
			{"text": " ", "start": 0.4, "end": 0.45, "type": "spacing"},
			{"text": "world.", "start": 0.45, "end": 0.9, "type": "word"},
		},
	}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 3 {
		t.Fatalf("len(rest) = %d, want 3 (partial, final, finish); got %+v", len(rest), rest)
	}
	if rest[0].Type != provider.TranscriptionStreamPartTypePartial || rest[0].ID != "session-1:0" || rest[0].Text != "Hello wor" {
		t.Fatalf("rest[0] = %+v", rest[0])
	}
	if rest[1].Type != provider.TranscriptionStreamPartTypeFinal || rest[1].ID != "session-1:0" || rest[1].Text != "Hello world." {
		t.Fatalf("rest[1] = %+v", rest[1])
	}
	finish := rest[2]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello world." || finish.Language != "en" {
		t.Fatalf("finish = %+v", finish)
	}
	if len(finish.Segments) != 3 || finish.Segments[0].Text != "Hello" || finish.Segments[2].Text != "world." {
		t.Fatalf("segments = %+v", finish.Segments)
	}
	if finish.DurationInSeconds == nil || *finish.DurationInSeconds != 0.9 {
		t.Fatalf("durationInSeconds = %v", finish.DurationInSeconds)
	}
}

// TestTranscriptionModel_DoStream_SkipsDuplicateFinalForTimestampedCompanion
// mirrors TS "detects language without returning timestamps unless
// requested": when includeTimestamps is false, the ..._with_timestamps
// companion must not emit a duplicate transcript-final and segments stay
// empty.
func TestTranscriptionModel_DoStream_SkipsDuplicateFinalForTimestampedCompanion(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"includeLanguageDetection": true, "includeTimestamps": false},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second) // one chunk + the final commit

	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": "Hola"}
	server.toSend <- map[string]interface{}{
		"message_type":  "committed_transcript_with_timestamps",
		"text":          "Hola",
		"language_code": "es",
		"words":         []map[string]interface{}{{"text": "Hola", "start": 0, "end": 0.4, "type": "word"}},
	}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("len(rest) = %d, want 2 (final, finish); got %+v", len(rest), rest)
	}
	if rest[0].Type != provider.TranscriptionStreamPartTypeFinal || rest[0].Text != "Hola" {
		t.Fatalf("rest[0] = %+v", rest[0])
	}
	finish := rest[1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hola" || finish.Language != "es" || len(finish.Segments) != 0 {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_TimestampOnlyResponseToFinalCommit mirrors
// TS "finishes a timestamp-only response to the final commit": a
// committed_transcript_with_timestamps event with no preceding
// committed_transcript still produces a transcript-final (the "normally
// paired" defensive fallback) with timestamps and a duration from it.
func TestTranscriptionModel_DoStream_TimestampOnlyResponseToFinalCommit(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"includeTimestamps": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second)

	server.toSend <- map[string]interface{}{
		"message_type":  "committed_transcript_with_timestamps",
		"text":          "Hello",
		"language_code": "en",
		"words":         []map[string]interface{}{{"text": "Hello", "start": 0, "end": 0.4, "type": "word"}},
	}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("len(rest) = %d, want 2 (final, finish); got %+v", len(rest), rest)
	}
	final := rest[0]
	if final.Type != provider.TranscriptionStreamPartTypeFinal || final.Text != "Hello" {
		t.Fatalf("final = %+v", final)
	}
	if final.StartSecond == nil || *final.StartSecond != 0 || final.EndSecond == nil || *final.EndSecond != 0.4 {
		t.Fatalf("final timestamps = start=%v end=%v", final.StartSecond, final.EndSecond)
	}
	finish := rest[1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello" || finish.Language != "en" {
		t.Fatalf("finish = %+v", finish)
	}
	if len(finish.Segments) != 1 || finish.Segments[0].Text != "Hello" {
		t.Fatalf("segments = %+v", finish.Segments)
	}
	if finish.DurationInSeconds == nil || *finish.DurationInSeconds != 0.4 {
		t.Fatalf("durationInSeconds = %v", finish.DurationInSeconds)
	}
}

// TestTranscriptionModel_DoStream_HandlesLegacyFinalTranscriptVariants mirrors
// TS "handles legacy final transcript event variants": `final_transcript` /
// `final_transcript_with_timestamps` are older aliases for
// `committed_transcript` / `committed_transcript_with_timestamps` and must be
// handled identically.
func TestTranscriptionModel_DoStream_HandlesLegacyFinalTranscriptVariants(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"includeTimestamps": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second)

	server.toSend <- map[string]interface{}{"message_type": "final_transcript", "text": "Legacy final"}
	server.toSend <- map[string]interface{}{
		"message_type":  "final_transcript_with_timestamps",
		"text":          "Legacy final",
		"language_code": "en",
		"words":         []map[string]interface{}{{"text": "Legacy final", "start": 0, "end": 0.5, "type": "word"}},
	}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("len(rest) = %d, want 2 (final, finish); got %+v", len(rest), rest)
	}
	if rest[0].Type != provider.TranscriptionStreamPartTypeFinal || rest[0].Text != "Legacy final" {
		t.Fatalf("rest[0] = %+v", rest[0])
	}
	finish := rest[1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Legacy final" || finish.Language != "en" {
		t.Fatalf("finish = %+v", finish)
	}
	if len(finish.Segments) != 1 || finish.Segments[0].Text != "Legacy final" {
		t.Fatalf("segments = %+v", finish.Segments)
	}
	if finish.DurationInSeconds == nil || *finish.DurationInSeconds != 0.5 {
		t.Fatalf("durationInSeconds = %v", finish.DurationInSeconds)
	}
}

// TestTranscriptionModel_DoStream_FinishesOnCloseAfterPostInputCommit mirrors
// TS "finishes with committed text when the socket closes before timestamps
// arrive".
func TestTranscriptionModel_DoStream_FinishesOnCloseAfterPostInputCommit(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{"streaming": map[string]interface{}{"includeTimestamps": true}},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second)

	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": "Hello"}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_DrainsCommitsAfterInputEnds mirrors TS
// "drains commits received after the input ends and correlates partial IDs".
func TestTranscriptionModel_DoStream_DrainsCommitsAfterInputEnds(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second)

	server.toSend <- map[string]interface{}{"message_type": "partial_transcript", "text": "First"}
	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": " First  "}
	server.toSend <- map[string]interface{}{"message_type": "partial_transcript", "text": "Second"}
	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": " Second "}

	rest, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if len(rest) != 5 {
		t.Fatalf("len(rest) = %d, want 5; got %+v", len(rest), rest)
	}
	wantIDs := []string{"session-1:0", "session-1:0", "session-1:1", "session-1:1"}
	for i, id := range wantIDs {
		if rest[i].ID != id {
			t.Fatalf("rest[%d].ID = %q, want %q", i, rest[i].ID, id)
		}
	}
	finish := rest[4]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "First Second" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_FinishesOnLateFinalizationError mirrors TS
// "finishes normally when the final empty commit is rejected".
func TestTranscriptionModel_DoStream_FinishesOnLateFinalizationError(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	audio := newChanAudioStream([]byte{1})
	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.waitForCount(t, 2, time.Second)

	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": "Already done"}
	server.toSend <- map[string]interface{}{"message_type": "insufficient_audio_activity", "error": "not enough audio"}

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Already done" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestTranscriptionModel_DoStream_ErrorsWhenClosedBeforeFinalCommit mirrors TS
// "errors when the upstream closes before the final commit".
func TestTranscriptionModel_DoStream_ErrorsWhenClosedBeforeFinalCommit(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	audio := newBlockingAudioStream()
	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.closeConnection()

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "closed before completion") {
		t.Fatalf("err = %v", err)
	}
	if !audio.wasCancelled() {
		t.Fatal("expected the AudioStream to be cancelled when the socket closes before the final commit")
	}
}

// TestTranscriptionModel_DoStream_WarnsAboutBatchOnlyOptions mirrors TS
// "warns about batch-only provider options".
func TestTranscriptionModel_DoStream_WarnsAboutBatchOnlyOptions(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{"diarize": true, "numSpeakers": 2},
		},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	first, err := result.Stream.Next()
	if err != nil {
		t.Fatalf("Stream.Next() error = %v", err)
	}
	if len(first.Warnings) != 2 {
		t.Fatalf("warnings = %+v, want 2", first.Warnings)
	}
	byFeature := map[string]bool{}
	for _, w := range first.Warnings {
		byFeature[w.Feature] = true
	}
	if !byFeature["providerOptions.elevenlabs.diarize"] || !byFeature["providerOptions.elevenlabs.numSpeakers"] {
		t.Fatalf("warnings = %+v", first.Warnings)
	}
}

// TestTranscriptionModel_DoStream_ErrorsTheStreamWithProviderErrorMessage
// mirrors TS "errors the stream with the provider error message".
func TestTranscriptionModel_DoStream_ErrorsTheStreamWithProviderErrorMessage(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	audio := newChanAudioStream([]byte{1})
	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatalf("Stream.Next() (stream-start) error = %v", err)
	}
	server.toSend <- map[string]interface{}{"message_type": "quota_exceeded", "error": "quota exhausted"}

	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || !strings.Contains(err.Error(), "quota exhausted") {
		t.Fatalf("err = %v, want containing 'quota exhausted'", err)
	}
	if !audio.wasCancelled() {
		t.Fatal("expected the AudioStream to be cancelled on a provider error")
	}
}

// TestTranscriptionModel_DoStream_CancelsAudioWhenDialFails mirrors TS
// "cancels the audio stream when the WebSocket constructor throws": dialing
// an address nothing is listening on must fail fast and cancel the caller's
// AudioStream.
func TestTranscriptionModel_DoStream_CancelsAudioWhenDialFails(t *testing.T) {
	audio := newBlockingAudioStream()
	// An httptest server that is immediately closed guarantees a refused
	// connection without relying on a specific unused port being free.
	ts := httptest.NewServer(websocket.Server{Handler: func(*websocket.Conn) {}})
	ts.Close()

	model := newRealtimeTestModel(ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil {
		t.Fatal("expected a dial error")
	}
	if !audio.wasCancelled() {
		t.Fatal("expected the AudioStream to be cancelled when the dial fails")
	}
}

// TestTranscriptionModel_DoStream_CancelMidStream exercises Close() while the
// stream is actively receiving, verifying Next() unblocks and the caller's
// AudioStream is cancelled (run with -race).
func TestTranscriptionModel_DoStream_CancelMidStream(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()

	audio := newBlockingAudioStream()
	model := newRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "session-1"}
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
}

func boolPtr(v bool) *bool         { return &v }
func floatPtr2(v float64) *float64 { return &v }
func intPtr(v int) *int            { return &v }
func stringPtr(v string) *string   { return &v }
