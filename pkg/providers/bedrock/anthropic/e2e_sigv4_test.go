package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sigv4check"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The InvokeModel path embeds the model ID (':' and ARN '/'); the signature
// must be one the AWS front door accepts for those paths.
func TestSigV4AcceptedForInvokeModelPaths(t *testing.T) {
	var mu sync.Mutex
	var failures []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		if reason := sigv4check.Verify(r, body, "secret-key", "us-west-2", "bedrock"); reason != "" {
			failures = append(failures, r.RequestURI+": "+reason)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{Region: "us-west-2", BaseURL: srv.URL, Credentials: &AWSCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret-key", SessionToken: "tok"}})
	for _, id := range []string{
		"anthropic.claude-sonnet-4-5-20250929-v1:0",
		"arn:aws:bedrock:us-west-2:123456789012:inference-profile/us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	} {
		model, err := p.LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(failures) > 0 {
		t.Fatalf("signature rejected: %v", failures)
	}
}
