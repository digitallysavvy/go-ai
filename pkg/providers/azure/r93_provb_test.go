package azure

import (
	"context"
	"errors"
	"io"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS returns the MAI b64_json untouched and core decodes it with
// convertBase64ToUint8Array (atob's forgiving rules).
func TestR93provbMaiImageBase64Forgiving(t *testing.T) {
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVs\nbG8"}]}`))
	}))
	defer ts.Close()
	headers := func(context.Context) (map[string]string, error) { return map[string]string{}, nil }
	mai := &azureMaiImageModel{modelID: "m", url: func(string) (string, error) { return ts.URL, nil }, headers: headers, httpClient: ts.Client()}
	res, err := mai.doGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}, AzureImageModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 || string(res.Images[0]) != "hello" {
		t.Fatalf("images = %q", res.Images)
	}
}

// TS styleDegree z.number().min(0.01).max(2) rejects NaN.
func TestR93provbSpeechStyleDegreeNaN(t *testing.T) {
	_, err := parseAzureSpeechProviderOptions(map[string]interface{}{"azure": map[string]interface{}{"style": "excited", "styleDegree": math.NaN()}})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

// TS checks each error shape on its own: a mistyped member of one shape does
// not hide the other (z.union for transcription, the error object for
// speech synthesis).
func TestR93provbSpeechErrorShapesIndependent(t *testing.T) {
	err := azureSpeechAPIError("azure.transcription", 400, nil, []byte(`{"error":{"message":5},"message":"top"}`), false)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "top" {
		t.Fatalf("transcription err = %#v, want message top", err)
	}
	err = azureSpeechAPIError("azure.speech", 400, nil, []byte(`{"error":{"message":"nested"},"message":5}`), true)
	if !errors.As(err, &pe) || pe.Message != "nested" {
		t.Fatalf("speech err = %#v, want message nested", err)
	}
	err = azureSpeechAPIError("azure.transcription", 400, nil, []byte(`{"error":{"message":"nested"},"message":5}`), false)
	if !errors.As(err, &pe) || pe.Message != "nested" {
		t.Fatalf("transcription nested err = %#v, want message nested", err)
	}
}

// TS resolveVoice / buildSsml use /^([a-z]{2,3})-[a-z]{2,4}-/i without the u
// flag: only ASCII letters match, so a voice with a Kelvin sign has no
// locale.
func TestR93provbSpeechVoiceLocaleASCII(t *testing.T) {
	if m := azureVoiceLocalePattern.FindStringSubmatch("en-KR-Voice"); m != nil {
		t.Fatalf("locale match = %q, want none", m)
	}
	if m := azureVoiceLanguagePattern.FindStringSubmatch("ſv-SE-Voice"); m != nil {
		t.Fatalf("language match = %q, want none", m)
	}
	if m := azureVoiceLocalePattern.FindStringSubmatch("EN-us-Harper"); m == nil || m[1] != "EN-us" {
		t.Fatalf("locale match = %q", m)
	}
}

// TS formats `${event.error.code}` and the message with String(): a numeric
// code still fails the stream with the provider's message.
func TestR93provbMaiTranscriptionNumericErrorCode(t *testing.T) {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	errCh := make(chan error, 1)
	go func() {
		for {
			if _, err := result.Stream.Next(); err != nil {
				errCh <- err
				return
			}
		}
	}()
	server.toSend <- map[string]interface{}{"type": "error", "error": map[string]interface{}{"code": 429.0, "message": "slow down"}}
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	if err == nil || !strings.Contains(err.Error(), "Azure MAI transcription error (429): slow down") {
		t.Fatalf("err = %v", err)
	}
}

// TS mediaTypeToExtension: the lower-cased second segment ("a/b/c" -> "b").
func TestR93provbMediaTypeExtension(t *testing.T) {
	for in, want := range map[string]string{"audio/X-WAV": "wav", "audio/OGG": "ogg", "audio/webm/x": "webm", "": "", "audio": ""} {
		if got := mediaTypeToExtension(in); got != want {
			t.Errorf("mediaTypeToExtension(%q) = %q, want %q", in, got, want)
		}
	}
}
