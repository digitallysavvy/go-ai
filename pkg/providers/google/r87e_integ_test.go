package google

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // WebSocket accept key
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TS build-google-interactions-stream-transform.ts: `mime_type ?? default`
// keeps a present "", `delta.result !== undefined` stores a present null
// (the tool result is then null), and `service_tier != null` keeps ""
// (cr-r87e-provb-to-prova 3).
func TestR87eintegInteractionsStreamEmptyValues(t *testing.T) {
	body := strings.Join([]string{
		`data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"image","data":"aGk=","mime_type":""}}`,
		`data: {"event_type":"step.stop","index":0}`,
		`data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"c1","result":[{"url":"https://a.example"}]}}`,
		`data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":null}}`,
		`data: {"event_type":"step.stop","index":1}`,
		`data: {"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","service_tier":""}}`,
		``,
	}, "\n\n")
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, map[string]string{"X-Gemini-Service-Tier": "standard"}, 0)
	var file, result, finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeFile:
			file = c
		case provider.ChunkTypeToolResult:
			result = c
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if file == nil || file.GeneratedFileContent.MediaType != "" {
		t.Fatalf("file = %+v", file)
	}
	if result == nil || result.ToolResult.Result != nil {
		t.Fatalf("tool result = %+v", result)
	}
	var meta map[string]map[string]interface{}
	if finish == nil || json.Unmarshal(finish.ProviderMetadata, &meta) != nil {
		t.Fatalf("finish = %+v", finish)
	}
	if st, ok := meta["google"]["serviceTier"]; !ok || st != "" {
		t.Fatalf("serviceTier = %#v (present %v), want \"\"", st, ok)
	}
}

// TS google-interactions-language-model.ts `response.service_tier ??
// headerServiceTier`: a body "" wins over the header.
func TestR87eintegInteractionsServiceTierPrecedence(t *testing.T) {
	empty, standard := "", "standard"
	if got := interactionsServiceTier(&empty, &standard); got == nil || *got != "" {
		t.Fatalf("body \"\" = %v", got)
	}
	if got := interactionsServiceTier(nil, &standard); got == nil || *got != "standard" {
		t.Fatalf("header = %v", got)
	}
	if got := interactionsServiceTier(nil, nil); got != nil {
		t.Fatalf("none = %v", *got)
	}
}

// TS build-google-interactions-stream-transform.ts resets its interactionId
// on an interaction.created with an empty id (store: false), so later
// metadata has none; the id the stream reconnects to (stream-google-
// interactions.ts) is a separate value (cr-r87e-provb-to-prova 4).
func TestR87eintegInteractionsCreatedEmptyIDResetsMetadataID(t *testing.T) {
	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1","status":"in_progress"}}`,
		`data: {"event_type":"step.start","index":0,"step":{"type":"model_output","content":[{"type":"text","text":""}]}}`,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hi"}}`,
		`data: {"event_type":"step.stop","index":0}`,
		`data: {"event_type":"interaction.created","interaction":{"id":"","status":"in_progress"}}`,
		`data: {"event_type":"interaction.completed","interaction":{"status":"completed"}}`,
		``,
	}, "\n\n")
	st := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	var finish, textEnd *provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeFinish:
			finish = c
		case provider.ChunkTypeTextEnd:
			textEnd = c
		}
	}
	if textEnd == nil || !strings.Contains(string(textEnd.ProviderMetadata), `"interactionId":"v1"`) {
		t.Fatalf("text-end before the reset = %+v", textEnd)
	}
	if finish == nil || strings.Contains(string(finish.ProviderMetadata), "interactionId") {
		t.Fatalf("finish = %+v", finish)
	}
	if id := st.(*interactionsStream).interactionID; id != "v1" {
		t.Fatalf("reconnect id = %q, want v1", id)
	}
}
