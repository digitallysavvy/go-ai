package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS getGatewayAuthToken throws the GatewayAuthenticationError from
// headers() before fetch, and asGatewayError returns a GatewayError as is.
// Go resolves the credential inside its HTTP transport, so the error used to
// come back wrapped: `Cannot connect to API: Post "...": AI Gateway
// authentication failed: ...` (a *http.connectError).
func TestR87dprovrestGatewayMissingCredentialsUnwrapped(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN_FILE", "")
	p, err := New(Config{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, err error) {
		t.Helper()
		ae, ok := err.(*gatewayerrors.GatewayAuthenticationError)
		if !ok {
			t.Fatalf("%s: error %T (%v), TS a GatewayAuthenticationError", what, err, err)
		}
		if !strings.HasPrefix(ae.Error(), "AI Gateway authentication failed: No authentication provided.") {
			t.Errorf("%s: message %q", what, ae.Error())
		}
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	check("doGenerate", err)
	_, err = m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	check("doStream", err)
	_, err = p.GetAvailableModels(context.Background())
	check("getAvailableModels", err)
}

// A failed connection: TS handleFetchError builds the APICallError "Cannot
// connect to API: ..." (isRetryable, no status, no body), and asGatewayError
// gives createGatewayErrorFromResponse({ response: {}, statusCode: 500,
// defaultMessage: 'Gateway request failed', isRetryable: true }).
func TestR87dprovrestGatewayConnectFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	p, err := New(Config{APIKey: "k", BaseURL: "http://" + addr})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		_, err := r87dprovrestCall(m, stream, &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
		re, ok := err.(*gatewayerrors.GatewayResponseError)
		if !ok {
			t.Fatalf("stream=%v: error %T (%v)", stream, err, err)
		}
		if re.Error() != "Invalid error response format: Gateway request failed" || re.GetStatusCode() != 500 || !re.IsRetryable() {
			t.Errorf("stream=%v: %q status %d retryable %v", stream, re.Error(), re.GetStatusCode(), re.IsRetryable())
		}
		r87dprovrestEqual(t, "response", map[string]interface{}{}, re.Response)
		var ue *url.Error
		if !errors.As(re, &ue) {
			t.Errorf("stream=%v: cause %v is not the transport error", stream, errors.Unwrap(re))
		}
	}
}

var r87dprovrestPrompt = types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}}}

func r87dprovrestCall(m provider.LanguageModel, stream bool, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if !stream {
		return m.DoGenerate(context.Background(), opts)
	}
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	for {
		if _, err := s.Next(); err != nil {
			_ = s.Close()
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, err
		}
	}
}

func r87dprovrestEqual(t *testing.T, what string, want, got interface{}) {
	t.Helper()
	if d := tsfixture.DiffValues(want, got); d != "" {
		t.Errorf("%s: %s", what, d)
	}
}
