package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r92provaBatchResultsServer serves a terminal batch whose results_url is on
// the server itself (Anthropic's results_url is on the API host) and records
// the x-api-key header of the results request.
func r92provaBatchResultsServer(t *testing.T) (*httptest.Server, func() (string, bool)) {
	t.Helper()
	var mu sync.Mutex
	var resultsKey string
	var sawResults bool
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages/batches/b1/results" {
			mu.Lock()
			resultsKey, sawResults = r.Header.Get("x-api-key"), true
			mu.Unlock()
			if r.Header.Get("x-api-key") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"x-api-key header is required"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"custom_id":"req-1","result":{"type":"canceled"}}` + "\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":1,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srvURL + `/v1/messages/batches/b1/results"}`))
	}))
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return resultsKey, sawResults
	}
}

func r92provaReadBatchResults(t *testing.T, p *Provider) {
	t.Helper()
	stream, err := p.ExperimentalBatch().DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close() //nolint:errcheck
	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.ID != "req-1" || item.Status != provider.BatchItemCancelled {
		t.Fatalf("item = %+v, want req-1 cancelled", item)
	}
}

// TS getFromApi({credentialedOrigin: config.baseURL, trustedOrigin:
// config.baseURL}) compares results_url with the provider's resolved base URL
// (anthropic-batch.ts:412-418, anthropic-provider.ts createAnthropic: the
// ANTHROPIC_BASE_URL / default URL). A provider built without Config.BaseURL
// must still send its credentials to a results_url on the API origin.
func TestR92provaBatchResultsDefaultBaseURLIsCredentialed(t *testing.T) {
	srv, results := r92provaBatchResultsServer(t)
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL+"/v1")

	r92provaReadBatchResults(t, New(Config{APIKey: "k"}))
	if key, saw := results(); !saw || key != "k" {
		t.Fatalf("results request x-api-key = %q (sent %v), want k", key, saw)
	}
}

// The bare API host is normalized to .../v1 (TS normalizeBaseURL); a
// trailing slash on the configured base URL does not change the origin.
func TestR92provaBatchResultsTrailingSlashBaseURL(t *testing.T) {
	srv, results := r92provaBatchResultsServer(t)
	r92provaReadBatchResults(t, New(Config{APIKey: "k", BaseURL: srv.URL + "/v1/"}))
	if key, _ := results(); key != "k" {
		t.Fatalf("results request x-api-key = %q, want k", key)
	}
}

type r92provaRecordingTransport struct {
	mu    sync.Mutex
	paths []string
}

func (rt *r92provaRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.paths = append(rt.paths, req.URL.Path)
	rt.mu.Unlock()
	return http.DefaultTransport.RoundTrip(req)
}

// TS sends the results download through the provider's fetch
// (anthropic-batch.ts:424 `fetch: this.options.config.fetch`): a configured
// HTTPClient transport also carries the credentialed results_url hop.
func TestR92provaBatchResultsUseConfiguredHTTPClient(t *testing.T) {
	srv, _ := r92provaBatchResultsServer(t)
	rt := &r92provaRecordingTransport{}
	r92provaReadBatchResults(t, New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", HTTPClient: &http.Client{Transport: rt}}))
	rt.mu.Lock()
	defer rt.mu.Unlock()
	found := false
	for _, p := range rt.paths {
		if p == "/v1/messages/batches/b1/results" {
			found = true
		}
	}
	if !found {
		t.Fatalf("configured transport saw %v, want the results request too", rt.paths)
	}
}

// r92provaStreamParts runs the SSE events through the Anthropic stream and
// returns the content parts (TS stream minus stream-start, response-metadata,
// custom and finish).
func r92provaStreamParts(t *testing.T, usesJSONResponseTool bool, events []string) []*provider.StreamChunk {
	t.Helper()
	var sse strings.Builder
	for _, e := range events {
		sse.WriteString("data: " + e + "\n\n")
	}
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse.String())), usesJSONResponseTool)
	parts, err := r88provaDrain(stream)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var out []*provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata, provider.ChunkTypeCustom, provider.ChunkTypeFinish:
			continue
		}
		out = append(out, p)
	}
	return out
}

