package azure

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provbMaiImageURLType: a URL image is typed (and named) by the
// downloaded content type, as TS downloadBlob types the Blob
// (azure-mai-image-model.ts fileToBlob, toFormData).
func TestR90provbMaiImageURLType(t *testing.T) {
	server, c := newMaiServer(t, nil)
	_, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p",
		Files:  []provider.ImageFile{{Type: "url", URL: "data:image/jpeg;base64,AQI=", MediaType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.files, []string{"image-1.jpg"}) || !reflect.DeepEqual(c.fileType, []string{"image/jpeg"}) {
		t.Errorf("files = %v types = %v", c.files, c.fileType)
	}
}

// r90provbMaiRun drives one MAI streaming session to its commit, then sends
// the given server events and returns the finish text or the stream error.
func r90provbMaiRun(t *testing.T, events ...map[string]interface{}) (string, error) {
	t.Helper()
	server := newMAITestServer()
	defer server.close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", MaiBaseURL: server.ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.TranscriptionModel("mai-transcribe-2-streaming")
	rate := 16000
	result, err := model.(provider.TranscriptionStreamer).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream([]byte{1, 2}), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	type out struct {
		text string
		err  error
	}
	done := make(chan out, 1)
	go func() {
		var o out
		for {
			part, nextErr := result.Stream.Next()
			if nextErr != nil {
				if nextErr != io.EOF {
					o.err = nextErr
				}
				break
			}
			if part.Type == provider.TranscriptionStreamPartTypeFinish {
				o.text = part.FinishText
			}
		}
		done <- o
	}()
	server.toSend <- map[string]interface{}{"type": "session.created"}
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.updated"}
	server.waitForReceived(t, "input_audio_buffer.commit", time.Second)
	for _, e := range events {
		server.toSend <- e
	}
	select {
	case o := <-done:
		return o.text, o.err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
		return "", nil
	}
}

// TestR90provbMaiEmptyTranscriptAndError: TS `event.transcript ?? deltas`
// keeps an empty transcript, and the error text reads `code != null` and
// `message ?? 'unknown error'` (azure-mai-transcription-model.ts).
func TestR90provbMaiEmptyTranscriptAndError(t *testing.T) {
	text, err := r90provbMaiRun(t,
		map[string]interface{}{"type": "conversation.item.input_audio_transcription.delta", "item_id": "i", "delta": "hel"},
		map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "i", "transcript": ""})
	if err != nil || text != "" {
		t.Fatalf("finish = %q, %v", text, err)
	}
	_, err = r90provbMaiRun(t, map[string]interface{}{"type": "error", "error": map[string]interface{}{"code": "", "message": ""}})
	if err == nil || err.Error() != "Azure MAI transcription error (): " {
		t.Fatalf("err = %v", err)
	}
}
