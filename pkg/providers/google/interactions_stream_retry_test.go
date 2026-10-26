package google

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type interactionsRetryServer struct {
	mu      sync.Mutex
	gets    []string // raw queries of GET /interactions/i1 stream requests
	handler func(n int, w http.ResponseWriter, r *http.Request)
}

func (s *interactionsRetryServer) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"i1","status":"in_progress"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/interactions/i1":
			s.mu.Lock()
			s.gets = append(s.gets, r.URL.RawQuery)
			n := len(s.gets)
			s.mu.Unlock()
			s.handler(n, w, r)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func (s *interactionsRetryServer) getCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.gets)
}

func backgroundStream(t *testing.T, base string) provider.TextStream {
	t.Helper()
	// Agents run in the background: POST, then stream the interaction by id.
	model, err := New(Config{APIKey: "k", BaseURL: base}).InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatal(err)
	}
	st, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "x"},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	return st
}

func drainRetryStream(st provider.TextStream) (chunks []*provider.StreamChunk, err error) {
	for {
		c, err := st.Next()
		if errors.Is(err, io.EOF) {
			return chunks, nil
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, c)
	}
}

func shortRetryDelay(t *testing.T) {
	t.Helper()
	old := interactionsStreamRetryDelay
	interactionsStreamRetryDelay = time.Millisecond
	t.Cleanup(func() { interactionsStreamRetryDelay = old })
}

// TS streamGoogleInteractionEvents: connections that close without any event
// count as failed attempts and the stream errors after maxRetries (3), instead
// of reconnecting forever.
func TestInteractionsBackgroundStreamErrorsAfterEmptyConnections(t *testing.T) {
	shortRetryDelay(t)
	srv := &interactionsRetryServer{handler: func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}}
	ts := srv.serve(t)
	defer ts.Close()

	st := backgroundStream(t, ts.URL)
	done := make(chan error, 1)
	go func() { _, err := drainRetryStream(st); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "closed without producing any events") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream kept reconnecting forever")
	}
	if n := srv.getCount(); n != 3 {
		t.Fatalf("GET requests = %d, want 3", n)
	}
}

// A connection drop after events resumes with last_event_id and resets the
// failure counter.
func TestInteractionsBackgroundStreamResumesWithLastEventID(t *testing.T) {
	shortRetryDelay(t)
	srv := &interactionsRetryServer{handler: func(n int, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = fmt.Fprint(w, "id: e1\ndata: {\"event_type\":\"interaction.created\",\"event_id\":\"e1\",\"interaction\":{\"id\":\"i1\"}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "id: e2\ndata: {\"event_type\":\"interaction.completed\",\"event_id\":\"e2\",\"interaction\":{\"id\":\"i1\",\"status\":\"completed\"}}\n\n")
	}}
	ts := srv.serve(t)
	defer ts.Close()

	chunks, err := drainRetryStream(backgroundStream(t, ts.URL))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if last := chunks[len(chunks)-1]; last.Type != provider.ChunkTypeFinish {
		t.Fatalf("last chunk = %v", last.Type)
	}
	if srv.getCount() != 2 || !strings.Contains(srv.gets[1], "last_event_id=e1") {
		t.Fatalf("gets = %v", srv.gets)
	}
}
