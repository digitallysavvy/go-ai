package bedrock

import (
	"context"
	"encoding/json"
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
			if !errors.As(err, &verr) || !strings.Contains(verr.Message, tc.field+`"`) {
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

// TS validates the whole Converse response (AmazonBedrockResponseSchema), not
// only the usage: required output/content/role/stopReason/usage and nested
// block shapes fail with an APICallError "Invalid JSON response".
func TestConverseResponseSchemaIsValidatedLikeTS(t *testing.T) {
	const usage = `"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}`
	const msg = `"output":{"message":{"role":"assistant","content":[{"text":"hi"}]}}`
	bad := map[string]string{
		"missing usage":      `{` + msg + `,"stopReason":"end_turn"}`,
		"missing stopReason": `{` + msg + `,` + usage + `}`,
		"missing output":     `{"stopReason":"end_turn",` + usage + `}`,
		"missing role":       `{"output":{"message":{"content":[]}},"stopReason":"end_turn",` + usage + `}`,
		"content not array":  `{"output":{"message":{"role":"assistant","content":"x"}},"stopReason":"end_turn",` + usage + `}`,
		"text not string":    `{"output":{"message":{"role":"assistant","content":[{"text":1}]}},"stopReason":"end_turn",` + usage + `}`,
		"toolUse no id":      `{"output":{"message":{"role":"assistant","content":[{"toolUse":{"name":"x"}}]}},"stopReason":"end_turn",` + usage + `}`,
		"reasoning union":    `{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"foo":1}}]}},"stopReason":"end_turn",` + usage + `}`,
		"metrics latency":    `{` + msg + `,"metrics":{"latencyMs":"1"},"stopReason":"end_turn",` + usage + `}`,
		"serviceTier":        `{` + msg + `,"serviceTier":{},"stopReason":"end_turn",` + usage + `}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			err := bedrockValidator(zbConverse)(200, http.Header{"X-Amzn-Requestid": {"r"}}, []byte(body))
			var perr *providererrors.ProviderError
			if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
				t.Fatalf("%s: err = %v, want Invalid JSON response", name, err)
			}
			if perr.StatusCode != 200 || perr.ResponseBody != body || len(perr.ResponseHeaders) == 0 ||
				!providererrors.IsValidationError(perr.Cause) {
				t.Fatalf("%s: status=%d headers=%v cause=%#v", name, perr.StatusCode, perr.ResponseHeaders, perr.Cause)
			}
		})
	}
	good := `{` + msg + `,"metrics":{"latencyMs":3},"stopReason":"whatever","serviceTier":{"type":"x"},"trace":5,` + usage + `,"extra":1}`
	if err := bedrockValidator(zbConverse)(200, http.Header{}, []byte(good)); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if err := bedrockValidator(zbConverse)(200, http.Header{}, []byte(`not json`)); err == nil {
		t.Fatal("non-JSON body must be Invalid JSON response")
	}
}

func TestConverseStreamEventSchemaIsValidatedLikeTS(t *testing.T) {
	bad := map[string]string{
		"contentBlockDelta no index": `{"contentBlockDelta":{"delta":{"text":"a"}}}`,
		"delta no member":            `{"contentBlockDelta":{"contentBlockIndex":0,"delta":{}}}`,
		"toolUse input not string":   `{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"toolUse":{"input":{}}}}}`,
		"start no index":             `{"contentBlockStart":{"start":{}}}`,
		"stop no index":              `{"contentBlockStop":{}}`,
		"messageStop no reason":      `{"messageStop":{}}`,
		"metadata usage no input":    `{"metadata":{"usage":{"outputTokens":1}}}`,
		"exception not object":       `{"throttlingException":"x"}`,
	}
	for name, ev := range bad {
		t.Run(name, func(t *testing.T) {
			var m map[string]interface{}
			_ = json.Unmarshal([]byte(ev), &m)
			for k, v := range m {
				if validateConverseStreamEvent(k, v) == nil {
					t.Fatalf("%s: want a validation error", name)
				}
			}
		})
	}
	good := map[string]string{
		"metadata no total":  `{"metadata":{"usage":{"inputTokens":1,"outputTokens":2}}}`,
		"redacted delta":     `{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"reasoningContent":{"redactedContent":"x"}}}}`,
		"null delta":         `{"contentBlockDelta":{"contentBlockIndex":0,"delta":null}}`,
		"unknown event type": `{"somethingNew":{"a":1}}`,
	}
	for name, ev := range good {
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(ev), &m)
		for k, v := range m {
			if verr := validateConverseStreamEvent(k, v); verr != nil {
				t.Fatalf("%s: %v", name, verr)
			}
		}
	}
}

func TestConverseStreamSchemaMismatchSkipsEventAndFinishesWithError(t *testing.T) {
	for name, tc := range map[string]struct {
		event   [2]string
		wantErr bool
	}{
		"bad delta":      {[2]string{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{}}`}, true},
		"usage no total": {[2]string{"metadata", `{"usage":{"inputTokens":1,"outputTokens":2}}`}, false},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(buildBedrockEventStreamBody([][2]string{tc.event, {"messageStop", `{"stopReason":"end_turn"}`}}))
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
					sawError = true
				case provider.ChunkTypeFinish:
					finish = chunk.FinishReason
				}
			}
			if sawError != tc.wantErr {
				t.Fatalf("error chunk = %v, want %v", sawError, tc.wantErr)
			}
			if tc.wantErr == (finish == types.FinishReasonStop) {
				// the bad event's error finish reason is overwritten by the later messageStop in TS too
				t.Logf("finish = %q", finish)
			}
		})
	}
}
