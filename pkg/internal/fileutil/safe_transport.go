package fileutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Resolver resolves a hostname to IP addresses. *net.Resolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SafeDialOptions configures NewSafeDialContext.
type SafeDialOptions struct {
	// Resolver used for hostname lookups. Defaults to net.DefaultResolver.
	Resolver Resolver

	// Dialer used to open the pinned connection. Defaults to a net.Dialer with
	// a 30s timeout. Its Control hook is wrapped (not replaced) with a check of
	// the actual peer address.
	Dialer *net.Dialer

	// dial overrides the final connect step (tests only).
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// NewSafeDialContext returns a DialContext function that validates and pins
// DNS results at connect time. This is the Go equivalent of the TS SDK's
// createSafeLookup + undici Agent in provider-utils safe-node-fetch.ts:
//
//   - The hostname is resolved once, inside the dialer. Every returned address
//     is validated with ValidateDownloadAddress; if any address is disallowed
//     (or none are returned) the whole lookup is rejected. This covers DNS
//     aliases (CNAME to an internal name), mixed public/private answers and
//     single-address resolvers.
//   - The socket is opened to one of the validated IP literals, never to the
//     hostname, so DNS rebinding cannot introduce a second, unvalidated lookup.
//   - A net.Dialer Control hook re-checks the actual peer address as
//     defense in depth.
func NewSafeDialContext(opts SafeDialOptions) func(ctx context.Context, network, address string) (net.Conn, error) {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	base := opts.Dialer
	if base == nil {
		base = &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	}
	dialer := *base
	innerControl := base.Control
	dialer.Control = func(network, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if err := ValidateDownloadAddress(host, net.ParseIP(host)); err != nil {
			return err
		}
		if innerControl != nil {
			return innerControl(network, address, c)
		}
		return nil
	}
	dial := opts.dial
	if dial == nil {
		dial = dialer.DialContext
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			addrs, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addrs) == 0 {
				return nil, providererrors.NewDownloadError(host, 0, "", fmt.Sprintf("Hostname %s did not resolve to an address", host), nil)
			}
			ips = make([]net.IP, 0, len(addrs))
			for _, a := range addrs {
				ips = append(ips, a.IP)
			}
		}

		// Validate every address before connecting to any of them.
		for _, ip := range ips {
			if err := ValidateDownloadAddress(host, ip); err != nil {
				return nil, err
			}
		}

		var firstErr error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, firstErr
	}
}

// NewSafeTransport returns an *http.Transport whose connections are pinned to
// validated DNS results (see NewSafeDialContext). Environment proxies are
// disabled because a proxy would perform its own, unvalidated resolution.
func NewSafeTransport(opts SafeDialOptions) *http.Transport {
	// Same defaults as net/http's DefaultTransport, minus the environment proxy.
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           NewSafeDialContext(opts),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

var (
	defaultSafeTransportOnce sync.Once
	defaultSafeTransport     *http.Transport
)

// SafeTransport returns the shared DNS-pinning transport used for validated
// downloads by default.
func SafeTransport() http.RoundTripper {
	defaultSafeTransportOnce.Do(func() {
		defaultSafeTransport = NewSafeTransport(SafeDialOptions{})
	})
	return defaultSafeTransport
}
