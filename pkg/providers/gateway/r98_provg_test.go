package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS gateway-provider.ts mintClientSecret posts with postJsonToApi:
// createAuthHeaders' ai-sdk-gateway tag, then the runtime tag.
func TestR98provgMintClientSecretCarriesRuntimeUserAgent(t *testing.T) {
	var ua string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "openai/gpt-realtime"}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if !strings.HasPrefix(ua, "ai-sdk-gateway/") || !strings.HasSuffix(ua, " "+rt) {
		t.Fatalf("user-agent = %q, want ai-sdk-gateway/... %s", ua, rt)
	}
}

// TS mintClientSecret body: {model, routeKind?, expiresIn?} in that order.
func TestR98provgMintClientSecretBodyKeyOrder(t *testing.T) {
	var body string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	expires := 60
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "m", RouteKind: "transcription", ExpiresAfterSeconds: &expires}); err != nil {
		t.Fatal(err)
	}
	if want := `{"model":"m","routeKind":"transcription","expiresIn":60}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}

// TS gateway-transcription-model.ts startFrame: {type, inputAudioFormat,
// providerOptions, includeRawChunks}, sent with JSON.stringify.
func TestR98provgTranscriptionStartFrameKeyOrder(t *testing.T) {
	server := newGatewayTranscriptionTestServer(t)
	defer server.close()
	model := newTestGatewayTranscriptionModel(t, server.ts.URL)
	rate := 16000
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanTestAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
		ProviderOptions:  map[string]interface{}{"openai": map[string]interface{}{"language": "en"}},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)
	server.mu.Lock()
	got := server.receivedText[0]
	server.mu.Unlock()
	want := `{"type":"` + transcriptionStreamStartFrameType + `","inputAudioFormat":{"type":"audio/pcm","rate":16000},"providerOptions":{"openai":{"language":"en"}},"includeRawChunks":true}`
	if got != want {
		t.Fatalf("start frame =\n%s\nwant\n%s", got, want)
	}
}
