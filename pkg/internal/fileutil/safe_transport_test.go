package fileutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][][]string // successive answers per host
	calls   map[string]int
}

func (r *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	answers, ok := r.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	i := r.calls[host]
	r.calls[host]++
	if i >= len(answers) {
		i = len(answers) - 1
	}
	out := make([]net.IPAddr, 0, len(answers[i]))
	for _, a := range answers[i] {
		out = append(out, net.IPAddr{IP: net.ParseIP(a)})
	}
	return out, nil
}

type recordingDial struct {
	mu    sync.Mutex
	addrs []string
}

func (d *recordingDial) dial(_ context.Context, _, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, address)
	d.mu.Unlock()
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}

func requireDisallowed(t *testing.T, err error, wantAddr string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected disallowed address error")
	}
	var de *providererrors.DownloadError
	if !errors.As(err, &de) {
		t.Fatalf("expected DownloadError, got %T: %v", err, err)
	}
	if wantAddr != "" && !strings.Contains(de.Message, "resolved to disallowed IP address "+wantAddr) {
		t.Fatalf("message = %q, want disallowed %s", de.Message, wantAddr)
	}
}

// Ports TS safe-node-fetch.test.ts "createSafeLookup" cases.
func TestSafeDialContext(t *testing.T) {
	t.Run("returns the validated addresses to the connector (pins the IP)", func(t *testing.T) {
		rec := &recordingDial{}
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"example.com": {{"93.184.216.34", "2606:4700::1"}}}},
			dial:     rec.dial,
		})
		conn, err := dial(context.Background(), "tcp", "example.com:443")
		if err != nil {
			t.Fatalf("dial error: %v", err)
		}
		_ = conn.Close()
		if len(rec.addrs) != 1 || rec.addrs[0] != "93.184.216.34:443" {
			t.Fatalf("dialed %v, want pinned 93.184.216.34:443", rec.addrs)
		}
	})

	t.Run("single-address resolver", func(t *testing.T) {
		rec := &recordingDial{}
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"one.example": {{"8.8.8.8"}}}},
			dial:     rec.dial,
		})
		conn, err := dial(context.Background(), "tcp", "one.example:80")
		if err != nil {
			t.Fatalf("dial error: %v", err)
		}
		_ = conn.Close()
		if rec.addrs[0] != "8.8.8.8:80" {
			t.Fatalf("dialed %v", rec.addrs)
		}
	})

	t.Run("single-address resolver returning a private address is blocked", func(t *testing.T) {
		rec := &recordingDial{}
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"internal.example": {{"10.0.0.5"}}}},
			dial:     rec.dial,
		})
		_, err := dial(context.Background(), "tcp", "internal.example:80")
		requireDisallowed(t, err, "10.0.0.5")
		if len(rec.addrs) != 0 {
			t.Fatalf("dialed %v, want no connection", rec.addrs)
		}
	})

	t.Run("blocks a hostname that resolves to a private address", func(t *testing.T) {
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"alias.example": {{"127.0.0.1"}}}},
			dial:     (&recordingDial{}).dial,
		})
		_, err := dial(context.Background(), "tcp", "alias.example:80")
		requireDisallowed(t, err, "127.0.0.1")
	})

	t.Run("blocks mixed DNS results when any address is private", func(t *testing.T) {
		rec := &recordingDial{}
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"mixed.example": {{"93.184.216.34", "169.254.169.254"}}}},
			dial:     rec.dial,
		})
		_, err := dial(context.Background(), "tcp", "mixed.example:80")
		requireDisallowed(t, err, "169.254.169.254")
		if len(rec.addrs) != 0 {
			t.Fatalf("dialed %v, want no connection", rec.addrs)
		}
	})

	t.Run("blocks private IPv6 DNS results", func(t *testing.T) {
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"v6.example": {{"fd00::1"}}}},
			dial:     (&recordingDial{}).dial,
		})
		_, err := dial(context.Background(), "tcp", "v6.example:80")
		requireDisallowed(t, err, "fd00::1")
	})

	t.Run("blocks IPv4-mapped IPv6 DNS results", func(t *testing.T) {
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"mapped.example": {{"::ffff:127.0.0.1"}}}},
			dial:     (&recordingDial{}).dial,
		})
		_, err := dial(context.Background(), "tcp", "mapped.example:80")
		requireDisallowed(t, err, "")
	})

	t.Run("rejects an empty DNS answer", func(t *testing.T) {
		dial := NewSafeDialContext(SafeDialOptions{
			Resolver: &fakeResolver{answers: map[string][][]string{"empty.example": {{}}}},
			dial:     (&recordingDial{}).dial,
		})
		_, err := dial(context.Background(), "tcp", "empty.example:80")
		if err == nil || !strings.Contains(err.Error(), "did not resolve to an address") {
			t.Fatalf("err = %v, want did not resolve", err)
		}
	})

	t.Run("blocks literal private IPs", func(t *testing.T) {
		dial := NewSafeDialContext(SafeDialOptions{Resolver: &fakeResolver{}, dial: (&recordingDial{}).dial})
		_, err := dial(context.Background(), "tcp", "127.0.0.1:80")
		requireDisallowed(t, err, "127.0.0.1")
	})

	t.Run("DNS rebinding: each connection is pinned to its own validated lookup", func(t *testing.T) {
		rec := &recordingDial{}
		resolver := &fakeResolver{answers: map[string][][]string{"rebind.example": {{"93.184.216.34"}, {"127.0.0.1"}}}}
		dial := NewSafeDialContext(SafeDialOptions{Resolver: resolver, dial: rec.dial})
		conn, err := dial(context.Background(), "tcp", "rebind.example:80")
		if err != nil {
			t.Fatalf("first dial: %v", err)
		}
		_ = conn.Close()
		_, err = dial(context.Background(), "tcp", "rebind.example:80")
		requireDisallowed(t, err, "127.0.0.1")
		if len(rec.addrs) != 1 || rec.addrs[0] != "93.184.216.34:80" {
			t.Fatalf("dialed %v, want only the validated public IP", rec.addrs)
		}
	})
}

