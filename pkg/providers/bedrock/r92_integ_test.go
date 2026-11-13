package bedrock

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r92integBedrockDialTimeout struct{}

func (r92integBedrockDialTimeout) Error() string        { return "i/o timeout" }
func (r92integBedrockDialTimeout) Timeout() bool        { return true }
func (r92integBedrockDialTimeout) Temporary() bool      { return true }
func (r92integBedrockDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

// TestR92integProviderErrorFromTransportDialTimeout (cr-r92-carry-to-provider-owners):
// the shared bedrock transport mapper classifies with the request's context
// before anything else, so a dial timeout while ctx is live is the retryable
// "Cannot connect to API" error (TS handle-fetch-error.ts ETIMEDOUT /
// UND_ERR_CONNECT_TIMEOUT), while the caller's own cancel or deadline is
// returned unchanged.
func TestR92integProviderErrorFromTransportDialTimeout(t *testing.T) {
	dialErr := &url.Error{Op: "Post", URL: "https://bedrock-runtime.us-east-1.amazonaws.com/model/m/converse", Err: &net.OpError{Op: "dial", Net: "tcp", Err: r92integBedrockDialTimeout{}}}

	got := providerErrorFromTransport(context.Background(), dialErr)
	r, ok := got.(interface{ IsRetryable() bool })
	if got == nil || !strings.Contains(got.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("live ctx dial timeout = %T %v, want the retryable Cannot connect to API error", got, got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := &url.Error{Op: "Post", URL: dialErr.URL, Err: context.Canceled}
	if got := providerErrorFromTransport(ctx, canceled); got != error(canceled) {
		t.Fatalf("caller cancel = %v, want it unchanged", got)
	}

	dctx, dcancel := context.WithTimeout(context.Background(), 0)
	defer dcancel()
	<-dctx.Done()
	if got := providerErrorFromTransport(dctx, dialErr); got != error(dialErr) || !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("caller deadline = %v, want it unchanged", got)
	}
}

// TS amazon-bedrock-chat-language-model.ts 980 / 1055: the response
// timestamp is new Date(responseHeaders['date']) (Date.parse), so a date
// header in another form Date.parse reads (RFC 850, asctime) is kept.
func TestR92integDateHeaderIsJSDate(t *testing.T) {
	want := time.Date(1994, 11, 6, 8, 49, 37, 0, time.UTC)
	for _, h := range []string{"Sun, 06 Nov 1994 08:49:37 GMT", "Sunday, 06-Nov-94 08:49:37 GMT", "Sun Nov  6 08:49:37 1994 GMT"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Date", h)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hi"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
		}))
		m := NewLanguageModel(New(Config{APIKey: "k", Region: "us-east-1", BaseURL: srv.URL}), "anthropic.claude-3-haiku-20240307-v1:0")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}}})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(want) {
			t.Errorf("date %q: %+v, want %v", h, res.ResponseMetadata, want)
		}
	}
}
