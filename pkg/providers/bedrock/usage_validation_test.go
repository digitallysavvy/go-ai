package bedrock

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS validates the usage of a Converse response and of the stream metadata
// (amazon-bedrock-usage-raw.test.ts): a wrongly typed field is an APICallError
// "Invalid JSON response" whose cause names the field.
func TestConverseUsageIsValidatedLikeTS(t *testing.T) {
	const out = `"output":{"message":{"role":"assistant","content":[{"text":"hi"}]}},"stopReason":"end_turn"`
	cases := map[string]struct{ usage, field string }{
		"cache detail inputTokens": {`{"inputTokens":1,"outputTokens":1,"totalTokens":2,"cacheDetails":[{"inputTokens":"2","ttl":"T5M"}]}`, "inputTokens"},
		"total tokens":             {`{"inputTokens":1,"outputTokens":1,"totalTokens":"2"}`, "totalTokens"},
		"missing output":           {`{"inputTokens":1,"totalTokens":2}`, "outputTokens"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{` + out + `,"usage":` + tc.usage + `}`))
			}))
			defer server.Close()
			model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
			_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			var perr *providererrors.ProviderError
			if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
				t.Fatalf("err = %v, want APICallError Invalid JSON response", err)
			}
			var verr *providererrors.ValidationError
			if !errors.As(err, &verr) || !strings.Contains(verr.Message, `"`+tc.field+`"`) {
				t.Fatalf("cause = %v, want a validation error naming %q", verr, tc.field)
			}
		})
	}

	t.Run("stream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
				{"messageStop", `{"stopReason":"end_turn"}`},
				{"metadata", `{"usage":{"inputTokens":13,"outputTokens":5,"totalTokens":"18"}}`},
			}))
		}))
		defer server.Close()
		model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
		stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close() //nolint:errcheck
		sawError, finish := false, types.FinishReason("")
		for {
			chunk, err := stream.Next()
			if err != nil {
				break
			}
			switch chunk.Type {
			case provider.ChunkTypeError:
				sawError = strings.Contains(chunk.Text, "totalTokens")
			case provider.ChunkTypeFinish:
				finish = chunk.FinishReason
			}
		}
		if !sawError || finish != types.FinishReasonError {
			t.Fatalf("error chunk naming totalTokens = %v, finish reason = %q", sawError, finish)
		}
	})
}
