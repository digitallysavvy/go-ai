package cerebras

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS cerebras-chat-language-model.ts isStructuredOutputWithToolCallsFinishReason
// compares finishReason.raw with 'tool_calls'; a legacy `function_call`
// finish (also unified tool-calls) keeps its tool calls and finish reason.
func TestR89toolsCerebrasStructuredOutputUsesRawFinishReason(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		wantFinish types.FinishReason
		wantCalls  int
	}{
		{"tool_calls", types.FinishReasonStop, 0},
		{"function_call", types.FinishReasonToolCalls, 1},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			body := `{"id":"c","model":"gpt-oss-120b","choices":[{"message":{"role":"assistant","content":"{\"a\":1}","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"` + tc.raw + `"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}%s}`
			srv, _ := r88toolsServer(t, r89toolsJSONBody(body))
			opts := r88toolsOpts(nil)
			opts.ResponseFormat = &provider.ResponseFormat{Type: "json"}
			result, err := r88toolsModel(t, srv, nil).DoGenerate(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if result.FinishReason != tc.wantFinish || result.RawFinishReason != tc.raw || len(result.ToolCalls) != tc.wantCalls {
				t.Fatalf("finish=%q raw=%q calls=%d, want %q %q %d", result.FinishReason, result.RawFinishReason, len(result.ToolCalls), tc.wantFinish, tc.raw, tc.wantCalls)
			}
		})
	}
}

func r89toolsJSONBody(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Replace(body, "%s", "", 1))
	}
}

// provb CR (cr-r89-provb-to-tools 2): the openai-compatible
// createJsonErrorResponseHandler APICallError keeps response headers, body
// and data, so retry-after(-ms) reaches the retry loop.
func TestR89toolsCerebrasErrorKeepsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"slow down","type":"rate_limit","param":"","code":"too_many"}`)
	}))
	defer srv.Close()
	_, err := r88toolsModel(t, srv, nil).DoGenerate(context.Background(), r88toolsOpts(nil))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if perr.Message != "slow down" || perr.ErrorCode != "too_many" || perr.StatusCode != 429 || perr.ResponseBody == "" || perr.Data == nil {
		t.Fatalf("err = %#v", perr)
	}
	found := false
	for k, v := range perr.ResponseHeaders {
		if strings.EqualFold(k, "retry-after-ms") && v == "10" {
			found = true
		}
	}
	if !found {
		t.Fatalf("headers = %v", perr.ResponseHeaders)
	}
}
