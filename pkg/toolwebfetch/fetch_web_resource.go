package toolwebfetch

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const maxRedirects = 5

type errorCode string

const (
	errInvalidURL          errorCode = "invalid_url"
	errURLNotAllowed       errorCode = "url_not_allowed"
	errURLNotInUserMessage errorCode = "url_not_in_user_message"
	errHTTP                errorCode = "http_error"
	errTimeout             errorCode = "timeout"
	errTooLarge            errorCode = "too_large"
	errTooManyRedirects    errorCode = "too_many_redirects"
	errUnsupportedType     errorCode = "unsupported_content_type"
	errConversionFailed    errorCode = "conversion_failed"
	errNetwork             errorCode = "network_error"
)

type fetchResult struct {
	// Error fields, set when errCode != "".
	errCode    errorCode
	message    string
	statusCode int

	finalURL    string
	contentType string
	body        []byte
}

// Error messages never include URLs. The model already knows the URL it
// requested, and repeating model-provided input in results lets the model
// place arbitrary text into the conversation.

// errnoName names the error code Node's fetch would report for a network
// failure (TS formatErrorCode: the first cause with an uppercase code such as
// ENOTFOUND), which explains it without repeating the URL. Go errors carry no
// such codes, so the common ones are mapped: DNS (ENOTFOUND / EAI_AGAIN),
// socket errnos, undici's connect timeout and closed socket, and the
// OpenSSL certificate codes.
func errnoName(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTemporary || dnsErr.IsTimeout {
			return "EAI_AGAIN"
		}
		return "ENOTFOUND"
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return "ERR_TLS_CERT_ALTNAME_INVALID"
	}
	var invalidErr x509.CertificateInvalidError
	if errors.As(err, &invalidErr) {
		if invalidErr.Reason == x509.Expired {
			return "CERT_HAS_EXPIRED"
		}
		return ""
	}
	var authorityErr x509.UnknownAuthorityError
	if errors.As(err, &authorityErr) {
		if c := authorityErr.Cert; c != nil && bytes.Equal(c.RawIssuer, c.RawSubject) {
			return "DEPTH_ZERO_SELF_SIGNED_CERT"
		}
		return "UNABLE_TO_GET_ISSUER_CERT_LOCALLY"
	}
	for _, e := range []struct {
		errno syscall.Errno
		name  string
	}{
		{syscall.ECONNREFUSED, "ECONNREFUSED"},
		{syscall.ECONNRESET, "ECONNRESET"},
		{syscall.ECONNABORTED, "ECONNABORTED"},
		{syscall.EPIPE, "EPIPE"},
		{syscall.ETIMEDOUT, "ETIMEDOUT"},
		{syscall.EHOSTUNREACH, "EHOSTUNREACH"},
		{syscall.ENETUNREACH, "ENETUNREACH"},
	} {
		if errors.Is(err, e.errno) {
			return e.name
		}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Timeout() {
		return "UND_ERR_CONNECT_TIMEOUT"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		// undici: a body cut short is TypeError('terminated') caused by a
		// SocketError with code UND_ERR_SOCKET.
		return "UND_ERR_SOCKET"
	}
	return ""
}

func formatErrno(err error) string {
	if name := errnoName(err); name != "" {
		return " (" + name + ")"
	}
	return ""
}

