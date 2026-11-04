package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// Hand ports of the gateway-language-model.test.ts assertions the replay
// harness does not evaluate (R87D_TSFIXTURE closure rule): the error object
// of a rejected call, values destructured from a call, the fetch mock's
// arguments and fetch rejections. Each test uses the TS test's response
// (the prepareJsonResponse / prepareStreamResponse defaults, or its error
// response) and its expected values.

var r87dprovrestPorted = map[string]string{}

// r87dprovrestNA are the assertions that cannot apply to Go.
var r87dprovrestNA = map[string]string{
	tsfixture.PortedKey("should extract text response", "content", "toEqual"):   "TS doGenerate returns the response's content as is (here the test helper's single object, not the V4 content array the gateway sends); types.GenerateResult.Content is a typed slice, and Go decodes the object into its one text part (TestR87dprovrestGatewayGenerateResultFields)",
	tsfixture.PortedKey("should extract usage information", "usage", "toEqual"): "TS doGenerate returns the response's usage object as is (here the test helper's flat snake-case counts, not the V4 usage the gateway sends); types.Usage cannot hold an arbitrary object. Go reads no counts from that shape, as the TS core does not (TestR87dprovrestGatewayGenerateResultFields)",
}

func r87dprovrestMark(goTest, test string, exprMatchers ...string) {
	for i := 0; i < len(exprMatchers); i += 2 {
		r87dprovrestPorted[tsfixture.PortedKey(test, exprMatchers[i], exprMatchers[i+1])] = goTest
	}
}

func init() {
	r87dprovrestMark("TestR87dprovrestGatewayNoAbortSignalWithoutOne", "should not pass abortSignal to fetch when not provided", "fetchCallArgs[1].signal", "toBeUndefined")
	r87dprovrestMark("TestR87dprovrestGatewayNoAbortSignalWithoutOne", "should not pass abortSignal to fetch when not provided for streaming", "fetchCallArgs[1].signal", "toBeUndefined")
	for _, tc := range r87dprovrestErrorCases {
		for _, a := range tc.marks {
			r87dprovrestMark("TestR87dprovrestGatewayCallErrors", tc.test, a[0], a[1])
		}
	}
	r87dprovrestMark("TestR87dprovrestGatewayVariousErrorTypes", "should handle various error types with proper conversion",
		"GatewayInvalidRequestError.isInstance(error)", "toBe", "invalidError.message", "toBe", "invalidError.statusCode", "toBe", "invalidError.type", "toBe",
		"GatewayModelNotFoundError.isInstance(error)", "toBe", "modelError.message", "toBe", "modelError.statusCode", "toBe", "modelError.type", "toBe", "modelError.modelId", "toBe",
		"GatewayInternalServerError.isInstance(error)", "toBe", "serverError.message", "toBe", "serverError.statusCode", "toBe", "serverError.type", "toBe",
		"APICallError.isInstance(serverError.cause)", "toBe", "serverError.cause.message", "toContain")
	r87dprovrestMark("TestR87dprovrestGatewayFetchRejections", "should not double-wrap existing Gateway errors", "error.message", "toBe")
	r87dprovrestMark("TestR87dprovrestGatewayFetchRejections", "should handle network errors gracefully", "GatewayResponseError.isInstance(error)", "toBe", "responseError.message", "toBe")
	r87dprovrestMark("TestR87dprovrestGatewayFetchRejections", "should handle network errors gracefully in streaming", "GatewayResponseError.isInstance(error)", "toBe", "responseError.message", "toBe")
	tsfixture.RegisterPorted(r87dprovrestPorted)
	tsfixture.RegisterNotApplicable(r87dprovrestNA)
}

// TestMain fails an unfiltered run whose hand-port or n/a marks are stale.
func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m, r87dprovrestPorted, r87dprovrestNA)) }

