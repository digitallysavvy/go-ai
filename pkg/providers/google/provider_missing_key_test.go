package google

import (
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

type missingKeyRT func(*http.Request) (*http.Response, error)

func (f missingKeyRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TS getHeaders → loadApiKey runs per request: a missing key fails the
// request (nothing is sent) with a LoadAPIKeyError, and a key set after
// construction is used.
func TestMissingAPIKeyErrorsAtRequestTime(t *testing.T) {
	t.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", "")
	var sent int
	var gotKey string
	hc := &http.Client{Transport: missingKeyRT(func(r *http.Request) (*http.Response, error) {
		sent++
		gotKey = r.Header.Get("x-goog-api-key")
		return nil, errors.New("stop")
	})}
	p := New(Config{HTTPClient: hc})
	m, err := p.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	gen := func() error {
		_, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return err
	}
	var le *providererrors.LoadAPIKeyError
	if err := gen(); !errors.As(err, &le) || sent != 0 {
		t.Fatalf("err = %v sent = %d", err, sent)
	}
	if _, direct := gen().(*providererrors.LoadAPIKeyError); !direct {
		t.Fatalf("error is wrapped: %T", gen())
	}
	if le.Message != "Google Generative AI API key is missing. Pass it using the 'apiKey' parameter or the GOOGLE_GENERATIVE_AI_API_KEY environment variable." {
		t.Fatalf("message = %q", le.Message)
	}
	t.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", "late")
	_ = gen()
	if gotKey != "late" {
		t.Fatalf("key = %q", gotKey)
	}
}
