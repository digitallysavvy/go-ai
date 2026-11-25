package google

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler(responseSchema): a non-conforming 200 body is
// an APICallError "Invalid JSON response" caused by a TypeValidationError.
func TestDoGenerate_ResponseSchemaMismatchIsInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":5}]}}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, err := p.LanguageModel(ModelGemini20Flash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" || perr.StatusCode != 200 {
		t.Fatalf("err = %#v", err)
	}
	if !providererrors.IsValidationError(perr.Cause) || !strings.Contains(perr.ResponseBody, `"text":5`) {
		t.Fatalf("cause = %#v body=%q", perr.Cause, perr.ResponseBody)
	}
}