// TS prepareJsonResponse({ content, usage }) and prepareStreamResponse({ content }).
func r87dprovrestJSONBody(text string, usage string) string {
	if usage == "" {
		usage = `{"prompt_tokens":4,"completion_tokens":30}`
	}
	return `{"id":"test-id","created":1711115037,"model":"test-model","content":{"type":"text","text":"` + text + `"},"finish_reason":"stop","usage":` + usage + `}`
}

func r87dprovrestStreamBody(texts ...string) string {
	var b strings.Builder
	for _, s := range texts {
		b.WriteString(`data: {"type":"text-delta","textDelta":"` + s + `"}` + "\n\n")
	}
	b.WriteString(`data: {"type":"finish","finishReason":"stop","usage":{"prompt_tokens":10,"completion_tokens":20}}` + "\n\n")
	return b.String()
}

type r87dprovrestServer struct {
	mu      sync.Mutex
	headers []http.Header
}

func r87dprovrestServe(t *testing.T, status int, ctype, body string) (*r87dprovrestServer, string) {
	t.Helper()
	rec := &r87dprovrestServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.headers = append(rec.headers, r.Header.Clone())
		rec.mu.Unlock()
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func r87dprovrestModel(t *testing.T, base string, client *http.Client) provider.LanguageModel {
	t.Helper()
	p, err := New(Config{APIKey: "test-token", BaseURL: base, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TS 136-192: const { content } / { usage } / { warnings } = await doGenerate(...).
func TestR87dprovrestGatewayGenerateResultFields(t *testing.T) {
	// should extract text response: TS returns the gateway's content object
	// as is ({ type: 'text', text: 'Hello, World!' }); Go decodes it into
	// its one text part.
	_, base := r87dprovrestServe(t, 200, "application/json", r87dprovrestJSONBody("Hello, World!", ""))
	res, err := r87dprovrestModel(t, base, nil).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	if err != nil {
		t.Fatal(err)
	}
	if txt, ok := r87dprovrestOne(res.Content).(types.TextContent); !ok || txt.Text != "Hello, World!" || len(txt.ProviderMetadata) != 0 {
		t.Errorf("content = %#v, TS { type: 'text', text: 'Hello, World!' }", res.Content)
	}

	// should extract usage information (n/a in Go): TS doGenerate returns
	// the response's usage object as is, here the flat snake-case counts
	// { prompt_tokens: 10, completion_tokens: 20 } of the test helper (not
	// the V4 usage the gateway sends). types.Usage cannot hold an arbitrary
	// object; Go reads no counts from that shape, as the TS core does not
	// (asLanguageModelUsage reads inputTokens.total / outputTokens.total).
	_, base = r87dprovrestServe(t, 200, "application/json", r87dprovrestJSONBody("Test", `{"prompt_tokens":10,"completion_tokens":20}`))
	res, err = r87dprovrestModel(t, base, nil).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil {
		t.Errorf("usage = %+v, want no counts from the flat snake-case shape", res.Usage)
	}
}

type r87dprovrestTransport struct {
	fn func(*http.Request) (*http.Response, error)
}

func (rt r87dprovrestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return rt.fn(r) }

// TS 231-245 and 853-867: without an abortSignal no signal reaches fetch. Go
// has no signal; the equivalent is a request context that never cancels.
func TestR87dprovrestGatewayNoAbortSignalWithoutOne(t *testing.T) {
	for _, stream := range []bool{false, true} {
		ctype, body := "application/json", r87dprovrestJSONBody("Test response", "")
		if stream {
			ctype, body = "text/event-stream", r87dprovrestStreamBody("Test content")
		}
		_, base := r87dprovrestServe(t, 200, ctype, body)
		var done []<-chan struct{}
		var deadline []bool
		client := &http.Client{Transport: r87dprovrestTransport{fn: func(r *http.Request) (*http.Response, error) {
			done = append(done, r.Context().Done())
			_, has := r.Context().Deadline()
			deadline = append(deadline, has)
			return http.DefaultTransport.RoundTrip(r)
		}}}
		if _, err := r87dprovrestCall(r87dprovrestModel(t, base, client), stream, &provider.GenerateOptions{Prompt: r87dprovrestPrompt}); err != nil {
			t.Fatalf("stream=%v: %v", stream, err)
		}
		if len(done) == 0 {
			t.Fatalf("stream=%v: fetch not called", stream)
		}
		if deadline[0] {
			t.Errorf("stream=%v: request context has a deadline; TS passes no signal", stream)
		}
		if done[0] != nil {
			t.Errorf("stream=%v: request context can be cancelled; TS passes no signal", stream)
		}
	}
}

type r87dprovrestErrorCase struct {
	test   string
	stream bool
	status int
	body   string
	check  func(t *testing.T, err error)
	marks  [][2]string
}

func r87dprovrestAs[T error](t *testing.T, err error) T {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Fatalf("error %T (%v) is not a %T", err, err, target)
	}
	return target
}

func r87dprovrestFields(t *testing.T, ge gatewayerrors.GatewayError, message string, contains []string, status int, typ string) {
	t.Helper()
	if message != "" && ge.Error() != message {
		t.Errorf("message = %q, TS %q", ge.Error(), message)
	}
	for _, s := range contains {
		if !strings.Contains(ge.Error(), s) {
			t.Errorf("message %q does not contain %q", ge.Error(), s)
		}
	}
	if ge.GetStatusCode() != status {
		t.Errorf("statusCode = %d, TS %d", ge.GetStatusCode(), status)
	}
	if ge.GetType() != typ {
		t.Errorf("type = %q, TS %q", ge.GetType(), typ)
	}
}

func r87dprovrestErrBody(message, typ string) string {
	return `{"error":{"message":"` + message + `","type":"` + typ + `"}}`
}

var r87dprovrestErrorCases = []r87dprovrestErrorCase{
	{"should convert API call errors to Gateway errors", false, 401, r87dprovrestErrBody("Invalid API key provided", "authentication_error"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayAuthenticationError](t, err)
		r87dprovrestFields(t, e, "", []string{"Invalid API key", "vercel.com/d?to="}, 401, "authentication_error")
	}, [][2]string{{"GatewayAuthenticationError.isInstance(error)", "toBe"}, {"authError.message", "toContain"}, {"authError.statusCode", "toBe"}, {"authError.type", "toBe"}}},
	{"should handle malformed error responses", false, 500, "Not JSON", func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayResponseError](t, err)
		r87dprovrestFields(t, e, "", nil, 500, "response_error")
	}, [][2]string{{"GatewayResponseError.isInstance(error)", "toBe"}, {"responseError.statusCode", "toBe"}, {"responseError.type", "toBe"}}},
	{"should handle rate limit errors", false, 429, r87dprovrestErrBody("Rate limit exceeded. Try again later.", "rate_limit_exceeded"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayRateLimitError](t, err)
		r87dprovrestFields(t, e, "Rate limit exceeded. Try again later.", nil, 429, "rate_limit_exceeded")
	}, [][2]string{{"GatewayRateLimitError.isInstance(error)", "toBe"}, {"rateLimitError.message", "toBe"}, {"rateLimitError.statusCode", "toBe"}, {"rateLimitError.type", "toBe"}}},
	{"should handle invalid request errors", false, 400, r87dprovrestErrBody("Invalid prompt format", "invalid_request_error"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayInvalidRequestError](t, err)
		r87dprovrestFields(t, e, "Invalid prompt format", nil, 400, "invalid_request_error")
	}, [][2]string{{"GatewayInvalidRequestError.isInstance(error)", "toBe"}, {"invalidError.message", "toBe"}, {"invalidError.statusCode", "toBe"}, {"invalidError.type", "toBe"}}},
	{"should include actual response body when APICallError has no data", false, 404, `{"ferror":{"message":"Model not found","type":"model_not_found"}}`, func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayResponseError](t, err)
		r87dprovrestEqual(t, "gatewayError.response", map[string]interface{}{"ferror": map[string]interface{}{"message": "Model not found", "type": "model_not_found"}}, e.Response)
		if e.ValidationError == nil {
			t.Error("gatewayError.validationError is undefined")
		}
	}, [][2]string{{"GatewayResponseError.isInstance(error)", "toBe"}, {"gatewayError.validationError", "toBeDefined"}}},
	{"should use raw response body when JSON parsing fails", false, 500, "invalid json response", func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayResponseError](t, err)
		if e.Response != "invalid json response" {
			t.Errorf("gatewayError.response = %#v, TS 'invalid json response'", e.Response)
		}
		if e.ValidationError == nil {
			t.Error("gatewayError.validationError is undefined")
		}
	}, [][2]string{{"GatewayResponseError.isInstance(error)", "toBe"}, {"gatewayError.validationError", "toBeDefined"}}},
	{"should convert API call errors to Gateway errors in streaming", true, 429, r87dprovrestErrBody("Rate limit exceeded", "rate_limit_exceeded"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayRateLimitError](t, err)
		r87dprovrestFields(t, e, "Rate limit exceeded", nil, 429, "rate_limit_exceeded")
	}, [][2]string{{"GatewayRateLimitError.isInstance(error)", "toBe"}, {"rateLimitError.message", "toBe"}, {"rateLimitError.statusCode", "toBe"}, {"rateLimitError.type", "toBe"}}},
	{"should handle authentication errors in streaming", true, 401, r87dprovrestErrBody("Authentication failed for streaming", "authentication_error"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayAuthenticationError](t, err)
		r87dprovrestFields(t, e, "", []string{"Invalid API key", "vercel.com/d?to="}, 401, "authentication_error")
	}, [][2]string{{"GatewayAuthenticationError.isInstance(error)", "toBe"}, {"authError.message", "toContain"}, {"authError.statusCode", "toBe"}, {"authError.type", "toBe"}}},
	{"should handle invalid request errors in streaming", true, 400, r87dprovrestErrBody("Invalid streaming request", "invalid_request_error"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayInvalidRequestError](t, err)
		r87dprovrestFields(t, e, "Invalid streaming request", nil, 400, "invalid_request_error")
	}, [][2]string{{"GatewayInvalidRequestError.isInstance(error)", "toBe"}, {"invalidError.message", "toBe"}, {"invalidError.statusCode", "toBe"}, {"invalidError.type", "toBe"}}},
	{"should handle malformed error responses in streaming", true, 500, "Invalid JSON for streaming", func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayResponseError](t, err)
		r87dprovrestFields(t, e, "", nil, 500, "response_error")
	}, [][2]string{{"GatewayResponseError.isInstance(error)", "toBe"}, {"responseError.statusCode", "toBe"}, {"responseError.type", "toBe"}}},
	{"should preserve error cause chain", false, 401, r87dprovrestErrBody("Token expired", "authentication_error"), func(t *testing.T, err error) {
		e := r87dprovrestAs[*gatewayerrors.GatewayAuthenticationError](t, err)
		if errors.Unwrap(e) == nil {
			t.Error("authError.cause is undefined")
		}
	}, [][2]string{{"GatewayAuthenticationError.isInstance(error)", "toBe"}, {"authError.cause", "toBeDefined"}}},
}

