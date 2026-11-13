package googlevertex

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS onMessageText reads message.setupComplete of the safeParseJSON value: a
// JSON null message throws a TypeError, which fails the stream
// (cr-r97-provb-to-provg 1).
func TestR97provgLiveTranscriptionNullMessageTypeError(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	server.toSend <- nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := newLiveTestModel(t, server.ts.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	for i := 0; i < 100; i++ {
		if _, err = result.Stream.Next(); err != nil {
			break
		}
	}
	if err == nil || err.Error() != "Cannot read properties of null (reading 'setupComplete')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
}
