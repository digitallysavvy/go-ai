package googlevertex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r90provgImageServers starts an API server answering /images/generations
// with one image URL (built by imageURL from the API and foreign server
// URLs), and a foreign server that serves image bytes and records each
// request's headers.
type r90provgImageServers struct {
	mu         sync.Mutex
	apiHits    []http.Header
	foreignHit []http.Header
	api        *httptest.Server
	foreign    *httptest.Server
}

func r90provgStartImageServers(t *testing.T, imageURL func(api, foreign string) string) *r90provgImageServers {
	t.Helper()
	s := &r90provgImageServers{}
	s.foreign = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.foreignHit = append(s.foreignHit, r.Header.Clone())
		s.mu.Unlock()
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	t.Cleanup(s.foreign.Close)
	s.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.apiHits = append(s.apiHits, r.Header.Clone())
		s.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"url":"`+imageURL(s.api.URL, s.foreign.URL)+`"}]}`)
		case r.URL.Path == "/v1/redirect.png":
			w.Header().Set("Location", s.foreign.URL+"/image.png")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = w.Write([]byte{4, 5, 6})
		}
	}))
	t.Cleanup(s.api.Close)
	return s
}

func (s *r90provgImageServers) generate(t *testing.T) ([][]byte, error) {
	t.Helper()
	p := NewMaaS(MaaSConfig{
		BaseURL: s.api.URL + "/v1",
		Headers: StaticHeaders(map[string]string{"X-Provider-Key": "test-key"}),
		AuthToken: func(context.Context) (string, error) {
			return "test-token", nil
		},
	})
	model, err := p.ImageModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	res, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "A small blue square", N: &n})
	if err != nil {
		return nil, err
	}
	return res.Images, nil
}

// TestR90provgMaaSForeignImageDownloadIsValidated ports TS acdccac63d
// (google-vertex-maas-provider-node-download.test.ts): an image URL on a
// foreign origin is fetched through the validated download transport, never
// the MaaS auth transport. The foreign test server is on loopback, which the
// download guard rejects, so it must receive no request and no credential.
func TestR90provgMaaSForeignImageDownloadIsValidated(t *testing.T) {
	s := r90provgStartImageServers(t, func(_, foreign string) string { return foreign + "/image.png" })
	_, err := s.generate(t)
	if err == nil {
		t.Fatal("expected the foreign loopback image URL to be rejected by the download guard")
	}
	if len(s.foreignHit) != 0 {
		t.Fatalf("foreign server got %d requests (headers %v), want 0", len(s.foreignHit), s.foreignHit)
	}
	if len(s.apiHits) != 1 || s.apiHits[0].Get("Authorization") != "Bearer test-token" || s.apiHits[0].Get("X-Provider-Key") != "test-key" {
		t.Fatalf("API request headers = %v, want the token and provider header", s.apiHits)
	}
}

// TestR90provgMaaSRedirectHopIsValidated: an API-origin image URL that
// redirects to a foreign origin has the foreign hop validated (TS: every
// foreign hop, including redirect hops, uses fetchWithValidatedEndpoint).
func TestR90provgMaaSRedirectHopIsValidated(t *testing.T) {
	s := r90provgStartImageServers(t, func(api, _ string) string { return api + "/v1/redirect.png" })
	_, err := s.generate(t)
	if err == nil {
		t.Fatal("expected the foreign loopback redirect hop to be rejected by the download guard")
	}
	if len(s.foreignHit) != 0 {
		t.Fatalf("foreign server got %d requests (headers %v), want 0", len(s.foreignHit), s.foreignHit)
	}
}

// TestR90provgMaaSAPIOriginImageGetsCredentials: an image on the API origin
// (TS credentialedOrigin) is downloaded with the MaaS credentials.
func TestR90provgMaaSAPIOriginImageGetsCredentials(t *testing.T) {
	s := r90provgStartImageServers(t, func(api, _ string) string { return api + "/v1/files/image.png" })
	images, err := s.generate(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || string(images[0]) != string([]byte{4, 5, 6}) {
		t.Fatalf("images = %v, want [[4 5 6]]", images)
	}
	if len(s.apiHits) != 2 {
		t.Fatalf("API requests = %d, want 2", len(s.apiHits))
	}
	if h := s.apiHits[1]; h.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("download Authorization = %q, want the MaaS token", h.Get("Authorization"))
	}
}

// TestR90provgMaaSProviderIDs: TS createGoogleVertexMaas builds on
// createOpenAICompatible({name: 'vertex.maas'}), whose models report
// `${name}.${modelType}` (openai-compatible-provider.ts).
func TestR90provgMaaSProviderIDs(t *testing.T) {
	p := NewMaaS(MaaSConfig{BaseURL: "https://example.com/v1", AuthToken: func(context.Context) (string, error) { return "t", nil }})
	lm, _ := p.LanguageModel("m")
	cm, _ := p.CompletionModel("m")
	em, _ := p.EmbeddingModel("m")
	im, _ := p.ImageModel("m")
	got := []string{lm.Provider(), cm.Provider(), em.Provider(), im.Provider()}
	want := []string{"vertex.maas.chat", "vertex.maas.completion", "vertex.maas.embedding", "vertex.maas.image"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("provider %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestR90provgEmbeddingFractionalTokens: TS usage.tokens is the sum of the
// z.number() statistics.token_count values, kept exactly.
func TestR90provgEmbeddingFractionalTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[{"embeddings":{"values":[0.1],"statistics":{"token_count":1.5}}},{"embeddings":{"values":[0.2],"statistics":{"token_count":2}}}]}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, AccessToken: "t"})
	m, _ := p.EmbeddingModel("text-embedding-005")
	res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Tokens != 3.5 {
		t.Fatalf("tokens = %v, want 3.5", res.Usage.Tokens)
	}
}

// TestR90provgVideoNoVideosErrorText: TS throws `No videos in response.
// Response: ${JSON.stringify(finalOperation)}` (google-vertex-video-model.ts
// buildCompletedResult); an absent name is left out and an empty videos array
// prints as [].
func TestR90provgVideoNoVideosErrorText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"done":true,"response":{"videos":[]}}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, AccessToken: "t"})
	m := NewVideoModel(p, "veo-3.0-generate-001")
	_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"operationName":"op"}`)})
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `No videos in response. Response: {"done":true,"response":{"videos":[]}}`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestR90provgLiveRawChunkForMismatchedMessage: TS onMessageText reports
// every safeParseJSON-parsed message raw before reading fields, so a message
// whose fields do not fit the expected shape still yields a raw part, while a
// message with a forbidden prototype key does not parse at all.
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

// TestR90provgCloudTTSRequestBodyText: TS request.body is
// JSON.stringify(requestBody), which leaves HTML characters literal.
func TestR90provgCloudTTSRequestBodyText(t *testing.T) {
	server, _ := newChirpTestServer(t, map[string]interface{}{"audioContent": chirpAudioBase64})
	defer server.Close()
	p := newChirpProvider(t, server.URL)
	model, _ := p.SpeechModel("chirp-3-hd")
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "<b>Tom & Jerry</b>"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"input":{"text":"<b>Tom & Jerry</b>"},"voice":{"languageCode":"en-US","name":"en-US-Chirp3-HD-Kore"},"audioConfig":{"audioEncoding":"LINEAR16"}}`
	if got := result.Request.Body; got != want {
		t.Fatalf("request body = %v, want %s", got, want)
	}
}
