package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// ---- live transcription (TS 378af05f85, google-vertex-gemini-transcription-model.test.ts) ----

type portprovgLive struct {
	t       *testing.T
	server  *liveTranscriptionTestServer
	audio   *chanAudioStream
	result  *provider.TranscriptionStreamResult
	done    chan struct{}
	partsMu sync.Mutex
	parts   []*provider.TranscriptionStreamPart
	err     error
}

func portprovgStartLive(t *testing.T, providerOptions map[string]interface{}, grace time.Duration) *portprovgLive {
	t.Helper()
	server := newLiveTranscriptionTestServer(t)
	t.Cleanup(server.close)
	model := newLiveTestModel(t, server.ts.URL)
	model.finishGrace = grace
	audio := &chanAudioStream{chunks: make(chan []byte, 8)}
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions:  providerOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Stream.Close() })
	l := &portprovgLive{t: t, server: server, audio: audio, result: result, done: make(chan struct{})}
	first, err := result.Stream.Next() // stream-start unblocks the message loop
	if err != nil {
		t.Fatal(err)
	}
	l.parts = append(l.parts, first)
	go func() {
		defer close(l.done)
		for {
			part, err := result.Stream.Next()
			l.partsMu.Lock()
			if err != nil {
				if err != io.EOF {
					l.err = err
				}
				l.partsMu.Unlock()
				return
			}
			l.parts = append(l.parts, part)
			l.partsMu.Unlock()
			if part.Type == provider.TranscriptionStreamPartTypeFinish {
				return
			}
		}
	}()
	server.toSend <- map[string]interface{}{"setupComplete": map[string]interface{}{}}
	return l
}

func (l *portprovgLive) send(message string) {
	l.t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(message), &decoded); err != nil {
		l.t.Fatal(err)
	}
	l.server.toSend <- decoded
}

func (l *portprovgLive) settled(within time.Duration) bool {
	select {
	case <-l.done:
		return true
	case <-time.After(within):
		return false
	}
}

func (l *portprovgLive) finish() *provider.TranscriptionStreamPart {
	l.t.Helper()
	if !l.settled(2 * time.Second) {
		l.t.Fatal("stream did not finish")
	}
	l.partsMu.Lock()
	defer l.partsMu.Unlock()
	if l.err != nil {
		l.t.Fatalf("stream error: %v", l.err)
	}
	last := l.parts[len(l.parts)-1]
	if last.Type != provider.TranscriptionStreamPartTypeFinish {
		l.t.Fatalf("last part = %+v", last)
	}
	return last
}

func (l *portprovgLive) pushAudio(seconds int) {
	l.audio.chunks <- make([]byte, seconds*32000)
}

// closeAudio ends the input audio and waits for the end marker.
func (l *portprovgLive) closeAudio(marker string) {
	close(l.audio.chunks)
	l.server.waitFor(l.t, hasRealtimeInputKey(marker), time.Second)
}

func TestPortprovgLiveCompletesSegmentsOnGenerationCompleteAndVAD(t *testing.T) {
	l := portprovgStartLive(t, nil, 0)
	l.pushAudio(5)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"It was delivered into Earth orbit"}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"3s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if l.settled(100 * time.Millisecond) {
		t.Fatal("finished before the last segment")
	}
	// The server transcribes the rest and ends the speech at the end of the audio.
	l.send(`{"serverContent":{"inputTranscription":{"text":"on October 18th, 1989."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"5s"}}`)
	start := time.Now()
	finish := l.finish()
	if time.Since(start) > time.Second {
		t.Fatalf("finish took %s: the 3 s grace was waited", time.Since(start))
	}
	if finish.FinishText != "It was delivered into Earth orbit on October 18th, 1989." {
		t.Fatalf("text = %q", finish.FinishText)
	}
	var finals []string
	for _, part := range l.parts {
		if part.Type == provider.TranscriptionStreamPartTypeFinal {
			finals = append(finals, part.ID+"="+part.Text)
		}
	}
	if !reflect.DeepEqual(finals, []string{"google-segment-0=It was delivered into Earth orbit", "google-segment-1=on October 18th, 1989."}) {
		t.Fatalf("finals = %v", finals)
	}
}

