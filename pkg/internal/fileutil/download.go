package fileutil

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	// DefaultMaxDownloadSize is the default maximum download size: 2 GiB.
	//
	// This limit prevents memory exhaustion from unbounded downloads.
	// Very large downloads risk exceeding default memory limits and causing
	// out-of-memory errors. Setting this limit converts an unrecoverable OOM
	// crash into a catchable DownloadError.
	DefaultMaxDownloadSize = 2 * 1024 * 1024 * 1024 // 2 GiB
)

// DownloadOptions contains options for downloading files
type DownloadOptions struct {
	// Timeout for the download operation
	Timeout time.Duration

	// Headers to include in the request
	Headers map[string]string

	// MaxSize limits the size of the download (in bytes)
	// Default: 2 GiB (DefaultMaxDownloadSize)
	MaxSize int64

	// URLValidator is called before fetching and after each redirect to validate
	// the URL. Return a non-nil error to abort the download. Nil means no validation.
	URLValidator func(string) error

	// Transport performs the HTTP requests. DefaultDownloadOptions sets it to
	// SafeTransport(), which validates and pins DNS results at connect time
	// (TS safe-node-fetch). A nil Transport uses http.DefaultTransport and
	// performs no connect-time validation; a custom Transport is responsible
	// for equivalent validation (TS: an injected fetch).
	Transport http.RoundTripper
}

// DownloadResult contains downloaded file bytes and response metadata.
type DownloadResult struct {
	Data        []byte
	ContentType string

	// Headers are the successful response's HTTP headers, when the request
	// reached an HTTP response (nil for data: URLs). PollJSON callers use
	// this to surface a provider's response headers on the caller-visible
	// result, matching TS getFromApi's responseHeaders.
	Headers map[string][]string
}

// maxErrorBodyBytes bounds how much of a non-2xx response body
// DownloadWithMetadata retains on providererrors.DownloadError, so JSON
// status-poll callers (fileutil.PollJSON) can decode a provider's structured
// error envelope without risking unbounded memory use from a hostile or
// oversized error response.
const maxErrorBodyBytes = 64 * 1024

// DefaultDownloadOptions returns default download options
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		Timeout:      60 * time.Second,
		Headers:      make(map[string]string),
		MaxSize:      DefaultMaxDownloadSize,
		URLValidator: ValidateDownloadURL,
		Transport:    SafeTransport(),
	}
}

// Download downloads a file from a URL with size limits to prevent memory exhaustion.
//
// It checks the Content-Length header for early rejection, then reads the body
// incrementally and aborts with a DownloadError when the limit is exceeded.
func Download(ctx context.Context, url string, opts DownloadOptions) ([]byte, error) {
	result, err := DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// DownloadWithMetadata downloads a file from a URL with size limits and returns
// response metadata needed by provider wire encoders.
func DownloadWithMetadata(ctx context.Context, url string, opts DownloadOptions) (*DownloadResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = DefaultMaxDownloadSize
	}

	// Validate URL before fetching (SSRF prevention).
	if opts.URLValidator != nil {
		if err := opts.URLValidator(url); err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := decodeDataURL(url)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > opts.MaxSize {
			return nil, providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize), nil)
		}
		return &DownloadResult{Data: data, ContentType: dataURLMediaType(url)}, nil
	}

	client := NewDownloadClient(url, opts)

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Add custom headers (proxy/metadata/cookie/hop-by-hop headers stripped).
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	SanitizeRequestHeaders(req.Header)
	setDownloadUserAgent(req.Header)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, downloadRequestError(url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		downloadErr := providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			responseStatusText(resp),
			"",
			nil,
		)
		downloadErr.Body = body
		downloadErr.Headers = map[string][]string(resp.Header)
		return nil, downloadErr
	}

	data, err := ReadResponseWithSizeLimit(resp, url, opts.MaxSize)
	if err != nil {
		return nil, err
	}

	return &DownloadResult{
		Data:        data,
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     map[string][]string(resp.Header),
	}, nil
}