// The error object of each rejected TS call (TS 263-361, 602-658, 892-990, 1221-1245).
func TestR87dprovrestGatewayCallErrors(t *testing.T) {
	for _, tc := range r87dprovrestErrorCases {
		t.Run(tc.test, func(t *testing.T) {
			_, base := r87dprovrestServe(t, tc.status, "application/json", tc.body)
			_, err := r87dprovrestCall(r87dprovrestModel(t, base, nil), tc.stream, &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
			if err == nil {
				t.Fatal("the call succeeded; TS rejects")
			}
			tc.check(t, err)
		})
	}
}

// TS 523-599: one model, three error responses in turn.
func TestR87dprovrestGatewayVariousErrorTypes(t *testing.T) {
	var mu sync.Mutex
	status, body := 0, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		s, b := status, body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s)
		_, _ = io.WriteString(w, b)
	}))
	defer srv.Close()
	m := r87dprovrestModel(t, srv.URL, nil)
	set := func(s int, b string) { mu.Lock(); status, body = s, b; mu.Unlock() }

	set(400, r87dprovrestErrBody("Invalid request format", "invalid_request_error"))
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	r87dprovrestFields(t, r87dprovrestAs[*gatewayerrors.GatewayInvalidRequestError](t, err), "Invalid request format", nil, 400, "invalid_request_error")

	set(404, `{"error":{"message":"Model xyz not found","type":"model_not_found","param":{"modelId":"xyz"}}}`)
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	mnf := r87dprovrestAs[*gatewayerrors.GatewayModelNotFoundError](t, err)
	r87dprovrestFields(t, mnf, "Model xyz not found", nil, 404, "model_not_found")
	if mnf.ModelID != "xyz" {
		t.Errorf("modelError.modelId = %q, TS xyz", mnf.ModelID)
	}

	set(500, r87dprovrestErrBody("Database connection failed", "internal_server_error"))
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	se := r87dprovrestAs[*gatewayerrors.GatewayInternalServerError](t, err)
	r87dprovrestFields(t, se, "Database connection failed", nil, 500, "internal_server_error")
	// APICallError.isInstance(serverError.cause): Go's APICallError is
	// providererrors.ProviderError.
	var apiErr *providererrors.ProviderError
	if cause := errors.Unwrap(se); cause == nil || !errors.As(cause, &apiErr) {
		t.Fatalf("serverError.cause = %T, TS an APICallError", errors.Unwrap(se))
	}
	if !strings.Contains(apiErr.Message, "Database connection failed") {
		t.Errorf("serverError.cause.message = %q, TS contains 'Database connection failed'", apiErr.Message)
	}
}