func TestPortprovgLiveFallsBackToGraceWhenAudioEndsInSilence(t *testing.T) {
	l := portprovgStartLive(t, nil, 50*time.Millisecond)
	l.pushAudio(4)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"hello."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if got := l.finish().FinishText; got != "hello." {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveWaitsForBufferedAudioSegments(t *testing.T) {
	l := portprovgStartLive(t, nil, 0)
	l.pushAudio(2)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0.280s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"hello."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"1s"}}`)
	time.Sleep(50 * time.Millisecond)
	l.closeAudio("audioStreamEnd")
	if l.settled(150 * time.Millisecond) {
		t.Fatal("finished while the server was still processing buffered audio")
	}
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"1.200s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"world."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	if l.settled(150 * time.Millisecond) {
		t.Fatal("finished before the speech ended at the end of the audio")
	}
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"2s"}}`)
	if got := l.finish().FinishText; got != "hello. world." {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveManualActivityDetection(t *testing.T) {
	options := map[string]interface{}{"googleVertex": map[string]interface{}{"streaming": map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}}}
	l := portprovgStartLive(t, options, 0)
	setupMsg := l.server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	wantConfig := map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}
	if got := setupMsg["setup"].(map[string]interface{})["realtimeInputConfig"]; !reflect.DeepEqual(got, wantConfig) {
		t.Fatalf("setup.realtimeInputConfig = %v", got)
	}
	if got := l.result.RequestBody.(map[string]interface{})["realtimeInputConfig"]; !reflect.DeepEqual(got, wantConfig) {
		t.Fatalf("request body realtimeInputConfig = %v", got)
	}

	l.pushAudio(3)
	l.closeAudio("activityEnd")
	sent := l.server.all()
	var kinds []string
	for _, message := range sent[1:] {
		input, _ := message["realtimeInput"].(map[string]interface{})
		for key := range input {
			kinds = append(kinds, key)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"activityStart", "audio", "activityEnd"}) {
		t.Fatalf("realtimeInput sequence = %v", kinds)
	}

	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0s"}}`)
	l.send(`{"serverContent":{"inputTranscription":{"text":"Book a venue. Then ask Sam."}}}`)
	l.send(`{"serverContent":{"generationComplete":true}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"3s"}}`)
	finish := l.finish()
	if finish.FinishText != "Book a venue. Then ask Sam." {
		t.Fatalf("text = %q", finish.FinishText)
	}
	finals := 0
	for _, part := range l.parts {
		if part.Type == provider.TranscriptionStreamPartTypeFinal {
			finals++
		}
	}
	if finals != 1 {
		t.Fatalf("%d final segments", finals)
	}
}

func TestPortprovgLiveManualActivityWithoutSpeechFinishes(t *testing.T) {
	options := map[string]interface{}{"googleVertex": map[string]interface{}{"streaming": map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": true}}}}
	l := portprovgStartLive(t, options, 0)
	l.pushAudio(3)
	l.closeAudio("activityEnd")
	l.send(`{"voiceActivity":{"type":"ACTIVITY_START","audioOffset":"0s"}}`)
	l.send(`{"voiceActivity":{"type":"ACTIVITY_END","audioOffset":"3s"}}`)
	if got := l.finish().FinishText; got != "" {
		t.Fatalf("text = %q", got)
	}
}

func TestPortprovgLiveStreamingOptionsValidation(t *testing.T) {
	for name, streaming := range map[string]interface{}{
		"streaming not an object": "x",
		"detection not an object": map[string]interface{}{"automaticActivityDetection": true},
		"disabled not a boolean":  map[string]interface{}{"automaticActivityDetection": map[string]interface{}{"disabled": "yes"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseVertexGeminiTranscriptionOptionsChecked(map[string]interface{}{"googleVertex": map[string]interface{}{"streaming": streaming}})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
}
