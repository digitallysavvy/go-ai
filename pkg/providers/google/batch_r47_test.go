package google

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TS sizes the inline body with JSON.stringify, which leaves "<" as one byte;
// Go's default escaping (<, six bytes) pushed a 5 MB prompt of them over
// the 20 MB inline threshold and into a file upload.
func TestR47_BatchInlineThresholdIgnoresHTMLEscaping(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	}))
	defer srv.Close()
	b := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("a", "gemini-2.5-flash", strings.Repeat("<", 5_000_000))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1beta/models/gemini-2.5-flash:batchGenerateContent" {
		t.Fatalf("path = %q", path)
	}
}

// TS inlines while inlineInputBytes < 20_000_000, where inlineInputBytes
// starts at the JSON.stringify bytes of the creation body with an empty
// request list (batch wrapper and a 16-character generateId displayName) and
// adds each inlined request's bytes plus one per separator. The sizes here
// are computed with that formula independently of the provider code, and the
// prompt is padded so the total is exactly the limit minus one (inline) or the
// limit (file upload).
func TestR47_BatchInlineThresholdCountsTheWholeCreationBody(t *testing.T) {
	const model = "gemini-2.5-flash"
	p := New(Config{APIKey: "k"})
	tsBytes := func(reqs []provider.BatchV4Request) int {
		total := len(`{"batch":{"displayName":"ai-sdk-batch-0123456789abcdef","inputConfig":{"requests":{"requests":[]}}}}`)
		for i, r := range reqs {
			body, _, err := NewLanguageModel(p, model).PrepareBatchRequestBody(&r.Text.Options)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(map[string]interface{}{"request": body, "metadata": map[string]interface{}{"key": r.RequestID()}}); err != nil {
				t.Fatal(err)
			}
			total += buf.Len() - 1
			if i > 0 {
				total++
			}
		}
		return total
	}
	build := func(target int) []provider.BatchV4Request {
		reqs := []provider.BatchV4Request{googleTextBatchRequest("a", model, "x"), googleTextBatchRequest("b", model, "y")}
		pad := target - tsBytes(reqs)
		reqs[1] = googleTextBatchRequest("b", model, "y"+strings.Repeat("y", pad))
		if got := tsBytes(reqs); got != target {
			t.Fatalf("padded size = %d, want %d", got, target)
		}
		return reqs
	}
	for _, tc := range []struct {
		size int
		path string
	}{
		{googleBatchInlineCreationMaxBytes - 1, "/v1beta/models/" + model + ":batchGenerateContent"},
		{googleBatchInlineCreationMaxBytes, "/upload/v1beta/files"},
	} {
		var mu sync.Mutex
		var first string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			if first == "" {
				first = r.URL.Path
			}
			mu.Unlock()
			_, _ = io.Copy(io.Discard, r.Body)
			if strings.HasSuffix(r.URL.Path, ":batchGenerateContent") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(googleBatchOperationJSON))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		}))
		b := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).ExperimentalBatch()
		_, _ = b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: build(tc.size)})
		srv.Close()
		mu.Lock()
		got := first
		mu.Unlock()
		if got != tc.path {
			t.Fatalf("inline bytes %d: first request path = %q, want %q", tc.size, got, tc.path)
		}
	}
}
