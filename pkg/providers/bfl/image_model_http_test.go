package bfl

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBFLImageModel_MetadataAndClient(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k"})
	if p.Client() == nil {
		t.Fatal("Client() returned nil")
	}
	m := NewImageModel(p, "flux-pro")
	if m.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "bfl" {
		t.Fatalf("Provider() = %q", m.Provider())
	}
}

func TestBFLImageModel_DoGenerateSuccess(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/flux-pro":
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"http://` + r.Host + `/get_result","cost":0.12,"input_mp":1.5,"output_mp":1.0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/get_result":
			_, _ = w.Write([]byte(`{"id":"req-1","status":"Ready","result":{"sample":"data:image/png;base64,iVBORw==","seed":123,"start_time":1,"end_time":2,"duration":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		Size:   "1024x1024",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(res.Image) == 0 {
		t.Fatal("image bytes must not be empty")
	}
	if res.URL == "" {
		t.Fatal("result URL must be set")
	}
	if res.Response == nil || res.Response.ModelID != "flux-pro" {
		t.Fatalf("response metadata = %#v", res.Response)
	}
	if res.Response.Timestamp.IsZero() {
		t.Fatalf("response timestamp must be set like TS response.timestamp")
	}
	if capturedBody["aspect_ratio"] != "1:1" {
		t.Fatalf("aspect_ratio = %#v, want 1:1", capturedBody["aspect_ratio"])
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Feature != "size" {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
	metadata, ok := res.ProviderMetadata["blackForestLabs"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata = %#v", res.ProviderMetadata)
	}
	images := metadata["images"].([]map[string]interface{})
	if images[0]["cost"] != 0.12 || images[0]["inputMegapixels"] != 1.5 || images[0]["seed"] != 123.0 {
		t.Fatalf("metadata image = %#v", images[0])
	}
}

func TestBFLImageModel_HeadersForTrustedAndForeignURLs(t *testing.T) {
	// Not parallel: this test swaps the process-global http.DefaultTransport
	// and the package download transport, which races with parallel tests.

	var submitHeaders http.Header
	var pollHeaders http.Header
	var imageHeaders http.Header
	var foreignHeaders http.Header
	useForeign := false
	oldDefaultTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "delivery-us1.bfl.ai":
			imageHeaders = r.Header.Clone()
		case "foreign.example":
			foreignHeaders = r.Header.Clone()
		default:
			return oldDefaultTransport.RoundTrip(r)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader("image")),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldDefaultTransport })
	oldDownloadTransport := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = oldDownloadTransport })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			submitHeaders = r.Header.Clone()
			_, _ = w.Write([]byte(`{"id":"req-headers","polling_url":"http://` + r.Host + `/poll"}`))
		case "/poll":
			pollHeaders = r.Header.Clone()
			sample := "https://delivery-us1.bfl.ai/image.png"
			if useForeign {
				sample = "http://foreign.example/image.png"
			}
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + sample + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "k",
		BaseURL: server.URL,
		Headers: map[string]string{
			"Custom-Provider-Header": "provider",
		},
	})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
		Headers: map[string]string{"Custom-Request-Header": "request"},
	})
	if err != nil {
		t.Fatalf("DoGenerate trusted: %v", err)
	}
	if submitHeaders.Get("Custom-Provider-Header") != "provider" || submitHeaders.Get("Custom-Request-Header") != "request" {
		t.Fatalf("submit headers = %#v", submitHeaders)
	}
	if pollHeaders.Get("Custom-Provider-Header") != "provider" || pollHeaders.Get("Custom-Request-Header") != "request" || pollHeaders.Get("Content-Type") != "" {
		t.Fatalf("poll headers = %#v", pollHeaders)
	}
	if imageHeaders.Get("Custom-Provider-Header") != "provider" || imageHeaders.Get("Custom-Request-Header") != "request" {
		t.Fatalf("image headers = %#v", imageHeaders)
	}

	useForeign = true
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
		Headers: map[string]string{"Custom-Request-Header": "request"},
	})
	if err != nil {
		t.Fatalf("DoGenerate foreign: %v", err)
	}
	if foreignHeaders.Get("X-Key") != "" || foreignHeaders.Get("Custom-Provider-Header") != "" || foreignHeaders.Get("Custom-Request-Header") != "" {
		t.Fatalf("foreign headers = %#v", foreignHeaders)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// fakeDNSResolver implements fileutil.Resolver with a fixed hostname->IP
// answer set, for tests that need to prove a hop was resolved and pinned
// through fileutil.SafeTransport rather than dialed via a plain transport.
type fakeDNSResolver struct {
	answers map[string][]string
}

func (r *fakeDNSResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addrs, ok := r.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, net.IPAddr{IP: net.ParseIP(a)})
	}
	return out, nil
}

// TestBFLImageModel_TrustedRedirectToForeignHostIsDNSPinned guards against
// F3: a trusted URL (same-origin with the configured base URL, so it skips
// the SSRF string check) redirects to a foreign hostname. That redirect hop
// must still be validated and dialed through the DNS-pinning transport, not
// the plain transport picked for the (trusted) first hop. Before the fix,
// the whole client used a single transport chosen from the first URL, so a
// hostname resolving to a link-local/metadata address (169.254.169.254)
// would have been dialed unpinned and, with the old code path, without ever
// going through the injected resolver at all.
func TestBFLImageModel_TrustedRedirectToForeignHostIsDNSPinned(t *testing.T) {
	// Not parallel: overrides the package-level downloadTransport.
	oldDownloadTransport := downloadTransport
	resolver := &fakeDNSResolver{answers: map[string][]string{"evil.internal": {"169.254.169.254"}}}
	downloadTransport = func() http.RoundTripper {
		return fileutil.NewSafeTransport(fileutil.SafeDialOptions{Resolver: resolver})
	}
	t.Cleanup(func() { downloadTransport = oldDownloadTransport })

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"` + server.URL + `/poll"}`))
		case "/poll":
			// The sample URL is same-origin with the configured base URL, so
			// it is trusted, but the server redirects it to a foreign host.
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + server.URL + `/redirect"}}`))
		case "/redirect":
			http.Redirect(w, r, "http://evil.internal/image.png", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
	})
	if err == nil {
		t.Fatal("expected the foreign redirect target to be rejected as a disallowed IP")
	}
	if !strings.Contains(err.Error(), "disallowed IP address 169.254.169.254") {
		t.Fatalf("error = %v, want a DNS-pinning rejection of the resolved disallowed IP", err)
	}
}