// fetchWebResource fetches an untrusted URL with a GET request.
//
// Redirects are followed manually so that every hop is checked against the
// domain filter and the private-address guard before it is requested. The
// default transport additionally validates and pins DNS results at connect
// time; a custom transport must provide equivalent validation. A returned
// error is a context error (timeout or cancellation); everything else,
// including a Location TS new URL rejects, is reported in the result.
func fetchWebResource(ctx context.Context, rawURL string, headers map[string]string, maxBytes int64, transport http.RoundTripper, filter domainFilter) (fetchResult, error) {
	// Requests go straight to the transport: http.Client would parse every
	// Location header itself and fail on hosts net/url cannot parse (for
	// example a percent-encoded host) before the guards below see them.
	// Redirects are handled below, one validated hop at a time.
	if transport == nil {
		transport = http.DefaultTransport
	}

	current := rawURL
	for redirectCount := 0; redirectCount <= maxRedirects; redirectCount++ {
		currentURL, err := url.Parse(current)
		if err != nil {
			return fetchResult{errCode: errInvalidURL, message: "The URL is not a valid absolute URL."}, nil
		}
		if !filter.allows(currentURL) {
			message := "The domain of the URL is not allowed."
			if redirectCount > 0 {
				message = "The URL redirected to a domain that is not allowed."
			}
			return fetchResult{errCode: errURLNotAllowed, message: message}, nil
		}
		if err := fileutil.ValidateDownloadURL(current); err != nil {
			message := "The URL points to a local, private, or reserved address, which is not allowed."
			if redirectCount > 0 {
				message = "The URL redirected to a local, private, or reserved address, which is not allowed."
			}
			return fetchResult{errCode: errURLNotAllowed, message: message}, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return fetchResult{errCode: errInvalidURL, message: "The URL is not a valid absolute URL."}, nil
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			if ctx.Err() != nil {
				return fetchResult{}, ctx.Err()
			}
			// The DNS-pinned transport rejects hostnames that resolve to
			// private addresses with a DownloadError.
			var downloadErr *providerrors.DownloadError
			if errors.As(err, &downloadErr) {
				return fetchResult{errCode: errURLNotAllowed, message: "The URL resolves to a private or reserved address, which is not allowed."}, nil
			}
			return fetchResult{errCode: errNetwork, message: fmt.Sprintf("The request failed%s.", formatErrno(err))}, nil
		}

		// TS response.headers.get(...): repeated lines joined with ", ", each
		// byte one character (a raw UTF-8 Location reads as Latin-1, as in TS).
		location := providerutils.FetchHeaderText(resp.Header, "Location")
		if isRedirectStatus(resp.StatusCode) && location != "" {
			drainAndClose(resp)
			next, ok := resolveRedirect(currentURL, location)
			if !ok {
				// TS dffe4053c1: an unparseable Location is a structured
				// network_error result, not a failed tool call.
				return fetchResult{errCode: errNetwork, message: "The server returned an invalid redirect location."}, nil
			}
			current = next.String()
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			drainAndClose(resp)
			message := fmt.Sprintf("The request failed with HTTP %d", resp.StatusCode)
			if text := statusText(resp); text != "" {
				message += " " + text
			}
			message += "."
			if providerutils.FetchHeaderValue(resp.Header, "Cf-Mitigated") == "challenge" {
				message = fmt.Sprintf("The request was blocked by bot protection (HTTP %d).", resp.StatusCode)
			}
			return fetchResult{errCode: errHTTP, message: message, statusCode: resp.StatusCode}, nil
		}

		body, err := fileutil.ReadResponseWithSizeLimit(resp, current, maxBytes)
		_ = resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return fetchResult{}, ctx.Err()
			}
			var downloadErr *providerrors.DownloadError
			if errors.As(err, &downloadErr) && isSizeLimitError(downloadErr) {
				return fetchResult{errCode: errTooLarge, message: fmt.Sprintf("The response exceeds the maximum size of %d bytes.", maxBytes)}, nil
			}
			return fetchResult{errCode: errNetwork, message: fmt.Sprintf("Reading the response failed%s.", formatErrno(err))}, nil
		}
		return fetchResult{finalURL: current, contentType: providerutils.FetchHeaderText(resp.Header, "Content-Type"), body: body}, nil
	}
	return fetchResult{errCode: errTooManyRedirects, message: fmt.Sprintf("The URL exceeded the maximum of %d redirects.", maxRedirects)}, nil
}

// resolveRedirect is TS new URL(location, currentUrl): an absolute location
// is canonicalized like the requested URL (host percent-decoded, IPv4 forms,
// dot segments, encoding), so the guards see the real host; a relative one is
// resolved against the current URL first. ok is false when TS would throw.
func resolveRedirect(current *url.URL, location string) (*url.URL, bool) {
	location = whatwgPreprocess(strings.TrimFunc(location, func(r rune) bool { return r <= 0x20 }))
	// "http:path" with the current scheme and without "//" is relative.
	if prefix := current.Scheme + ":"; len(location) >= len(prefix) && strings.EqualFold(location[:len(prefix)], prefix) &&
		!strings.HasPrefix(location[len(prefix):], "//") {
		location = location[len(prefix):]
	}
	// The fragment is not sent but stays in the URL (TS finalUrl keeps it).
	fragment := ""
	if i := strings.IndexByte(location, '#'); i >= 0 {
		fragment = percentEncode(location[i+1:], inFragmentSet)
	}
	next, ok := canonicalURL(location)
	if !ok || !next.IsAbs() {
		if hasURLScheme(location) {
			return nil, false
		}
		relative, err := url.Parse(escapeStrayPercents(location))
		if err != nil {
			return nil, false
		}
		if next, ok = canonicalURL(current.ResolveReference(relative).String()); !ok {
			return nil, false
		}
	}
	if strings.Contains(location, "#") {
		next.Fragment, next.RawFragment = unescapeLenient(fragment), fragment
	}
	return next, true
}

// isSizeLimitError reports the "exceeded maximum size" DownloadError that
// ReadResponseWithSizeLimit returns, as opposed to a failed body read.
func isSizeLimitError(err *providerrors.DownloadError) bool {
	return strings.Contains(err.Message, "exceeded maximum size")
}

// isRedirectStatus lists the fetch spec's redirect status codes.
func isRedirectStatus(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

func statusText(resp *http.Response) string {
	prefix := fmt.Sprintf("%d ", resp.StatusCode)
	if strings.HasPrefix(resp.Status, prefix) {
		return strings.TrimPrefix(resp.Status, prefix)
	}
	if resp.Status == "" {
		return http.StatusText(resp.StatusCode)
	}
	return ""
}

// drainAndClose releases the connection without reading an unbounded body.
func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}
