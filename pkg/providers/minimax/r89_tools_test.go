package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provb CR class (cr-r89-provb-to-tools 2, swept): TS
// minimaxVideoFailedResponseHandler (createJsonErrorResponseHandler) is an
// APICallError with responseHeaders, so the retry loop sees retry-after-ms.
// The MiniMax error now unwraps to a ProviderError carrying them.
func TestR89toolsMiniMaxVideoAPIErrorKeepsHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
	}))
	defer srv.Close()
	model := newVideoModel(New(Config{APIKey: "k", VideoBaseURL: srv.URL}), ModelH3)
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *minimaxDefaultCallOptions(map[string]interface{}{})})
	var me *Error
	if !errors.As(err, &me) || me.Message != "busy" || me.StatusCode != 429 {
		t.Fatalf("err = %#v", err)
	}
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 429 || !perr.IsRetryable() {
		t.Fatalf("provider error = %#v", perr)
	}
	found := false
	for k, v := range perr.ResponseHeaders {
		if strings.EqualFold(k, "retry-after-ms") && v == "10" {
			found = true
		}
	}
	if !found {
		t.Fatalf("headers = %v", perr.ResponseHeaders)
	}
}

// TS getFromApi with minimaxVideoFailedResponseHandler: a failed status
// poll is an APICallError with responseHeaders too.
func TestR89toolsMiniMaxVideoStatusErrorKeepsHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "10")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
	}))
	defer srv.Close()
	model := newVideoModel(providerForMinimaxServer(srv.URL), ModelH3)
	op, _ := json.Marshal(minimaxOperation{TaskID: minimaxTestTaskID})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 503 || perr.Message != "busy" {
		t.Fatalf("err = %#v", err)
	}
	found := false
	for k, v := range perr.ResponseHeaders {
		if strings.EqualFold(k, "retry-after-ms") && v == "10" {
			found = true
		}
	}
	if !found {
		t.Fatalf("headers = %v", perr.ResponseHeaders)
	}
}