// DownloadToWriter downloads a file from a URL and writes it to an io.Writer
// with size limits to prevent memory exhaustion.
func DownloadToWriter(ctx context.Context, url string, writer io.Writer, opts DownloadOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = DefaultMaxDownloadSize
	}

	// Validate URL before fetching (SSRF prevention).
	if opts.URLValidator != nil {
		if err := opts.URLValidator(url); err != nil {
			return err
		}
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := decodeDataURL(url)
		if err != nil {
			return err
		}
		if int64(len(data)) > opts.MaxSize {
			return providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize), nil)
		}
		_, err = writer.Write(data)
		return err
	}

	client := NewDownloadClient(url, opts)

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Add custom headers (proxy/metadata/cookie/hop-by-hop headers stripped).
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	SanitizeRequestHeaders(req.Header)
	setDownloadUserAgent(req.Header)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return downloadRequestError(url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			responseStatusText(resp),
			"",
			nil,
		)
	}

	// Early rejection based on Content-Length header
	if contentLength, ok := responseContentLength(resp); ok && contentLength > opts.MaxSize {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).",
				url, opts.MaxSize, contentLength),
			nil,
		)
	}

	// Copy to writer with size limit
	reader := io.LimitReader(resp.Body, opts.MaxSize+1)

	written, err := io.Copy(writer, reader)
	if err != nil {
		return providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Check if we exceeded the size limit during download
	if written > opts.MaxSize {
		return providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize),
			nil,
		)
	}

	return nil
}

const maxDownloadRedirects = 10

// NewDownloadClient builds a per-download HTTP client using opts.Transport. Every redirect hop is
// validated with opts.URLValidator before it is requested, and all caller
// headers except User-Agent are dropped when a redirect crosses origin (TS
// fetchWithValidatedRedirects): providers authenticate with custom headers
// too, and nothing else stops them from riding to a foreign host.
func NewDownloadClient(rawURL string, opts DownloadOptions) *http.Client {
	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: opts.Transport,
	}
	validator := opts.URLValidator
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxDownloadRedirects {
			return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("Too many redirects (max %d)", maxDownloadRedirects), nil)
		}
		if validator != nil {
			if err := validator(req.URL.String()); err != nil {
				return err
			}
		}
		// http.Client re-copies the ORIGINAL request's headers onto every hop,
		// so once any hop in the chain has left the original origin, headers
		// must stay stripped for the rest of the chain (TS keeps a sticky flag).
		if len(via) > 0 && redirectChainCrossedOrigin(req, via) {
			userAgent := req.Header.Get("User-Agent")
			for k := range req.Header {
				delete(req.Header, k)
			}
			if userAgent != "" {
				req.Header.Set("User-Agent", userAgent)
			}
		}
		return nil
	}
	return client
}

// redirectChainCrossedOrigin reports whether req or any earlier hop has a
// different origin from the first request in the chain.
func redirectChainCrossedOrigin(req *http.Request, via []*http.Request) bool {
	origin := via[0].URL
	if !sameOrigin(req.URL, origin) {
		return true
	}
	for _, hop := range via[1:] {
		if !sameOrigin(hop.URL, origin) {
			return true
		}
	}
	return false
}

// blockedRequestHeaders mirrors TS sanitize-request-headers.ts.
var blockedRequestHeaders = []string{
	// Hop-by-hop / transport (RFC 7230 §6.1)
	"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	// Host / virtual-host routing
	"Host",
	// Proxy / origin spoofing
	"Forwarded", "Proxy-Authorization", "Via", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip",
	// Cloud metadata
	"Metadata", "Metadata-Flavor", "X-Aws-Ec2-Metadata-Token", "X-Metadata-Token",
	// Session / cookie
	"Cookie", "Set-Cookie",
}

// setDownloadUserAgent tags req's User-Agent with the SDK-wide tag plus the
// runtime tag, appending to (not replacing) any caller-supplied value.
// Matches TS packages/ai/src/util/download/download.ts's
// `withUserAgentSuffix({}, ai-sdk/${VERSION}, getRuntimeEnvironmentUserAgent())`.
func setDownloadUserAgent(h http.Header) {
	merged := providerutils.WithUserAgentSuffix(
		map[string]string{"user-agent": h.Get("User-Agent")},
		version.SDKUserAgent(),
		providerutils.RuntimeEnvironmentUserAgent(),
	)
	h.Set("User-Agent", merged["user-agent"])
}

// SanitizeRequestHeaders removes proxy, cloud-metadata, cookie and hop-by-hop
// headers before fetching an untrusted URL (TS sanitizeRequestHeaders).
// Credential headers are intentionally kept for the first hop; they are
// dropped on cross-origin redirects instead.
func SanitizeRequestHeaders(h http.Header) {
	for _, name := range blockedRequestHeaders {
		h.Del(name)
	}
}

