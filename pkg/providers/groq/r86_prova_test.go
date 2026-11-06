package groq

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS groqFailedResponseHandler parses response.text() (BOM dropped).
func TestR86provaGroqErrorBodyBOM(t *testing.T) {
	err := parseGroqProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte("\xEF\xBB\xBF{\"error\":{\"message\":\"boom\",\"type\":\"invalid_request_error\"}}")})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "boom" || pe.ErrorCode != "invalid_request_error" {
		t.Fatalf("%#v", err)
	}
}

// cr-r86-provb-to-prova item 1: a []string timestampGranularities is read
// like its JSON form.
func TestR86provaGroqTypedTimestampGranularities(t *testing.T) {
	po := map[string]interface{}{"groq": map[string]interface{}{"timestampGranularities": []string{"word"}}}
	if err := validateGroqTranscriptionOptions(po); err != nil {
		t.Fatal(err)
	}
	if got := resolveGroqTranscriptionOptions(po).TimestampGranularities; len(got) != 1 || got[0] != "word" {
		t.Fatalf("got %v", got)
	}
}
