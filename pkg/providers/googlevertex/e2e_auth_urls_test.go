package googlevertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// recorder is an http.RoundTripper that answers from a TS fixture and keeps
// every request, so the real URL construction and auth headers are exercised
// without a listener.
type recorder struct {
	mu      sync.Mutex
	reqs    []*http.Request
	fixture func(r *http.Request) (string, []byte)
}

func (rc *recorder) RoundTrip(r *http.Request) (*http.Response, error) {
	rc.mu.Lock()
	rc.reqs = append(rc.reqs, r.Clone(context.Background()))
	rc.mu.Unlock()
	ct, body := rc.fixture(r)
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{ct}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    r,
	}, nil
}

func geminiFixtureFor(t *testing.T, name string) func(r *http.Request) (string, []byte) {
	return func(r *http.Request) (string, []byte) {
		if strings.Contains(r.URL.Path, ":streamGenerateContent") {
			raw, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", name+".chunks.txt"))
			if err != nil {
				t.Fatal(err)
			}
			var sb strings.Builder
			for _, l := range strings.Split(string(raw), "\n") {
				if strings.TrimSpace(l) != "" {
					sb.WriteString("data: " + l + "\n\n")
				}
			}
			return "text/event-stream", []byte(sb.String())
		}
		raw, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return "application/json", raw
	}
}

func run(t *testing.T, m provider.LanguageModel) {
	t.Helper()
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer s.Close() //nolint:errcheck
	for {
		if _, err := s.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			t.Fatalf("stream next: %v", err)
		}
	}
}

func TestVertexGeminiURLsAndBearerAuth(t *testing.T) {
	for _, tc := range []struct {
		location string
		host     string
	}{
		{"us-central1", "us-central1-aiplatform.googleapis.com"},
		{"global", "aiplatform.googleapis.com"},
		{"eu", "aiplatform.eu.rep.googleapis.com"},
	} {
		rc := &recorder{fixture: geminiFixtureFor(t, "google-text")}
		p, err := New(Config{Project: "my-proj", Location: tc.location, AccessToken: "tok-123", HTTPClient: &http.Client{Transport: rc}})
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.LanguageModel("gemini-2.5-flash")
		if err != nil {
			t.Fatal(err)
		}
		run(t, m)
		if len(rc.reqs) != 2 {
			t.Fatalf("%s: requests = %d", tc.location, len(rc.reqs))
		}
		base := "https://" + tc.host + "/v1beta1/projects/my-proj/locations/" + tc.location + "/publishers/google/models/gemini-2.5-flash"
		if got := rc.reqs[0].URL.String(); got != base+":generateContent" {
			t.Errorf("generate URL = %s", got)
		}
		if got := rc.reqs[1].URL.String(); got != base+":streamGenerateContent?alt=sse" {
			t.Errorf("stream URL = %s", got)
		}
		for _, r := range rc.reqs {
			if r.Header.Get("Authorization") != "Bearer tok-123" || r.Header.Get("x-goog-api-key") != "" {
				t.Errorf("auth headers: Authorization=%q x-goog-api-key=%q", r.Header.Get("Authorization"), r.Header.Get("x-goog-api-key"))
			}
			if !strings.Contains(r.Header.Get("User-Agent"), "google-vertex") {
				t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
			}
		}
	}
}

func TestVertexExpressModeAPIKey(t *testing.T) {
	rc := &recorder{fixture: geminiFixtureFor(t, "google-text")}
	p, err := New(Config{APIKey: "express-key", HTTPClient: &http.Client{Transport: rc}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("gemini-2.5-flash")
	run(t, m)
	if got := rc.reqs[0].URL.String(); got != "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:generateContent" {
		t.Errorf("express URL = %s", got)
	}
	for _, r := range rc.reqs {
		if r.Header.Get("x-goog-api-key") != "express-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("express auth: x-goog-api-key=%q Authorization=%q", r.Header.Get("x-goog-api-key"), r.Header.Get("Authorization"))
		}
	}
}

// The token callback runs per request and its failure fails the call instead
// of sending an unauthenticated request.
func TestVertexAuthTokenCallbackPerRequestAndFailure(t *testing.T) {
	rc := &recorder{fixture: geminiFixtureFor(t, "google-text")}
	n := 0
	p, err := New(Config{Project: "p", Location: "us-central1", HTTPClient: &http.Client{Transport: rc},
		AuthToken: func(ctx context.Context) (string, error) {
			n++
			return "tok-" + string(rune('0'+n)), nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("gemini-2.5-flash")
	run(t, m)
	if rc.reqs[0].Header.Get("Authorization") != "Bearer tok-1" || rc.reqs[1].Header.Get("Authorization") != "Bearer tok-2" {
		t.Errorf("tokens: %q %q", rc.reqs[0].Header.Get("Authorization"), rc.reqs[1].Header.Get("Authorization"))
	}

	rc2 := &recorder{fixture: geminiFixtureFor(t, "google-text")}
	p2, _ := New(Config{Project: "p", Location: "us-central1", HTTPClient: &http.Client{Transport: rc2},
		AuthToken: func(ctx context.Context) (string, error) { return "", errors.New("adc unavailable") }})
	m2, _ := p2.LanguageModel("gemini-2.5-flash")
	_, err = m2.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "adc unavailable") || len(rc2.reqs) != 0 {
		t.Fatalf("err = %v, requests sent = %d", err, len(rc2.reqs))
	}
}

// Provider metadata is written under the vertex key (and the legacy
// googleVertex key), never under google.
func TestVertexProviderMetadataKeys(t *testing.T) {
	rc := &recorder{fixture: geminiFixtureFor(t, "google-text")}
	p, _ := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", HTTPClient: &http.Client{Transport: rc}})
	m, _ := p.LanguageModel("gemini-2.5-flash")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.ProviderMetadata["vertex"]; !ok {
		t.Fatalf("provider metadata keys = %v", res.ProviderMetadata)
	}
	if _, ok := res.ProviderMetadata["google"]; ok {
		t.Fatalf("vertex must not write the google key: %v", res.ProviderMetadata)
	}
}
