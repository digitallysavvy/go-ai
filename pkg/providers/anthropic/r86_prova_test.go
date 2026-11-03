package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
