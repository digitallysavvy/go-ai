package google

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // WebSocket accept key
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r87eintegRawWSServer completes the WebSocket handshake, waits for the
// client's first frame (setup), then writes frames verbatim and closes the
// TCP connection.
func r87eintegRawWSServer(t *testing.T, frames ...[]byte) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
			base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
		_ = rw.Flush()
		_, _ = bufio.NewReader(rw).ReadByte() // the setup frame
		for _, f := range frames {
			_, _ = rw.Write(f)
		}
		_ = rw.Flush()
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func r87eintegCloseFrame(code uint16, reason string) []byte {
	p := append([]byte{byte(code >> 8), byte(code)}, reason...)
	return append([]byte{0x88, byte(len(p))}, p...)
}

// TS google-transcription-model.ts / google-speech-translation-model.ts
// onClose: `... before finishing (code ${code}${reason ? `, reason: ${reason}`
// : ”}).`; ws reports 1006 without a close frame (cr-r87e-tools-to-prova).
func TestR87eintegLiveCloseCodeAndReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames [][]byte
		want   string
	}{
		{"code and reason", [][]byte{r87eintegCloseFrame(1011, "overloaded")}, "WebSocket closed unexpectedly before finishing (code 1011, reason: overloaded)."},
		{"code only", [][]byte{r87eintegCloseFrame(4000, "")}, "WebSocket closed unexpectedly before finishing (code 4000)."},
		{"no close frame", nil, "WebSocket closed unexpectedly before finishing (code 1006)."},
	} {
		t.Run("transcription "+tc.name, func(t *testing.T) {
			model := newLiveTestModel(t, r87eintegRawWSServer(t, tc.frames...))
			result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio:            newBlockingAudioStream(),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			_, err = drainUntilFinishOrError(t, result.Stream)
			if want := "Google Live transcription " + tc.want; err == nil || err.Error() != want {
				t.Fatalf("err = %v\nwant %s", err, want)
			}
		})
		t.Run("translation "+tc.name, func(t *testing.T) {
			model := newSpeechTranslationTestModel(t, r87eintegRawWSServer(t, tc.frames...), 0)
			result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
				Audio:            newBlockingAudioStream(),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
				TargetLanguage:   "es",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			_, err = drainUntilTranslationFinishOrError(t, result.Stream)
			if want := "Google Live translation " + tc.want; err == nil || err.Error() != want {
				t.Fatalf("err = %v\nwant %s", err, want)
			}
		})
	}
}
