package googlevertex

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `new Error(message.error.message ?? 'Vertex Live API error')`: an empty
// server error message is kept.
func TestR84provaVertexLiveEmptyErrorMessageKept(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &chanAudioStream{chunks: make(chan []byte)},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if _, err := result.Stream.Next(); err != nil {
		t.Fatal(err)
	}
	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	server.toSend <- map[string]interface{}{"error": map[string]interface{}{"message": ""}}
	_, err = drainUntilFinishOrError(t, result.Stream)
	if err == nil || err.Error() != "" {
		t.Fatalf("err = %v, want empty message", err)
	}
}