// TS message_start returns on a repeated message_start of the open message
// before it clears heldBackTextByIndex (anthropic-language-model.ts:3309-3333),
// so the held-back text of a block started before the repeat keeps its
// deltas. Recorded with node on @ai-sdk/anthropic 2ebc78b654: text-start 0,
// text-delta "hello", text-end 0.
func TestR92provaRepeatedMessageStartKeepsHeldBackText(t *testing.T) {
	parts := r92provaStreamParts(t, true, []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":1}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	})
	if len(parts) != 3 || parts[0].Type != provider.ChunkTypeTextStart || parts[1].Type != provider.ChunkTypeText ||
		parts[1].Text != "hello" || parts[2].Type != provider.ChunkTypeTextEnd {
		t.Fatalf("parts = %s, want text-start, text-delta hello, text-end", r88provaJSON(parts))
	}
}

// TS anthropicCitationSchema members are z.object schemas: keys a citation
// does not declare are stripped from providerMetadata.anthropic.citations and
// from each citation source's providerMetadata (recorded with node on
// @ai-sdk/anthropic 2ebc78b654, doGenerate and doStream).
func TestR92provaCitationUndeclaredKeysStripped(t *testing.T) {
	citations := []map[string]interface{}{
		{"type": "web_search_result_location", "cited_text": "c", "url": "https://x", "title": nil, "encrypted_index": "e", "extra": 1.0},
		{"type": "char_location", "cited_text": "c", "document_index": 0.0, "document_title": nil, "start_char_index": 0.0, "end_char_index": 1.0, "future": "x"},
		{"type": "search_result_location", "cited_text": "c", "search_result_index": 0.0, "source": "s", "title": "t", "start_block_index": 0.0, "end_block_index": 1.0, "z": true},
	}
	want := `{"anthropic":{"citations":[{"cited_text":"c","encrypted_index":"e","title":null,"type":"web_search_result_location","url":"https://x"},` +
		`{"cited_text":"c","document_index":0,"document_title":null,"end_char_index":1,"start_char_index":0,"type":"char_location"},` +
		`{"cited_text":"c","end_block_index":1,"search_result_index":0,"source":"s","start_block_index":0,"title":"t","type":"search_result_location"}]}}`
	if got := string(citationsProviderMetadata(citations)); got != want {
		t.Fatalf("providerMetadata = %s\nwant %s", got, want)
	}
	wantSource := []string{
		`{"anthropic":{"cited_text":"c","encrypted_index":"e","title":null,"type":"web_search_result_location","url":"https://x"}}`,
		`{"anthropic":{"cited_text":"c","document_index":0,"document_title":null,"end_char_index":1,"start_char_index":0,"type":"char_location"}}`,
		`{"anthropic":{"cited_text":"c","end_block_index":1,"search_result_index":0,"source":"s","start_block_index":0,"title":"t","type":"search_result_location"}}`,
	}
	for i, c := range mapAnthropicCitations(citations, nil) {
		if got := string(c.Source.ProviderMetadata); got != wantSource[i] {
			t.Fatalf("citation %d source metadata = %s\nwant %s", i, got, wantSource[i])
		}
	}
}

// TS anthropicInputTransformationSchema and anthropicSafeguardResultSchema are
// z.object schemas: undeclared keys are stripped from
// providerMetadata.anthropic.inputTransformations / safeguardResults (recorded
// with node on @ai-sdk/anthropic 2ebc78b654).
func TestR92provaInputTransformationsSafeguardResultsStripped(t *testing.T) {
	const wantIT = `[{"path":"p","reason":"r","type":"t"}]`
	const wantSG = `[{"status":{"tool_uses":{"tu_1":{"explanation":null,"outcome":"o","type":"x"}},"type":"ok"},"type":"s"}]`
	check := func(name string, meta map[string]interface{}) {
		t.Helper()
		anth, _ := meta["anthropic"].(map[string]interface{})
		if got := r88provaJSON(anth["inputTransformations"]); got != wantIT {
			t.Errorf("%s inputTransformations = %s, want %s", name, got, wantIT)
		}
		if got := r88provaJSON(anth["safeguardResults"]); got != wantSG {
			t.Errorf("%s safeguardResults = %s, want %s", name, got, wantSG)
		}
	}
	const it = `[{"type":"t","path":"p","reason":"r","extra":1}]`
	const sg = `[{"type":"s","extra":2,"status":{"type":"ok","more":3,"tool_uses":{"tu_1":{"type":"x","outcome":"o","explanation":null,"z":4}}}}]`

	var response anthropicResponse
	if err := json.Unmarshal([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"text","text":"a"}],"input_transformations":`+it+`,"safeguard_results":`+sg+`}`), &response); err != nil {
		t.Fatal(err)
	}
	lm := &LanguageModel{provider: New(Config{APIKey: "k"})}
	check("generate", lm.convertResponseWithOptions(response, convertOptions{}).ProviderMetadata)

	var sse strings.Builder
	for _, e := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":1},"input_transformations":` + it + `}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","safeguard_results":` + sg + `},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	} {
		sse.WriteString("data: " + e + "\n\n")
	}
	parts, err := r88provaDrain(newAnthropicStream(io.NopCloser(strings.NewReader(sse.String())), false))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFinish {
			var meta map[string]interface{}
			if err := json.Unmarshal(p.ProviderMetadata, &meta); err != nil {
				t.Fatal(err)
			}
			check("stream", meta)
			return
		}
	}
	t.Fatal("no finish chunk")
}
