package http

import (
	"context"
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Round 87d integrator: TS handleFetchError returns a fetch rejection that is
// not a failed fetch unchanged (provider-utils handle-fetch-error.ts,
// 3fab50aa9b). A FetchRejection a RoundTripper raises comes back from the
// client without the *url.Error "Post <url>:" prefix or the retryable
// "Cannot connect to API" wrapper, and so does a request-time header
// function's error (TS `await resolve(this.config.headers)`). A
// LoadAPIKeyError from a header function stays as it was.

var errR87dinteg = errors.New("r87d signing failed")

type r87dintegTransport struct{ err error }

func (t r87dintegTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	return nil, t.err
}

func TestR87dintegFetchRejectionUnchanged(t *testing.T) {
	cases := map[string]Config{
		"round tripper": {BaseURL: "http://127.0.0.1:1", HTTPClient: &http.Client{Transport: r87dintegTransport{RejectFetch(errR87dinteg)}}},
		"headers func":  {BaseURL: "http://127.0.0.1:1", HeadersFunc: func() (map[string]string, error) { return nil, errR87dinteg }},
		"headers ctx":   {BaseURL: "http://127.0.0.1:1", HeadersFuncCtx: func(context.Context) (map[string]string, error) { return nil, errR87dinteg }},
	}
	for name, cfg := range cases {
		c := NewClient(cfg)
		for _, stream := range []bool{false, true} {
			var err error
			if stream {
				_, err = c.DoStream(context.Background(), Request{Method: http.MethodPost, Path: "/x", Body: map[string]string{"a": "b"}})
			} else {
				_, err = c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/x", Body: map[string]string{"a": "b"}})
			}
			if err == nil || err.Error() != errR87dinteg.Error() {
				t.Fatalf("%s stream=%v: err = %v, want %q", name, stream, err, errR87dinteg.Error())
			}
			cause, ok := FetchRejectionCause(err)
			if !ok || cause != errR87dinteg || UnwrapFetchRejection(err) != errR87dinteg {
				t.Fatalf("%s stream=%v: FetchRejectionCause = %v, %v", name, stream, cause, ok)
			}
			var retryable interface{ IsRetryable() bool }
			if errors.As(err, &retryable) {
				t.Fatalf("%s stream=%v: classifies itself as retryable=%v", name, stream, retryable.IsRetryable())
			}
		}
	}

	// A plain error of a RoundTripper is still a failed fetch.
	c := NewClient(Config{BaseURL: "http://127.0.0.1:1", HTTPClient: &http.Client{Transport: r87dintegTransport{errR87dinteg}}})
	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/x"})
	var ce *connectError
	if !errors.As(err, &ce) {
		t.Fatalf("plain transport error = %T %v, want the connect error", err, err)
	}
	if _, ok := FetchRejectionCause(err); ok {
		t.Fatal("plain transport error marked as a fetch rejection")
	}

	// A LoadAPIKeyError from a header function is returned as it was.
	keyErr := providererrors.NewLoadAPIKeyError("X", "X_API_KEY")
	c = NewClient(Config{BaseURL: "http://127.0.0.1:1", HeadersFunc: func() (map[string]string, error) { return nil, keyErr }})
	if _, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/x"}); err != keyErr {
		t.Fatalf("LoadAPIKeyError = %T %v, want it as is", err, err)
	}
	if RejectFetch(nil) != nil {
		t.Fatal("RejectFetch(nil) != nil")
	}
	marked := RejectFetch(errR87dinteg)
	if RejectFetch(marked) != marked {
		t.Fatal("RejectFetch marks twice")
	}
}
