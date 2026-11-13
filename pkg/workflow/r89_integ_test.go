package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// r89integRedirectPair returns an origin server that redirects every request
// (307, method and body kept) to another port of the same host, and a
// function reporting the Authorization the second origin saw. Fetch drops
// Authorization on any cross-origin hop (scheme, host or port); net/http
// alone keeps it when only the port changes.
func r89integRedirectPair(t *testing.T, body string) (string, func() (string, bool)) {
	t.Helper()
	var mu sync.Mutex
	var got string
	var hit bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got, hit = r.Header.Get("Authorization"), true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	return origin.URL, func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return got, hit
	}
}

func TestR89integChatTransportRedirectDropsAuthorization(t *testing.T) {
	url, seen := r89integRedirectPair(t, `{}`)
	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{
		API: url + "/api/chat",
		PrepareSendMessagesRequest: func(ai.ChatTransportSendMessagesRequest) (PreparedChatRequest, error) {
			return PreparedChatRequest{API: url + "/api/chat", Headers: map[string]string{"Authorization": "Bearer user"}}, nil
		},
		MaxConsecutiveErrors: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks, errs := tr.SendMessages(ctx, ai.ChatTransportSendMessagesRequest{ChatID: "c1"})
	for range chunks {
	}
	for range errs {
	}
	got, hit := seen()
	if !hit {
		t.Fatal("redirect target not reached")
	}
	if got != "" {
		t.Fatalf("other origin got Authorization %q", got)
	}
}
