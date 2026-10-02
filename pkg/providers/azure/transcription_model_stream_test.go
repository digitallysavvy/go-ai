package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// These tests cover the Azure OpenAI-protocol TranscriptionModel's DoStream,
// a gap vs TS AzureTranscriptionModel#doStream's default ("openai") branch,
// which wraps an OpenAITranscriptionModel instance (gpt-realtime-whisper*
// deployments stream over the OpenAI-compatible realtime WebSocket). Before
// this, azure.TranscriptionModel had no DoStream at all, so
// azureTranscriptionDispatchModel.DoStream's default case always fell
// through to UnsupportedFunctionalityError for every Azure deployment,
// including gpt-realtime-whisper ones.

// realtimeWhisperTestServer is a minimal OpenAI-realtime-compatible
// WebSocket test double (no session.created/session.updated handshake,
// unlike MAI): it records every received message and lets the test script
// server->client events over a channel. Mirrors
// pkg/providers/openai's realtimeTranscriptionTestServer.
type realtimeWhisperTestServer struct {
	ts *httptest.Server

	mu       sync.Mutex
	received []map[string]interface{}
	toSend   chan interface{}
}

func newRealtimeWhisperTestServer() *realtimeWhisperTestServer {
	s := &realtimeWhisperTestServer{toSend: make(chan interface{}, 16)}
	handler := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *stdhttp.Request) error {
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: func(conn *websocket.Conn) {
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
		},
	}
	s.ts = httptest.NewServer(handler)
	return s
}

func (s *realtimeWhisperTestServer) close() {
	close(s.toSend)
	s.ts.Close()
}

func (s *realtimeWhisperTestServer) waitForReceived(t *testing.T, want string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, m := range s.received {
			if m["type"] == want {
				s.mu.Unlock()
				return m
			}
		}
		s.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a %q message", want)
	return nil
}

// TestAzureTranscriptionOpenAI_DoStream_RejectsNonRealtimeModel mirrors TS:
// only gpt-realtime-whisper(-dated) deployments support streaming; every
// other deployment rejects with UnsupportedFunctionalityError, same as
// DoTranscribe rejects gpt-realtime-whisper for non-streaming.
func TestAzureTranscriptionOpenAI_DoStream_RejectsNonRealtimeModel(t *testing.T) {
	p := mustNewProvider(t, Config{APIKey: "k", ResourceName: "r"})
	model, err := p.TranscriptionModel("whisper-1")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	streamer, ok := model.(provider.TranscriptionStreamer)
	if !ok {
		t.Fatalf("model does not implement TranscriptionStreamer")
	}
	_, err = streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

// TestAzureTranscriptionOpenAI_DoStream_StreamsDeltasAndFinal exercises the
// gpt-realtime-whisper deployment end-to-end over a real WebSocket: the
// dispatcher routes to the "openai" backend, which sends session.update and
// streams audio immediately (no session.created/session.updated handshake,
// unlike MAI), and the server's delta/completed events surface as
// delta/final/finish stream parts.
func TestAzureTranscriptionOpenAI_DoStream_StreamsDeltasAndFinal(t *testing.T) {
	server := newRealtimeWhisperTestServer()
	defer server.close()

	p, err := New(Config{APIKey: "azure-key", ResourceName: "r", BaseURL: server.ts.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.TranscriptionModel("gpt-realtime-whisper")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	streamer, ok := model.(provider.TranscriptionStreamer)
	if !ok {
		t.Fatalf("model does not implement TranscriptionStreamer")
	}

	rate := 16000
	audio := newChanAudioStream([]byte{1, 2, 3})
	result, err := streamer.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer result.Stream.Close() //nolint:errcheck

	server.waitForReceived(t, "session.update", time.Second)
	appendMsg := server.waitForReceived(t, "input_audio_buffer.append", time.Second)
	if audioB64, _ := appendMsg["audio"].(string); audioB64 == "" {
		t.Fatal("expected base64 audio in input_audio_buffer.append")
	} else if decoded, decErr := base64.StdEncoding.DecodeString(audioB64); decErr != nil || string(decoded) != "\x01\x02\x03" {
		t.Fatalf("decoded audio = %q, err = %v", decoded, decErr)
	}
	server.waitForReceived(t, "input_audio_buffer.commit", time.Second)

	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "Hel"}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item-1", "delta": "lo"}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item-1", "transcript": "Hello"}

	var parts []*provider.TranscriptionStreamPart
	for {
		part, nextErr := result.Stream.Next()
		if nextErr != nil {
			break
		}
		parts = append(parts, part)
		if part.Type == provider.TranscriptionStreamPartTypeFinish {
			break
		}
	}

	if len(parts) != 5 {
		t.Fatalf("len(parts) = %d, want 5 (stream-start, delta, delta, final, finish); got %+v", len(parts), parts)
	}
	finish := parts[len(parts)-1]
	if finish.Type != provider.TranscriptionStreamPartTypeFinish || finish.FinishText != "Hello" {
		t.Fatalf("finish = %+v", finish)
	}
}

// TestAzureTranscriptionOpenAI_RealtimeWebSocketURL_IncludesDeploymentAndAPIVersion
// verifies the streaming URL builder matches the REST one's host/deployment/
// api-version construction (shared responsesBaseURL/includeAPIVersionQuery),
// converted to wss://, for a recognized Azure OpenAI host.
func TestAzureTranscriptionOpenAI_RealtimeWebSocketURL_IncludesDeploymentAndAPIVersion(t *testing.T) {
	p, err := New(Config{APIKey: "k", ResourceName: "my-resource", APIVersion: "2024-10-21"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tm := NewTranscriptionModel(p, "gpt-realtime-whisper")
	wsURL, err := tm.realtimeWebSocketURL()
	if err != nil {
		t.Fatalf("realtimeWebSocketURL: %v", err)
	}
	if !strings.HasPrefix(wsURL, "wss://my-resource.openai.azure.com/openai/v1/realtime?intent=transcription") {
		t.Fatalf("wsURL = %q", wsURL)
	}
	if !strings.Contains(wsURL, "api-version=2024-10-21") {
		t.Fatalf("wsURL missing api-version: %q", wsURL)
	}
}
