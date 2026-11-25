package mistral

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// TS mistral-transcription-stream.ts sends each audio slice as
// JSON.stringify({type: 'input_audio.append', audio}): type first.
func TestR98provgRealtimeAudioAppendKeyOrder(t *testing.T) {
	var mu sync.Mutex
	var frames []string
	srv := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		send := func(msg string) { _ = websocket.Message.Send(conn, msg) }
		send(`{"type":"session.created"}`)
		for {
			var text string
			if websocket.Message.Receive(conn, &text) != nil {
				return
			}
			mu.Lock()
			frames = append(frames, text)
			mu.Unlock()
			switch {
			case strings.Contains(text, `"session.update"`):
				send(`{"type":"session.updated"}`)
			case strings.Contains(text, `"input_audio.end"`):
				send(`{"type":"transcription.done","text":"","language":"en","model":"m","segments":[],"usage":{}}`)
			}
		}
	}})
	defer srv.Close()
	m := r87provaRealtimeModel(t, &r87provaRealtimeServer{srv: srv}, Config{})
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &r87provaAudio{chunks: [][]byte{{1, 2}}},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r87provaRate(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r87provaDrain(t, res.Stream); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(frames) < 2 || frames[1] != `{"type":"input_audio.append","audio":"AQI="}` {
		t.Fatalf("frames = %q", frames)
	}
}
