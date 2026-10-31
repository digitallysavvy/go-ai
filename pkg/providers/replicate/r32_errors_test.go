package replicate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS lets a transport failure propagate unchanged.
func TestTransportFailuresAreNotWrapped(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := (&ImageModel{}).handleError(sentinel); got != sentinel {
		t.Errorf("image handleError wrapped: %v", got)
	}
	if got := (&VideoModel{}).handleError(sentinel); got != sentinel {
		t.Errorf("video handleError wrapped: %v", got)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	lm := NewLanguageModel(p, "meta/llama")
	server.Close()
	_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("language err = %#v", err)
	}
}

// TS getFromApi lets a poll transport failure propagate unchanged.
func TestPollTransportFailureIsNotWrapped(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := (&ImageModel{}).handlePollError(sentinel); got != sentinel {
		t.Errorf("image handlePollError wrapped: %v", got)
	}
	if got := (&VideoModel{}).handlePollError(sentinel); got != sentinel {
		t.Errorf("video handlePollError wrapped: %v", got)
	}
}

// z.string().optional() rejects null, so the body is off-schema and the
// message is the status text.
func TestReplicateErrorMessageNullFieldIsOffSchema(t *testing.T) {
	for body, want := range map[string]string{
		`{"detail":null,"error":"x"}`: "Bad Request",
		`{"detail":"d","error":null}`: "Bad Request",
		`{"detail":5}`:                "Bad Request",
		`{"error":"x"}`:               "x",
		`{}`:                          "Unknown Replicate error",
	} {
		if got := replicateErrorMessage(400, []byte(body)); got != want {
			t.Errorf("%s: got %q, want %q", body, got, want)
		}
	}
}
