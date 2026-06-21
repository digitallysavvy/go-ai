package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	bedrock "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

func TestBedrockAnthropicProviderSurface(t *testing.T) {
	p := New(Config{})
	if p.Name() != "bedrock-anthropic" {
		t.Fatalf("name mismatch: %s", p.Name())
	}
	lmAny, err := p.LanguageModel("anthropic.claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	lm := lmAny.(*BedrockAnthropicLanguageModel)
	if lm.SpecificationVersion() != "v4" || lm.Provider() != "bedrock-anthropic" || lm.ModelID() != "anthropic.claude-3-5-sonnet" {
		t.Fatalf("model metadata mismatch")
	}
	if !lm.SupportsTools() || lm.SupportsStructuredOutput() || !lm.SupportsImageInput() {
		t.Fatalf("capabilities mismatch")
	}
	if _, err := p.EmbeddingModel("x"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("EmbeddingModel error = %v, want ErrModelNotFound", err)
	}
	if _, err := p.ImageModel("x"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("ImageModel error = %v, want ErrModelNotFound", err)
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking")
	}
}

func TestBedrockAnthropicBuildRequestBodyAndCachePoints(t *testing.T) {
	ttl := CacheTTL1Hour
	p := New(Config{
		BearerToken: "tok",
		CacheConfig: NewCacheConfig(
			WithCacheTTL(ttl),
			WithSystemCache(),
			WithToolCache(),
			WithMessageCacheIndices(0),
		),
	})
	m := &BedrockAnthropicLanguageModel{provider: p, modelID: "anthropic.claude"}
	maxTokens := 321
	temp := 0.5
	topP := 0.8
	topK := 20
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "sys",
			Messages: []types.Message{
				{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "hello"}},
				},
			},
		},
		MaxTokens:     &maxTokens,
		Temperature:   &temp,
		TopP:          &topP,
		TopK:          &topK,
		StopSequences: []string{"END"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: map[string]interface{}{"type": "object"},
		},
		Tools: []types.Tool{
			{Name: "bash_20241022", Description: "bash", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	}, true)

	if body["anthropic_version"] != AnthropicVersion || body["max_tokens"] != 321 {
		t.Fatalf("request basics mismatch: %#v", body)
	}
	if body["temperature"] != temp || body["top_p"] != topP || body["top_k"] != topK {
		t.Fatalf("sampling mismatch: %#v", body)
	}
	if _, ok := body["output_config"].(map[string]interface{}); !ok {
		t.Fatalf("missing output_config: %#v", body["output_config"])
	}
	if _, ok := body["anthropic_beta"].([]string); !ok {
		t.Fatalf("missing anthropic_beta: %#v", body["anthropic_beta"])
	}
	if _, ok := body["tools"].([]interface{}); !ok {
		t.Fatalf("tools should include cachePoint suffix: %#v", body["tools"])
	}
	if _, ok := body["tool_choice"].(map[string]interface{}); !ok {
		t.Fatalf("tool_choice mismatch: %#v", body["tool_choice"])
	}
	if systemBlocks, ok := body["system"].([]interface{}); !ok || len(systemBlocks) == 0 {
		t.Fatalf("expected cached system blocks: %#v", body["system"])
	}
}

