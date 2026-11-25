package replicate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type r88provcRecorded struct {
	method, path string
	headers      http.Header
	bodyLen      int
}

type r88provcReplicate struct {
	mu       sync.Mutex
	requests []r88provcRecorded
	statuses int
}

func (r *r88provcReplicate) cancels() []r88provcRecorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []r88provcRecorded
	for _, q := range r.requests {
		if strings.HasSuffix(q.path, "/cancel") {
			out = append(out, q)
		}
	}
	return out
}

func r88provcWaitCancels(t *testing.T, r *r88provcReplicate, n int) []r88provcRecorded {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c := r.cancels(); len(c) >= n || time.Now().After(deadline) {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// r88provcServer serves a prediction that stays "processing" (or the body
// done returns); onStatus runs on every status GET.
func r88provcServer(t *testing.T, id string, done func(n int) string, onStatus func(n int)) (*httptest.Server, *r88provcReplicate) {
	t.Helper()
	rec := &r88provcReplicate{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		rec.mu.Lock()
		rec.requests = append(rec.requests, r88provcRecorded{method: r.Method, path: r.URL.EscapedPath(), headers: r.Header.Clone(), bodyLen: n})
		if r.Method == http.MethodGet {
			rec.statuses++
		}
		statuses := rec.statuses
		rec.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			w.WriteHeader(http.StatusInternalServerError) // cancel errors are swallowed
			return
		}
		body := `{"id":"` + id + `","status":"processing","output":null,"error":null,"urls":{"get":"` + srv.URL + `/predictions/` + id + `"}}`
		if r.Method == http.MethodGet {
			if onStatus != nil {
				onStatus(statuses)
			}
			if done != nil {
				if d := done(statuses); d != "" {
					body = d
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func r88provcGenerate(ctx context.Context, baseURL string, attempts int) error {
	m := NewImageModel(New(Config{APIKey: "test-api-token", BaseURL: baseURL}), "black-forest-labs/flux-schnell")
	_, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:  "a cat",
		Headers: map[string]string{"idempotency-key": "create-key", "x-per-call-header": "per-call-value"},
		ProviderOptions: map[string]interface{}{"replicate": map[string]interface{}{
			"pollIntervalMillis": 1, "maxPollAttempts": attempts,
		}},
	})
	return err
}

func r88provcCheckCancel(t *testing.T, c r88provcRecorded, id string) {
	t.Helper()
	if c.method != http.MethodPost || c.path != "/predictions/"+id+"/cancel" || c.bodyLen != 0 {
		t.Fatalf("cancel = %+v", c)
	}
	if c.headers.Get("Authorization") != "Bearer test-api-token" || c.headers.Get("Idempotency-Key") != "" || c.headers.Get("X-Per-Call-Header") != "" || c.headers.Get("Prefer") != "" {
		t.Fatalf("cancel headers = %v", c.headers)
	}
}

// TS replicate-image-model.test.ts / cancel-prediction.test.ts (0d47a6cf8e).
func TestR88provcReplicateCancelPrediction(t *testing.T) {
	t.Run("abort while polling cancels once and returns the abort error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		srv, rec := r88provcServer(t, "aborted-prediction", nil, func(n int) {
			if n == 2 {
				cancel()
			}
		})
		err := r88provcGenerate(ctx, srv.URL, 10)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		c := r88provcWaitCancels(t, rec, 1)
		if len(c) != 1 {
			t.Fatalf("cancels = %d", len(c))
		}
		r88provcCheckCancel(t, c[0], "aborted-prediction")
	})

	t.Run("exhausted attempts cancel once and keep the timeout error", func(t *testing.T) {
		srv, rec := r88provcServer(t, "timed-out-prediction", nil, nil)
		err := r88provcGenerate(context.Background(), srv.URL, 2)
		if err == nil || err.Error() != "Replicate image generation did not complete after 2 polling attempts." {
			t.Fatalf("err = %v", err)
		}
		c := r88provcWaitCancels(t, rec, 1)
		if len(c) != 1 {
			t.Fatalf("cancels = %d", len(c))
		}
		r88provcCheckCancel(t, c[0], "timed-out-prediction")
	})

	t.Run("no cancel when the prediction completes", func(t *testing.T) {
		srv, rec := r88provcServer(t, "completed-prediction", func(int) string {
			return `{"id":"completed-prediction","status":"succeeded","output":["data:image/webp;base64,AAAA"],"error":null,"urls":{"get":"https://api.replicate.com/v1/predictions/x"}}`
		}, nil)
		if err := r88provcGenerate(context.Background(), srv.URL, 10); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		if c := rec.cancels(); len(c) != 0 {
			t.Fatalf("cancels = %v", c)
		}
	})

	t.Run("the prediction id is path-encoded", func(t *testing.T) {
		srv, rec := r88provcServer(t, "a?b", nil, nil)
		_ = r88provcGenerate(context.Background(), srv.URL, 1)
		c := r88provcWaitCancels(t, rec, 1)
		if len(c) != 1 || c[0].path != "/predictions/a%3Fb/cancel" {
			t.Fatalf("cancels = %+v", c)
		}
	})

	t.Run("a failed poll without an abort does not cancel", func(t *testing.T) {
		var srv *httptest.Server
		rec := &r88provcReplicate{}
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.requests = append(rec.requests, r88provcRecorded{method: r.Method, path: r.URL.Path})
			rec.mu.Unlock()
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"detail":"bad"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"p","status":"processing","urls":{"get":"` + srv.URL + `/predictions/p"}}`))
		}))
		defer srv.Close()
		if err := r88provcGenerate(context.Background(), srv.URL, 3); err == nil {
			t.Fatal("expected the poll error")
		}
		time.Sleep(20 * time.Millisecond)
		if c := rec.cancels(); len(c) != 0 {
			t.Fatalf("cancels = %v", c)
		}
	})

	t.Run("a prediction without id is an invalid response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"processing","urls":{"get":"https://api.replicate.com/v1/predictions/x"}}`))
		}))
		defer srv.Close()
		err := r88provcGenerate(context.Background(), srv.URL, 2)
		if err == nil || !strings.Contains(err.Error(), "Invalid JSON response") {
			t.Fatalf("err = %v", err)
		}
	})
}
