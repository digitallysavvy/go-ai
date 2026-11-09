package toolwebfetch

import (
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

// TS fetch-web-resource.ts formatErrorCode reports the first uppercase
// `code` on the error's cause chain, as Node's fetch sets them (ENOTFOUND,
// EAI_AGAIN, UND_ERR_*, the OpenSSL certificate codes).
func TestR89toolsErrnoNames(t *testing.T) {
	tls := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tls.Close()
	_, tlsErr := (&http.Client{Timeout: 5 * time.Second}).Get(tls.URL)

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"dns not found", &net.DNSError{Err: "no such host", IsNotFound: true}, "ENOTFOUND"},
		{"dns temporary", &net.DNSError{Err: "server misbehaving", IsTemporary: true}, "EAI_AGAIN"},
		{"refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), "ECONNREFUSED"},
		{"broken pipe", fmt.Errorf("write: %w", syscall.EPIPE), "EPIPE"},
		{"aborted", fmt.Errorf("read: %w", syscall.ECONNABORTED), "ECONNABORTED"},
		{"self-signed leaf", tlsErr, "DEPTH_ZERO_SELF_SIGNED_CERT"},
		{"hostname", x509.HostnameError{Host: "example.com", Certificate: &x509.Certificate{}}, "ERR_TLS_CERT_ALTNAME_INVALID"},
		{"expired", x509.CertificateInvalidError{Reason: x509.Expired}, "CERT_HAS_EXPIRED"},
		{"connect timeout", &net.OpError{Op: "dial", Err: r89toolsTimeoutErr{}}, "UND_ERR_CONNECT_TIMEOUT"},
		{"body cut short", fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), "UND_ERR_SOCKET"},
		{"other", fmt.Errorf("boom"), ""},
	} {
		if got := errnoName(tc.err); got != tc.want {
			t.Errorf("%s: errnoName(%v) = %q, want %q", tc.name, tc.err, got, tc.want)
		}
	}
}

type r89toolsTimeoutErr struct{}

func (r89toolsTimeoutErr) Error() string   { return "i/o timeout" }
func (r89toolsTimeoutErr) Timeout() bool   { return true }
func (r89toolsTimeoutErr) Temporary() bool { return true }
