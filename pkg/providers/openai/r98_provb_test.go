package openai

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// r98provbDrain reads a stream to its end and returns the parts and the
// final error (nil at a clean end).
func r98provbDrain(t *testing.T, s provider.TextStream) ([]*provider.StreamChunk, error) {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, c)
	}
}

// TestR98provbChatErrorKeyOnDataChunk: TS openaiChatChunkSchema tries the
// data member first, and its z.object strips an `error` key, so a frame with
// `choices` and an `error` member (null or an object) is a data chunk: the
// text is streamed and `'error' in value` is false. Go routed it as an error
// (a failed call before output).
func TestR98provbChatErrorKeyOnDataChunk(t *testing.T) {
	for _, errValue := range []string{`null`, `{"message":"x"}`} {
		sse := `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"error":` + errValue + "}\n\n"
		s := newOpenAIStream(io.NopCloser(strings.NewReader(sse)))
		parts, err := r98provbDrain(t, s)
		if err != nil {
			t.Fatalf("error %s: err = %v, want the data chunk", errValue, err)
		}
		text, finish := "", ""
		for _, p := range parts {
			switch p.Type {
			case provider.ChunkTypeText:
				text += p.Text
			case provider.ChunkTypeFinish:
				finish = string(p.FinishReason)
			case provider.ChunkTypeError:
				t.Errorf("error %s: unexpected error part %v", errValue, p.Err)
			}
		}
		if text != "hi" || finish != "stop" {
			t.Errorf("error %s: text %q finish %q, want hi / stop", errValue, text, finish)
		}
	}
	// A frame that fails the data member stays an error (TS error member).
	s := newOpenAIStream(io.NopCloser(strings.NewReader("data: {\"error\":{\"message\":\"boom\"}}\n\n")))
	if _, err := r98provbDrain(t, s); err == nil {
		t.Errorf("error frame: no error, want the stream error")
	}
}

// TestR98provbCompletionErrorKeyOnDataChunk: the completion chunk union's
// data member is a z.object too (OpenAI and openai-compatible), so the same
// frame is a data chunk.
func TestR98provbCompletionErrorKeyOnDataChunk(t *testing.T) {
	for _, compat := range []bool{false, true} {
		sse := `data: {"id":"c1","choices":[{"text":"hi","index":0,"finish_reason":"stop"}],"error":null}` + "\n\n"
		s := newCompletionStream(io.NopCloser(strings.NewReader(sse)), false)
		s.compat = compat
		parts, err := r98provbDrain(t, s)
		if err != nil {
			t.Fatalf("compat %v: err = %v", compat, err)
		}
		text := ""
		for _, p := range parts {
			if p.Type == provider.ChunkTypeError {
				t.Errorf("compat %v: unexpected error part %v", compat, p.Err)
			}
			if p.Type == provider.ChunkTypeText {
				text += p.Text
			}
		}
		if text != "hi" {
			t.Errorf("compat %v: text %q, want hi", compat, text)
		}
	}
}

// TestR98provbServerConnectionURLBeforeIntent: TS buildServerUrl parses
// new URL(baseURL) before its intent switch, so an invalid base URL fails
// with the URL error even when the intent is unknown too.
func TestR98provbServerConnectionURLBeforeIntent(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: "not a url"}), "gpt-realtime")
	_, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: "translation"})
	if err == nil || strings.Contains(err.Error(), "intent") {
		t.Fatalf("err = %v, want the invalid URL error", err)
	}
}

// TestR98provbResponsesJSNumberPlatformInt: a numeric option is sent as the
// JS number; an integral value outside the platform int (32-bit targets)
// stays a float64 instead of overflowing the int conversion.
func TestR98provbResponsesJSNumberPlatformInt(t *testing.T) {
	for in, want := range map[float64]string{3e9: "3000000000", -3e9: "-3000000000", 7: "7", 2.5: "2.5", 1e16: "10000000000000000"} {
		v, ok := responsesJSNumber(in)
		b, _ := jsonstr.Marshal(v)
		if !ok || string(b) != want {
			t.Errorf("%v: %s, want %s", in, b, want)
		}
	}
}

// TestR98provbLiveMsPlatformInt: a Live start_ms / end_ms / offset_ms beyond
// the platform int is clamped instead of an implementation-defined
// conversion (a negative value on amd64, overflow on 32-bit targets).
func TestR98provbLiveMsPlatformInt(t *testing.T) {
	events := parseOpenAILiveServerEvent([]byte(`{"type":"session.output_transcript.delta","delta":"x","start_ms":1,"end_ms":1e19}`))
	if len(events) != 1 || events[0].EndMs == nil || *events[0].EndMs != math.MaxInt || *events[0].StartMs != 1 {
		t.Fatalf("events = %+v, want end clamped to MaxInt", events)
	}
	events = parseOpenAILiveServerEvent([]byte(`{"type":"session.delegation.created","delegation":{"id":"d"},"offset_ms":1e19}`))
	if len(events) != 1 || events[0].OffsetMs == nil || *events[0].OffsetMs != math.MaxInt {
		t.Fatalf("events = %+v, want offset clamped to MaxInt", events)
	}
	if got := liveMsInt(2.9); got != 2 {
		t.Errorf("liveMsInt(2.9) = %d, want 2", got)
	}
}