func sameOrigin(a, b *neturl.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return originOf(a) == originOf(b)
}

func originOf(u *neturl.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http", "ws":
			port = "80"
		case "https", "wss":
			port = "443"
		case "ftp":
			port = "21"
		}
	}
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// ReadResponseWithSizeLimit reads resp.Body, rejecting early when
// Content-Length exceeds maxBytes and aborting with a DownloadError once more
// than maxBytes have been read (TS readResponseWithSizeLimit). maxBytes <= 0
// uses DefaultMaxDownloadSize. The body is not closed.
func ReadResponseWithSizeLimit(resp *http.Response, rawURL string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxDownloadSize
	}
	if resp == nil || resp.Body == nil {
		return []byte{}, nil
	}
	if contentLength, ok := responseContentLength(resp); ok && contentLength > maxBytes {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, providererrors.NewDownloadError(
			rawURL,
			resp.StatusCode,
			responseStatusText(resp),
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).",
				rawURL, maxBytes, contentLength),
			nil,
		)
	}
	// Preserve the real HTTP status code even when the body read/decode
	// itself fails (e.g. a truncated 2xx response), so callers such as the
	// gateway retry classifier (pkg/ai.isGatewayCallRetryable) see the
	// status the provider actually returned instead of losing it to a
	// generic body-read error (hand-off: "internal/http real status on
	// body-read errors"; TS keeps e.g. 200 on a truncated 2xx).
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, providererrors.NewDownloadError(rawURL, resp.StatusCode, responseStatusText(resp), "", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, providererrors.NewDownloadError(
			rawURL,
			resp.StatusCode,
			responseStatusText(resp),
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", rawURL, maxBytes),
			nil,
		)
	}
	return data, nil
}

func responseStatusText(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	prefix := fmt.Sprintf("%d ", resp.StatusCode)
	if strings.HasPrefix(resp.Status, prefix) {
		return strings.TrimPrefix(resp.Status, prefix)
	}
	return http.StatusText(resp.StatusCode)
}

func responseContentLength(resp *http.Response) (int64, bool) {
	if resp == nil {
		return 0, false
	}
	raw := resp.Header.Get("Content-Length")
	if raw == "" {
		if resp.ContentLength > 0 {
			return resp.ContentLength, true
		}
		return 0, false
	}
	raw = strings.TrimLeft(raw, " \t\n\r\f\v")
	sign := int64(1)
	if strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	} else if strings.HasPrefix(raw, "-") {
		sign = -1
		raw = raw[1:]
	}
	var value int64
	digits := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			break
		}
		digits++
		if value <= (1<<63-1-int64(r-'0'))/10 {
			value = value*10 + int64(r-'0')
		} else {
			value = 1<<63 - 1
		}
	}
	if digits == 0 {
		return 0, false
	}
	return sign * value, true
}

func downloadRequestError(raw string, err error) *providererrors.DownloadError {
	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) {
		return downloadErr
	}
	return providererrors.NewDownloadError(raw, 0, "", "", err)
}

// decodeDataURL follows the fetch data: URL processor that TS download relies
// on: the payload is percent-decoded ('+' stays '+'), and a base64 payload is
// decoded with the forgiving-base64 rules (ASCII whitespace ignored, padding
// optional).
func decodeDataURL(raw string) ([]byte, error) {
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return nil, providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), nil)
	}
	meta := raw[len("data:"):comma]
	payload := raw[comma+1:]
	if i := strings.IndexByte(payload, '#'); i >= 0 {
		payload = payload[:i] // a fragment is not part of the data
	}
	decoded := percentDecode(payload)
	if isBase64DataURLMeta(meta) {
		data, err := DecodeForgivingBase64(decoded)
		if err != nil {
			return nil, providererrors.NewDownloadError(raw, 0, "", "", err)
		}
		return data, nil
	}
	return []byte(decoded), nil
}

// DecodeForgivingBase64 is the WHATWG forgiving-base64 decode behind JS atob:
// ASCII whitespace is ignored, up to two trailing "=" are dropped when the
// length is a multiple of 4, and any other "=" or a length of 1 mod 4 fails.
func DecodeForgivingBase64(s string) ([]byte, error) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, s)
	if len(compact)%4 == 0 {
		if strings.HasSuffix(compact, "==") {
			compact = compact[:len(compact)-2]
		} else if strings.HasSuffix(compact, "=") {
			compact = compact[:len(compact)-1]
		}
	}
	if strings.Contains(compact, "=") || len(compact)%4 == 1 {
		return nil, base64.CorruptInputError(0)
	}
	return base64.RawStdEncoding.DecodeString(compact)
}

