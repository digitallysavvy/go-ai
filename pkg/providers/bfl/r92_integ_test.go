package bfl

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

type r92integBFLDialTimeout struct{}

func (r92integBFLDialTimeout) Error() string        { return "i/o timeout" }
func (r92integBFLDialTimeout) Timeout() bool        { return true }
func (r92integBFLDialTimeout) Temporary() bool      { return true }
func (r92integBFLDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

// TestR92integPollAndDownloadNetworkFailureIsConnectError: TS polls and
// downloads with getFromApi, whose handleFetchError turns a network failure
// (a dial timeout or a refused connection) into the retryable "Cannot
// connect to API" APICallError. Go returned the raw *url.Error.
func TestR92integPollAndDownloadNetworkFailureIsConnectError(t *testing.T) {
	old := downloadTransport
	downloadTransport = func() http.RoundTripper {
		return &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: r92integBFLDialTimeout{}}
		}}
	}
	t.Cleanup(func() { downloadTransport = old })

	im, err := New(Config{APIKey: "k"}).ImageModel("flux-pro-1.1")
	if err != nil {
		t.Fatal(err)
	}
	m := im.(*ImageModel)
	check := func(name string, err error) {
		t.Helper()
		r, ok := err.(interface{ IsRetryable() bool })
		if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
			t.Errorf("%s: %T %v, want the retryable Cannot connect to API error", name, err, err)
		}
	}
	_, err = m.getPollBody(context.Background(), "https://poll.example.com/get_result?id=1", nil)
	check("poll", err)
	_, _, err = m.downloadImage(context.Background(), "https://delivery.example.com/x.png", nil)
	check("download", err)
}
