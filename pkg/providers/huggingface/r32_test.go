package huggingface

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// z.string().optional() rejects an explicit null for type and code, so the
// body is off-schema and the message is the status text; the failed response
// keeps its body, data and headers.
func TestParseHuggingFaceErrorNullTypeAndCodeAreOffSchema(t *testing.T) {
	for _, body := range []string{`{"error":{"message":"bad","type":null}}`, `{"error":{"message":"bad","code":null}}`} {
		err := parseHuggingFaceError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != "Bad Request" {
			t.Errorf("%s: %#v", body, perr)
		}
	}
	err := parseHuggingFaceError(&internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r"}}, Body: []byte(`{"error":{"message":"bad"}}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "bad" || perr.Data == nil || perr.ResponseHeaders["x-request-id"] != "r" {
		t.Fatalf("got %#v", perr)
	}
}

// TS postJsonToApi lets a transport failure propagate unchanged.
func TestTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("meta-llama/Llama-3")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("generate err = %#v", err)
	}
	_, err = m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("stream err = %#v", err)
	}
}