// r98provbRawFrame returns the first raw client frame whose text starts with
// prefix.
func r98provbRawFrame(t *testing.T, s *realtimeTranscriptionTestServer, want string) string {
	t.Helper()
	s.waitForReceived(t, want, time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, raw := range s.rawReceived {
		if strings.Contains(raw, `"`+want+`"`) {
			return raw
		}
	}
	return ""
}

// TestR98provbRealtimeFrameKeyOrder: TS sends JSON.stringify of object
// literals, so the session.update and append frames keep their literal key
// order (type first). Go marshaled maps with sorted keys.
func TestR98provbRealtimeFrameKeyOrder(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()
	rate := 16000
	result, err := newTestTranscriptionModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		ProviderOptions:  map[string]interface{}{"openai": map[string]interface{}{"language": "en"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if got, want := r98provbRawFrame(t, server, "session.update"), `{"type":"session.update","session":{"type":"transcription","audio":{"input":{"format":{"type":"audio/pcm","rate":16000},"transcription":{"model":"gpt-realtime-whisper","language":"en"},"turn_detection":null}}}}`; got != want {
		t.Errorf("session.update = %s\nwant %s", got, want)
	}
	if got, want := r98provbRawFrame(t, server, "input_audio_buffer.append"), `{"type":"input_audio_buffer.append","audio":"AQID"}`; got != want {
		t.Errorf("append = %s, want %s", got, want)
	}

	server2 := newRealtimeTranscriptionTestServer(t)
	defer server2.close()
	rate2 := 24000
	res2, err := newTestSpeechTranslationModel(server2.ts.URL).DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanTestAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate2},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Stream.Close() //nolint:errcheck
	if got, want := r98provbRawFrame(t, server2, "session.update"), `{"type":"session.update","session":{"audio":{"input":{"transcription":{"model":"gpt-realtime-whisper"},"noise_reduction":null},"output":{"language":"es"}}}}`; got != want {
		t.Errorf("translation session.update = %s\nwant %s", got, want)
	}
	if got, want := r98provbRawFrame(t, server2, "session.input_audio_buffer.append"), `{"type":"session.input_audio_buffer.append","audio":"AQID"}`; got != want {
		t.Errorf("translation append = %s, want %s", got, want)
	}
}

// TestR98provbRealtimeClientEventKeyOrder: the GA realtime, Live and client
// secret payloads keep the TS object literal key order.
func TestR98provbRealtimeClientEventKeyOrder(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "gpt-realtime")
	instructions, voice, threshold := "Hi", "alloy", 0.5
	raw, err := m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-update", EventID: "e1", Config: provider.RealtimeSessionConfig{
		Instructions: &instructions, Voice: &voice, OutputModalities: []string{"audio"},
		TurnDetection: &provider.RealtimeTurnDetection{Type: "server-vad", Threshold: &threshold},
		Tools:         []provider.RealtimeToolDefinition{{Name: "f", Parameters: map[string]interface{}{"type": "object"}}},
	}})
	want := `{"type":"session.update","session":{"type":"realtime","model":"gpt-realtime","instructions":"Hi","output_modalities":["audio"],"audio":{"input":{"turn_detection":{"type":"server_vad","threshold":0.5}},"output":{"voice":"alloy"}},"tools":[{"type":"function","name":"f","parameters":{"type":"object"}}],"tool_choice":"auto"},"event_id":"e1"}`
	if err != nil || string(raw) != want {
		t.Errorf("session-update = %s, %v\nwant %s", raw, err, want)
	}
	raw, _ = m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "conversation-item-truncate", ItemID: "i", ContentIndex: 0, AudioEndMs: 5})
	if want := `{"type":"conversation.item.truncate","item_id":"i","content_index":0,"audio_end_ms":5}`; string(raw) != want {
		t.Errorf("truncate = %s", raw)
	}
	raw, _ = m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "conversation-item-create", Item: provider.RealtimeConversationItem{Type: "function-call-output", CallID: "c", Output: "o"}})
	if want := `{"type":"conversation.item.create","item":{"type":"function_call_output","call_id":"c","output":"o"}}`; string(raw) != want {
		t.Errorf("item create = %s", raw)
	}
	live := NewRealtimeModelLive(New(Config{APIKey: "k"}), "gpt-live-1")
	raw, err = live.SerializeClientEvent(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "AA==", EventID: "e"})
	if want := `{"type":"session.input_audio.append","audio":"AA==","event_id":"e"}`; err != nil || string(raw) != want {
		t.Errorf("live append = %s, %v", raw, err)
	}

	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"value":"v"}`)
	}))
	defer srv.Close()
	secs := 60
	if _, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-realtime").DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{ExpiresAfterSeconds: &secs}); err != nil {
		t.Fatal(err)
	}
	if want := `{"session":{"type":"realtime","model":"gpt-realtime"},"expires_after":{"anchor":"created_at","seconds":60}}`; body != want {
		t.Errorf("client secret body = %s, want %s", body, want)
	}
}
