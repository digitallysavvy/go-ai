package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// gateway-provider.test.ts async apiKey (TS 7716e189ee): the key func is
// resolved per request and wins over the environment; an empty key or a
// failing func is a GatewayAuthenticationError.
func TestR85provaGatewayAPIKeyFunc(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization")+"|"+r.Header.Get("ai-gateway-auth-method"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"finish_reason":"stop","usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	t.Setenv("AI_GATEWAY_API_KEY", "env-key")
	key := "fn-key"
	calls := 0
	p, err := New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) { calls++; return key, nil }})
	if err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(p, "openai/gpt-4o")
	_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if len(auth) != 1 || auth[0] != "Bearer fn-key|api-key" || calls != 1 {
		t.Fatalf("auth = %q, calls = %d", auth, calls)
	}
	key = " "
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var authErr *gatewayerrors.GatewayAuthenticationError
	var loadErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &authErr) || !errors.As(err, &loadErr) || loadErr.Message != "AI Gateway API key must be a non-empty string." {
		t.Fatalf("blank key: %T %v", err, err)
	}
}