// TS 1158-1219: fetch rejects (mockRejectedValue).
func TestR87dprovrestGatewayFetchRejections(t *testing.T) {
	existing := gatewayerrors.NewGatewayAuthenticationError("Already a Gateway error", 401, nil, "")
	m := r87dprovrestModel(t, "http://gateway.invalid", &http.Client{Transport: r87dprovrestTransport{fn: func(*http.Request) (*http.Response, error) { return nil, existing }}})
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
	if got := r87dprovrestAs[*gatewayerrors.GatewayAuthenticationError](t, err); got != existing {
		t.Errorf("error = %#v, TS the same GatewayAuthenticationError instance", got)
	}
	if err == nil || err.Error() != "Already a Gateway error" {
		t.Errorf("error.message = %v, TS 'Already a Gateway error'", err)
	}

	for _, stream := range []bool{false, true} {
		networkError := errors.New("Network connection failed")
		m := r87dprovrestModel(t, "http://gateway.invalid", &http.Client{Transport: r87dprovrestTransport{fn: func(*http.Request) (*http.Response, error) { return nil, networkError }}})
		_, err := r87dprovrestCall(m, stream, &provider.GenerateOptions{Prompt: r87dprovrestPrompt})
		re := r87dprovrestAs[*gatewayerrors.GatewayResponseError](t, err)
		if want := "Invalid error response format: Gateway request failed: Network connection failed"; re.Error() != want {
			t.Errorf("stream=%v: responseError.message = %q, TS %q", stream, re.Error(), want)
		}
		if !errors.Is(re, networkError) {
			t.Errorf("stream=%v: responseError.cause = %v, TS the network error", stream, errors.Unwrap(re))
		}
	}
}

func r87dprovrestOne(parts []types.ContentPart) types.ContentPart {
	if len(parts) != 1 {
		return nil
	}
	return parts[0]
}
