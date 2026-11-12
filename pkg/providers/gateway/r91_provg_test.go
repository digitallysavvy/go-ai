package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91provgBatchServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body []byte)) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		handler(w, r, body)
	}))
	t.Cleanup(server.Close)
	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// provg2-02: TS gateway-batch.test.ts 'should revive response.timestamp into
// a Date on succeeded items' with the V4 succeededItem.
func TestR91provgBatchResultsDecodeV4Result(t *testing.T) {
	const succeeded = `{"type":"text","id":"req-1","status":"succeeded","result":{"content":[{"type":"text","text":"pong 1"}],"finishReason":{"unified":"stop"},"response":{"modelId":"openai/gpt-5.6-luna","timestamp":"2026-08-18T00:00:00.000Z"},"usage":{"inputTokens":{"total":4,"noCache":4},"outputTokens":{"total":2,"text":2}},"warnings":[]}}`
	p := r91provgBatchServer(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(succeeded + "\n\n" + `{"type":"text","id":"req-3","status":"cancelled"}` + "\n"))
	})
	stream, err := p.ExperimentalBatch().DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer func() { _ = stream.Close() }()
	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	res := item.TextResult
	if res == nil || res.Text != "pong 1" || len(res.Content) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if res.FinishReason != types.FinishReasonStop {
		t.Fatalf("finishReason = %q", res.FinishReason)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 4 || res.Usage.OutputTokens == nil || *res.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.ModelID != "openai/gpt-5.6-luna" ||
		!res.ResponseMetadata.Timestamp.Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("response = %+v", res.ResponseMetadata)
	}
	if res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
	item2, err := stream.Next()
	if err != nil || item2.ID != "req-3" || item2.Status != provider.BatchItemCancelled {
		t.Fatalf("item2 = %+v, %v", item2, err)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("end = %v", err)
	}
}

type r91provgCloseRecorder struct {
	io.Reader
	closed bool
}

func (c *r91provgCloseRecorder) Close() error { c.closed = true; return nil }

// provg2-02: a line that passes the schema but fails the typed decode closes
// the body.
func TestR91provgBatchResultsLineDecodeErrorClosesBody(t *testing.T) {
	body := &r91provgCloseRecorder{Reader: strings.NewReader(`{"type":"text","id":"r","status":"failed","error":{"message":5}}` + "\n")}
	s := newGatewayBatchResultsStream(body, "u", 1<<20)
	if _, err := s.Next(); err == nil {
		t.Fatal("want decode error")
	}
	if !body.closed {
		t.Fatal("body not closed after line decode error")
	}
}

// provg2-04: TS normalizeBatchRequestCounts omits inconsistent counts.
func TestR91provgBatchStatusNormalizesRequestCounts(t *testing.T) {
	cases := []struct {
		counts string
		want   *provider.BatchRequestCounts
	}{
		{`{"total":10,"pending":1,"completed":2,"failed":3}`, nil},
		{`{"total":1.5,"pending":1,"completed":0,"failed":0}`, nil},
		{`{"total":1,"pending":-1,"completed":2,"failed":0}`, nil},
		{`{"total":6,"pending":1,"completed":2,"failed":3}`, &provider.BatchRequestCounts{Total: 6, Pending: 1, Completed: 2, Failed: 3}},
	}
	for _, tc := range cases {
		p := r91provgBatchServer(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"pending","requestCounts":` + tc.counts + `}`))
		})
		st, err := p.ExperimentalBatch().DoGetBatchStatus(context.Background(), provider.BatchV4OperationOptions{BatchID: "b"})
		if err != nil {
			t.Fatalf("%s: %v", tc.counts, err)
		}
		if (st.RequestCounts == nil) != (tc.want == nil) || (tc.want != nil && *st.RequestCounts != *tc.want) {
			t.Fatalf("%s: requestCounts = %+v, want %+v", tc.counts, st.RequestCounts, tc.want)
		}
	}
}

// provg2-13: TS omitGatewayIdempotencyKey forwards a caller's `{}` and drops
// providerOptions only when stripping the key leaves nothing.
func TestR91provgBatchStartEmptyProviderOptions(t *testing.T) {
	cases := []struct {
		opts    map[string]interface{}
		present bool
		want    string
	}{
		{map[string]interface{}{}, true, `{}`},
		{nil, false, ``},
		{map[string]interface{}{"gateway": map[string]interface{}{"idempotencyKey": "k"}}, false, ``},
		{map[string]interface{}{"gateway": map[string]interface{}{}}, true, `{"gateway":{}}`},
	}
	for i, tc := range cases {
		var got map[string]json.RawMessage
		p := r91provgBatchServer(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
			_ = json.Unmarshal(body, &got)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"batchId":"job","status":"pending"}`))
		})
		_, err := p.ExperimentalBatch().DoStartBatch(context.Background(), provider.BatchV4StartOptions{
			Requests: []provider.BatchV4Request{{Type: provider.BatchRequestTypeText, Text: &provider.TextBatchV4Request{
				ID: "r1", ModelID: "openai/gpt-5", Options: provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}},
			}}},
			ProviderOptions: tc.opts,
		})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		raw, ok := got["providerOptions"]
		if ok != tc.present || (ok && string(raw) != tc.want) {
			t.Fatalf("case %d: providerOptions present=%v %s, want present=%v %s", i, ok, raw, tc.present, tc.want)
		}
	}
}
