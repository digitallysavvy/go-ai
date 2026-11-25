package google

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TestR90provgUnaryTranscriptionNullTexts: TS google-transcription-model.ts
// doGenerate skips a text content whose text is null (annotations
// included), keeps an empty word text, reports an empty usage object, and
// returns warnings: [].
func TestR90provgUnaryTranscriptionNullTexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"steps":[{"type":"model_output","content":[`+
			`{"type":"text","annotations":[{"type":"word_info","text":"skipped","start_offset":"0s","end_offset":"1s"}]},`+
			`{"type":"text","text":"hi","annotations":[{"type":"word_info","text":"","start_offset":"1s","end_offset":"2s"}]}]}],"usage":{}}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewTranscriptionModel(p, ModelGemini35Transcribe)
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi" || len(res.Segments) != 1 || res.Segments[0].Text != "" || res.Segments[0].Start != 1 {
		t.Fatalf("text = %q, segments = %+v", res.Text, res.Segments)
	}
	if res.ProviderUsage == nil || res.ProviderMetadata == nil || res.Warnings == nil {
		t.Fatalf("usage = %v, metadata = %v, warnings = %v", res.ProviderUsage, res.ProviderMetadata, res.Warnings)
	}
}

// TestR90provgInteractionsMediaTypeQuoting: TS resolveFullMediaType writes
// `file of media type "${part.mediaType}" ...`, quoting the value as is.
func TestR90provgInteractionsMediaTypeQuoting(t *testing.T) {
	_, err := resolveInteractionMediaType(`image\x`, []byte{0}, "")
	if err == nil || !strings.Contains(err.Error(), `file of media type "image\x" must specify subtype`) {
		t.Fatalf("err = %v", err)
	}
}

// TestR90provgFilesFractionalPollOptions: TS googleFilesUploadOptionsSchema
// reads pollIntervalMs / pollTimeoutMs as z.number().positive(), so 0.5 is a
// valid interval and NaN is not a number.
func TestR90provgFilesFractionalPollOptions(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload/v1beta/files" {
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"file": map[string]interface{}{"name": "files/a", "mimeType": "text/plain", "uri": "u", "state": "ACTIVE"}})
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	upload := func(v interface{}) error {
		_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
			Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
			MediaType:       "text/plain",
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": v}},
		})
		return err
	}
	if err := upload(0.5); err != nil {
		t.Fatalf("0.5 rejected: %v", err)
	}
	if err := upload(uint8(5)); err != nil {
		t.Fatalf("uint8 rejected: %v", err)
	}
	if err := upload(math.NaN()); err == nil {
		t.Fatal("NaN accepted")
	}
}
