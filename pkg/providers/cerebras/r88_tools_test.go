package cerebras

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of TS 4a4e854fa6 cerebras-chat-language-model.test.ts "service
// tiers" and cerebras-provider.test.ts "service tier options".

const r88toolsTierBody = `{"id":"chatcmpl-test","model":"gpt-oss-120b","choices":[{"message":{"role":"assistant","content":"blue"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}%s}`

type r88toolsRecorder struct {
	mu      sync.Mutex
	headers []http.Header
	bodies  []map[string]any
}

func r88toolsServer(t *testing.T, respond func(w http.ResponseWriter)) (*httptest.Server, *r88toolsRecorder) {
	t.Helper()
	rec := &r88toolsRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		rec.mu.Lock()
		rec.headers = append(rec.headers, r.Header.Clone())
		rec.bodies = append(rec.bodies, body)
		rec.mu.Unlock()
		respond(w)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func r88toolsJSON(extra string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Replace(r88toolsTierBody, "%s", extra, 1))
	}
}

func r88toolsSSE(chunks ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = io.WriteString(w, "data: "+c+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
}

func r88toolsModel(t *testing.T, srv *httptest.Server, headers map[string]string) provider.LanguageModel {
	t.Helper()
	m, err := New(Config{APIKey: "test-api-key", BaseURL: srv.URL, Headers: headers}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func r88toolsOpts(cerebras map[string]any) *provider.GenerateOptions {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}
	if cerebras != nil {
		opts.ProviderOptions = map[string]interface{}{"cerebras": cerebras}
	}
	return opts
}

func r88toolsFinishMetadata(t *testing.T, stream provider.TextStream) map[string]any {
	t.Helper()
	defer func() { _ = stream.Close() }()
	var meta map[string]any
	for {
		chunk, err := stream.Next()
		if err != nil || chunk == nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish && len(chunk.ProviderMetadata) > 0 {
			_ = json.Unmarshal(chunk.ProviderMetadata, &meta)
		}
	}
	return meta
}

func TestR88toolsCerebrasQueueThresholdHeader(t *testing.T) {
	srv, rec := r88toolsServer(t, r88toolsJSON(""))
	m := r88toolsModel(t, srv, nil)
	if _, err := m.DoGenerate(context.Background(), r88toolsOpts(map[string]any{"serviceTier": "flex", "queueThreshold": 200})); err != nil {
		t.Fatal(err)
	}
	body := rec.bodies[0]
	if body["service_tier"] != "flex" {
		t.Fatalf("service_tier = %v", body["service_tier"])
	}
	for _, k := range []string{"serviceTier", "queueThreshold", "queue_threshold"} {
		if _, ok := body[k]; ok {
			t.Fatalf("body leaks %q: %v", k, body)
		}
	}
	if got := rec.headers[0].Get("queue_threshold"); got != "200" {
		t.Fatalf("queue_threshold header = %q", got)
	}

	if _, err := m.DoGenerate(context.Background(), r88toolsOpts(map[string]any{"serviceTier": "auto"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.headers[1]["Queue_threshold"]; ok {
		t.Fatalf("header sent without queueThreshold: %v", rec.headers[1])
	}
}

// "gives queueThreshold precedence over header casing without mutating callers"
func TestR88toolsCerebrasQueueThresholdHeaderPrecedence(t *testing.T) {
	srv, rec := r88toolsServer(t, r88toolsJSON(""))
	m := r88toolsModel(t, srv, map[string]string{"queue_threshold": "777"})
	headers := map[string]string{"queue_threshold": "999", "QUEUE_THRESHOLD": "888", "X-Request-ID": "request-123"}
	opts := r88toolsOpts(map[string]any{"queueThreshold": 200})
	opts.Headers = headers
	if _, err := m.DoGenerate(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := rec.headers[0].Values("queue_threshold"); !reflect.DeepEqual(got, []string{"200"}) {
		t.Fatalf("queue_threshold = %v", got)
	}
	if rec.headers[0].Get("x-request-id") != "request-123" {
		t.Fatalf("headers = %v", rec.headers[0])
	}
	if !reflect.DeepEqual(headers, map[string]string{"queue_threshold": "999", "QUEUE_THRESHOLD": "888", "X-Request-ID": "request-123"}) || !reflect.DeepEqual(opts.Headers, headers) {
		t.Fatalf("caller headers mutated: %v", headers)
	}
}

func TestR88toolsCerebrasServiceTierMetadata(t *testing.T) {
	for extra, want := range map[string]any{`,"service_tier":"flex"`: "flex", ``: nil, `,"service_tier":null`: nil, `,"service_tier":""`: ""} {
		srv, _ := r88toolsServer(t, r88toolsJSON(extra))
		res, err := r88toolsModel(t, srv, nil).DoGenerate(context.Background(), r88toolsOpts(map[string]any{"serviceTier": "auto"}))
		if err != nil {
			t.Fatal(err)
		}
		c, _ := res.ProviderMetadata["cerebras"].(map[string]interface{})
		got, present := c["serviceTier"]
		if (want == nil && present) || (want != nil && got != want) {
			t.Fatalf("%q: providerMetadata = %v", extra, res.ProviderMetadata)
		}
	}
}

func TestR88toolsCerebrasStreamServiceTier(t *testing.T) {
	first := `{"id":"chatcmpl-test","model":"gpt-oss-120b","choices":[{"delta":{"role":"assistant","content":"blue"}}]%s}`
	last := `{"id":"chatcmpl-test","model":"gpt-oss-120b","choices":[{"delta":{},"finish_reason":"stop"}],"usage":%s%s}`
	plainUsage := `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`
	for _, tier := range []string{``, `,"service_tier":null`} {
		srv, _ := r88toolsServer(t, r88toolsSSE(strings.Replace(first, "%s", tier, 1), strings.Replace(strings.Replace(last, "%s", plainUsage, 1), "%s", tier, 1)))
		stream, err := r88toolsModel(t, srv, nil).DoStream(context.Background(), r88toolsOpts(map[string]any{"serviceTier": "auto"}))
		if err != nil {
			t.Fatal(err)
		}
		if meta := r88toolsFinishMetadata(t, stream); meta["cerebras"] != nil {
			if c, _ := meta["cerebras"].(map[string]any); c["serviceTier"] != nil {
				t.Fatalf("%q: fabricated tier %v", tier, meta)
			}
		}
	}

	// "uses the final returned tier while preserving prediction metadata"
	predUsage := `{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"completion_tokens_details":{"accepted_prediction_tokens":1,"rejected_prediction_tokens":0}}`
	srv, _ := r88toolsServer(t, r88toolsSSE(strings.Replace(first, "%s", `,"service_tier":"auto"`, 1), strings.Replace(strings.Replace(last, "%s", predUsage, 1), "%s", `,"service_tier":"flex"`, 1)))
	stream, err := r88toolsModel(t, srv, nil).DoStream(context.Background(), r88toolsOpts(nil))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r88toolsFinishMetadata(t, stream)["cerebras"].(map[string]any)
	if c["serviceTier"] != "flex" || c["acceptedPredictionTokens"] != float64(1) || c["rejectedPredictionTokens"] != float64(0) {
		t.Fatalf("cerebras metadata = %v", c)
	}

	// "sends the queue_threshold header and exposes the effective tier"
	srv, rec := r88toolsServer(t, r88toolsSSE(strings.Replace(first, "%s", "", 1), strings.Replace(strings.Replace(last, "%s", plainUsage, 1), "%s", `,"service_tier":"flex"`, 1)))
	stream, err = r88toolsModel(t, srv, nil).DoStream(context.Background(), r88toolsOpts(map[string]any{"serviceTier": "flex", "queueThreshold": 150}))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = r88toolsFinishMetadata(t, stream)["cerebras"].(map[string]any)
	if rec.headers[0].Get("queue_threshold") != "150" || c["serviceTier"] != "flex" {
		t.Fatalf("header %q, metadata %v", rec.headers[0].Get("queue_threshold"), c)
	}
}

func TestR88toolsCerebrasQueueThresholdValidation(t *testing.T) {
	srv, rec := r88toolsServer(t, r88toolsJSON(""))
	m := r88toolsModel(t, srv, nil)
	for _, v := range []any{5, 25000, 100.5, "100"} {
		_, err := m.DoGenerate(context.Background(), r88toolsOpts(map[string]any{"queueThreshold": v}))
		var argErr *providererrors.InvalidArgumentError
		if !errors.As(err, &argErr) {
			t.Fatalf("%v: err = %v, want an InvalidArgumentError", v, err)
		}
	}
	if _, err := m.DoStream(context.Background(), r88toolsOpts(map[string]any{"queueThreshold": 5})); err == nil {
		t.Fatal("stream accepted queueThreshold 5")
	}
	if len(rec.bodies) != 0 {
		t.Fatalf("invalid options reached the API: %d requests", len(rec.bodies))
	}
	for _, v := range []any{50, 20000} {
		if _, err := m.DoGenerate(context.Background(), r88toolsOpts(map[string]any{"queueThreshold": v})); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
	}
	// "preserves the existing runtime option pass-through"
	long := strings.Repeat("x", 1025)
	if _, err := m.DoGenerate(context.Background(), r88toolsOpts(map[string]any{
		"queueThreshold": 200, "serviceTier": "future-tier", "reasoningEffort": nil, "topLogprobs": 21, "promptCacheKey": long,
	})); err != nil {
		t.Fatal(err)
	}
	body := rec.bodies[len(rec.bodies)-1]
	if body["service_tier"] != "future-tier" || body["top_logprobs"] != float64(21) || body["prompt_cache_key"] != long {
		t.Fatalf("body = %v", body)
	}
	if v, ok := body["reasoning_effort"]; !ok || v != nil {
		t.Fatalf("reasoning_effort = %v (present %v)", v, ok)
	}
}

func TestR88toolsCerebrasMetadataExtractor(t *testing.T) {
	ex := cerebrasMetadataExtractor{}
	got, _ := ex.ExtractMetadata(context.Background(), map[string]interface{}{"service_tier": "flex"})
	if !reflect.DeepEqual(got, map[string]interface{}{"cerebras": map[string]interface{}{"serviceTier": "flex"}}) {
		t.Fatalf("got %v", got)
	}
	for _, body := range []interface{}{map[string]interface{}{}, map[string]interface{}{"service_tier": 3}, []interface{}{}} {
		if got, _ := ex.ExtractMetadata(context.Background(), body); got != nil {
			t.Fatalf("%v: got %v", body, got)
		}
	}
	withTier := ex.CreateStreamExtractor()
	withTier.ProcessChunk(map[string]interface{}{"choices": []interface{}{}})
	withTier.ProcessChunk(map[string]interface{}{"service_tier": "flex"})
	withTier.ProcessChunk(map[string]interface{}{"service_tier": nil})
	if got := withTier.BuildMetadata(); !reflect.DeepEqual(got, map[string]interface{}{"cerebras": map[string]interface{}{"serviceTier": "flex"}}) {
		t.Fatalf("stream got %v", got)
	}
	if got := ex.CreateStreamExtractor().BuildMetadata(); got != nil {
		t.Fatalf("empty stream got %v", got)
	}
}
