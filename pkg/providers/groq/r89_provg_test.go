package groq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r89provgGroqErrorServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.Header().Set("X-Request-Id", "req-89")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func r89provgGroqPrompt() *provider.GenerateOptions {
	return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}},
	}}}}
}

// TS groqFailedResponseHandler (groq-error.ts 13, createJsonErrorResponseHandler
// in provider-utils response-handler.ts 128-179) builds an APICallError with
// the response headers and body in both branches, and data = the parsed error
// only when the body matches groqErrorDataSchema. Retry delays read
// retry-after from those headers (retry-with-exponential-backoff.ts).
func TestR89provgGroqErrorKeepsResponseHeadersAndBody(t *testing.T) {
	const envelope = `{"error":{"message":"Rate limit reached","type":"rate_limit_error","extra":1}}`
	cases := []struct {
		name     string
		body     string
		message  string
		code     string
		wantData interface{}
	}{
		{"schema body", envelope, "Rate limit reached", "rate_limit_error",
			map[string]interface{}{"error": map[string]interface{}{"message": "Rate limit reached", "type": "rate_limit_error"}}},
		{"non-schema body", `{"detail":"slow down"}`, "Too Many Requests", "", nil},
		{"empty body", ``, "Too Many Requests", "", nil},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			name := tc.name
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				url := r89provgGroqErrorServer(t, http.StatusTooManyRequests, tc.body)
				m, err := New(Config{APIKey: "k", BaseURL: url}).LanguageModel("llama-3.1-8b-instant")
				if err != nil {
					t.Fatal(err)
				}
				if stream {
					_, err = m.DoStream(context.Background(), r89provgGroqPrompt())
				} else {
					_, err = m.DoGenerate(context.Background(), r89provgGroqPrompt())
				}
				r89provgCheckProviderError(t, err, tc.body, tc.message, tc.code, tc.wantData)
			})
		}
		t.Run(tc.name+"/transcription", func(t *testing.T) {
			url := r89provgGroqErrorServer(t, http.StatusTooManyRequests, tc.body)
			m, err := New(Config{APIKey: "k", BaseURL: url}).TranscriptionModel("whisper-large-v3-turbo")
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("abc"), MimeType: "audio/wav"})
			r89provgCheckProviderError(t, err, tc.body, tc.message, tc.code, tc.wantData)
		})
	}
}

func r89provgCheckProviderError(t *testing.T, err error, body, message, code string, wantData interface{}) {
	t.Helper()
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("error = %T %v, want *ProviderError", err, err)
	}
	if perr.StatusCode != http.StatusTooManyRequests || perr.Message != message || perr.ErrorCode != code {
		t.Fatalf("status/message/code = %d %q %q, want 429 %q %q", perr.StatusCode, perr.Message, perr.ErrorCode, message, code)
	}
	if perr.ResponseBody != body {
		t.Fatalf("ResponseBody = %q, want %q", perr.ResponseBody, body)
	}
	if perr.ResponseHeaders["Retry-After"] != "7" || perr.ResponseHeaders["X-Request-Id"] != "req-89" {
		t.Fatalf("ResponseHeaders = %#v, want Retry-After and X-Request-Id", perr.ResponseHeaders)
	}
	if !reflect.DeepEqual(perr.Data, wantData) {
		t.Fatalf("Data = %#v, want %#v", perr.Data, wantData)
	}
	if !perr.IsRetryable() {
		t.Fatal("429 must be retryable")
	}
}

// TS convert-groq-usage.ts: input details are total, noCache and cacheRead
// only; prompt_tokens_details.text_tokens / image_tokens are not read.
func TestR89provgGroqUsageIgnoresTextImageDetails(t *testing.T) {
	var u groqUsage
	if err := u.UnmarshalJSON([]byte(`{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":4,"text_tokens":6,"image_tokens":3}}`)); err != nil {
		t.Fatal(err)
	}
	got := convertGroqUsage(u)
	if got.InputDetails == nil || got.InputDetails.TextTokens != nil || got.InputDetails.ImageTokens != nil {
		t.Fatalf("input details = %+v", got.InputDetails)
	}
	if *got.InputDetails.CacheReadTokens != 4 || *got.InputDetails.NoCacheTokens != 6 {
		t.Fatalf("input details = %+v", got.InputDetails)
	}
}
