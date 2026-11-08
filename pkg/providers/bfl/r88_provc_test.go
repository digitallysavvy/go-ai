package bfl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r88provcWebhookSubmit = `{"id":"req-webhook","status":"processing","webhook_url":"https://example.com/webhook"}`
const r88provcReady = `{"id":"req-webhook","status":"Ready","result":{"sample":"data:image/png;base64,iVBORw==","seed":42}}`

// r88provcServer answers the submit with the webhook-shaped body and every
// get_result poll with poll(n), n counting from 1.
func r88provcServer(t *testing.T, poll func(n int) (int, string)) (*httptest.Server, *int32, *sync.Map) {
	t.Helper()
	var polls int32
	seen := &sync.Map{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/flux-2-pro":
			_, _ = w.Write([]byte(r88provcWebhookSubmit))
		case r.Method == http.MethodGet && r.URL.Path == "/get_result":
			n := int(atomic.AddInt32(&polls, 1))
			seen.Store(n, r.URL.RequestURI()+"|"+r.Header.Get("X-Key"))
			status, body := poll(n)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &polls, seen
}

func r88provcGenerate(ctx context.Context, baseURL string, interval, timeout int) (*types.ImageResult, error) {
	p := New(Config{APIKey: "test-key", BaseURL: baseURL, PollIntervalMillis: interval, PollTimeoutMillis: timeout})
	return NewImageModel(p, "flux-2-pro").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat", AspectRatio: "1:1"})
}

// TS black-forest-labs-image-model.test.ts (eded4b8ed0).
func TestR88provcBFLWebhookSubmitPollsGetResult(t *testing.T) {
	t.Run("polls get_result by id when the submit response has no polling_url", func(t *testing.T) {
		srv, polls, seen := r88provcServer(t, func(int) (int, string) { return 200, r88provcReady })
		res, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Image) == 0 || *polls != 1 {
			t.Fatalf("image %d bytes, polls %d", len(res.Image), *polls)
		}
		first, _ := seen.Load(1)
		if first != "/get_result?id=req-webhook|test-key" {
			t.Fatalf("poll = %v", first)
		}
		images := res.ProviderMetadata["blackForestLabs"].(map[string]interface{})["images"].([]map[string]interface{})
		if images[0]["seed"] != 42.0 {
			t.Fatalf("metadata = %#v", images[0])
		}
	})

	t.Run("the request id is query-encoded", func(t *testing.T) {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"a&b c#d"}`))
				return
			}
			got = r.URL.RawQuery
			_, _ = w.Write([]byte(r88provcReady))
		}))
		defer srv.Close()
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err != nil {
			t.Fatal(err)
		}
		if got != "id=a%26b+c%23d" {
			t.Fatalf("query = %q", got)
		}
	})

	t.Run("retries get_result while a webhook job is not yet registered", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(n int) (int, string) {
			if n <= 2 {
				return 404, `{"id":"req-webhook","status":"Task not found"}`
			}
			return 200, r88provcReady
		})
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err != nil {
			t.Fatal(err)
		}
		if *polls != 3 {
			t.Fatalf("polls = %d", *polls)
		}
	})

	t.Run("404 with state and null status is not found too", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(n int) (int, string) {
			if n == 1 {
				return 404, `{"status":null,"state":"Task not found","detail":{"x":1}}`
			}
			return 200, r88provcReady
		})
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err != nil || *polls != 2 {
			t.Fatalf("err = %v, polls = %d", err, *polls)
		}
	})

	t.Run("retries get_result when the poll response status is Task not found", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(n int) (int, string) {
			if n == 1 {
				return 200, `{"id":"req-webhook","status":"Task not found"}`
			}
			return 200, r88provcReady
		})
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err != nil || *polls != 2 {
			t.Fatalf("err = %v, polls = %d", err, *polls)
		}
	})

	notFound := func(int) (int, string) { return 404, `{"id":"req-webhook","status":"Task not found"}` }
	const wantNotFound = "Black Forest Labs task req-webhook was not found via get_result before timing out"

	t.Run("fails with a clear error when get_result keeps answering 404 Task not found", func(t *testing.T) {
		srv, _, _ := r88provcServer(t, notFound)
		_, err := r88provcGenerate(context.Background(), srv.URL, 10, 100)
		if err == nil || err.Error() != wantNotFound {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("gives up on Task not found without waiting for the overall poll timeout", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, notFound)
		start := time.Now()
		_, err := r88provcGenerate(context.Background(), srv.URL, 5, 5000)
		if err == nil || err.Error() != wantNotFound || time.Since(start) > 2*time.Second {
			t.Fatalf("err = %v after %v", err, time.Since(start))
		}
		if *polls != 20 {
			t.Fatalf("polls = %d, want 20", *polls)
		}
	})

	t.Run("does not retry get_result 404 responses that are not Task not found", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(int) (int, string) { return 404, `{"message":"Unknown request id"}` })
		_, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000)
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Unknown request id" || pe.StatusCode != 404 {
			t.Fatalf("err = %#v", err)
		}
		if *polls != 1 {
			t.Fatalf("polls = %d", *polls)
		}
	})

	t.Run("a 404 whose message is not a string is not retried", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(int) (int, string) { return 404, `{"message":null,"status":"Task not found"}` })
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err == nil || *polls != 1 {
			t.Fatalf("err = %v, polls = %d", err, *polls)
		}
	})

	t.Run("a 404 with another task status is not retried", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, func(int) (int, string) { return 404, `{"status":"Error"}` })
		if _, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000); err == nil || *polls != 1 {
			t.Fatalf("err = %v, polls = %d", err, *polls)
		}
	})

	t.Run("a later pending status times out with the generic text", func(t *testing.T) {
		srv, _, _ := r88provcServer(t, func(n int) (int, string) {
			if n == 1 {
				return 200, `{"status":"Task not found"}`
			}
			return 200, `{"status":"Pending"}`
		})
		_, err := r88provcGenerate(context.Background(), srv.URL, 10, 150)
		if err == nil || err.Error() != "Black Forest Labs generation timed out." {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("rejects with the context error when aborted during the not-found retry delay", func(t *testing.T) {
		srv, polls, _ := r88provcServer(t, notFound)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for atomic.LoadInt32(polls) < 1 {
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
		}()
		_, err := r88provcGenerate(ctx, srv.URL, 500, 5000)
		if !errors.Is(err, context.Canceled) || *polls != 1 {
			t.Fatalf("err = %v, polls = %d", err, *polls)
		}
	})

	t.Run("a polling_url submit keeps Task not found terminal", func(t *testing.T) {
		var polls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"http://` + r.Host + `/poll"}`))
				return
			}
			atomic.AddInt32(&polls, 1)
			_, _ = w.Write([]byte(`{"status":"Task not found"}`))
		}))
		defer srv.Close()
		_, err := r88provcGenerate(context.Background(), srv.URL, 10, 1000)
		if err == nil || err.Error() != "Black Forest Labs generation failed: Task not found." || polls != 1 {
			t.Fatalf("err = %v, polls = %d", err, polls)
		}
	})

	t.Run("an empty or invalid polling_url still fails the submit", func(t *testing.T) {
		for _, body := range []string{`{"id":"x","polling_url":""}`, `{"id":"x","polling_url":1}`} {
			if err := validateBFLImageSubmitBody([]byte(body)); err == nil {
				t.Fatalf("%s accepted", body)
			}
		}
		if err := validateBFLImageSubmitBody([]byte(`{"id":"x","polling_url":null}`)); err != nil {
			t.Fatal(err)
		}
		if err := validateBFLSubmitBody([]byte(`{"id":"x"}`)); err == nil || !strings.Contains(err.Error(), "polling_url") {
			t.Fatalf("video submit without polling_url: %v", err)
		}
	})
}
