package mistral

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// TS safeParseJSON wraps the secureJsonParse rejection in one JSONParseError
// whose cause is the SyntaxError (cr-r97-prova-to-provg).
func TestR97provgRealtimeTranscriptionSingleJSONParseError(t *testing.T) {
	rs := r87provaStartRealtime(t, false, func(send func(string), _ *websocket.Conn) {
		send(`{"type":"transcription.text.delta","text":"x","__proto__":{}}`)
	})
	audio := &r87provaAudio{block: make(chan struct{})}
	res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r87provaDrain(t, res.Stream)
	var parseErr *providererrors.JSONParseError
	if !errors.As(err, &parseErr) || parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("err = %#v, want one JSONParseError caused by the SyntaxError", err)
	}
	if strings.Count(err.Error(), "JSON parsing failed") != 1 {
		t.Fatalf("message = %q", err.Error())
	}
}
