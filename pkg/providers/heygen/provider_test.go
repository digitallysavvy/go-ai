package heygen

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Ported from packages/heygen/src/heygen-provider.test.ts.

func TestProvider_CreatesVideoModelsThroughBothAliases(t *testing.T) {
	p := New(Config{})
	if p.Name() != "heygen" || CreateHeyGen(Config{}).Name() != "heygen" {
		t.Fatalf("Name = %q", p.Name())
	}
	for _, get := range []func(string) (provider.VideoModelV3, error){p.Video, p.VideoModel} {
		model, err := get("heygen-video-1")
		if err != nil {
			t.Fatal(err)
		}
		if model.SpecificationVersion() != "v4" || model.Provider() != "heygen.video" ||
			model.ModelID() != "heygen-video-1" || model.MaxVideosPerCall() == nil || *model.MaxVideosPerCall() != 1 {
			t.Fatalf("model = %#v", model)
		}
		if _, ok := model.(provider.VideoModelStarter); !ok {
			t.Fatal("model must implement VideoModelStarter")
		}
		if _, ok := model.(provider.VideoModelStatusChecker); !ok {
			t.Fatal("model must implement VideoModelStatusChecker")
		}
	}
}

func TestProvider_RejectsUnsupportedModelTypes(t *testing.T) {
	p := New(Config{})
	checks := []error{}
	_, err := p.LanguageModel("other")
	checks = append(checks, err)
	_, err = p.EmbeddingModel("other")
	checks = append(checks, err)
	_, err = p.ImageModel("other")
	checks = append(checks, err)
	_, err = p.SpeechModel("other")
	checks = append(checks, err)
	_, err = p.TranscriptionModel("other")
	checks = append(checks, err)
	_, err = p.RerankingModel("other")
	checks = append(checks, err)
	for _, err := range checks {
		var nsm *providererrors.NoSuchModelError
		if !errors.As(err, &nsm) {
			t.Fatalf("err = %v, want NoSuchModelError", err)
		}
	}
}

func TestProvider_KeysEndpointsHeadersAndUserAgent(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Setenv("HEYGEN_API_KEY", "env-key")
		var mu sync.Mutex
		var gotPath string
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotPath, gotHeaders = r.URL.Path, r.Header.Clone()
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"status":"pending"}}`))
		}))
		cfg := Config{BaseURL: srv.URL + "/heygen/", Headers: map[string]string{"X-Custom": "custom"}}
		if explicit {
			cfg.APIKey = "explicit-key"
		}
		model, _ := New(cfg).VideoModel("heygen-video-1")
		_, err := model.(*VideoModel).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{
			Operation: testOperation,
			Headers:   map[string]string{"X-Request": "request"},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := "env-key"
		if explicit {
			want = "explicit-key"
		}
		mu.Lock()
		if gotPath != "/heygen/v3/models/videos/video-1" || gotHeaders.Get("x-api-key") != want ||
			gotHeaders.Get("x-custom") != "custom" || gotHeaders.Get("x-request") != "request" ||
			!strings.Contains(gotHeaders.Get("user-agent"), "ai-sdk-heygen/"+version.Version) {
			t.Fatalf("explicit=%v path=%q headers=%#v", explicit, gotPath, gotHeaders)
		}
		mu.Unlock()
	}
}

func TestProvider_LoadsCredentialsLazilyAndReportsMissingKey(t *testing.T) {
	t.Setenv("HEYGEN_API_KEY", "")
	model, _ := New(Config{}).VideoModel("heygen-video-1")
	_, err := model.(*VideoModel).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err == nil || !strings.Contains(err.Error(), "HEYGEN_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestSerializeAndDeserializeVideoModel(t *testing.T) {
	model, _ := New(Config{APIKey: "secret"}).VideoModel("heygen-video-1")
	serialized := model.(*VideoModel).Serialize()
	if serialized.Provider != "heygen.video" || serialized.ModelID != "heygen-video-1" {
		t.Fatalf("serialized = %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey must not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeVideoModel(serialized)
	if err != nil || restored.Provider() != "heygen.video" || restored.ModelID() != "heygen-video-1" {
		t.Fatalf("restored = %#v err=%v", restored, err)
	}
}
