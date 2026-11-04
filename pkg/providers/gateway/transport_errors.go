package gateway

import (
	"errors"
	"net"
	"net/http"
	"net/url"

	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// gatewayErrorIn mirrors TS asGatewayError's `GatewayError.isInstance(error)`
// check, which returns the gateway error itself. In Go a gateway error
// raised inside the HTTP client (the credential resolver of
// gatewayAuthTransport: a missing API key or OIDC token) reaches the error
// handler wrapped in the client's *url.Error and "Cannot connect to API"
// error; TS raises it from headers() before fetch, unwrapped. The gateway
// error is returned, not its wrappers.
func gatewayErrorIn(err error) (error, bool) {
	var ge gatewayerrors.GatewayError
	if errors.As(err, &ge) {
		return ge, true
	}
	return nil, false
}

// gatewayConnectError converts a failure of the HTTP client (every
// http.Client.Do failure is a *url.Error) the way TS asGatewayError converts
// the APICallError "Cannot connect to API" that provider-utils
// handleFetchError builds for a failed fetch: createGatewayErrorFromResponse
// with response {} (the APICallError has no data or body), status 500,
// "Gateway request failed" and isRetryable true, the transport error kept as
// the cause.
func gatewayConnectError(err error) (error, bool) {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return nil, false
	}
	out := gatewayerrors.CreateGatewayErrorFromResponse([]byte("{}"), http.StatusInternalServerError, "Gateway request failed", err, "")
	if re, ok := out.(*gatewayerrors.GatewayResponseError); ok {
		retryable := true
		re.Retryable = &retryable
	}
	return out, true
}

// gatewayFetchRejection is an error of a caller-supplied RoundTripper (the
// TS `fetch` option) that is not a network error. TS provider-utils
// handleFetchError turns only a failed fetch (`TypeError: fetch failed`)
// into the APICallError "Cannot connect to API" and returns any other
// rejection unchanged, so asGatewayError returns a rejected gateway error as
// is and converts any other error to a GatewayResponseError "Invalid error
// response format: Gateway request failed: <message>" with the error as the
// cause. A stdlib *http.Transport only fails the way fetch fails (network,
// TLS, protocol), so only a custom RoundTripper's errors are marked.
type gatewayFetchRejection struct{ err error }

func (e *gatewayFetchRejection) Error() string { return e.err.Error() }
func (e *gatewayFetchRejection) Unwrap() error { return e.err }

// customFetchError marks err of a custom RoundTripper as a fetch rejection
// unless it is a network error (a custom RoundTripper that delegates to a
// stdlib transport: the TS failed-fetch path).
func customFetchError(err error) error {
	var ne net.Error
	if errors.As(err, &ne) {
		return err
	}
	return &gatewayFetchRejection{err: err}
}
