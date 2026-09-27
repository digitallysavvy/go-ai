package openai_test

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"golang.org/x/net/websocket"
)

// TestExperimentalStreamTranscribe_WithOpenAIProvider is an end-to-end check
// that ai.ExperimentalStreamTranscribe drives the OpenAI gpt-realtime-whisper
// provider.TranscriptionStreamer implementation correctly: the ai-level
// orchestration (stream-start/response-metadata/finish handling, FullStream
// pass-through) composes with the provider's real WebSocket transport.
func TestExperimentalStreamTranscribe_WithOpenAIProvider(t *testing.T) {
	toSend := make(chan interface{}, 4)
	handler := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *stdhttp.Request) error {
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: func(conn *websocket.Conn) {
			go func() {
				for msg := range toSend {
					encoded, _ := json.Marshal(msg)
					if err := websocket.Message.Send(conn, string(encoded)); err != nil {
						return
					}
				}
			}()
			for {
				var raw string
				if err := websocket.Message.Receive(conn, &raw); err != nil {
					return
				}
			}
		},
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	p := openai.New(openai.Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.TranscriptionModel("gpt-realtime-whisper")
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}

	audioCh := make(chan []byte, 1)
	audioCh <- []byte{1, 2, 3}
	close(audioCh)

	result, err := ai.ExperimentalStreamTranscribe(context.Background(), ai.StreamTranscribeOptions{
		Model:            model,
		Audio:            ai.NewChannelAudioStream(audioCh, nil),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}

	stream, err := result.FullStream()
	if err != nil {
		t.Fatalf("FullStream error = %v", err)
	}

	// Give the provider a moment to reach the receive loop, then complete
	// the transcription from the "server" side.
	time.Sleep(50 * time.Millisecond)
	toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "Hi"}
	toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item-1", "transcript": "Hi there"}

	var sawDelta bool
	for {
		part, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		if part.Type == "transcript-delta" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Fatal("expected at least one transcript-delta part")
	}

	text, err := result.Text()
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if !strings.Contains(text, "Hi there") {
		t.Fatalf("Text() = %q, want containing 'Hi there'", text)
	}
	close(toSend)
}
