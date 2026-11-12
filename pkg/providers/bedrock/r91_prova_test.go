package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r91provaDeadURL is a loopback URL nothing listens on.
func r91provaDeadURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

func r91provaCheckConnectError(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", what)
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("%s: err = %T %v, want a *ProviderError", what, err, err)
	}
	if !pe.IsRetryable() {
		t.Fatalf("%s: not retryable", what)
	}
	if !strings.Contains(err.Error(), "Cannot connect to API: ") {
		t.Fatalf("%s: err = %v, want Cannot connect to API", what, err)
	}
	if strings.Contains(err.Error(), "amazon-bedrock request failed") {
		t.Fatalf("%s: err = %v", what, err)
	}
}

// prova-02: a network failure of the raw Bedrock requests is the TS
// handleFetchError result (provider-utils handle-fetch-error.ts:42-100): a
// retryable APICallError "Cannot connect to API: <cause>".
func TestR91provaTransportErrors(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1", BaseURL: r91provaDeadURL(t)})
	ctx := context.Background()
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lm.DoGenerate(ctx, opts)
	r91provaCheckConnectError(t, "DoGenerate", err)
	_, err = lm.DoStream(ctx, opts)
	r91provaCheckConnectError(t, "DoStream", err)

	em, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = em.DoEmbed(ctx, "hi", nil)
	r91provaCheckConnectError(t, "DoEmbed", err)

	im, err := p.ImageModel("amazon.nova-canvas-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = im.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"})
	r91provaCheckConnectError(t, "image DoGenerate", err)

	rm, err := p.RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = rm.DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	r91provaCheckConnectError(t, "DoRerank", err)
}

// prova-02: a cancelled request stays the context error (TS abort errors
// are returned unchanged).
func TestR91provaTransportCanceled(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1", BaseURL: r91provaDeadURL(t)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var pe *providererrors.ProviderError
	if errors.As(err, &pe) {
		t.Fatalf("err = %v, want the context error, not a ProviderError", err)
	}
}

func r91provaArgsJSON(t *testing.T, opts *provider.GenerateOptions) string {
	t.Helper()
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1"})
	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Prompt.Text == "" && len(opts.Prompt.Messages) == 0 {
		opts.Prompt = types.Prompt{Text: "hi"}
	}
	args, err := lm.(*LanguageModel).getArgs(opts)
	if err != nil {
		t.Fatalf("getArgs: %v", err)
	}
	b, _ := json.Marshal(args.Body)
	return string(b)
}

// prova-08 / provg1-01 (bedrock site): Go-typed option literals send the same
// Converse request as the map[string]interface{} form (TS option values are
// JSON; convert-to-amazon-bedrock-chat-messages.ts:48-160).
func TestR91provaTypedOptionLiterals(t *testing.T) {
	file := func(po map[string]interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: "application/pdf", Data: []byte("%PDF-1.4"), Filename: "a.pdf", ProviderOptions: po},
		}}}}}
	}
	text := func(po map[string]interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "hi", ProviderOptions: po},
		}}}}}
	}
	cases := []struct {
		name         string
		typed, plain *provider.GenerateOptions
	}{
		{
			"requestMetadata map[string]string",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"requestMetadata": map[string]string{"k": "v"}}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"requestMetadata": map[string]interface{}{"k": "v"}}}},
		},
		{
			"reasoningConfig int64 budget",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"type": "enabled", "budgetTokens": float64(2048)}}}},
		},
		{
			"anthropicBeta []string under map[string][]string",
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"amazonBedrock": map[string][]string{"anthropicBeta": {"b1"}}}},
			&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"anthropicBeta": []interface{}{"b1"}}}},
		},
		{
			"citations map[string]bool",
			file(map[string]interface{}{"bedrock": map[string]interface{}{"citations": map[string]bool{"enabled": true}}}),
			file(map[string]interface{}{"bedrock": map[string]interface{}{"citations": map[string]interface{}{"enabled": true}}}),
		},
		{
			"part options map[string]bool",
			text(map[string]interface{}{"bedrock": map[string]bool{"guardContent": true}}),
			text(map[string]interface{}{"bedrock": map[string]interface{}{"guardContent": true}}),
		},
		{
			"cachePoint map[string]string ttl",
			text(map[string]interface{}{"bedrock": map[string]interface{}{"cachePoint": map[string]string{"type": "default", "ttl": "1h"}}}),
			text(map[string]interface{}{"bedrock": map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default", "ttl": "1h"}}}),
		},
	}
	for _, c := range cases {
		typed := r91provaArgsJSON(t, c.typed)
		plain := r91provaArgsJSON(t, c.plain)
		if typed != plain {
			t.Errorf("%s:\n typed %s\n plain %s", c.name, typed, plain)
		}
	}
	if got := r91provaArgsJSON(t, text(map[string]interface{}{"bedrock": map[string]interface{}{"cachePoint": map[string]string{"type": "default", "ttl": "1h"}}})); !strings.Contains(got, `"ttl":"1h"`) {
		t.Errorf("cachePoint ttl lost: %s", got)
	}
	if ok, err := bedrockCitationsEnabled(map[string]interface{}{"bedrock": map[string]interface{}{"citations": map[string]bool{"enabled": true}}}); err != nil || !ok {
		t.Errorf("bedrockCitationsEnabled = %v, %v", ok, err)
	}
}

