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

// TestR86bprovbStreamingTranscriptionQueryOrder pins TS
// buildXaiStreamingTranscriptionUrl: url.searchParams.append in
// getXaiSttParams order then encoding and the streaming options, form
// escaping ("~" -> %7E, "*" literal, space "+") and String(number). Want printed by Node 24.
func TestR86bprovbStreamingTranscriptionQueryOrder(t *testing.T) {
	lang := "en~x*y z"
	channels, vad, smart := 2, 1e-7, 0.5
	yes := true
	rate := 16000
	u, err := buildXAIStreamingTranscriptionURL("https://api.x.ai/v1", "grok-stt", provider.AudioFormat{Type: "audio/pcm", Rate: &rate}, XAITranscriptionProviderOptions{
		Language: &lang, Channels: &channels, Diarize: &yes, VadThreshold: &vad, Keyterm: []string{"a b", "c~"},
		Streaming: &XAIStreamingTranscriptionOptions{InterimResults: &yes, SmartTurn: &smart},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://api.x.ai/v1/stt?model=grok-stt&sample_rate=16000&language=en%7Ex*y+z&channels=2&diarize=true&vad_threshold=1e-7&keyterm=a+b&keyterm=c%7E&encoding=pcm&interim_results=true&smart_turn=0.5"
	if got := u.String(); got != want {
		t.Errorf("URL =\n%s\nwant\n%s", got, want)
	}
}
