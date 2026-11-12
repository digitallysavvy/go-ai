package http

import (
	"errors"
)

// NoResponseError reports whether err is an aborted request that never got
// an HTTP response: a cancellation, the caller's deadline or the
// http.Client's own Timeout (TS abort and timeout errors). It returns err
// unchanged with true, so a provider error handler returns it as is instead
// of wrapping it in a status-0 ProviderError, like TS provider-utils
// handleFetchError (isAbortError: `return error`). A failed connection is not
// one, a transport timeout (connect, TLS, response headers) included: TS
// turns it into the retryable "Cannot connect to API" APICallError, which
// each handler keeps building. An error that carries an HTTP status
// (*HTTPStatusError) is never one.
func NoResponseError(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return nil, false
	}
	var connErr *connectError
	if errors.As(err, &connErr) {
		return nil, false
	}
	if isCallerAbort(err, nil) {
		return err, true
	}
	return nil, false
}
