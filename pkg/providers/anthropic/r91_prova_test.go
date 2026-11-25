package anthropic

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r91provaBatchJSON = `{
	"id": "msgbatch_1", "type": "message_batch", "processing_status": "in_progress",
	"request_counts": {"processing": 1, "succeeded": 0, "errored": 0, "canceled": 0, "expired": 0},
	"created_at": "2024-01-01T00:00:00Z", "expires_at": "2024-01-02T00:00:00Z"
}`

// prova-01: TS getStartBatchHeaders sets 'anthropic-beta' to the computed
// set or to undefined, so a provider-level Config.Headers anthropic-beta never
// reaches the create-batch request (anthropic-batch.ts:462-476), and it never
// joins the batch beta set (:254-262).
func TestR91provaStartBatchDropsConfigBetaHeader(t *testing.T) {
	var got [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Values("anthropic-beta"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(r91provaBatchJSON))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL, Headers: map[string]string{"Anthropic-Beta": "cfg-beta", "X-Custom": "c"}})
	b := p.ExperimentalBatch()
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"explicit-beta"}},
		},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	// Other batch calls keep the configured header (TS getBatchHeaders).
	if _, err := b.DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "msgbatch_1"}); err != nil {
		t.Fatalf("DoGetBatchStatus: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("requests = %d", len(got))
	}
	if len(got[0]) != 0 {
		t.Fatalf("no betas: anthropic-beta = %v, want none", got[0])
	}
	if len(got[1]) != 1 || got[1][0] != "explicit-beta" {
		t.Fatalf("explicit betas: anthropic-beta = %v, want [explicit-beta]", got[1])
	}
	if len(got[2]) != 1 || got[2][0] != "cfg-beta" {
		t.Fatalf("status: anthropic-beta = %v, want [cfg-beta]", got[2])
	}
}

// prova-03: DoListBatches builds its query in TS url.searchParams.set order
// (anthropic-batch.ts:355-361): limit, then after_id.
func TestR91provaListBatchesQueryOrder(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [], "has_more": false, "last_id": null}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	if _, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{Limit: intPtr(5), Cursor: "abc"}); err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if gotQuery != "limit=5&after_id=abc" {
		t.Fatalf("query = %q, want limit=5&after_id=abc", gotQuery)
	}
}

// prova-02: a transport failure following results_url is the TS
// handleFetchError result (anthropic-batch.ts:412 getFromApi): the
// retryable "Cannot connect to API: ..." error.
func TestR91provaBatchResultsTransportError(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/results" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("no hijacker")
				return
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srvURL + `/results"}`))
	}))
	defer srv.Close()
	srvURL = srv.URL

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.ExperimentalBatch().DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if !strings.Contains(err.Error(), "Cannot connect to API: ") {
		t.Fatalf("err = %v, want Cannot connect to API", err)
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || !pe.IsRetryable() {
		t.Fatalf("err = %#v, want a retryable ProviderError", err)
	}
}

