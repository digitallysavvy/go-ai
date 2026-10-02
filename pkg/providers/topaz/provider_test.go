package topaz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestNewProvider_Defaults mirrors TS "reports specification version v4" /
// basic construction.
func TestNewProvider_Defaults(t *testing.T) {
	t.Setenv("TOPAZ_API_KEY", "")
	prov := New(Config{APIKey: "test-key"})
	if prov.Name() != "topaz" {
		t.Errorf("Name() = %q, want topaz", prov.Name())
	}
	if prov.baseURL() != defaultBaseURL {
		t.Errorf("baseURL() = %q, want %q", prov.baseURL(), defaultBaseURL)
	}
}

// TestNewProvider_APIKeyFromEnv verifies the TOPAZ_API_KEY environment
// variable is used when Config.APIKey is empty.
func TestNewProvider_APIKeyFromEnv(t *testing.T) {
	t.Setenv("TOPAZ_API_KEY", "env-key")
	prov := New(Config{})
	if prov.config.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key", prov.config.APIKey)
	}
}

// TestNewProvider_CustomBaseURLAndHeaders mirrors TS "sends the API key,
// custom headers and user agent to a custom base URL".
func TestNewProvider_CustomBaseURLAndHeaders(t *testing.T) {
	prov := New(Config{
		APIKey:  "custom-api-key",
		BaseURL: "https://custom-api.topazlabs.com",
		Headers: map[string]string{"X-Custom-Header": "value"},
	})
	if prov.baseURL() != "https://custom-api.topazlabs.com" {
		t.Errorf("baseURL() = %q", prov.baseURL())
	}
}

// TestCreateImageModel mirrors TS "creates image models".
func TestCreateImageModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m, err := prov.ImageModel(ImageModelWonder35)
	if err != nil {
		t.Fatalf("ImageModel() error: %v", err)
	}
	if m.Provider() != "topaz.image" {
		t.Errorf("Provider() = %q, want topaz.image", m.Provider())
	}
	if m.ModelID() != ImageModelWonder35 {
		t.Errorf("ModelID() = %q, want %q", m.ModelID(), ImageModelWonder35)
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", m.SpecificationVersion())
	}
}

// TestCreateVideoModel mirrors TS "creates video models".
func TestCreateVideoModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m, err := prov.VideoModel(VideoModelStarlightPrecise26)
	if err != nil {
		t.Fatalf("VideoModel() error: %v", err)
	}
	if m.Provider() != "topaz.video" {
		t.Errorf("Provider() = %q, want topaz.video", m.Provider())
	}
	if m.ModelID() != VideoModelStarlightPrecise26 {
		t.Errorf("ModelID() = %q, want %q", m.ModelID(), VideoModelStarlightPrecise26)
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", m.SpecificationVersion())
	}
}

// TestAliases mirrors TS "exposes short aliases for both model types".
func TestAliases(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	image, err := prov.Image(ImageModelWonder35)
	if err != nil || image.ModelID() != ImageModelWonder35 {
		t.Errorf("Image() alias = %v, %v", image, err)
	}
	video, err := prov.Video(VideoModelProteus)
	if err != nil || video.ModelID() != VideoModelProteus {
		t.Errorf("Video() alias = %v, %v", video, err)
	}
}

// TestDefaultModelIDs verifies an empty model ID defaults sensibly (Go
// factory convention across providers), matching the other model-id
// defaults in this package.
func TestDefaultModelIDs(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	image, err := prov.ImageModel("")
	if err != nil || image.ModelID() != ImageModelWonder35 {
		t.Errorf("default image ModelID() = %v, %v", image, err)
	}
	video, err := prov.VideoModel("")
	if err != nil || video.ModelID() != VideoModelProteus {
		t.Errorf("default video ModelID() = %v, %v", video, err)
	}
}

// TestProvider_UnsupportedModels mirrors TS "throws NoSuchModelError for
// unsupported model types" (language/embedding) plus the other model
// types Topaz has no support for at all.
func TestProvider_UnsupportedModels(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	if _, err := prov.LanguageModel("x"); err == nil {
		t.Error("LanguageModel should be unsupported")
	}
	if _, err := prov.EmbeddingModel("x"); err == nil {
		t.Error("EmbeddingModel should be unsupported")
	}
	if _, err := prov.SpeechModel("x"); err == nil {
		t.Error("SpeechModel should be unsupported")
	}
	if _, err := prov.TranscriptionModel("x"); err == nil {
		t.Error("TranscriptionModel should be unsupported")
	}
	if _, err := prov.RerankingModel("x"); err == nil {
		t.Error("RerankingModel should be unsupported")
	}
}

func TestCreateTopaz(t *testing.T) {
	prov := CreateTopaz(Config{APIKey: "test-key"})
	if prov == nil || prov.Name() != "topaz" {
		t.Fatalf("CreateTopaz() = %v", prov)
	}
}

// TestProvider_SendsAPIKeyCustomHeadersAndUserAgent mirrors TS "sends the
// API key, custom headers and user agent to a custom base URL", using the
// video model's DoStatus call (a plain GET against the configured base
// URL) to inspect the outgoing request.
func TestProvider_SendsAPIKeyCustomHeadersAndUserAgent(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"processing"}`))
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Headers: map[string]string{"X-Custom": "custom-value"},
	})

	video, err := prov.VideoModel(VideoModelProteus)
	if err != nil {
		t.Fatalf("VideoModel() error: %v", err)
	}

	operation, _ := json.Marshal(map[string]string{"requestId": "req-1"})
	_, err = video.(*VideoModel).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: operation})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}

	if got := gotHeader.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key header = %q, want test-key", got)
	}
	if got := gotHeader.Get("X-Custom"); got != "custom-value" {
		t.Errorf("X-Custom header = %q, want custom-value", got)
	}
	if ua := gotHeader.Get("User-Agent"); ua == "" {
		t.Error("User-Agent header is empty")
	} else if want := "ai-sdk-topaz/"; !strings.Contains(ua, want) {
		t.Errorf("User-Agent header = %q, want to contain %q", ua, want)
	}
}
