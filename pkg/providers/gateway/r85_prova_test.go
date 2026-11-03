package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// gateway-language-model.test.ts "cancellation" (TS 9ca8bcf731): a caller
// cancellation is returned unchanged, not as a retryable 500; a transport
// timeout while the caller's ctx is live stays a GatewayTimeoutError (round
// 85 ruling 4).
func TestR85provaGatewayCancellationUnchanged(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)

	p, err := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(p, "openai/gpt-4o")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err = m.DoGenerate(ctx, opts)
	if !errors.Is(err, context.Canceled) || gatewayerrors.IsGatewayError(err) {
		t.Fatalf("generate cancel: err = %T %v", err, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err = m.DoStream(ctx, opts)
	if !errors.Is(err, context.Canceled) || gatewayerrors.IsGatewayError(err) {
		t.Fatalf("stream cancel: err = %T %v", err, err)
	}

	dctx, dcancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer dcancel()
	_, err = m.DoGenerate(dctx, opts)
	if !errors.Is(err, context.DeadlineExceeded) || gatewayerrors.IsGatewayError(err) {
		t.Fatalf("ctx deadline: err = %T %v", err, err)
	}

	tp, err := New(Config{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewLanguageModel(tp, "openai/gpt-4o").DoGenerate(context.Background(), opts)
	if !gatewayerrors.IsGatewayTimeoutError(err) {
		t.Fatalf("transport timeout: err = %T %v", err, err)
	}
}
