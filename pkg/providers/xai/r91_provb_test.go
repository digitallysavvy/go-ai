package xai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91provbServer(t *testing.T, h http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL})
}

// PB-22 (provb2a-04): a failure with no HTTP response (here a cancelled
// context) is returned unchanged like TS handleFetchError, not wrapped in a
// status-0 ProviderError.
func TestR91provbXAITransportErrorsUnchanged(t *testing.T) {
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	check := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.Is(err, context.Canceled) || errors.As(err, &pe) {
			t.Fatalf("%s: err = %#v", name, err)
		}
	}
	_, err := NewResponsesLanguageModel(p, "grok-4").DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("responses", err)
	_, err = NewSpeechModel(p, "grok-tts").DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi"})
	check("speech", err)
	_, err = NewTranscriptionModel(p, "grok-stt").DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav"})
	check("transcription", err)
	_, err = NewImageModel(p, ModelGrokImagineImage).DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
	check("image", err)
}

// PB-23 (provb2a-05): TS throws a plain Error / AISDKError with the bare
// message for an image moderation block, a video start without request_id
// and a failed realtime client secret.
func TestR91provbXAIPlainErrors(t *testing.T) {
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/images/generations":
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"AA==","respect_moderation":false}]}`))
		case "/videos/generations":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`nope`))
		}
	})
	_, err := NewImageModel(p, ModelGrokImagineImage).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err == nil || err.Error() != "Image generation was blocked due to a content policy violation." {
		t.Fatalf("image: %v", err)
	}
	_, err = NewVideoModel(p, "grok-imagine-video").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	var vge *providererrors.VideoGenerationError
	if !errors.As(err, &vge) || err.Error() != "No request_id returned from xAI API." {
		t.Fatalf("video: %v", err)
	}
	_, err = NewRealtimeModel(p, "grok-voice").DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil || err.Error() != "xAI realtime client secret request failed: 403 nope" {
		t.Fatalf("realtime: %v", err)
	}
}

// PB-24 (provb2a-06): replace: {} and explicit nulls are sent (TS copies
// xaiOptions?.<key>, zod .nullish keeps null).
func TestR91provbXAISpeechNullAndEmptyReplace(t *testing.T) {
	var body map[string]interface{}
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusBadRequest)
	})
	_, _ = NewSpeechModel(p, "grok-tts").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{
		"xai": map[string]interface{}{"replace": map[string]interface{}{}, "textNormalization": nil, "withTimestamps": nil},
	}})
	if r, ok := body["replace"].(map[string]interface{}); !ok || len(r) != 0 {
		t.Fatalf("replace = %#v", body["replace"])
	}
	for _, k := range []string{"text_normalization", "with_timestamps"} {
		if v, present := body[k]; !present || v != nil {
			t.Fatalf("%s = %#v, present=%v", k, v, present)
		}
	}
}

// PB-25 (provb2a-07): too many referenceVoiceIds / keyframes is TS
// parseProviderOptions's "invalid xai provider options".
func TestR91provbXAIVideoLimitsError(t *testing.T) {
	p := New(Config{APIKey: "k"})
	for _, o := range []map[string]interface{}{
		{"referenceVoiceIds": []string{"a", "b", "c", "d"}},
		{"keyframes": []map[string]interface{}{{"imageUrl": "https://e/1", "timestampSeconds": 1}, {"imageUrl": "https://e/2", "timestampSeconds": 2}, {"imageUrl": "https://e/3", "timestampSeconds": 3}, {"imageUrl": "https://e/4", "timestampSeconds": 4}, {"imageUrl": "https://e/5", "timestampSeconds": 5}}},
	} {
		_, err := NewVideoModel(p, "grok-imagine-video").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"xai": o}}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" || ia.Message != "invalid xai provider options" {
			t.Fatalf("%v: err = %v", o, err)
		}
	}
}

// PB-26 (provc-06 / provb2a-10): repeated response headers are joined like
// TS extractResponseHeaders.
func TestR91provbXAIRepeatedHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("X-Test", "a")
	h.Add("X-Test", "b")
	if got := flattenHeaders(h); got["X-Test"] != "a, b" {
		t.Fatalf("headers = %v", got)
	}
}
