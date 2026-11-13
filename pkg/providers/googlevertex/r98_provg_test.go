package googlevertex

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS google-vertex-transcription-model.ts posts with postJsonToApi, which
// tags the user-agent with the runtime after the provider tag.
func TestR98provgChirpRecognizeCarriesRuntimeUserAgent(t *testing.T) {
	var ua string
	p, err := New(Config{
		Project: "p", Location: "us-central1", AccessToken: "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			ua = r.Header.Get("User-Agent")
			return jsonResponse(200, `{"results":[]}`), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.TranscriptionModel("chirp_2")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQID", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if !strings.HasPrefix(ua, "ai-sdk-google-vertex/") || !strings.HasSuffix(ua, " "+rt) {
		t.Fatalf("user-agent = %q, want ai-sdk-google-vertex/... %s", ua, rt)
	}
}
