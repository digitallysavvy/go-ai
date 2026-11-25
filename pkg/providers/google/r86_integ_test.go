package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r86integCtxKey struct{}

// The cancel TS sends after a caller abort keeps the call's context values
// (an APIKeyFunc reads them) without its cancellation; it used
// context.Background().
func TestR86integInteractionsAbortCancelKeyFuncSeesCallerValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), r86integCtxKey{}, "caller"))
	cancelled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions":
			_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"in_progress"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/interactions/v1_agent":
			cancel()
			_, _ = fmt.Fprint(w, `{"id":"v1_agent","status":"in_progress"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/interactions/v1_agent/cancel":
			cancelled <- struct{}{}
			_, _ = fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var mu sync.Mutex
	var seen []interface{}
	keyFunc := func(c context.Context) (string, error) {
		mu.Lock()
		seen = append(seen, c.Value(r86integCtxKey{}))
		mu.Unlock()
		return "k", nil
	}
	model, err := New(Config{APIKeyFunc: keyFunc, BaseURL: server.URL}).InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "research"}}); err == nil {
		t.Fatal("expected an abort error")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the interaction was not cancelled")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, v := range seen {
		if v != "caller" {
			t.Fatalf("key func ctx values = %v, want the caller's on every request", seen)
		}
	}
}
