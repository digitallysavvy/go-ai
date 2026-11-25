package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func anthropicFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "anthropic", "testdata", "ts", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Vertex serves the Anthropic Messages API behind :rawPredict /
// :streamRawPredict, with the model in the URL and anthropic_version in the
// body; the shared stream/response handling must still produce TS parts.
func TestVertexAnthropicFixtureRoundTrip(t *testing.T) {
	var paths []string
	var bodies []map[string]interface{}
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		auth = append(auth, r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key")+"|"+r.Header.Get("anthropic-version"))
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		if strings.HasSuffix(r.URL.Path, ":streamRawPredict") {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, l := range strings.Split(string(anthropicFixture(t, "anthropic-tool-no-args.chunks.txt")), "\n") {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(anthropicFixture(t, "anthropic-text.json"))
	}))
	defer srv.Close()

	p := New(Options{Project: "proj", Location: "us-east5", BaseURL: srv.URL + "/v1/projects/proj/locations/us-east5/publishers/anthropic/models",
		AuthToken: func(context.Context) (string, error) { return "gcp-token", nil }, HTTPClient: srv.Client()})
	m, err := p.LanguageModel("claude-sonnet-4-5@20250929")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == "" || res.FinishReason != types.FinishReasonStop || *res.Usage.InputTokens != 12 {
		t.Fatalf("generate: %+v", res)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	var kinds []string
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, string(c.Type))
	}
	if !strings.Contains(strings.Join(kinds, ","), "tool-input-start,tool-input-end,tool-call,finish") {
		t.Fatalf("stream kinds = %v", kinds)
	}

	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/models/claude-sonnet-4-5@20250929:rawPredict") || !strings.HasSuffix(paths[1], "/models/claude-sonnet-4-5@20250929:streamRawPredict") {
		t.Fatalf("paths = %v", paths)
	}
	for i, b := range bodies {
		if b["anthropic_version"] != "vertex-2023-10-16" || b["model"] != nil {
			t.Errorf("body %d: anthropic_version=%v model=%v", i, b["anthropic_version"], b["model"])
		}
		if auth[i] != "Bearer gcp-token||" {
			t.Errorf("auth %d = %q (bearer only; no x-api-key, no anthropic-version header)", i, auth[i])
		}
	}
	if bodies[1]["stream"] != true {
		t.Errorf("stream flag = %v", bodies[1]["stream"])
	}
}
