package anthropicaws

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// stubRoundTripper returns a canned 200 response without touching the
// request body, so sigV4Transport's own concurrency is what's under test.
type stubRoundTripper struct{}

func (stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// TestSigV4Transport_LastCredentialsRace is a regression test for a data
// race: sigV4Transport.lastCredentials was a plain struct field, written on
// every RoundTrip call and read by Provider.LastSigningCredentials() with no
// synchronization. Go's http.Client/http.Transport are documented safe for
// concurrent use, so two in-flight requests through the same *http.Client
// race on this field under `go test -race`. Guarding it with a mutex fixes
// the race.
func TestSigV4Transport_LastCredentialsRace(t *testing.T) {
	tr := &sigV4Transport{
		base:            stubRoundTripper{},
		region:          "us-west-2",
		accessKeyID:     "akid",
		secretAccessKey: "secret",
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n + 1)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := tr.RoundTrip(req); err != nil {
				t.Error(err)
			}
		}()
	}
	go func() {
		defer wg.Done()
		_ = tr.getLastCredentials()
	}()

	wg.Wait()

	got := tr.getLastCredentials()
	if got.AccessKeyID != "akid" {
		t.Fatalf("lastCredentials.AccessKeyID = %q, want %q", got.AccessKeyID, "akid")
	}
}