// TestBFLImageModel_TrustedSelfHostedLocalhostBaseURL guards against the F3
// minor: ValidateDownloadURL used to run unconditionally on the initial URL
// even when it was trusted, so a self-hosted BaseURL such as
// "http://localhost:PORT" was rejected outright. A trusted hop must skip the
// generic SSRF validator entirely, matching TS (trusted hops skip
// validation).
func TestBFLImageModel_TrustedSelfHostedLocalhostBaseURL(t *testing.T) {
	var localhostBaseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"` + localhostBaseURL + `/poll"}`))
		case "/poll":
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + localhostBaseURL + `/image.png"}}`))
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	localhostBaseURL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	p := New(Config{APIKey: "k", BaseURL: localhostBaseURL})
	m := NewImageModel(p, "flux-pro")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
	})
	if err != nil {
		t.Fatalf("self-hosted localhost base URL must be trusted, got error: %v", err)
	}
	if string(res.Image) != "image-bytes" {
		t.Fatalf("image = %q, want image-bytes", res.Image)
	}
}

// TestBFLImageModel_PollingURLToPrivateHostIsRejected guards against the
// getPollBody SSRF gap found in P0 review round 2: the poll target
// (bflCreateResponse.PollingURL) is provider-response data, exactly like the
// image sample URL, so it must be validated and never fetched with a plain,
// unguarded HTTP client. Before the fix, getPollBody built the request with
// http.NewRequestWithContext and sent it through
// m.provider.client.HTTPClient().Do directly -- no ValidateDownloadURL check,
// no DNS pinning, no redirect protection, and an unbounded read -- so a
// malicious or compromised polling_url could reach an internal address (e.g.
// a cloud metadata endpoint) unimpeded. Mirrors TS pollForImageUrl's
// getFromApi({validateUrl: true, trustedOrigin}).
func TestBFLImageModel_PollingURLToPrivateHostIsRejected(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			// The create response's polling_url points straight at a
			// disallowed private/link-local address, not at the configured
			// base URL or a *.bfl.ai host.
			_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"http://169.254.169.254/poll"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
	})
	if err == nil {
		t.Fatal("expected the untrusted poll URL to a disallowed IP to be rejected")
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Fatalf("error = %v, want a rejection naming the disallowed IP", err)
	}
}

