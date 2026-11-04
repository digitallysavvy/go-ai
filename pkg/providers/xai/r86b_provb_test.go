package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR86bprovbBaseURLOneTrailingSlash pins TS createXai's
// withoutTrailingSlash(options.baseURL) (one "/" removed) and
// `${baseURL}/stt` for the streaming transcription URL. Wants printed by
// Node 24.
func TestR86bprovbBaseURLOneTrailingSlash(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://x.example/v1//"})
	if p.config.BaseURL != "https://x.example/v1/" || p.realtimeBaseURL != "https://x.example/v1/" {
		t.Fatalf("base URLs = %q, %q; want https://x.example/v1/", p.config.BaseURL, p.realtimeBaseURL)
	}
	u, err := buildXAIStreamingTranscriptionURL(p.config.BaseURL, "grok-stt", provider.AudioFormat{Type: "audio/pcm"}, XAITranscriptionProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/v1//stt" {
		t.Errorf("stt path = %q, want /v1//stt", u.Path)
	}
}
