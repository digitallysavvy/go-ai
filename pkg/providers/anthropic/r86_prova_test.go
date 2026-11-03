package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r86provaBOMErrorBody is an Anthropic error body behind a UTF-8 BOM.
const r86provaBOMErrorBody = "\xEF\xBB\xBF{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"boom\"}}"

// TS createJsonErrorResponseHandler parses response.text(), which drops a
// leading BOM: a streamed request's error body (handed over undecoded) gives
// the API error message, like the generate path.
func TestR86provaAnthropicErrorBodyBOM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(r86provaBOMErrorBody))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	_, genErr := m.DoGenerate(context.Background(), opts)
	_, streamErr := m.DoStream(context.Background(), opts)
	for name, err := range map[string]error{"generate": genErr, "stream": streamErr} {
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "boom" || pe.ErrorCode != "invalid_request_error" {
			t.Fatalf("%s: %#v", name, err)
		}
		if pe.ResponseBody != r86provaBOMErrorBody[3:] {
			t.Fatalf("%s: ResponseBody = %q", name, pe.ResponseBody)
		}
	}
}

type r86provaCtxKey struct{}

// A batch results download resolves the AuthTokenFunc once, with the
// caller's ctx (it was called a second time with context.Background()).
func TestR86provaBatchResultsAuthTokenFuncOncePerRequest(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var auth []string
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srv.URL + `/results"}`))
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"custom_id":"a","result":{"type":"succeeded","message":{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-opus-5-5","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}}}`))
	})
	var calls int
	var withoutCtx int
	p := New(Config{BaseURL: srv.URL, AuthTokenFunc: func(ctx context.Context) (string, error) {
		calls++
		if ctx.Value(r86provaCtxKey{}) == nil {
			withoutCtx++
		}
		return "tok", nil
	}})
	ctx := context.WithValue(t.Context(), r86provaCtxKey{}, true)
	stream, err := p.ExperimentalBatch().DoGetBatchResults(ctx, provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || withoutCtx != 0 || len(auth) != 2 || auth[1] != "Bearer tok" {
		t.Fatalf("calls = %d (without the caller ctx: %d), Authorization = %q", calls, withoutCtx, auth)
	}
}

// Ports of the TS anthropic-provider.test.ts "authToken option" cases the
// wire replay skips (the recording cannot carry a token function).
func TestR86provaAuthTokenFuncTSCases(t *testing.T) {
	var requests []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Header.Clone())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-3-haiku-20240307","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	opts := func() *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	}

	// "propagates token supplier errors without sending an API request"
	tokenErr := errors.New("Token exchange failed")
	m, _ := New(Config{BaseURL: srv.URL, AuthTokenFunc: func(context.Context) (string, error) { return "", tokenErr }}).LanguageModel("claude-3-haiku-20240307")
	if _, err := m.DoGenerate(context.Background(), opts()); !errors.Is(err, tokenErr) {
		t.Fatalf("supplier error: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("a request was sent: %d", len(requests))
	}

	// "merges the OAuth beta with provider, request, and feature betas"
	m, _ = New(Config{
		BaseURL:       srv.URL,
		AuthTokenFunc: func(context.Context) (string, error) { return "test-auth-token", nil },
		Headers:       map[string]string{"anthropic-beta": "provider-beta,oauth-2025-04-20"},
	}).LanguageModel("claude-3-haiku-20240307")
	o := opts()
	o.Headers = map[string]string{"anthropic-beta": "request-beta,oauth-2025-04-20"}
	o.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"feature-beta"}}}
	if _, err := m.DoGenerate(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	betas := strings.Split(requests[0].Get("anthropic-beta"), ",")
	sort.Strings(betas)
	if strings.Join(betas, ",") != "feature-beta,oauth-2025-04-20,provider-beta,request-beta" {
		t.Fatalf("anthropic-beta = %q", requests[0].Get("anthropic-beta"))
	}
	if requests[0].Get("Authorization") != "Bearer test-auth-token" || requests[0].Get("x-api-key") != "" {
		t.Fatalf("auth headers = %v", requests[0])
	}
}
