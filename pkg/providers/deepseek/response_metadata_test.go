package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestDeepSeekDoGenerateProviderMetadataAndResponseMetadata(t *testing.T) {
	p := newDeepseekProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"chatcmpl-1",
				"object":"chat.completion",
				"created":1700000000,
				"model":"deepseek-chat",
				"system_fingerprint":"fp_123",
				"choices":[{
					"index":0,
					"finish_reason":"stop",
					"message":{"role":"assistant","content":"ok","tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
					"logprobs":{"content":[{"token":"ok","logprob":-0.1,"bytes":[111,107],"top_logprobs":[]}]}
				}],
				"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7,"prompt_cache_hit_tokens":1}
			}`)),
		}, nil
	})
	m := NewLanguageModel(p, "deepseek-chat")

	out, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}

	if out.ResponseMetadata == nil || out.ResponseMetadata.ID != "chatcmpl-1" || out.ResponseMetadata.ModelID != "deepseek-chat" {
		t.Fatalf("ResponseMetadata = %#v", out.ResponseMetadata)
	}
	if out.ResponseMetadata.Timestamp.Unix() != 1700000000 {
		t.Fatalf("ResponseMetadata.Timestamp = %v", out.ResponseMetadata.Timestamp)
	}

	meta, ok := out.ProviderMetadata["deepseek"].(map[string]interface{})
	if !ok {
		t.Fatalf("ProviderMetadata[deepseek] missing: %#v", out.ProviderMetadata)
	}
	if meta["responseObject"] != "chat.completion" {
		t.Errorf("responseObject = %v", meta["responseObject"])
	}
	if meta["choiceIndex"] != 0 {
		t.Errorf("choiceIndex = %v", meta["choiceIndex"])
	}
	if meta["messageRole"] != "assistant" {
		t.Errorf("messageRole = %v", meta["messageRole"])
	}
	if meta["systemFingerprint"] != "fp_123" {
		t.Errorf("systemFingerprint = %v", meta["systemFingerprint"])
	}
	if meta["promptCacheHitTokens"] != 1 {
		t.Errorf("promptCacheHitTokens = %v, want 1", meta["promptCacheHitTokens"])
	}
	if _, ok := meta["promptCacheMissTokens"]; ok {
		t.Errorf("promptCacheMissTokens = %v, want absent (not in response)", meta["promptCacheMissTokens"])
	}
	toolCallTypes, ok := meta["toolCallTypes"].([]string)
	if !ok || len(toolCallTypes) != 1 || toolCallTypes[0] != "function" {
		t.Errorf("toolCallTypes = %#v", meta["toolCallTypes"])
	}
	if _, ok := meta["logprobs"]; !ok {
		t.Errorf("logprobs missing from metadata: %#v", meta)
	}

	// Usage.Raw must preserve every field from the wire object, including
	// prompt_cache_hit_tokens which the typed struct doesn't declare.
	if out.Usage.Raw["prompt_cache_hit_tokens"] != float64(1) {
		t.Errorf("usage.Raw[prompt_cache_hit_tokens] = %#v, want 1 (raw JSON decode)", out.Usage.Raw["prompt_cache_hit_tokens"])
	}
}

func TestDeepSeekDoGenerateNoChoicesReturnsInvalidResponseDataError(t *testing.T) {
	p := newDeepseekProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"id":"x","choices":[],"usage":{}}`)),
		}, nil
	})
	m := NewLanguageModel(p, "deepseek-chat")

	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !providererrors.IsInvalidResponseDataError(err) {
		t.Fatalf("error = %v (%T), want InvalidResponseDataError", err, err)
	}
}

func TestDeepSeekUsageRawClampsNegativeTextTokens(t *testing.T) {
	// reasoning_tokens (10) exceeds completion_tokens (5): text must clamp at 0.
	raw := json.RawMessage(`{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":10}}`)
	usage := convertDeepseekUsage(raw)
	if usage.OutputDetails == nil || usage.OutputDetails.TextTokens == nil {
		t.Fatalf("OutputDetails missing: %#v", usage.OutputDetails)
	}
	if *usage.OutputDetails.TextTokens != 0 {
		t.Fatalf("TextTokens = %d, want 0 (clamped)", *usage.OutputDetails.TextTokens)
	}
	// Raw must be the full decoded object, not a hand-picked subset.
	if usage.Raw["completion_tokens_details"] == nil {
		t.Fatalf("Raw missing completion_tokens_details: %#v", usage.Raw)
	}
}

func TestDeepSeekStreamFinishChunkCarriesUsageAndMetadata(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000001,"model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"system_fingerprint":"fp_stream"}

data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	var finish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk
		}
	}

	if finish == nil {
		t.Fatal("expected a finish chunk")
	}
	if finish.Usage == nil {
		t.Fatal("expected finish chunk to carry usage from the trailing usage-only chunk")
	}
	if *finish.Usage.TotalTokens != 5 {
		t.Fatalf("finish usage total = %d, want 5", *finish.Usage.TotalTokens)
	}
	if len(finish.ProviderMetadata) == 0 {
		t.Fatal("expected finish chunk to carry providerMetadata")
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatalf("unmarshal providerMetadata: %v", err)
	}
	if meta["deepseek"]["systemFingerprint"] != "fp_stream" {
		t.Fatalf("systemFingerprint = %v", meta["deepseek"]["systemFingerprint"])
	}
	if meta["deepseek"]["messageRole"] != "assistant" {
		t.Fatalf("messageRole = %v", meta["deepseek"]["messageRole"])
	}
}
