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
