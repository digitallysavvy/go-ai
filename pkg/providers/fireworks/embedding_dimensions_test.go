package fireworks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #22338: the top-level embedding dimensions setting maps to the provider
// field dimensions; an explicit provider option wins.
func TestEmbeddingTopLevelDimensions(t *testing.T) {
	cases := []struct {
		name     string
		top      *int
		provider interface{}
		want     interface{}
	}{
		{"unset", nil, nil, nil},
		{"top-level only", dimPtr(256), nil, float64(256)},
		{"provider option only", nil, float64(512), float64(512)},
		{"provider option wins", dimPtr(256), float64(512), float64(512)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()
			m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
			opts := &provider.EmbedModelOptions{Dimensions: tc.top}
			if tc.provider != nil {
				opts.ProviderOptions = map[string]interface{}{"fireworks": map[string]interface{}{"dimensions": tc.provider}}
			}
			_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
			if body == nil {
				t.Fatal("no request captured")
			}
			got := body["dimensions"]
			if got != tc.want {
				t.Fatalf("dimensions = %v, want %v", got, tc.want)
			}
		})
	}
}

func dimPtr(v int) *int { return &v }

// TS openai(-compatible) embedding options: providerOptions.fireworks.user is
// forwarded as the request's `user` field.
func TestEmbeddingUserOption(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"fireworks": map[string]interface{}{"user": "user-1"}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if body["user"] != "user-1" {
		t.Fatalf("user = %v, body = %#v", body["user"], body)
	}
	body = nil
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{})
	if _, ok := body["user"]; ok {
		t.Fatalf("user sent without option: %#v", body)
	}
}

// TS OpenAICompatibleEmbeddingModel always sends encoding_format: "float".
func TestEmbeddingSendsEncodingFormatFloat(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if body["encoding_format"] != "float" {
		t.Fatalf("encoding_format = %v, want float", body["encoding_format"])
	}
}