func TestSafeDialContextControlRejectsPrivatePeer(t *testing.T) {
	// Real dial path (no dial override): Control hook rejects the loopback peer
	// even though the address is passed as a literal.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close() //nolint:errcheck
	dial := NewSafeDialContext(SafeDialOptions{})
	_, err = dial(context.Background(), "tcp", listener.Addr().String())
	requireDisallowed(t, err, "127.0.0.1")
}

func TestDownloadBlocksHostnameResolvingToLoopback(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("secret"))
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))

	opts := DefaultDownloadOptions()
	opts.Transport = NewSafeTransport(SafeDialOptions{
		Resolver: &fakeResolver{answers: map[string][][]string{"attacker.example": {{"127.0.0.1"}}}},
	})
	_, err := Download(context.Background(), "http://attacker.example:"+port+"/", opts)
	requireDisallowed(t, err, "127.0.0.1")
	if hits.Load() != 0 {
		t.Fatal("server behind rebinding hostname was contacted")
	}
}

func TestDownloadBlocksRedirectToRebindingHost(t *testing.T) {
	var privateHits atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateHits.Add(1)
	}))
	defer private.Close()
	_, privatePort, _ := net.SplitHostPort(strings.TrimPrefix(private.URL, "http://"))

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://rebind.example:"+privatePort+"/", http.StatusFound)
	}))
	defer redirector.Close()

	safe := NewSafeTransport(SafeDialOptions{
		Resolver: &fakeResolver{answers: map[string][][]string{"rebind.example": {{"127.0.0.1"}}}},
	})
	opts := DefaultDownloadOptions()
	opts.URLValidator = func(raw string) error {
		if strings.HasPrefix(raw, redirector.URL) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}
	// Trusted first hop goes to the loopback fixture directly; later hops use
	// the pinned transport.
	opts.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.String(), redirector.URL) {
			return http.DefaultTransport.RoundTrip(req)
		}
		return safe.RoundTrip(req)
	})
	_, err := Download(context.Background(), redirector.URL, opts)
	requireDisallowed(t, err, "127.0.0.1")
	if privateHits.Load() != 0 {
		t.Fatal("redirect target behind rebinding hostname was contacted")
	}
}

func TestDownloadDropsCallerHeadersOnCrossOriginRedirect(t *testing.T) {
	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()
	var firstHop http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHop = r.Header.Clone()
		http.Redirect(w, r, target.URL+"/file", http.StatusFound)
	}))
	defer origin.Close()

	opts := insecureDownloadOptions()
	opts.Headers = map[string]string{
		"Authorization":   "Bearer secret",
		"X-Key":           "secret",
		"User-Agent":      "ai-sdk-test",
		"Cookie":          "session=1",
		"X-Forwarded-For": "1.2.3.4",
	}
	if _, err := Download(context.Background(), origin.URL, opts); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if firstHop.Get("X-Key") != "secret" || firstHop.Get("Authorization") != "Bearer secret" {
		t.Fatalf("first hop should keep credentials: %v", firstHop)
	}
	if firstHop.Get("Cookie") != "" || firstHop.Get("X-Forwarded-For") != "" {
		t.Fatalf("first hop should strip blocked headers: %v", firstHop)
	}
	if got.Get("X-Key") != "" || got.Get("Authorization") != "" {
		t.Fatalf("cross-origin hop leaked credentials: %v", got)
	}
	if got.Get("User-Agent") != "ai-sdk-test" {
		t.Fatalf("cross-origin hop should keep User-Agent, got %q", got.Get("User-Agent"))
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
