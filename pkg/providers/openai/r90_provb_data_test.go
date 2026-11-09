package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR90provbErrorDataIsSchemaOutput: TS createJsonErrorResponseHandler
// sets APICallError.data to the error schema output (undeclared keys
// stripped) for a matching body and leaves it undefined otherwise (the
// provc class of 252b3d76).
func TestR90provbErrorDataIsSchemaOutput(t *testing.T) {
	check := func(name string, err error, want interface{}) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if !reflect.DeepEqual(pe.Data, want) {
			t.Fatalf("%s: data = %#v, want %#v", name, pe.Data, want)
		}
	}
	httpErr := func(body string) error {
		return &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}
	}
	_ = httpErr
	p, _ := newDecisionProvider(t, `{"error":{"message":"m","param":null,"x":1}}`, 403)
	_, err := decide(t, p, decisionTestOptions())
	check("decision", err, map[string]interface{}{"error": map[string]interface{}{"message": "m", "param": nil}})
	p, _ = newDecisionProvider(t, `{"message":"m"}`, 403)
	_, err = decide(t, p, decisionTestOptions())
	check("decision off schema", err, nil)
}

// TestR90provbImageDownloadErrorUnchanged: a download that fails URL
// validation propagates unchanged (TS downloadBlob / getFromApi), without
// a "failed to download image" wrap (the provc class of f4673dbc).
func TestR90provbImageDownloadErrorUnchanged(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), "gpt-image-1")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p",
		Files: []provider.ImageFile{{Type: "url", URL: "http://169.254.169.254/x.png"}}})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || strings.Contains(err.Error(), "failed to download image") {
		t.Fatalf("edit download err = %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"http://169.254.169.254/x.png"}]}`))
	}))
	defer srv.Close()
	cm := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "host", CompatErrorStructure: providerutils.OpenAICompatibleAPIError}), "m")
	_, err = cm.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &dl) || errors.As(err, &pe) {
		t.Fatalf("compat download err = %T %v", err, err)
	}
}

// TestR90provbRealtimeTranscriptionEmptyItemID: TS finish(text, id) sends
// the transcript-final part when item_id != null, an empty id included
// (openai-transcription-model.ts createOpenAIRealtimeTranscriptionStream).
func TestR90provbRealtimeTranscriptionEmptyItemID(t *testing.T) {
	server := newRealtimeTranscriptionTestServer(t)
	defer server.close()
	model := newTestTranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio: newChanTestAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForReceived(t, "session.update", time.Second)
	server.toSend <- map[string]interface{}{"type": "conversation.item.input_audio_transcription.completed", "item_id": "", "transcript": "hi"}
	var types []string
	for {
		part, err := result.Stream.Next()
		if err != nil {
			break
		}
		types = append(types, string(part.Type))
	}
	if len(types) != 3 || types[1] != string(provider.TranscriptionStreamPartTypeFinal) || types[2] != string(provider.TranscriptionStreamPartTypeFinish) {
		t.Fatalf("parts = %v", types)
	}
}
