package google

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS validates a batch result with responseSchema, a non-strict z.object that
// strips the in-stream `error` key; a malformed `error` must not fail the item.
func TestR47_BatchResultIgnoresErrorKeyOfResponseSchema(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	resp := json.RawMessage(`{"error":"oops","candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
	if got := b.convertGoogleBatchResultLine("k", resp, nil); got.Status != provider.BatchItemSucceeded {
		t.Fatalf("item = %+v", got)
	}
}

// TS keeps an empty blockReason (`blockReason == null` is false for "").
func TestR47_BatchEmptyBlockReasonIsStillBlocked(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	got := b.convertGoogleBatchResultLine("k", json.RawMessage(`{"promptFeedback":{"blockReason":""}}`), nil)
	if got.Error == nil || got.Error.Code != "prompt_blocked" || got.Error.Message != "Google blocked the batch request ()." {
		t.Fatalf("item = %+v", got.Error)
	}
}

// TS convertGoogleRpcError: `message ?? fallback`, String(code) of a JS number.
func TestR47_BatchRPCErrorEmptyMessageAndLargeCode(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	empty, code := "", float64(3000000000)
	got := b.convertGoogleBatchResultLine("k", nil, &googleRPCStatusWire{Message: &empty, Code: code})
	if got.Error.Message != "" || got.Error.Code != "3000000000" {
		t.Fatalf("error = %+v", got.Error)
	}
}

// TS createJsonLinesResponseHandler(googleBatchResultLineSchema) fails the
// stream on a line without a string key, and on a line that is not JSON.
func TestR47_BatchResultsFileLineValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		line      string
		wantParse bool
	}{
		"missing key": {line: `{"response":{}}`},
		"bad error":   {line: `{"key":"a","error":{"code":true}}`},
		"not json":    {line: `{not json`, wantParse: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/download/") {
					_, _ = w.Write([]byte(tc.line + "\n"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"name":"batches/b","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/o"}}}`))
			}))
			defer srv.Close()
			b := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch()
			stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/b"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			item, err := stream.Next()
			if err == nil || err == io.EOF {
				t.Fatalf("want error, got item %+v err %v", item, err)
			}
			var parse *providererrors.JSONParseError
			var val *providererrors.ValidationError
			if tc.wantParse && !errors.As(err, &parse) || !tc.wantParse && !errors.As(err, &val) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}
