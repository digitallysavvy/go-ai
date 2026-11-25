package tsfixture

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r75integChildEnv makes the test run the looping case in a child process.
const r75integChildEnv = "GOAI_R75INTEG_TSFIXTURE_CHILD"

// Run fails a case whose model keeps requesting (a case extracted with one
// of several responses, replayed forever by a continuing model) after
// runMaxRequests requests instead of hanging (review round 75, provb fix 8).
// The failing Run executes in a child test process, whose failure the parent
// asserts.
func TestR75Integ_RunFailsAfterMaxRequests(t *testing.T) {
	if os.Getenv(r75integChildEnv) == "1" {
		cases := []Case{{
			Test:     "loops",
			CallKind: "generate",
			ModelID:  "m",
			Call:     map[string]interface{}{},
			Response: &Response{Kind: "json", Body: []byte(`{}`)},
		}}
		Run(t, cases, func(baseURL, _ string) provider.LanguageModel {
			return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				for i := 0; i < runMaxRequests+5; i++ {
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader("{}"))
					if err != nil {
						return nil, err
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return nil, err
					}
					_ = resp.Body.Close()
				}
				return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
			}}
		}, nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestR75Integ_RunFailsAfterMaxRequests$", "-test.count=1")
	cmd.Env = append(os.Environ(), r75integChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child run passed, want the more-than-%d-requests failure:\n%s", runMaxRequests, out)
	}
	if !strings.Contains(string(out), "more than 20 requests") {
		t.Fatalf("child output lacks the request cap failure:\n%s", out)
	}
}
