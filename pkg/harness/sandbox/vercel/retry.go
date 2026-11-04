package vercel

import (
	"context"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"time"
)

// Request retries of the @vercel/sandbox 3.9.0 base client
// (dist/api-client/with-retry.js, async-retry with retries 2, minTimeout
// 400, factor 2 and randomized timeouts): every request is retried after a
// network error, a 429 or a 5xx response, then the last response (or error)
// is returned. A 429 whose Retry-After exceeds 20 seconds is returned at
// once; a positive Retry-After is waited before the backoff. Requests TS
// sends with `retry: {retries: 0}` (runCommand with wait, the stdin writes,
// which have their own rules) opt out with withoutRetry.

// sandboxRequestRetries is the with-retry.js default `retries: 2`.
const sandboxRequestRetries = 2

type noRetryKey struct{}

// withoutRetry marks ctx's requests as TS `retry: {retries: 0}`.
func withoutRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, noRetryKey{}, true)
}

// requestRetryBackoff is async-retry's timeout before retry n (0-based):
// 400 ms * 2^n * a random factor in [1, 2).
var requestRetryBackoff = func(n int) time.Duration {
	return time.Duration(float64(400*time.Millisecond) * math.Pow(2, float64(n)) * (1 + rand.Float64()))
}

// retryAfterDelay converts a Retry-After value in seconds to a wait.
var retryAfterDelay = func(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

// retryTransport implements with-retry.js around a base RoundTripper.
type retryTransport struct {
	base http.RoundTripper
}

func discardBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	retries := sandboxRequestRetries
	if ctx.Value(noRetryKey{}) != nil {
		retries = 0
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r := req
		if attempt > 1 && req.Body != nil && req.Body != http.NoBody {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r = req.Clone(ctx)
			r.Body = body
		}
		hasRetries := attempt <= retries && (req.Body == nil || req.Body == http.NoBody || req.GetBody != nil)
		resp, err := t.base.RoundTrip(r)
		switch {
		case err != nil:
			if ctx.Err() != nil || !hasRetries {
				return nil, err
			}
		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter := jsNumber(resp.Header.Get("Retry-After"))
			if retryAfter > 20 || !hasRetries {
				return resp, nil
			}
			discardBody(resp)
			if retryAfter > 0 {
				if err := sleepCtx(ctx, retryAfterDelay(retryAfter)); err != nil {
					return nil, err
				}
			}
		case resp.StatusCode >= 500 && resp.StatusCode < 600:
			if !hasRetries {
				return resp, nil
			}
			discardBody(resp)
		default:
			return resp, nil
		}
		if err := sleepCtx(ctx, requestRetryBackoff(attempt-1)); err != nil {
			return nil, err
		}
	}
}
