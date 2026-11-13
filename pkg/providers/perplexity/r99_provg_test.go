package perplexity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS sends combineHeaders(config.headers, options.headers) and
// postJsonToApi / normalizeHeaders lower-case them in entry order, so a call
// header replaces the Go streaming Accept default whatever its case. Go
// copied the call headers beside "Accept", and map order picked one.
func TestR99provgStreamCallAcceptHeaderWinsInAnyCase(t *testing.T) {
	var accept []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Values("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	model := NewLanguageModel(New(Config{BaseURL: srv.URL, APIKey: "test-token"}), "sonar")
	// The request headers were set in map order: repeat to catch either.
	for i := 0; i < 16; i++ {
		stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{
			Prompt:  types.Prompt{Text: "hi"},
			Headers: map[string]string{"ACCEPT": "application/x-ndjson"},
		})
		if err != nil {
			t.Fatalf("DoStream() error = %v", err)
		}
		_ = stream.Close()
		if len(accept) != 1 || accept[0] != "application/x-ndjson" {
			t.Fatalf("run %d: accept = %q, want the call header", i, accept)
		}
	}
}
