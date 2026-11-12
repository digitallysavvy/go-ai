package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
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

// provg2-03: concurrent first calls to Gateway() do not race and share one
// provider.
func TestR91provgDefaultGatewayConcurrent(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "k")
	defaultProviderMu.Lock()
	defaultProvider = nil
	defaultProviderMu.Unlock()
	t.Cleanup(func() {
		defaultProviderMu.Lock()
		defaultProvider = nil
		defaultProviderMu.Unlock()
	})
	var wg sync.WaitGroup
	got := make([]*Provider, 8)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], _ = Gateway()
		}(i)
	}
	wg.Wait()
	for _, p := range got {
		if p == nil || p != got[0] {
			t.Fatalf("providers = %v", got)
		}
	}
}

func r91provgSchemaError(t *testing.T, err error, status int) {
	t.Helper()
	var re *gatewayerrors.GatewayResponseError
	if !errors.As(err, &re) || err.Error() != "Invalid error response format: Gateway request failed" || re.GetStatusCode() != status {
		t.Fatalf("error = %T %v", err, err)
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
		t.Fatalf("cause = %v", err)
	}
}

// provg2-06: TS gateway-fetch-metadata.ts:37 createJsonResponseHandler +
// asGatewayError for /config, the spend report and the client secret.
func TestR91provgGatewaySuccessSchemaErrors(t *testing.T) {
	for _, body := range []string{`{"models":5}`, `not json`, `{"models":[{"id":"a","name":"A","description":5,"specification":{"specificationVersion":"v4","provider":"p","modelId":"a"}}]}`} {
		serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		p, _ := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
		_, err := p.GetAvailableModels(context.Background())
		closeServer()
		r91provgSchemaError(t, err, 200)
	}
	for _, body := range []string{`{"results":[{"total_cost":1,"day":null}]}`, `{"results":[{"total_cost":1,"input_tokens":"3"}]}`} {
		serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		p, _ := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
		_, err := p.GetSpendReport(context.Background(), SpendReportParams{StartDate: "a", EndDate: "b"})
		closeServer()
		r91provgSchemaError(t, err, 200)
	}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"token":5}`)
	}))
	defer closeServer()
	p, _ := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	_, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "m"})
	r91provgSchemaError(t, err, 201)
}

// provg2-12: a BOM-prefixed 401 mint error body is decoded like TS
// response.text() and gives a GatewayAuthenticationError.
func TestR91provgClientSecretBOMErrorBody(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, "\uFEFF"+`{"error":{"message":"bad key","type":"authentication_error"}}`)
	}))
	defer closeServer()
	p, _ := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	_, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "m"})
	var ae *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &ae) {
		t.Fatalf("error = %T %v", err, err)
	}
}

// provc-06 (gateway sites): repeated response headers are joined like TS
// extractResponseHeaders.
func TestR91provgFlattenHeadersJoinsValues(t *testing.T) {
	if got := flattenHeaders(http.Header{"X-A": {"1", "2"}}); got["X-A"] != "1, 2" {
		t.Fatalf("headers = %v", got)
	}
}

// core2-18 (gateway part): a base64 DataString (unpadded here) is sent as
// given (TS gateway-decision-model.ts:222).
func TestR91provgDecisionFileDataStringVerbatim(t *testing.T) {
	out, err := encodeGatewayDecisionState([]provider.DecisionStatePart{{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw0KGgo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"data":{"data":"iVBORw0KGgo","type":"data"}`) {
		t.Fatalf("state = %s", b)
	}
}
