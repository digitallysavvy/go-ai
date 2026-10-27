package replicate

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A non-2xx poll response is an API error, not an in-progress prediction
// (it spun 60 x 2 s before "timed out").
func TestLanguagePollHTTPErrorIsReported(t *testing.T) {
	for _, status := range []int{401, 404, 500} {
		polls := 0
		p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost {
				return replicateJSONResponse(200, `{"id":"p1","status":"starting"}`), nil
			}
			polls++
			return replicateJSONResponse(status, `{"detail":"bad"}`), nil
		})
		// Bounded so a regression fails fast instead of polling for 2 minutes.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := NewLanguageModel(p, "v").DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		cancel()
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.StatusCode != status {
			t.Fatalf("status %d: err = %T %v, want ProviderError", status, err, err)
		}
		if polls != 1 {
			t.Errorf("status %d: polls = %d, want 1", status, polls)
		}
	}
}
