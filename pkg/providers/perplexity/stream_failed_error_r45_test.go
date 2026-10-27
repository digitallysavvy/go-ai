package perplexity

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS enqueues the failed chunk's error object, which the core normalizes into a
// stream provider error carrying the object's type and code.
func TestPerplexityAgentStreamFailedChunkErrorIsStreamProviderError(t *testing.T) {
	body := `data: {"type":"response.failed","error":{"message":"Agent run failed","type":"server_error","code":"500"}}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no error chunk")
		}
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("err = %#v", c.Err)
		}
		if spe.Message != "Agent run failed" || spe.Type != "server_error" || spe.Code != "500" {
			t.Fatalf("stream error = %+v", spe)
		}
		return
	}
}
