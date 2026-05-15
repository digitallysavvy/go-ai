package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestElevenLabsSpeechModelMetadataAndGenerate(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}

	p := newElevenLabsProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("AUDIO")),
		}, nil
	})
	m := NewSpeechModel(p, "eleven_multilingual_v2")
	if m.SpecificationVersion() != "v3" || m.Provider() != "elevenlabs" || m.ModelID() != "eleven_multilingual_v2" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}

	out, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/text-to-speech/21m00Tcm4TlvDq8ikWAM" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["model_id"] != "eleven_multilingual_v2" || seenBody["text"] != "hello" {
		t.Fatalf("body mismatch: %#v", seenBody)
	}
	if string(out.Audio) != "AUDIO" || out.MimeType != "audio/mpeg" || out.Usage.CharacterCount != 5 {
		t.Fatalf("result mismatch: %#v", out)
	}
}

func TestElevenLabsSpeechModelErrorPaths(t *testing.T) {
	pErr := newElevenLabsProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewSpeechModel(pErr, "m").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err == nil {
		t.Fatal("expected transport error")
	}

	pStatus := newElevenLabsProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
		}, nil
	})
	if _, err := NewSpeechModel(pStatus, "m").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected status error, got %v", err)
	}
}

type elevenLabsRoundTripper func(*http.Request) (*http.Response, error)

func (f elevenLabsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newElevenLabsProviderWithTransport(t *testing.T, rt elevenLabsRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://elevenlabs.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://elevenlabs.example",
		Headers: map[string]string{
			"xi-api-key":   "k",
			"Content-Type": "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
