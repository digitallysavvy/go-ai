package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sigv4check"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Every Bedrock runtime endpoint must carry a signature AWS would accept,
// including for model IDs with ':' (and ARNs), which are escaped differently
// on the wire per endpoint.
func TestSigV4AcceptedForAllEndpoints(t *testing.T) {
	var mu sync.Mutex
	var failures []string
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.URL.EscapedPath())
		if reason := sigv4check.Verify(r, body, "secret-key", "us-east-1", "bedrock"); reason != "" {
			failures = append(failures, r.Method+" "+r.RequestURI+": "+reason)
		}
		if r.Header.Get("X-Amz-Security-Token") != "session-token" {
			failures = append(failures, "missing session token header")
		}
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/converse"):
			_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
		case strings.Contains(r.URL.Path, "titan-embed"):
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2],"inputTextTokenCount":3}`))
		case strings.Contains(r.URL.Path, "nova-canvas"):
			_, _ = w.Write([]byte(`{"images":["aGk="]}`))
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	p := New(Config{AWSAccessKeyID: "AKIDEXAMPLE", AWSSecretAccessKey: "secret-key", SessionToken: "session-token", Region: "us-east-1", BaseURL: srv.URL})
	ctx := context.Background()

	for _, id := range []string{
		"anthropic.claude-3-5-sonnet-20241022-v2:0",
		"arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	} {
		lm, err := p.LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatalf("converse %s: %v", id, err)
		}
	}
	em, _ := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if _, err := em.DoEmbed(ctx, "hello", nil); err != nil {
		t.Fatalf("embed: %v", err)
	}
	im, _ := p.ImageModel("amazon.nova-canvas-v1:0")
	if _, err := im.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"}); err != nil {
		t.Fatalf("image: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(failures) > 0 {
		t.Fatalf("signature rejected:\n%s\npaths seen: %v", strings.Join(failures, "\n"), seen)
	}
}
