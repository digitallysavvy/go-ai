package bedrock

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r91provaDeadURL is a loopback URL nothing listens on.
func r91provaDeadURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

func r91provaCheckConnectError(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", what)
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("%s: err = %T %v, want a *ProviderError", what, err, err)
	}
	if !pe.IsRetryable() {
		t.Fatalf("%s: not retryable", what)
	}
	if !strings.Contains(err.Error(), "Cannot connect to API: ") {
		t.Fatalf("%s: err = %v, want Cannot connect to API", what, err)
	}
	if strings.Contains(err.Error(), "amazon-bedrock request failed") {
		t.Fatalf("%s: err = %v", what, err)
	}
}

// prova-02: a network failure of the raw Bedrock requests is the TS
// handleFetchError result (provider-utils handle-fetch-error.ts:42-100): a
// retryable APICallError "Cannot connect to API: <cause>".
func TestR91provaTransportErrors(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1", BaseURL: r91provaDeadURL(t)})
	ctx := context.Background()
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lm.DoGenerate(ctx, opts)
	r91provaCheckConnectError(t, "DoGenerate", err)
	_, err = lm.DoStream(ctx, opts)
	r91provaCheckConnectError(t, "DoStream", err)

	em, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = em.DoEmbed(ctx, "hi", nil)
	r91provaCheckConnectError(t, "DoEmbed", err)

	im, err := p.ImageModel("amazon.nova-canvas-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = im.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"})
	r91provaCheckConnectError(t, "image DoGenerate", err)

	rm, err := p.RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = rm.DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	r91provaCheckConnectError(t, "DoRerank", err)
}

// prova-02: a cancelled request stays the context error (TS abort errors
// are returned unchanged).
func TestR91provaTransportCanceled(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1", BaseURL: r91provaDeadURL(t)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var pe *providererrors.ProviderError
	if errors.As(err, &pe) {
		t.Fatalf("err = %v, want the context error, not a ProviderError", err)
	}
}