func TestBedrockAnthropicAuthenticateAndErrorHandling(t *testing.T) {
	mBearer := &BedrockAnthropicLanguageModel{
		provider: &BedrockAnthropicProvider{bearerToken: "tok"},
	}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err := mBearer.authenticateRequest(req, nil); err != nil {
		t.Fatalf("authenticateRequest bearer error = %v", err)
	}
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("missing bearer auth header")
	}

	mNoCreds := &BedrockAnthropicLanguageModel{
		provider: &BedrockAnthropicProvider{},
	}
	req2, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err := mNoCreds.authenticateRequest(req2, nil); err == nil {
		t.Fatal("expected credentials error")
	}

	errJSON := mBearer.handleErrorResponse(&http.Response{
		StatusCode: 403,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"forbidden","message":"denied"}}`)),
	})
	if errJSON == nil || !strings.Contains(errJSON.Error(), "denied") {
		t.Fatalf("expected parsed JSON error, got %v", errJSON)
	}

	errPlain := mBearer.handleErrorResponse(&http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader(`not-json`)),
	})
	if errPlain == nil || !strings.Contains(errPlain.Error(), "HTTP 500") {
		t.Fatalf("expected fallback HTTP error, got %v", errPlain)
	}
}

func TestBedrockAnthropicProviderEnvFallbacksMatchTS(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-2")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", " env-bearer ")

	p := New(Config{})
	if p.region != "us-west-2" {
		t.Fatalf("region should prefer AWS_REGION, got %q", p.region)
	}
	if p.bearerToken != "env-bearer" {
		t.Fatalf("bearer token env fallback mismatch: %q", p.bearerToken)
	}
	if p.baseURL != fmt.Sprintf(BaseURLFormat, "us-west-2") {
		t.Fatalf("base URL should derive from env region, got %q", p.baseURL)
	}
}

func TestBedrockAnthropicRequiresRegionLikeTS(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-2")

	p := New(Config{BearerToken: "tok"})
	if p.region != "" || p.baseURL != "" {
		t.Fatalf("provider should not fall back to AWS_DEFAULT_REGION, region=%q baseURL=%q", p.region, p.baseURL)
	}
	if _, err := p.runtimeBaseURL(); err == nil {
		t.Fatal("expected missing region error")
	}
}

func TestBedrockAnthropicCredentialProviderWinsAndIsDynamic(t *testing.T) {
	calls := 0
	p := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     "static-akid",
			SecretAccessKey: "static-secret",
		},
		CredentialProvider: func(ctx context.Context) (bedrock.Credentials, error) {
			calls++
			return bedrock.Credentials{
				AccessKeyID:     fmt.Sprintf("dynamic-akid-%d", calls),
				SecretAccessKey: "dynamic-secret",
				SessionToken:    "dynamic-session",
			}, nil
		},
	})

	first, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("first resolveCredentials error = %v", err)
	}
	second, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("second resolveCredentials error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("credential provider should be called once per request, got %d", calls)
	}
	if first.AccessKeyID != "dynamic-akid-1" || second.AccessKeyID != "dynamic-akid-2" {
		t.Fatalf("dynamic credentials not used: first=%#v second=%#v", first, second)
	}
	if first.SessionToken != "dynamic-session" || second.SecretAccessKey != "dynamic-secret" {
		t.Fatalf("dynamic credential fields not preserved: first=%#v second=%#v", first, second)
	}
}

func TestBedrockAnthropicStaticCredentialsUseEnvSessionTokenLikeTS(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	p := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     "akid",
			SecretAccessKey: "secret",
		},
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials error = %v", err)
	}
	if creds.SessionToken != "env-session" {
		t.Fatalf("session token should fall back to env like TS, got %q", creds.SessionToken)
	}
}

func TestBedrockAnthropicBearerTokenWinsOverCredentialProvider(t *testing.T) {
	calls := 0
	p := New(Config{
		BearerToken: "tok",
		CredentialProvider: func(ctx context.Context) (bedrock.Credentials, error) {
			calls++
			return bedrock.Credentials{AccessKeyID: "akid", SecretAccessKey: "secret"}, nil
		},
	})
	m := &BedrockAnthropicLanguageModel{provider: p}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err := m.authenticateRequest(req, nil); err != nil {
		t.Fatalf("authenticateRequest bearer error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("credential provider should not run when bearer token is configured, got %d calls", calls)
	}
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("missing bearer auth header")
	}
}

func TestBedrockAnthropicDoGenerateAndDoStream(t *testing.T) {
	var seenAuth string
	var seenPath string
	var seenAccept string
	rt := bedrockAnthropicRoundTripper(func(req *http.Request) (*http.Response, error) {
		seenAuth = req.Header.Get("Authorization")
		seenPath = req.URL.Path
		seenAccept = req.Header.Get("Accept")

		if strings.Contains(req.URL.Path, "invoke-with-response-stream") {
			chunkEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`
			chunkPayload := map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(chunkEvent))}
			chunkPayloadJSON, _ := json.Marshal(chunkPayload)
			msg1 := buildEventStreamMessage("event", "chunk", string(chunkPayloadJSON))
			msg2 := buildEventStreamMessage("event", "messageStop", "{}")
			var buf strings.Builder
			buf.Write(msg1)
			buf.Write(msg2)
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(buf.String())),
			}, nil
		}

		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"m1",
				"type":"message",
				"role":"assistant",
				"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"tc1","name":"lookup","input":{"x":1}}],
				"stop_reason":"tool_use",
				"usage":{"input_tokens":2,"output_tokens":3,"cache_creation_input_tokens":1,"cache_read_input_tokens":1}
			}`)),
		}, nil
	})

	p := New(Config{
		BearerToken: "tok",
		BaseURL:     "https://bedrock.example",
		HTTPClient:  &http.Client{Transport: rt},
	})
	modelAny, _ := p.LanguageModel("anthropic.claude")
	m := modelAny.(*BedrockAnthropicLanguageModel)

	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenAuth != "Bearer tok" || !strings.Contains(seenPath, "/model/anthropic.claude/invoke") {
		t.Fatalf("request auth/path mismatch auth=%q path=%q", seenAuth, seenPath)
	}
	if res.Text != "ok" || res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 {
		t.Fatalf("generate result mismatch: %#v", res)
	}
	if res.Usage.InputDetails == nil || res.Usage.InputDetails.CacheWriteTokens == nil || res.Usage.InputDetails.CacheReadTokens == nil {
		t.Fatalf("usage cache details missing: %#v", res.Usage)
	}

	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	ch1, err := stream.Next()
	if err != nil || ch1.Type != provider.ChunkTypeText || ch1.Text != "hello" {
		t.Fatalf("stream text mismatch chunk=%#v err=%v", ch1, err)
	}
	if seenAccept != "application/vnd.amazon.eventstream" {
		t.Fatalf("stream accept header mismatch: %s", seenAccept)
	}
}

func TestBedrockAnthropicConvertResponseAndMessageCachePoints(t *testing.T) {
	m := &BedrockAnthropicLanguageModel{provider: New(Config{}), modelID: "m"}
	out := m.convertResponse(anthropicResponse{
		Content: []anthropicContent{
			{Type: "text", Text: "first"},
			{Type: "text", Text: "second"},
			{Type: "tool_use", ID: "tc", Name: "lookup", Input: map[string]interface{}{"a": 1}},
		},
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheCreationInputTokens: 2,
			CacheReadInputTokens:     1,
		},
	}, map[string]interface{}{"k": "v"})
	if out.Text != "first" || out.FinishReason != types.FinishReasonStop || len(out.ToolCalls) != 1 {
		t.Fatalf("convert response mismatch: %#v", out)
	}
	if out.Usage.InputDetails == nil || out.Usage.InputDetails.CacheReadTokens == nil {
		t.Fatalf("usage details mismatch: %#v", out.Usage)
	}

	cfg := NewCacheConfig(WithMessageCacheIndices(0, 999))
	msgs := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}
	got := m.insertMessageCachePoints(msgs, cfg)
	content, ok := got[0]["content"].([]interface{})
	if !ok || len(content) < 2 {
		t.Fatalf("expected cached content blocks, got %#v", got[0]["content"])
	}
}

type bedrockAnthropicRoundTripper func(*http.Request) (*http.Response, error)

func (f bedrockAnthropicRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
