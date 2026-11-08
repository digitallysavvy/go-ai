package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR88provgAuthTransportRedirectHopGetsNoCredentials covers the
// credential transport class sweep (S2): the gateway transport runs once per
// redirect hop, so a redirect to another origin must not receive the bearer
// token or the auth-method header, and the resolver is not called for it.
func TestR88provgAuthTransportRedirectHopGetsNoCredentials(t *testing.T) {
	var mu sync.Mutex
	var foreignAuth, foreignMethod string
	foreignHits := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		foreignHits++
		foreignAuth = r.Header.Get("Authorization")
		foreignMethod = r.Header.Get("ai-gateway-auth-method")
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer foreign.Close()
	var apiAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiAuth = r.Header.Get("Authorization")
		mu.Unlock()
		http.Redirect(w, r, foreign.URL+"/x", http.StatusTemporaryRedirect)
	}))
	defer api.Close()

	resolverCalls := 0
	client := newGatewayHTTPClient(nil, func(context.Context) (string, string, error) {
		resolverCalls++
		return "gw-key", "api-key", nil
	})
	resp, err := client.Get(api.URL + "/v4/ai/config")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if apiAuth != "Bearer gw-key" {
		t.Fatalf("API hop Authorization = %q", apiAuth)
	}
	if foreignHits != 1 || foreignAuth != "" || foreignMethod != "" {
		t.Fatalf("foreign hop: hits=%d Authorization=%q auth-method=%q, want 1 hit and no credentials", foreignHits, foreignAuth, foreignMethod)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolverCalls)
	}
}

// cr-r88-stream-to-provg 1 (TS 9754014925): a forwarded {"type":"ping"}
// part is a ChunkTypePing chunk, not dropped.
func TestR88provgGatewayStreamForwardsPing(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"stream-start","warnings":[]}`, "",
			`data: {"type":"ping"}`, "",
			`data: {"type":"finish","finishReason":"stop"}`, "", "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	var types []provider.ChunkType
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		types = append(types, c.Type)
	}
	pings := 0
	for _, ty := range types {
		if ty == provider.ChunkTypePing {
			pings++
		}
	}
	if pings != 1 {
		t.Fatalf("chunk types = %v, want one ping", types)
	}
}

// Ports the gateway-generation-info.test.ts cost breakdown cases (TS
// 32acf432db): marketCost / surchargeCost / gatewayCost are decoded from the
// snake_case fields, zero is kept, and null or absent leaves them unset.
func TestR88provgGenerationInfoCostBreakdown(t *testing.T) {
	const base = `"id":"gen_1","total_cost":0.00123,"upstream_inference_cost":0.0011,"usage":0.00123,"created_at":"2024-01-01T00:00:00.000Z","model":"m","is_byok":false,"provider_name":"anthropic","streamed":false,"finish_reason":"stop","latency":1,"generation_time":1,"native_tokens_prompt":1,"native_tokens_completion":1,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"billable_web_search_calls":0`
	get := func(extra string) *GenerationInfo {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{`+base+extra+`}}`)
		}))
		defer srv.Close()
		p, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v3/ai"})
		if err != nil {
			t.Fatal(err)
		}
		info, err := p.GetGenerationInfo(context.Background(), GenerationInfoParams{ID: "gen_1"})
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	info := get(`,"market_cost":0.0011,"surcharge_cost":0.00013,"gateway_cost":0.00123`)
	if info.MarketCost == nil || *info.MarketCost != 0.0011 || *info.SurchargeCost != 0.00013 || *info.GatewayCost != 0.00123 {
		t.Fatalf("costs = %v %v %v", info.MarketCost, info.SurchargeCost, info.GatewayCost)
	}
	b, _ := json.Marshal(info)
	if !strings.Contains(string(b), `"marketCost":0.0011,"surchargeCost":0.00013,"gatewayCost":0.00123`) {
		t.Fatalf("json = %s", b)
	}
	info = get(`,"market_cost":0,"surcharge_cost":0,"gateway_cost":0`)
	if info.MarketCost == nil || *info.MarketCost != 0 || info.SurchargeCost == nil || info.GatewayCost == nil {
		t.Fatal("zero costs must be kept")
	}
	for _, extra := range []string{``, `,"market_cost":null,"surcharge_cost":null,"gateway_cost":null`} {
		info = get(extra)
		if info.MarketCost != nil || info.SurchargeCost != nil || info.GatewayCost != nil || info.TotalCost != 0.00123 {
			t.Fatalf("%q: costs = %+v", extra, info)
		}
		if b, _ := json.Marshal(info); strings.Contains(string(b), "marketCost") {
			t.Fatalf("json = %s", b)
		}
	}
}

