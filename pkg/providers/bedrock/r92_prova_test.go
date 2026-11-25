package bedrock

import (
	"bytes"
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
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// r92provaAuthServer answers Converse requests and records each request's
// Authorization header.
func r92provaAuthServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return auth
	}
}

// TS createAmazonBedrock reads AWS_BEARER_TOKEN_BEDROCK once, when the
// provider is created (loadOptionalSetting, amazon-bedrock-provider.ts:198-212),
// and picks bearer or SigV4 authentication then.
func TestR92provaBearerTokenEnvReadAtCreation(t *testing.T) {
	srv, lastAuth := r92provaAuthServer(t)
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	generate := func(p *Provider) string {
		t.Helper()
		lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
			t.Fatal(err)
		}
		return lastAuth()
	}

	p := New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "late-token")
	if auth := generate(p); !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Fatalf("Authorization = %q, want SigV4 (token set after creation)", auth)
	}
	p = New(Config{Region: "us-east-1", BaseURL: srv.URL})
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	if auth := generate(p); auth != "Bearer late-token" {
		t.Fatalf("Authorization = %q, want the token read at creation", auth)
	}
}

// TS AmazonBedrockResponseSchema / AmazonBedrockStreamSchema declare
// performanceConfig {latency} and serviceTier {type} as z.object, so other
// keys are stripped from the provider metadata (recorded with node on
// @ai-sdk/amazon-bedrock 2ebc78b654: {"performanceConfig":{"latency":
// "standard"},"serviceTier":{"type":"default"},"stopSequence":null}).
func TestR92provaPerformanceConfigServiceTierKeysStripped(t *testing.T) {
	m := NewLanguageModel(New(Config{Region: "us-east-1"}), "anthropic.claude-3-haiku-20240307-v1:0")
	result, err := m.convertConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn",`+
		`"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2},"performanceConfig":{"latency":"standard","extra":1},"serviceTier":{"type":"default","x":2}}`), false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amazonBedrock":{"performanceConfig":{"latency":"standard"},"serviceTier":{"type":"default"},"stopSequence":null},"bedrock":{"performanceConfig":{"latency":"standard"},"serviceTier":{"type":"default"},"stopSequence":null}}`
	if got := r92provaJSON(t, result.ProviderMetadata); got != want {
		t.Fatalf("generate providerMetadata = %s\nwant %s", got, want)
	}

	body := buildBedrockEventStreamBody([][2]string{
		{"messageStop", `{"stopReason":"end_turn"}`},
		{"metadata", `{"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2},"performanceConfig":{"latency":"standard","extra":1},"serviceTier":{"type":"default","x":2}}`},
	})
	s := &bedrockConverseStream{
		decoder:       eventstream.NewDecoder(bytes.NewReader(body)),
		body:          io.NopCloser(bytes.NewReader(nil)),
		contentBlocks: map[int]*bedrockStreamContentBlock{},
		finishReason:  types.FinishReasonInterrupted,
	}
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk")
	}
	streamWant := `{"amazonBedrock":{"performanceConfig":{"latency":"standard"},"serviceTier":{"type":"default"}},"bedrock":{"performanceConfig":{"latency":"standard"},"serviceTier":{"type":"default"}}}`
	if got := r92provaJSON(t, finish.ProviderMetadata); got != streamWant {
		t.Fatalf("stream providerMetadata = %s\nwant %s", got, streamWant)
	}
}

func r92provaJSON(t *testing.T, v interface{}) string {
	t.Helper()
	if raw, ok := v.(json.RawMessage); ok {
		var decoded interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		v = decoded
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS getCachePoint: `amazonBedrock?.cachePoint ?? bedrock?.cachePoint`, then
// `if (!cachePointConfig) return undefined` -- a JS-falsy cachePoint (false,
// 0, "") adds no cache point and does not fall back to the bedrock namespace.
func TestR92provaFalsyCachePoint(t *testing.T) {
	for _, v := range []interface{}{false, 0, 0.0, int64(0), ""} {
		opts := map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"cachePoint": v},
			"bedrock":       map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default"}},
		}
		if cp, ok := bedrockCachePoint(opts); ok {
			t.Errorf("cachePoint %#v: got %v, want none", v, cp)
		}
	}
	if cp, ok := bedrockCachePoint(map[string]interface{}{"amazonBedrock": map[string]interface{}{"cachePoint": 1}}); !ok || cp["type"] != "default" {
		t.Errorf("truthy cachePoint 1 = %v, %v (known difference: {type: default})", cp, ok)
	}
}

// TS `reasons.join(', ')` stringifies each moderation reason like
// Array.prototype.join: null is empty, numbers use Number::toString and
// objects are "[object Object]" (Go printed <nil>, map[...]).
func TestR92provaModerationReasonsJoin(t *testing.T) {
	m := NewImageModel(New(Config{Region: "us-east-1"}), "amazon.nova-canvas-v1:0")
	_, err := m.convertResponse([]byte(`{"status":"Request Moderated","details":{"Moderation Reasons":["a",null,1.5,true,{"k":1},[1,null,2]]}}`), http.Header{}, nil)
	const want = "Amazon Bedrock request was moderated: a, , 1.5, true, [object Object], 1,,2"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
