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

// TS minimax-video-model.ts template literals use JS number formatting:
// `${task.error.code}` and `${options.duration}` (Go %v printed 1e+06).
func TestR89toolsMiniMaxVideoNumberText(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
		`{"task":{"id":"task-123","status":"failed","error":{"message":"boom","code":1000000}}}`,
	})
	defer server.Close()
	model := newVideoModel(providerForMinimaxServer(server.URL), ModelH3)
	_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
	if err == nil || !strings.Contains(err.Error(), "boom (1000000). Task ID:") {
		t.Fatalf("err = %v", err)
	}

	server2, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server2.Close()
	model2 := newVideoModel(providerForMinimaxServer(server2.URL), ModelH3)
	opts := minimaxDefaultCallOptions(nil)
	d := 1e6 + 0.5
	opts.Duration = &d
	result, err := model2.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"MiniMax-H3 requires a whole number of seconds. The requested duration of 1000000.5 was rounded to 1000001.",
		"MiniMax-H3 supports at most 15 seconds. The requested duration of 1000000.5 was clamped to 15.",
	}
	if len(result.Warnings) < 2 || result.Warnings[0].Details != want[0] || result.Warnings[1].Details != want[1] {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}
