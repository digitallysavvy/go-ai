package mistral

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// provg CR §2 (round 96): tool.execution.done info is
// z.record(z.string(), z.json()): a non-finite number (JSON.parse of 1e400)
// fails the event, a finite one passes.
func TestR96integConversationExecutionInfoJSONValue(t *testing.T) {
	run := func(info string) []*provider.StreamChunk {
		m := New(Config{APIKey: "k"}).ConversationModel("mistral-medium-latest")
		s := &conversationStream{executions: map[string]*conversationExecution{}, converter: m.newConverter(nil)}
		s.handle(`{"type":"tool.execution.started","id":"e","name":"web_search","arguments":"{}"}`)
		s.handle(`{"type":"tool.execution.done","id":"e","name":"web_search","info":` + info + `}`)
		return s.queue
	}
	hasError := func(q []*provider.StreamChunk) bool {
		for _, c := range q {
			if c.Type == provider.ChunkTypeError {
				return true
			}
		}
		return false
	}
	if q := run(`{"a":[1,"x"]}`); hasError(q) {
		t.Fatalf("finite info: queue = %+v", q)
	}
	if q := run(`{"a":[1e400]}`); !hasError(q) {
		t.Fatalf("1e400 info: no error chunk, queue = %+v", q)
	}
}

// provg CR §2: the realtime transcription.done usage is
// z.record(z.string(), z.json()): 1e400 there fails the event.
func TestR96integRealtimeTranscriptionUsageJSONValue(t *testing.T) {
	for _, c := range []struct {
		usage   string
		wantErr bool
	}{{`{"prompt_audio_seconds":2}`, false}, {`{"prompt_audio_seconds":1e400}`, true}} {
		usage := c.usage
		rs := r87provaStartRealtime(t, true, func(send func(string), _ *websocket.Conn) {
			send(`{"type":"transcription.done","text":"x","usage":` + usage + `}`)
		})
		res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio: &r87provaAudio{chunks: [][]byte{{1}}}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = r87provaDrain(t, res.Stream)
		if (err != nil) != c.wantErr {
			t.Errorf("usage %s: err = %v, wantErr %v", c.usage, err, c.wantErr)
		}
	}
}