// percentDecode is the URL standard's percent-decode: each valid %XX becomes
// its byte and anything else (including a malformed escape) stays literal.
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	hex := func(c byte) int {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
		return -1
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && hex(s[i+1]) >= 0 && hex(s[i+2]) >= 0 {
			out = append(out, byte(hex(s[i+1])<<4|hex(s[i+2])))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

func isBase64DataURLMeta(meta string) bool {
	_, ok := stripBase64Marker(meta)
	return ok
}

// stripBase64Marker removes the WHATWG data URL base64 marker (a ";", any
// spaces, then "base64" in any case, at the end of the media type) and
// reports whether it was there.
func stripBase64Marker(meta string) (string, bool) {
	trimmed := strings.Trim(meta, " \t\n\r\f")
	if len(trimmed) < len("base64") || !strings.EqualFold(trimmed[len(trimmed)-len("base64"):], "base64") {
		return meta, false
	}
	rest := strings.TrimRight(trimmed[:len(trimmed)-len("base64")], " ")
	if !strings.HasSuffix(rest, ";") {
		return meta, false
	}
	return strings.TrimRight(strings.TrimSuffix(rest, ";"), " "), true
}

// dataURLMediaType is the Content-Type fetch gives a data: URL response (the
// WHATWG data: URL processor): the media type parsed and serialized, minus
// the base64 marker; "text/plain" is prepended when it starts with ";", and an
// absent or unparseable type is text/plain;charset=US-ASCII.
func dataURLMediaType(raw string) string {
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return ""
	}
	meta := strings.Trim(raw[len("data:"):comma], " \t\n\r\f")
	if stripped, ok := stripBase64Marker(meta); ok {
		meta = stripped
	}
	if strings.HasPrefix(meta, ";") {
		meta = "text/plain" + meta
	}
	if mt, ok := parseWHATWGMediaType(meta); ok {
		return mt
	}
	return "text/plain;charset=US-ASCII"
}

func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// parseWHATWGMediaType parses and serializes a MIME type like the MIME Sniffing
// standard (parameters keep their order, the first of a repeated name wins).
func parseWHATWGMediaType(input string) (string, bool) {
	input = strings.Trim(input, " \t\n\r")
	slash := strings.IndexByte(input, '/')
	if slash < 0 {
		return "", false
	}
	typ := input[:slash]
	rest := input[slash+1:]
	subtype, params, _ := strings.Cut(rest, ";")
	subtype = strings.TrimRight(subtype, " \t\n\r")
	if !isHTTPToken(typ) || !isHTTPToken(subtype) {
		return "", false
	}
	var out strings.Builder
	out.WriteString(strings.ToLower(typ) + "/" + strings.ToLower(subtype))
	seen := map[string]bool{}
	for params != "" {
		params = strings.TrimLeft(params, " \t\n\r")
		end := strings.IndexAny(params, ";=")
		var name string
		if end < 0 {
			name, params = params, ""
		} else {
			name = params[:end]
			if params[end] == ';' {
				params = params[end+1:]
				continue
			}
			params = params[end+1:]
		}
		name = strings.ToLower(name)
		var value string
		if strings.HasPrefix(params, `"`) {
			var b strings.Builder
			i := 1
			for ; i < len(params); i++ {
				c := params[i]
				if c == '\\' && i+1 < len(params) {
					i++
					b.WriteByte(params[i])
					continue
				}
				if c == '"' {
					i++
					break
				}
				b.WriteByte(c)
			}
			value = b.String()
			rem := params[i:]
			if k := strings.IndexByte(rem, ';'); k >= 0 {
				params = rem[k+1:]
			} else {
				params = ""
			}
			if !isHTTPToken(name) || seen[name] {
				continue
			}
		} else {
			var v string
			if k := strings.IndexByte(params, ';'); k >= 0 {
				v, params = params[:k], params[k+1:]
			} else {
				v, params = params, ""
			}
			value = strings.TrimRight(v, " \t\n\r")
			if value == "" || !isHTTPToken(name) || seen[name] {
				continue
			}
		}
		seen[name] = true
		out.WriteString(";" + name + "=")
		if isHTTPToken(value) {
			out.WriteString(value)
		} else {
			out.WriteString(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`)
		}
	}
	return out.String(), true
}
