package elevenlabs

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getElevenLabsAudioFormat: rate = Number(rate), kept when finite. Node:
// Number("0x5DC0") = 24000, Number(" 16000\u3000") = 16000, Number("") = 0,
// Number("16k") and Number("inf") are NaN.
func TestR86bprovaAudioFormatRateJSNumber(t *testing.T) {
	for format, want := range map[string]int{"pcm_0x5DC0": 24000, "pcm_ 16000\u3000": 16000, "mp3_": 0, "pcm_1e3": 1000, "pcm_16k": -1, "mp3_inf": -1} {
		got := getElevenLabsAudioFormat(format)
		if want < 0 {
			if got.Rate != nil {
				t.Fatalf("%q: rate %d, want none", format, *got.Rate)
			}
			continue
		}
		if got.Rate == nil || *got.Rate != want {
			t.Fatalf("%q: rate %v, want %d", format, got.Rate, want)
		}
	}
}

func r86bprovaMusicBody(boundary, jsonHeaders, audioHeaders string, audio []byte) []byte {
	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n" + jsonHeaders + "\r\n\r\n" + `{"words_timestamps":null}` + "\r\n")
	b.WriteString("--" + boundary + "\r\n" + audioHeaders + "\r\n\r\n")
	b.Write(audio)
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return b.Bytes()
}

// elevenlabs-music-response.ts regexps with JS semantics: \s includes U+00A0,
// U+3000 and U+FEFF (not U+0085), the /m ^ also follows \r and U+2028, and
// the part type is trimmed with String.prototype.trim. Node, on literal copies
// of the TS regexps: boundary "mb", "mb", null; part types "audio/mpeg",
// "audio/mpeg", "application/json".
func TestR86bprovaMusicResponseJSRegexps(t *testing.T) {
	audio := []byte{1, 2, 3}
	const jsonH, audioH = "Content-Type: application/json", "Content-Type: audio/mpeg"
	for _, tc := range []struct {
		ct, jsonHeaders, audioHeaders string
		ok                            bool
	}{
		{"multipart/mixed;\u00a0boundary=mb", jsonH, audioH, true},
		{"multipart/mixed; boundary=mb\u3000x", jsonH, audioH, true},
		{"multipart/mixed;\u0085boundary=mb", jsonH, audioH, false},
		{"multipart/mixed; boundary=mb", "Content-Type:\ufeff application/JSON\ufeff", "X-A: 1\rContent-Type: audio/mpeg", true},
		{"multipart/mixed; boundary=mb", jsonH, "X-A: 1\u2028content-type: audio/mpeg", true},
	} {
		got, err := parseMusicTimestampsResponse(tc.ct, r86bprovaMusicBody("mb", tc.jsonHeaders, tc.audioHeaders, audio))
		if !tc.ok {
			if err == nil || !strings.Contains(err.Error(), "missing a multipart boundary") {
				t.Fatalf("%q: err = %v", tc.ct, err)
			}
			continue
		}
		if err != nil || !bytes.Equal(got.audio, audio) {
			t.Fatalf("%q %q %q: %+v %v", tc.ct, tc.jsonHeaders, tc.audioHeaders, got, err)
		}
	}
}

// TS (raw.text ?? ”).trim() and finalTexts.join(' ').trim() are JS trims:
// U+FEFF is trimmed, U+0085 is kept.
func TestR86bprovaRealtimeTranscriptJSTrim(t *testing.T) {
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

	server.toSend <- map[string]interface{}{"message_type": "committed_transcript", "text": "\ufeffHello\u0085"}
	time.Sleep(20 * time.Millisecond)
	server.closeConnection()

	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err != nil {
		t.Fatalf("stream error = %v", err)
	}
	var final string
	for _, p := range parts {
		if p.Type == provider.TranscriptionStreamPartTypeFinal {
			final = p.Text
		}
	}
	finish := parts[len(parts)-1]
	if final != "Hello\u0085" || finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello\u0085" {
		t.Fatalf("final = %q, finish = %+v", final, finish)
	}
}

// TS toElevenLabsStreamOutputFormat: format.type.split(';')[0].trim().
func TestR86bprovaStreamOutputFormatJSTrim(t *testing.T) {
	got, err := toElevenLabsStreamOutputFormat(&provider.AudioFormat{Type: "\ufeffaudio/mpeg ; x=1"})
	if err != nil || got != "mp3_44100_128" {
		t.Fatalf("got %q, %v", got, err)
	}
}