func TestBFLImageModel_DoGenerateErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("create call non-200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer server.Close()

		p := New(Config{APIKey: "k", BaseURL: server.URL})
		m := NewImageModel(p, "flux-pro")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("poll returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/flux-pro":
				_, _ = w.Write([]byte(`{"id":"req-2","polling_url":"http://` + r.Host + `/get_result"}`))
			case "/get_result":
				_, _ = w.Write([]byte(`{"id":"req-2","status":"Failed","result":{}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		p := New(Config{APIKey: "k", BaseURL: server.URL})
		m := NewImageModel(p, "flux-pro")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "Black Forest Labs generation failed.") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBFLImageModel_SubmitResponseRequiresPollingURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"req-missing"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "missing polling_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBFLImageModel_UsesPollingURLAndStateAlias(t *testing.T) {
	t.Parallel()

	var sawProviderPollURL bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"req-3","polling_url":"http://` + r.Host + `/custom_poll"}`))
		case "/custom_poll":
			sawProviderPollURL = true
			if r.URL.Query().Get("id") != "req-3" {
				t.Fatalf("poll id = %q", r.URL.Query().Get("id"))
			}
			if got := r.Header.Get("X-Key"); got != "k" {
				t.Fatalf("X-Key = %q", got)
			}
			_, _ = w.Write([]byte(`{"state":"Ready","result":{"sample":"data:image/png;base64,iVBORw=="}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	createResp := bflCreateResponse{ID: "req-3", PollingURL: server.URL + "/custom_poll"}
	result, err := m.pollResult(context.Background(), createResp, bflPollConfig{}, map[string]string{"Custom": "value"})
	if err != nil {
		t.Fatalf("pollResult: %v", err)
	}
	if !sawProviderPollURL || result.Result.Sample == "" {
		t.Fatalf("pollURL used=%v result=%#v", sawProviderPollURL, result)
	}
}

func TestBFLImageModel_PollResultContextCancelled(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	m := NewImageModel(p, "flux-pro")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.pollResult(ctx, bflCreateResponse{ID: "req"}, bflPollConfig{}, nil)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBFLImageModel_PollResultWallClockDeadline verifies that pollResult
// enforces a wall-clock deadline (TS e4e761e) rather than a fixed attempt
// count: each poll response is slow enough that a count-based budget
// (ceil(timeout/interval) attempts) would let the loop run several times
// longer than the configured timeout before giving up.
func TestBFLImageModel_PollResultWallClockDeadline(t *testing.T) {
	t.Parallel()

	const pollLatency = 30 * time.Millisecond
	const pollTimeout = 50 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_result":
			// Always slow and never terminal: a count-based budget of
			// ceil(50ms/1ms) = 50 attempts at 30ms each would take ~1.5s.
			time.Sleep(pollLatency)
			_, _ = w.Write([]byte(`{"status":"Pending"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")

	start := time.Now()
	_, err := m.pollResult(context.Background(), bflCreateResponse{ID: "req", PollingURL: server.URL + "/get_result"}, bflPollConfig{
		Interval: time.Millisecond,
		Timeout:  pollTimeout,
	}, nil)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
	// A count-based budget would need ~1.5s (50 attempts x 30ms); the
	// wall-clock deadline must abort well before that, close to the
	// configured timeout plus one in-flight poll.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected wall-clock deadline to abort quickly, took %v", elapsed)
	}
}
