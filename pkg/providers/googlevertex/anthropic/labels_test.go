package anthropic

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type labelsCapture struct {
	mu      sync.Mutex
	headers []http.Header
	bodies  []string
}

func (c *labelsCapture) add(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers = append(c.headers, r.Header.Clone())
	c.bodies = append(c.bodies, string(b))
}

func newLabelsModel(t *testing.T, providerHeaders map[string]string) (provider.LanguageModel, *labelsCapture) {
	t.Helper()
	cap := &labelsCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.add(r)
		if strings.HasSuffix(r.URL.Path, "streamRawPredict") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"x\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg","type":"message","role":"assistant","model":"m","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	p := New(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: srv.Client(),
		Headers:    providerHeaders,
	})
	m, err := p.LanguageModel("claude-sonnet-4-6@default")
	if err != nil {
		t.Fatal(err)
	}
	return m, cap
}

func callLabels(t *testing.T, m provider.LanguageModel, stream bool, opts provider.GenerateOptions) error {
	t.Helper()
	opts.Prompt = types.Prompt{Text: "Hello"}
	if !stream {
		_, err := m.DoGenerate(context.Background(), &opts)
		return err
	}
	s, err := m.DoStream(context.Background(), &opts)
	if err != nil {
		return err
	}
	defer s.Close() //nolint:errcheck
	for {
		if _, err := s.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestVertexAnthropicLabelsHeader(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "generate", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			t.Run("sends labels and preserves other headers", func(t *testing.T) {
				m, cap := newLabelsModel(t, map[string]string{"x-custom-header": "preserved"})
				err := callLabels(t, m, stream, provider.GenerateOptions{
					ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]interface{}{"team": "data-platform", "feature": "my-app"}}},
					Headers:         map[string]string{"x-request-header": "preserved"},
				})
				if err != nil {
					t.Fatal(err)
				}
				h := cap.headers[0]
				if got, want := h.Get("x-vertex-ai-labels"), b64(`{"feature":"my-app","team":"data-platform"}`); got != want {
					t.Fatalf("labels header = %q, want %q", got, want)
				}
				if h.Get("Authorization") != "Bearer test-token" || h.Get("x-custom-header") != "preserved" || h.Get("x-request-header") != "preserved" {
					t.Fatalf("headers = %v", h)
				}
				if got := cap.bodies[0]; strings.Contains(got, `"labels"`) {
					t.Fatalf("labels must not be in body: %s", got)
				}
			})
			t.Run("omitted when absent", func(t *testing.T) {
				for _, po := range []map[string]interface{}{nil, {"vertex": map[string]interface{}{}}} {
					m, cap := newLabelsModel(t, nil)
					if err := callLabels(t, m, stream, provider.GenerateOptions{ProviderOptions: po}); err != nil {
						t.Fatal(err)
					}
					if _, ok := cap.headers[0]["X-Vertex-Ai-Labels"]; ok {
						t.Fatalf("unexpected labels header")
					}
				}
			})
			t.Run("empty map and utf-8", func(t *testing.T) {
				m, cap := newLabelsModel(t, nil)
				_ = callLabels(t, m, stream, provider.GenerateOptions{ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]string{}}}})
				_ = callLabels(t, m, stream, provider.GenerateOptions{ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]string{"team": "équipe"}}}})
				if got := cap.headers[0].Get("x-vertex-ai-labels"); got != "e30=" {
					t.Fatalf("empty = %q", got)
				}
				if got := cap.headers[1].Get("x-vertex-ai-labels"); got != "eyJ0ZWFtIjoiw6lxdWlwZSJ9" {
					t.Fatalf("utf8 = %q", got)
				}
			})
			t.Run("explicit request header wins", func(t *testing.T) {
				for _, hn := range []string{"x-vertex-ai-labels", "X-Vertex-AI-Labels"} {
					m, cap := newLabelsModel(t, nil)
					if err := callLabels(t, m, stream, provider.GenerateOptions{
						ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]string{"team": "x"}}},
						Headers:         map[string]string{hn: "explicit-labels"},
					}); err != nil {
						t.Fatal(err)
					}
					if got := cap.headers[0].Get("x-vertex-ai-labels"); got != "explicit-labels" {
						t.Fatalf("%s: got %q", hn, got)
					}
				}
			})
			t.Run("explicit provider header wins", func(t *testing.T) {
				m, cap := newLabelsModel(t, map[string]string{"X-Vertex-AI-Labels": "provider-labels"})
				if err := callLabels(t, m, stream, provider.GenerateOptions{
					ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]string{"team": "x"}}},
				}); err != nil {
					t.Fatal(err)
				}
				if got := cap.headers[0].Get("x-vertex-ai-labels"); got != "provider-labels" {
					t.Fatalf("got %q", got)
				}
			})
			t.Run("invalid labels rejected before sending", func(t *testing.T) {
				for _, bad := range []interface{}{nil, "invalid", map[string]interface{}{"team": 123}, []interface{}{"team"}} {
					m, cap := newLabelsModel(t, nil)
					err := callLabels(t, m, stream, provider.GenerateOptions{ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"labels": bad}}})
					var iae *providererrors.InvalidArgumentError
					if !errors.As(err, &iae) {
						t.Fatalf("labels %v: err = %v, want InvalidArgumentError", bad, err)
					}
					if len(cap.headers) != 0 {
						t.Fatalf("request was sent for invalid labels %v", bad)
					}
				}
			})
		})
	}
}
