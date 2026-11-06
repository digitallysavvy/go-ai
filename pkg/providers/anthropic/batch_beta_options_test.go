package anthropic

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// A []string anthropicBeta (the natural Go type) reaches the wire like the
// []interface{} a JSON decode produces.
func TestBatch_StringSliceAnthropicBetaReachesHeader(t *testing.T) {
	var gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"id":"b","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"c","expires_at":"e"}`))
	}))
	defer srv.Close()
	b := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests:        []provider.BatchV4Request{textBatchRequest("r1", ClaudeSonnet4_5, "hi")},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"anthropicBeta": []string{"my-beta"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBeta != "my-beta" {
		t.Fatalf("anthropic-beta = %q", gotBeta)
	}
}

// TS parseProviderOptions(anthropicLanguageModelOptions.pick({anthropicBeta}))
// rejects a non-object entry and an anthropicBeta that is not a string array.
func TestBatch_InvalidAnthropicBetaProviderOptionsFail(t *testing.T) {
	b := New(Config{APIKey: "k"}).ExperimentalBatch()
	cases := map[string]map[string]interface{}{
		"string":   {"anthropic": map[string]interface{}{"anthropicBeta": "x"}},
		"null":     {"anthropic": map[string]interface{}{"anthropicBeta": nil}},
		"elements": {"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{1}}},
		"entry":    {"anthropic": "oops"},
	}
	for name, po := range cases {
		_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
			Requests:        []provider.BatchV4Request{textBatchRequest("r1", ClaudeSonnet4_5, "hi")},
			ProviderOptions: po,
		})
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Message != "invalid anthropic provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
