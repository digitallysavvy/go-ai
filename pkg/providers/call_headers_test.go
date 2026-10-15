package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestLanguageModelsForwardPerCallHeaders: TS combineHeaders(config.headers(),
// options.headers) -- headers passed on the call must reach the wire for both
// generate and stream, in every HTTP language model.
func TestLanguageModelsForwardPerCallHeaders(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Call-Header"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
	}))
	defer srv.Close()

	models := map[string]func() (provider.LanguageModel, error){
		"alibaba": func() (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"cohere": func() (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"deepseek": func() (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"fireworks": func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"gmicloud": func() (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"groq": func() (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"mistral": func() (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"moonshot": func() (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"ollama": func() (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: srv.URL}).LanguageModel("m")
		},
		"openai": func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gpt-4o")
		},
		"openai-resp": func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		},
		"openresponses": func() (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{Name: "x", BaseURL: srv.URL}).LanguageModel("m")
		},
		"together": func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"xai": func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"zai": func() (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
	}

	for name, mk := range models {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			opts := &provider.GenerateOptions{
				Prompt:  types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Headers: map[string]string{"X-Call-Header": "yes"},
			}
			mu.Lock()
			seen = nil
			mu.Unlock()
			_, _ = m.DoGenerate(context.Background(), opts)
			_, _ = m.DoStream(context.Background(), opts)
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 2 || seen[0] != "yes" || seen[1] != "yes" {
				t.Fatalf("call header not forwarded on generate+stream: %v", seen)
			}
		})
	}
}

// TestGenerateResponseMetadataAndHeaders: TS returns response {id, timestamp,
// modelId, headers} from doGenerate for the Responses-style and Cohere models.
func TestGenerateResponseMetadataAndHeaders(t *testing.T) {
	respBody := `{"id":"resp_1","created_at":1700000000,"model":"model-x","status":"completed","output":[],"generation_id":"gen_1","message":{"role":"assistant","content":[]},"finish_reason":"COMPLETE","usage":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Resp", "hdr")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	cases := map[string]struct {
		mk     func() (provider.LanguageModel, error)
		wantID string
		model  bool
	}{
		"openai-resp": {func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		}, "resp_1", true},
		"xai": {func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, "resp_1", true},
		"openresponses": {func() (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{Name: "x", BaseURL: srv.URL}).LanguageModel("m")
		}, "resp_1", true},
		"cohere": {func() (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}, "gen_1", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := c.mk()
			if err != nil {
				t.Fatal(err)
			}
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.ResponseHeaders["X-Resp"] == "" && res.ResponseHeaders["x-resp"] == "" {
				t.Errorf("response headers missing: %v", res.ResponseHeaders)
			}
			rm := res.ResponseMetadata
			if rm == nil || rm.ID != c.wantID {
				t.Fatalf("response metadata = %+v", rm)
			}
			if c.model && (rm.ModelID != "model-x" || rm.Timestamp.Unix() != 1700000000) {
				t.Errorf("model/timestamp = %q %v", rm.ModelID, rm.Timestamp)
			}
		})
	}
}

func TestImageModelsForwardPerCallHeaders(t *testing.T) {
	var mu sync.Mutex
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Get("X-Call-Header")
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	mks := map[string]func() (provider.ImageModel, error){
		"fal": func() (provider.ImageModel, error) {
			return fal.New(fal.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"together": func() (provider.ImageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"fireworks": func() (provider.ImageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"stability": func() (provider.ImageModel, error) {
			return stability.New(stability.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"replicate": func() (provider.ImageModel, error) {
			return replicate.New(replicate.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
	}
	for name, mk := range mks {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			seen = ""
			mu.Unlock()
			_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", Headers: map[string]string{"X-Call-Header": "yes"}})
			mu.Lock()
			defer mu.Unlock()
			if seen != "yes" {
				t.Fatalf("call header not forwarded: %q", seen)
			}
		})
	}
}

func TestResponsesStreamMetadataCarriesHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Resp", "hdr")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"created_at\":1700000000,\"model\":\"m\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))
	}))
	defer srv.Close()
	for name, mk := range map[string]func() (provider.LanguageModel, error){
		"openai": func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		},
		"xai": func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			st, err := m.DoStream(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close() //nolint:errcheck
			for {
				ch, err := st.Next()
				if err != nil {
					t.Fatalf("no response-metadata chunk: %v", err)
				}
				if ch != nil && ch.Type == provider.ChunkTypeResponseMetadata {
					h := ch.ResponseMetadata.Headers
					if h["X-Resp"] == "" && h["x-resp"] == "" {
						t.Fatalf("headers missing: %v", h)
					}
					return
				}
			}
		})
	}
}
