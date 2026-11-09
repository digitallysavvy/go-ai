package cerebras

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

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
