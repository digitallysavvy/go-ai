package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS buildXaiStreamingTranscriptionUrl appends to url.searchParams, which
// re-parses the base URL's query leniently (an invalid escape stays literal,
// "+" is a space). Want printed by Node 24:
// new URL("https://x.example/v1?k=%zz&p=a+b%41/stt") + append.
func TestR87provbStreamingTranscriptionBaseQueryLenient(t *testing.T) {
	u, err := buildXAIStreamingTranscriptionURL("https://x.example/v1?k=%zz&p=a+b%41", "grok-stt", provider.AudioFormat{Type: "audio/pcm"}, XAITranscriptionProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://x.example/v1?k=%25zz&p=a+bA%2Fstt&model=grok-stt&encoding=pcm"
	if got := u.String(); got != want {
		t.Errorf("URL =\n%s\nwant\n%s", got, want)
	}
}
