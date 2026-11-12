package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94provaSpeechNonObjectFrames: TS onMessageText takes any value
// safeParseJSON accepts (secure-json-parse, so a __proto__ key is dropped as
// unparsable). A primitive or array frame is a raw part and nothing else; a
// null frame is a raw part and then the TypeError of decodeEvent reading
// raw.error.
func TestR94provaSpeechNonObjectFrames(t *testing.T) {
	result, server := startSpeech(t, "eleven_flash_v2_5", &provider.SpeechStreamOptions{Text: speechTexts("Hi"), IncludeRawChunks: true})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- json.RawMessage(`{"__proto__":{"audio":"AQI="},"isFinal":true}`)
	server.toSend <- 5
	server.toSend <- []interface{}{"a"}
	server.toSend <- nil
	parts, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != "Cannot read properties of null (reading 'error')" {
		t.Fatalf("err = %v", err)
	}
	var raws []interface{}
	for _, p := range parts {
		switch p.Type {
		case provider.SpeechStreamPartTypeRaw:
			raws = append(raws, p.RawValue)
		case provider.SpeechStreamPartTypeStreamStart:
		default:
			t.Fatalf("unexpected part %+v", p)
		}
	}
	if want := []interface{}{float64(5), []interface{}{"a"}, nil}; !reflect.DeepEqual(raws, want) {
		t.Fatalf("raw = %#v, want %#v", raws, want)
	}
}

// TestR94provaTranscriptionNullFrame: TS reads raw.message_type of a null
// frame (after its raw part), and the TypeError fails the stream; a
// __proto__ frame does not parse and is skipped.
func TestR94provaTranscriptionNullFrame(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	audio := newBlockingAudioStream()
	result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.toSend <- json.RawMessage(`{"message_type":"session_started","session_id":"s","__proto__":{}}`)
	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "s"}
	server.toSend <- nil
	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err == nil || err.Error() != "Cannot read properties of null (reading 'message_type')" {
		t.Fatalf("err = %v", err)
	}
	var kinds []string
	var lastRaw interface{} = "unset"
	for _, p := range parts {
		kinds = append(kinds, string(p.Type))
		if p.Type == provider.TranscriptionStreamPartTypeRaw {
			lastRaw = p.RawValue
		}
	}
	if want := []string{"raw", "stream-start", "raw"}; !reflect.DeepEqual(kinds, want) || lastRaw != nil {
		t.Fatalf("parts = %v, last raw = %#v", kinds, lastRaw)
	}
}

// TestR94provaTranscriptionSessionID: TS ids are `${sessionId ?? 'session'}:n`
// with sessionId = raw.session_id, so an empty id stays empty and a number
// is its String form.
func TestR94provaTranscriptionSessionID(t *testing.T) {
	for _, tc := range []struct {
		sessionID interface{}
		want      string
	}{{"", ":0"}, {float64(7), "7:0"}, {nil, "session:0"}} {
		server := newRealtimeTestServer(t)
		result, err := newRealtimeTestModel(server.ts.URL).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newBlockingAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": tc.sessionID}
		server.toSend <- map[string]interface{}{"message_type": "partial_transcript", "text": "hi"}
		var id string
		for id == "" {
			part, err := result.Stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			if part.Type == provider.TranscriptionStreamPartTypePartial {
				id = part.ID
			}
		}
		if id != tc.want {
			t.Errorf("session_id %#v: id = %q, want %q", tc.sessionID, id, tc.want)
		}
		_ = result.Stream.Close()
		server.close()
	}
}

// TestR94provaTranscriptionAbortCause: TS onAbort passes abortSignal.reason,
// so a cancel cause is the stream error.
func TestR94provaTranscriptionAbortCause(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	ctx, cancel := context.WithCancelCause(context.Background())
	result, err := newRealtimeTestModel(server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio:            newBlockingAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.toSend <- map[string]interface{}{"message_type": "session_started", "session_id": "s"}
	if _, err := result.Stream.Next(); err != nil {
		t.Fatal(err)
	}
	reason := errors.New("r94prova user stop")
	cancel(reason)
	_, err = drainUntilFinishOrError(t, result.Stream)
	if err != reason {
		t.Fatalf("err = %v, want the cancel cause", err)
	}
}
