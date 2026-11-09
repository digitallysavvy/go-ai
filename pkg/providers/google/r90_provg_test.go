package google

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provgEmbeddingOutputDimensionalityNumber: googleEmbeddingModelOptions
// outputDimensionality is z.number(): every Go integer kind is accepted and
// sent; NaN is not a number.
func TestR90provgEmbeddingOutputDimensionalityNumber(t *testing.T) {
	for _, v := range []interface{}{int32(256), uint16(256), float32(256)} {
		opts, err := parseEmbeddingProviderOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"outputDimensionality": v}}})
		if err != nil {
			t.Fatalf("%T rejected: %v", v, err)
		}
		body := map[string]interface{}{}
		addEmbeddingModelOptions(body, opts)
		if body["outputDimensionality"] != v {
			t.Fatalf("%T: outputDimensionality = %#v", v, body["outputDimensionality"])
		}
	}
	if _, err := parseEmbeddingProviderOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"outputDimensionality": math.NaN()}}}); err == nil {
		t.Fatal("NaN accepted")
	}
}

// TestR90provgLiveRawChunkForMismatchedMessage: TS onMessageText
// (google-transcription-model.ts, google-speech-translation-model.ts) reports
// every safeParseJSON-parsed message raw before reading fields, so a message
// whose fields do not fit the expected shape still yields a raw part; a
// forbidden prototype key fails the parse.
func TestR90provgLiveRawChunkForMismatchedMessage(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	model := newLiveTestModel(t, server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if part, err := result.Stream.Next(); err != nil || part.Type != provider.TranscriptionStreamPartTypeStreamStart {
		t.Fatalf("first part = %+v, %v", part, err)
	}
	server.toSend <- map[string]interface{}{"__proto__": map[string]interface{}{}}
	server.toSend <- map[string]interface{}{"serverContent": "x"}
	part, err := result.Stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := part.RawValue.(map[string]interface{})
	if part.Type != provider.TranscriptionStreamPartTypeRaw || raw["serverContent"] != "x" {
		t.Fatalf("part = %+v, want the raw serverContent message", part)
	}
}

func TestR90provgSpeechTranslationRawChunkForMismatchedMessage(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		TargetLanguage:   "es",
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	if part, err := result.Stream.Next(); err != nil || part.Type != provider.SpeechTranslationStreamPartTypeStreamStart {
		t.Fatalf("first part = %+v, %v", part, err)
	}
	server.toSend <- map[string]interface{}{"__proto__": map[string]interface{}{}}
	server.toSend <- map[string]interface{}{"serverContent": "x"}
	done := make(chan *provider.SpeechTranslationStreamPart, 1)
	go func() {
		part, _ := result.Stream.Next()
		done <- part
	}()
	select {
	case part := <-done:
		raw, _ := part.RawValue.(map[string]interface{})
		if part == nil || part.Type != provider.SpeechTranslationStreamPartTypeRaw || raw["serverContent"] != "x" {
			t.Fatalf("part = %+v, want the raw serverContent message", part)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestR90provgSpeechRequestBodyText: TS google-speech-model.ts returns
// request.body = JSON.stringify(requestBody), which leaves HTML characters
// literal.
func TestR90provgSpeechRequestBodyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"AQI="}}]}}]}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.SpeechModel(ModelGemini38FlashTTS)
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "<b>Tom & Jerry</b>"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"contents":[{"role":"user","parts":[{"text":"<b>Tom & Jerry</b>"}]}],"generationConfig":{"responseModalities":["AUDIO"],"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}}}}}`
	if got := result.Request.Body; got != want {
		t.Fatalf("request body = %v, want %s", got, want)
	}
}
