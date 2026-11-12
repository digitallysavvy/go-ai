package http

import (
	"context"
	"errors"
	"net"
)

// NoResponseError reports whether err is an aborted request that never got
// an HTTP response: a cancellation or deadline (TS abort and timeout errors)
// or a transport timeout. It returns err unchanged with true, so a provider
// error handler returns it as is instead of wrapping it in a status-0
// ProviderError, like TS provider-utils handleFetchError (isAbortError:
// `return error`). A failed connection is not one: TS turns it into the
// retryable "Cannot connect to API" APICallError, which each handler keeps
// building. An error that carries an HTTP status (*HTTPStatusError) is never
// one.
func NoResponseError(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return nil, false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return err, true
	}
	return nil, false
}