// Typed-option ruling: the embedding, image and rerank option readers take
// Go-typed literals (int64, map[string]string, typed maps).
func TestR91provaTypedEmbeddingImageRerankOptions(t *testing.T) {
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1],"inputTextTokenCount":1}`))
	}))
	defer srv.Close()
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret", Region: "us-east-1", BaseURL: srv.URL})
	em, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, po := range []map[string]interface{}{
		{"amazonBedrock": map[string]interface{}{"dimensions": int64(512), "normalize": true}},
		{"amazonBedrock": map[string]interface{}{"dimensions": uint16(512), "normalize": true}},
		{"amazonBedrock": map[string]interface{}{"dimensions": float64(512), "normalize": true}},
	} {
		if _, err := em.DoEmbed(context.Background(), "hi", &provider.EmbedModelOptions{ProviderOptions: po}); err != nil {
			t.Fatalf("DoEmbed(%v): %v", po, err)
		}
	}
	for i, b := range bodies {
		if b["dimensions"] != float64(512) || b["normalize"] != true {
			t.Errorf("embed body %d = %v", i, b)
		}
	}
	bodies = nil
	if _, err := em.DoEmbed(context.Background(), "hi", &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]int64{"dimensions": 256}}}); err != nil {
		t.Fatalf("DoEmbed(map[string]int64): %v", err)
	}
	if len(bodies) != 1 || bodies[0]["dimensions"] != float64(256) {
		t.Errorf("embed body = %v, want dimensions 256", bodies)
	}
	for _, v := range []interface{}{int8(3), int16(3), uint(3), uint8(3), uint16(3), uint32(3), uint64(3), float32(3)} {
		if n, ok := intOption(v); !ok || n != 3 {
			t.Errorf("intOption(%T) = %d, %v", v, n, ok)
		}
		if f, ok := toFloat64(v); !ok || f != 3 {
			t.Errorf("toFloat64(%T) = %v, %v", v, f, ok)
		}
	}

	imageBody := func(po map[string]interface{}) string {
		body, _, err := NewImageModel(nil, "amazon.nova-canvas-v1:0").buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a cat", ProviderOptions: po})
		if err != nil {
			t.Fatalf("image buildRequestBody(%v): %v", po, err)
		}
		b, _ := json.Marshal(body)
		return string(b)
	}
	typed := imageBody(map[string]interface{}{"amazonBedrock": map[string]interface{}{"negativeText": "blurry", "cfgScale": int64(7), "quality": "premium"}})
	typed2 := imageBody(map[string]interface{}{"amazonBedrock": map[string]string{"negativeText": "blurry", "quality": "premium"}})
	plain := imageBody(map[string]interface{}{"amazonBedrock": map[string]interface{}{"negativeText": "blurry", "cfgScale": float64(7), "quality": "premium"}})
	if typed != plain || !strings.Contains(typed2, `"negativeText":"blurry"`) || !strings.Contains(typed2, `"quality":"premium"`) {
		t.Errorf("image bodies:\n typed  %s\n typed2 %s\n plain  %s", typed, typed2, plain)
	}

	rm, err := p.RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	rerankBody := func(po map[string]interface{}) string {
		body, err := rm.(*RerankingModel).buildRequestBody(&provider.RerankOptions{Query: "q", Documents: []string{"a"}, ProviderOptions: po})
		if err != nil {
			t.Fatalf("rerank buildRequestBody(%v): %v", po, err)
		}
		b, _ := json.Marshal(body)
		return string(b)
	}
	typedR := rerankBody(map[string]interface{}{"amazonBedrock": map[string]interface{}{"additionalModelRequestFields": map[string]string{"k": "v"}}})
	plainR := rerankBody(map[string]interface{}{"amazonBedrock": map[string]interface{}{"additionalModelRequestFields": map[string]interface{}{"k": "v"}}})
	if typedR != plainR || !strings.Contains(plainR, `"k":"v"`) {
		t.Errorf("rerank bodies:\n typed %s\n plain %s", typedR, plainR)
	}
}
