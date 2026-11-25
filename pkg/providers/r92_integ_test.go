package providers_test

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// r92integDialTimeout is a dialer timeout (an OS ETIMEDOUT / connect
// timeout): it reads as net's "i/o timeout", which also matches
// context.DeadlineExceeded, while the request's context is still live.
type r92integDialTimeout struct{}

func (r92integDialTimeout) Error() string        { return "i/o timeout" }
func (r92integDialTimeout) Timeout() bool        { return true }
func (r92integDialTimeout) Temporary() bool      { return true }
func (r92integDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

func r92integDialTimeoutClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: r92integDialTimeout{}}
	}}}
}

func r92integIsConnectError(err error) bool {
	r, ok := err.(interface{ IsRetryable() bool })
	if !ok {
		var pe interface{ IsRetryable() bool }
		ok = errors.As(err, &pe)
		r = pe
	}
	return err != nil && strings.Contains(err.Error(), "Cannot connect to API: ") && ok && r.IsRetryable()
}

// TestR92integRawDoDialTimeoutIsConnectError (cr-r92-carry-to-provider-owners,
// cr-r92-provg-to-integrator): the provider sites that call a raw
// http.Client classify a dial timeout with the request's context, so a
// transport timeout while the caller's context is live is TS's retryable
// "Cannot connect to API" error (handle-fetch-error.ts
// RETRYABLE_NETWORK_ERROR_CODES ETIMEDOUT / UND_ERR_CONNECT_TIMEOUT), not an
// abort returned as is.
func TestR92integRawDoDialTimeoutIsConnectError(t *testing.T) {
	hc := r92integDialTimeoutClient()
	ctx := context.Background()
	cases := map[string]func() error{
		// The Google files upload and realtime client secret sites call TS
		// fetchFn directly (no handleFetchError), so their transport failure
		// propagates as is (cr-r93-provg-to-carry).
		"azure speech": func() error {
			p, err := azure.New(azure.Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: "http://127.0.0.1:1", HTTPClient: hc})
			if err != nil {
				return err
			}
			m, err := p.SpeechModel("MAI-Voice-2")
			if err != nil {
				return err
			}
			_, err = m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi", Voice: "en-US-Jasper"})
			return err
		},
		"azure speech transcription": func() error {
			p, err := azure.New(azure.Config{APIKey: "k", ResourceName: "r", SpeechBaseURL: "http://127.0.0.1:1", HTTPClient: hc})
			if err != nil {
				return err
			}
			m, err := p.TranscriptionModel("MAI-Transcribe-2")
			if err != nil {
				return err
			}
			_, err = m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte{1, 2, 3}, MimeType: "audio/wav"})
			return err
		},
		"azure MAI image": func() error {
			p, err := azure.New(azure.Config{APIKey: "k", ResourceName: "r", HTTPClient: hc})
			if err != nil {
				return err
			}
			m, err := p.ImageModel("MAI-Image-2.6")
			if err != nil {
				return err
			}
			_, err = m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"})
			return err
		},
		"openai model catalog": func() error {
			_, err := openai.ModelCatalogRefresher{APIKey: "k", HTTPClient: hc, Endpoint: "http://127.0.0.1:1/v1/models"}.Fetch(ctx)
			return err
		},
	}
	for name, call := range cases {
		if err := call(); !r92integIsConnectError(err) {
			t.Errorf("%s: dial timeout = %T %v, want the retryable Cannot connect to API error", name, err, err)
		}
	}
}

// TestR92integNoRawTransportErrorWithoutContext is the class guard for the
// sweep above: every provider site that classifies a raw http.Client Do
// failure passes the request's context (TransportErrorCtx). TransportError
// without the context cannot tell a dial timeout from the caller's deadline.
func TestR92integNoRawTransportErrorWithoutContext(t *testing.T) {
	root := filepath.Join("..")
	re := regexp.MustCompile(`internalhttp\.TransportError\(`)
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if re.MatchString(line) {
				hits = append(hits, path+":"+itoaR92integ(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("raw TransportError sites without the request context (use TransportErrorCtx(req.Context(), err)):\n%s", strings.Join(hits, "\n"))
	}
}

func itoaR92integ(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