// Ports the gateway-transcription-model.test.ts word timing and shared
// timestampGranularities cases (TS 41465fd1b6): words are passed through
// (nullish), and a non-empty granularity request warns without changing the
// request body.
func TestR88provgTranscriptionWordsAndGranularityWarning(t *testing.T) {
	run := func(respWords string, granularities []string) (*types.TranscriptionResult, map[string]interface{}) {
		t.Helper()
		var mu sync.Mutex
		var sent map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(b, &sent)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"text":"Hello world","segments":[]`+respWords+`}`)
		}))
		defer srv.Close()
		p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.TranscriptionModel("openai/whisper-1")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1, 2}, MimeType: "audio/wav", TimestampGranularities: granularities})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return res, sent
	}
	res, body := run(`,"words":[{"text":"Hello","startSecond":0,"endSecond":0.5},{"text":"world","startSecond":0.5,"endSecond":1}]`, []string{"word"})
	if len(res.Words) != 2 || res.Words[1] != (types.TranscriptionWord{Text: "world", StartSecond: 0.5, EndSecond: 1}) {
		t.Fatalf("words = %+v", res.Words)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Feature != "timestampGranularities" || res.Warnings[0].Details != "The AI Gateway adapter does not forward the shared timestampGranularities option." {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if _, ok := body["timestampGranularities"]; ok {
		t.Fatalf("request body = %v", body)
	}
	for _, words := range []string{``, `,"words":null`} {
		for _, g := range [][]string{nil, {}} {
			res, _ = run(words, g)
			if res.Words != nil || len(res.Warnings) != 0 {
				t.Fatalf("%q %v: words %+v warnings %+v", words, g, res.Words, res.Warnings)
			}
		}
	}
}

// Ports the gateway-realtime-model.test.ts server connection tests (TS
// 892af4dab7): the realtime-model route with the HTTP auth headers (OIDC
// included), team and custom headers, conversation intent only.
func TestR88provgRealtimeServerConnection(t *testing.T) {
	p, err := New(Config{
		OIDCTokenFunc: func(context.Context) (string, error) { return "oidc-test-token", nil },
		TeamIDOrSlug:  "team-example",
		Headers:       map[string]string{"x-custom": "example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_GATEWAY_API_KEY", "")
	m := p.ExperimentalRealtime("openai/gpt-realtime")
	cfg, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "wss://ai-gateway.vercel.sh/v4/ai/realtime-model?ai-model-id=openai%2Fgpt-realtime" {
		t.Fatalf("url = %s", cfg.URL)
	}
	for k, want := range map[string]string{
		"authorization":               "Bearer oidc-test-token",
		"ai-gateway-auth-method":      "oidc",
		"ai-gateway-protocol-version": AIGatewayProtocolVersion,
		"x-vercel-ai-gateway-team":    "team-example",
		"x-custom":                    "example",
	} {
		if cfg.Headers[k] != want {
			t.Fatalf("header %s = %q, want %q (all %v)", k, cfg.Headers[k], want, cfg.Headers)
		}
	}
	if !strings.Contains(cfg.Headers["user-agent"], "ai-sdk-gateway/") {
		t.Fatalf("user-agent = %q", cfg.Headers["user-agent"])
	}
	const want = "AI Gateway realtime does not support the 'transcription' session intent on the normalized realtime route."
	if _, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: provider.RealtimeSessionIntentTranscription}); err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.SerializeClientEventForIntent(provider.RealtimeClientEvent{Type: "response-create"}, provider.RealtimeSessionIntentTranscription); err == nil || err.Error() != want {
		t.Fatalf("serialize err = %v", err)
	}
	if _, err := m.BuildSessionConfigForIntent(provider.RealtimeSessionConfig{}, provider.RealtimeSessionIntentTranscription); err == nil || err.Error() != want {
		t.Fatalf("build err = %v", err)
	}
}
