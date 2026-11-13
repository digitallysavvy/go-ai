package xai

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/net/websocket"
)

// TestR97provbBatchFractionalUsageRawExact: TS convertXaiBatchTextUsage
// keeps `raw: usage` and the counts are z.number(), so a fractional count
// stays exact in Usage.Raw (Go's raw held the truncated retry value).
func TestR97provbBatchFractionalUsageRawExact(t *testing.T) {
	var resp xaiBatchTextResponseWire
	body := `{"choices":[{"message":{"role":"assistant","content":"hi"},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":1.5,"completion_tokens":2,"total_tokens":3.5}}`
	if err := providerutils.UnmarshalChunk([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result, berr := convertXAIBatchTextResponse(resp)
	if berr != nil {
		t.Fatal(berr)
	}
	if *result.Usage.InputTokens != 1 || result.Usage.Raw["prompt_tokens"] != 1.5 {
		t.Fatalf("input = %d, raw = %#v", *result.Usage.InputTokens, result.Usage.Raw)
	}
}

// TestR97provbStreamingNullMessageTypeError: TS safeParseJSON parses a JSON
// null message and then reads raw.type of it; the TypeError fails the
// stream through onProcessingError (Go ignored the message).
func TestR97provbStreamingNullMessageTypeError(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, "null")
		var msg string
		_ = websocket.Message.Receive(conn, &msg)
	}})
	defer ts.Close()
	rate := 16000
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := newTestXAITranscriptionModel(ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newXAIChanAudioStream([]byte{1, 2}), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	_, err = drainXAIStream(result.Stream)
	if err == nil || err.Error() != "Cannot read properties of null (reading 'type')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}

// TestR97provbRealtimeParseNullEvent: TS parseXaiRealtimeServerEvent reads
// event.type of the parsed message, so a JSON null throws a TypeError (Go
// returned a custom event).
func TestR97provbRealtimeParseNullEvent(t *testing.T) {
	_, err := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice").ParseServerEvent([]byte("null"))
	if err == nil || err.Error() != "Cannot read properties of null (reading 'type')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}