// prova-12: TS z.number() usage counts accept fractional values; Go
// truncates them (known difference "Fractional counts in integer fields")
// instead of dropping message_start or failing on message_delta
// (anthropic-language-model.ts:3335-3342, 3435-3442).
func TestR91provaStreamFractionalUsage(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-3-haiku-20240307","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10.5,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2.5}}`,
		`{"type":"message_stop"}`,
	}
	s := r88provaNewServer(t, "", events)
	stream, err := r88provaModel(t, s, "claude-3-haiku-20240307").DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r88provaDrain(stream)
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}
	sawMeta := false
	var finish *provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeResponseMetadata:
			sawMeta = p.ResponseMetadata != nil && p.ResponseMetadata.ID == "msg_1"
		case provider.ChunkTypeFinish:
			finish = p
		}
	}
	if !sawMeta {
		t.Fatal("no response-metadata for msg_1")
	}
	if finish == nil || finish.Usage == nil || finish.Usage.InputTokens == nil || finish.Usage.OutputTokens == nil {
		t.Fatalf("finish = %+v", finish)
	}
	if *finish.Usage.InputTokens != 10 || *finish.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %d/%d, want 10/2", *finish.Usage.InputTokens, *finish.Usage.OutputTokens)
	}
}

// r91provaBody sends one generate call and returns its request body.
func r91provaBody(t *testing.T, opts *provider.GenerateOptions) string {
	t.Helper()
	s := r88provaNewServer(t, r88provaTextResponse, nil)
	if opts.Prompt.Text == "" && len(opts.Prompt.Messages) == 0 {
		opts.Prompt = types.Prompt{Text: "hi"}
	}
	if _, err := r88provaModel(t, s, "claude-sonnet-4-5").DoGenerate(t.Context(), opts); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	return r88provaJSON(s.body(0))
}

// Typed-option ruling (probe): Go-typed literals in the call provider
// options and the tool provider options send the same request as the
// map[string]interface{} form.
func TestR91provaTypedOptionLiterals(t *testing.T) {
	fn := func(po interface{}) []types.Tool {
		return []types.Tool{{Type: types.ToolTypeFunction, Name: "f", Parameters: map[string]interface{}{"type": "object"}, ProviderOptions: po}}
	}
	cases := []struct {
		name         string
		typed, plain *provider.GenerateOptions
	}{
		{
			"map[string]string namespace",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]string{"effort": "low"}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"effort": "low"}}},
		},
		{
			"int64 budget",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": float64(2048)}}}},
		},
		{
			"typed option struct",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": &ModelOptions{Effort: "low"}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"effort": "low"}}},
		},
		{
			"[]map[string]interface{} mcpServers",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"mcpServers": []map[string]interface{}{{"type": "url", "name": "s", "url": "https://example.com/mcp"}}}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"mcpServers": []interface{}{map[string]interface{}{"type": "url", "name": "s", "url": "https://example.com/mcp"}}}}},
		},
		{
			"tool map[string]map[string]bool",
			&provider.GenerateOptions{Tools: fn(map[string]map[string]bool{"anthropic": {"deferLoading": true}})},
			&provider.GenerateOptions{Tools: fn(map[string]interface{}{"anthropic": map[string]interface{}{"deferLoading": true}})},
		},
		{
			"tool nested map[string]bool",
			&provider.GenerateOptions{Tools: fn(map[string]interface{}{"anthropic": map[string]bool{"eagerInputStreaming": true}})},
			&provider.GenerateOptions{Tools: fn(map[string]interface{}{"anthropic": map[string]interface{}{"eagerInputStreaming": true}})},
		},
		{
			"tool cacheControl map[string]string",
			&provider.GenerateOptions{Tools: fn(map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]string{"type": "ephemeral"}}})},
			&provider.GenerateOptions{Tools: fn(map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}})},
		},
	}
	for _, c := range cases {
		typed := r91provaBody(t, c.typed)
		plain := r91provaBody(t, c.plain)
		if typed != plain {
			t.Errorf("%s:\n typed %s\n plain %s", c.name, typed, plain)
		}
		baseline := &provider.GenerateOptions{}
		if c.plain.Tools != nil {
			baseline.Tools = fn(nil)
		}
		if plain == r91provaBody(t, baseline) {
			t.Errorf("%s: the option has no effect on the request", c.name)
		}
	}
}

// The caller's batch request options are not modified by normalization.
func TestR91provaBatchTypedOptionsNotMutated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(r91provaBatchJSON))
	}))
	defer srv.Close()
	req := textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")
	inner := map[string]string{"effort": "low"}
	req.Text.Options.ProviderOptions = map[string]interface{}{"anthropic": inner}
	_, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests:        []provider.BatchV4Request{req},
		ProviderOptions: map[string]interface{}{"anthropic": map[string][]string{"anthropicBeta": {"b1"}}},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if _, ok := req.Text.Options.ProviderOptions["anthropic"].(map[string]string); !ok {
		t.Fatal("caller options were modified")
	}
}
