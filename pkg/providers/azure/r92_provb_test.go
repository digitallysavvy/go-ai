package azure

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The exported Azure option structs are read through their JSON form (round
// 91 typed-literal readers), so a typed literal passed as
// providerOptions.azure must carry the TS option keys. Without JSON tags the
// image options were silently ignored (keys "API", "WebGrounding") and the
// strict speech / transcription schemas rejected them as unrecognized keys.
func TestR92provbAzureTypedOptionStructs(t *testing.T) {
	yes := true
	t.Run("image", func(t *testing.T) {
		got, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": AzureImageModelOptions{API: "mai", WebGrounding: &yes}})
		if err != nil || got.API != "mai" || got.WebGrounding == nil || !*got.WebGrounding || got.AutoAspectRatio != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, err = parseAzureImageProviderOptions(map[string]interface{}{"azure": &AzureImageModelOptions{AutoAspectRatio: &yes}})
		if err != nil || got.AutoAspectRatio == nil || !*got.AutoAspectRatio {
			t.Fatalf("pointer: got %+v, %v", got, err)
		}
	})
	t.Run("speech", func(t *testing.T) {
		degree := 1.5
		got, err := parseAzureSpeechProviderOptions(map[string]interface{}{"azure": AzureSpeechModelOptions{
			API: AzureTranscriptionAPISpeech, AzureSpeechModelSpeechOptions: AzureSpeechModelSpeechOptions{Style: "excited", StyleDegree: &degree},
		}})
		if err != nil || got.API != AzureTranscriptionAPISpeech || got.Style != "excited" || got.StyleDegree == nil || *got.StyleDegree != 1.5 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("transcription", func(t *testing.T) {
		got, err := parseAzureTranscriptionProviderOptions(map[string]interface{}{"azure": AzureTranscriptionModelOptions{
			API: AzureTranscriptionAPISpeech,
			AzureSpeechTranscriptionOptions: AzureSpeechTranscriptionOptions{
				Timestamps: "word", TranscribeStyle: "clean", Locales: []string{"en"},
				Diarization: &AzureDiarizationOptions{Enabled: true}, PhraseList: &AzurePhraseListOptions{Phrases: []string{"go-ai"}},
			},
		}})
		if err != nil || got.API != AzureTranscriptionAPISpeech || got.Timestamps != "word" || got.TranscribeStyle != "clean" ||
			len(got.Locales) != 1 || got.Diarization == nil || !got.Diarization.Enabled || got.PhraseList == nil || got.PhraseList.Phrases[0] != "go-ai" {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, err = parseAzureTranscriptionProviderOptions(map[string]interface{}{"azure": &AzureTranscriptionModelOptions{API: AzureTranscriptionAPIMai, Language: "de"}})
		if err != nil || got.API != AzureTranscriptionAPIMai || got.Language != "de" {
			t.Fatalf("mai: got %+v, %v", got, err)
		}
	})
}

// MAI realtime: TS safeParseJSON reports any JSON message as a raw part, a
// non-object value and an object with mistyped fields included.
func TestR92provbMaiTranscriptionRawAnyJSON(t *testing.T) {
	server := newMAITestServer()
	defer server.close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", MaiBaseURL: server.ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.TranscriptionModel("mai-transcribe-2-streaming")
	if err != nil {
		t.Fatal(err)
	}
	rate := 16000
	result, err := model.(provider.TranscriptionStreamer).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	rawsCh := make(chan []interface{}, 1)
	go func() {
		var raws []interface{}
		for {
			part, err := result.Stream.Next()
			if err == io.EOF || err != nil {
				break
			}
			if part.Type == provider.TranscriptionStreamPartTypeRaw {
				raws = append(raws, part.RawValue)
			}
		}
		rawsCh <- raws
	}()
	server.toSend <- map[string]interface{}{"type": "session.created"}
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "session.updated"}
	server.waitForReceived(t, "input_audio_buffer.commit", time.Second)
	server.toSend <- "x"
	server.toSend <- map[string]interface{}{"type": 3.0}
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "i", "transcript": "t"}
	var raws []interface{}
	select {
	case raws = <-rawsCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	if len(raws) != 5 || raws[2] != "x" {
		t.Fatalf("raw parts = %v, want 5 with \"x\" third", raws)
	}
}

// MAI image n > 1: TS Promise.all rejects with the first request to fail;
// the requests cancelled after it must not replace that error.
func TestR92provbMaiImageFirstFailureWins(t *testing.T) {
	const n = 8
	var count int32
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if atomic.AddInt32(&count, 1) == n {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"bad prompt"}}`))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer ts.Close()
	headers := func(context.Context) (map[string]string, error) { return map[string]string{}, nil }
	mai := &azureMaiImageModel{modelID: "m", url: func(string) (string, error) { return ts.URL, nil }, headers: headers, httpClient: ts.Client()}
	count8 := n
	_, err := mai.doGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", N: &count8}, AzureImageModelOptions{})
	if err == nil || errors.Is(err, context.Canceled) || err.Error() == "" || !containsStr(err.Error(), "bad prompt") {
		t.Fatalf("err = %v, want the 400 error", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
