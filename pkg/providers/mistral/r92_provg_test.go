package mistral

import (
	"context"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS loadApiKey throws from the headers before the fetch: a missing key is
// the LoadAPIKeyError itself, not a status-0 ProviderError
// (cr-r92-provb-to-provg-tools).
func TestR92provgRealtimeMissingKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	r := New(Config{BaseURL: "http://127.0.0.1:1"}).ExperimentalRealtime()
	_, err := r.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "voxtral-mini-realtime"})
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("CreateSession err = %T %v", err, err)
	}
	if err := r.CloseSession(context.Background(), "s1"); err == nil {
		t.Fatal("CloseSession succeeded")
	} else if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("CloseSession err = %T %v", err, err)
	}
}
