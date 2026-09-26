package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func newTestBedrockModel() *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, "anthropic.claude-3-haiku-20240307-v1:0")
}

func newTestBedrockModelWithID(modelID string) *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, modelID)
}

func TestResolveCredentialsConfigWinsOverEnv(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	p := New(Config{
		AWSAccessKeyID:     "config-key",
		AWSSecretAccessKey: "config-secret",
		SessionToken:       "config-session",
		Region:             "us-east-1",
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "config-key" || creds.SecretAccessKey != "config-secret" || creds.SessionToken != "config-session" {
		t.Fatalf("creds = %#v, want config credentials", creds)
	}
}

func TestResolveCredentialsProviderWinsOverStaticCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	calls := 0
	p := New(Config{
		AWSAccessKeyID:     "static-key",
		AWSSecretAccessKey: "static-secret",
		SessionToken:       "static-session",
		Region:             "us-east-1",
		CredentialProvider: func(ctx context.Context) (Credentials, error) {
			calls++
			return Credentials{
				AccessKeyID:     "dynamic-key",
				SecretAccessKey: "dynamic-secret",
				SessionToken:    "dynamic-session",
			}, nil
		},
	})

	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if calls != 1 {
		t.Fatalf("credential provider calls = %d, want 1", calls)
	}
	if creds.AccessKeyID != "dynamic-key" || creds.SecretAccessKey != "dynamic-secret" || creds.SessionToken != "dynamic-session" {
		t.Fatalf("creds = %#v, want dynamic credentials", creds)
	}
}

func TestResolveCredentialsProviderIsCalledEachTimeLikeTS(t *testing.T) {
	calls := 0
	p := New(Config{
		Region: "us-east-1",
		CredentialProvider: func(ctx context.Context) (Credentials, error) {
			calls++
			return Credentials{
				AccessKeyID:     fmt.Sprintf("dynamic-key-%d", calls),
				SecretAccessKey: "dynamic-secret",
			}, nil
		},
	})

	first, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("first resolveCredentials: %v", err)
	}
	second, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("second resolveCredentials: %v", err)
	}
	if calls != 2 {
		t.Fatalf("credential provider calls = %d, want 2", calls)
	}
	if first.AccessKeyID != "dynamic-key-1" || second.AccessKeyID != "dynamic-key-2" {
		t.Fatalf("credentials = %#v then %#v, want fresh provider values", first, second)
	}
}

func TestNewUsesAWSRegionLikeTS(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")

	p := New(Config{APIKey: "bearer"})
	if p.Region() != "us-west-2" {
		t.Fatalf("Region = %q, want AWS_REGION", p.Region())
	}
	baseURL, err := p.runtimeBaseURL()
	if err != nil {
		t.Fatalf("runtimeBaseURL error = %v", err)
	}
	if baseURL != "https://bedrock-runtime.us-west-2.amazonaws.com" {
		t.Fatalf("baseURL = %q, want AWS_REGION endpoint", baseURL)
	}
}

func TestRuntimeBaseURLRequiresRegionLikeTS(t *testing.T) {
	t.Setenv("AWS_REGION", "")

	p := New(Config{APIKey: "bearer"})
	if _, err := p.runtimeBaseURL(); err == nil {
		t.Fatal("expected missing region error")
	}
}

func TestAuthenticateRequestAPIKeyWinsOverSigV4LikeTS(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", " env-bearer ")

	calls := 0
	p := New(Config{
		Region:             "us-east-1",
		AWSAccessKeyID:     "static-key",
		AWSSecretAccessKey: "static-secret",
		CredentialProvider: func(ctx context.Context) (Credentials, error) {
			calls++
			return Credentials{AccessKeyID: "dynamic-key", SecretAccessKey: "dynamic-secret"}, nil
		},
	})
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	if err := p.authenticateRequest(context.Background(), req, []byte("{}")); err != nil {
		t.Fatalf("authenticateRequest error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0 with bearer auth", calls)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer env-bearer" {
		t.Fatalf("Authorization = %q, want bearer env token", got)
	}
	if req.Header.Get("X-Amz-Date") != "" {
		t.Fatalf("SigV4 headers should not be set when bearer auth is used")
	}
}

func TestResolveCredentialsConfigDoesNotUseEnvSessionToken(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	p := New(Config{
		AWSAccessKeyID:     "config-key",
		AWSSecretAccessKey: "config-secret",
		Region:             "us-east-1",
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "config-key" || creds.SecretAccessKey != "config-secret" {
		t.Fatalf("creds = %#v, want config credentials", creds)
	}
	if creds.SessionToken != "" {
		t.Fatalf("SessionToken = %q, want empty", creds.SessionToken)
	}
}

func TestResolveCredentialsMixesConfigAndEnvLikeTS(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	p := New(Config{
		AWSAccessKeyID: "config-key",
		Region:         "us-east-1",
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "config-key" || creds.SecretAccessKey != "env-secret" || creds.SessionToken != "env-session" {
		t.Fatalf("creds = %#v, want mixed config/env credentials", creds)
	}
}

func TestResolveCredentialsExplicitSessionTokenWins(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	p := New(Config{
		AWSAccessKeyID:     "config-key",
		AWSSecretAccessKey: "config-secret",
		SessionToken:       "config-session",
		Region:             "us-east-1",
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "config-key" || creds.SecretAccessKey != "config-secret" || creds.SessionToken != "config-session" {
		t.Fatalf("creds = %#v, want explicit config credentials", creds)
	}
}

func TestResolveCredentialsSharedFile(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	data := "[default]\naws_access_key_id = default-key\naws_secret_access_key = default-secret\n\n[custom]\naws_access_key_id = shared-key\naws_secret_access_key = shared-secret\naws_session_token = shared-session\n"
	if err := os.WriteFile(credsPath, []byte(data), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	p := New(Config{
		Region:                "us-east-1",
		SharedCredentialsFile: credsPath,
		Profile:               "custom",
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "shared-key" || creds.SecretAccessKey != "shared-secret" || creds.SessionToken != "shared-session" {
		t.Fatalf("creds = %#v, want shared-file credentials", creds)
	}
}

func TestAWSSignerExplicitKeysDoNotUseEnvSessionToken(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "env-session-token")

	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	signer := NewAWSSigner("explicit-key", "explicit-secret", "", "us-east-1")
	if err := signer.SignRequest(req, []byte("{}")); err != nil {
		t.Fatalf("SignRequest error = %v", err)
	}
	if got := req.Header.Get("X-Amz-Security-Token"); got != "" {
		t.Fatalf("X-Amz-Security-Token = %q, want empty despite AWS_SESSION_TOKEN=%q", got, os.Getenv("AWS_SESSION_TOKEN"))
	}
}

// ─── Converse rewrite (WG-B1) ────────────────────────────────────────────────

func newHTTPTestBedrockModel(t *testing.T, server *httptest.Server, modelID string) *LanguageModel {
	t.Helper()
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
		BaseURL:            server.URL,
	})
	return NewLanguageModel(p, modelID)
}

func TestDoGenerate_PostsToConverseEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("x-amzn-requestid", "req-123")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"output": {"message": {"role": "assistant", "content": [{"text": "hello"}]}},
			"stopReason": "end_turn",
			"usage": {"inputTokens": 3, "outputTokens": 5, "totalTokens": 8}
		}`))
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotPath != "/model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/converse" {
		t.Fatalf("request path = %q, want the encoded /converse endpoint", gotPath)
	}
	if res.Text != "hello" {
		t.Fatalf("Text = %q, want hello", res.Text)
	}
	if res.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %v, want stop", res.FinishReason)
	}
	messages, _ := gotBody["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 user message", gotBody["messages"])
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.ID != "req-123" {
		t.Fatalf("ResponseMetadata = %#v, want request ID from header", res.ResponseMetadata)
	}
}

func TestDoGenerate_ParsesToolCallsReasoningAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"output": {"message": {"role": "assistant", "content": [
				{"reasoningContent": {"reasoningText": {"text": "thinking...", "signature": "sig123"}}},
				{"toolUse": {"toolUseId": "tool_1", "name": "get_weather", "input": {"city": "SF"}}}
			]}},
			"stopReason": "tool_use",
			"usage": {"inputTokens": 10, "outputTokens": 4, "totalTokens": 14, "cacheReadInputTokens": 2, "cacheWriteInputTokens": 1}
		}`))
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-opus-5")
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "weather?"},
		Tools:  []types.Tool{{Type: types.ToolTypeFunction, Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", res.FinishReason)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "get_weather" || res.ToolCalls[0].ID != "tool_1" {
		t.Fatalf("ToolCalls = %#v", res.ToolCalls)
	}
	if res.ToolCalls[0].Arguments["city"] != "SF" {
		t.Fatalf("tool call arguments = %#v", res.ToolCalls[0].Arguments)
	}
	if len(res.Content) != 1 {
		t.Fatalf("Content = %#v, want one reasoning part", res.Content)
	}
	reasoning, ok := res.Content[0].(types.ReasoningContent)
	if !ok || reasoning.Text != "thinking..." || reasoning.Signature != "sig123" {
		t.Fatalf("reasoning content = %#v", res.Content[0])
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 13 { // 10 + 2 + 1
		t.Fatalf("Usage.InputTokens = %v, want 13", res.Usage.InputTokens)
	}
	if res.Usage.InputDetails == nil || *res.Usage.InputDetails.CacheReadTokens != 2 || *res.Usage.InputDetails.CacheWriteTokens != 1 {
		t.Fatalf("Usage.InputDetails = %#v", res.Usage.InputDetails)
	}
}

func TestDoGenerate_ErrorResponseUsesTypeAndMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"model not found","type":"ValidationException"}`))
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ValidationException: model not found") {
		t.Fatalf("error = %v, want %q", err, "ValidationException: model not found")
	}
}

func TestDoStream_EmitsTextToolCallAndFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Hi "}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"there"}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"contentBlockStart", `{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"t1","name":"search"}}}`},
			{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"q\":"}}}`},
			{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"cats\"}"}}}`},
			{"contentBlockStop", `{"contentBlockIndex":1}`},
			{"messageStop", `{"stopReason":"tool_use"}`},
			{"metadata", `{"usage":{"inputTokens":5,"outputTokens":6,"totalTokens":11}}`},
		})
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Type: types.ToolTypeFunction, Name: "search", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var text strings.Builder
	var sawToolInputStart, sawToolInputDelta, sawToolInputEnd bool
	var toolCall *types.ToolCall
	var finishReason types.FinishReason
	var finishUsage *types.Usage
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeText:
			text.WriteString(chunk.Text)
		case provider.ChunkTypeToolInputStart:
			sawToolInputStart = true
		case provider.ChunkTypeToolInputDelta:
			sawToolInputDelta = true
		case provider.ChunkTypeToolInputEnd:
			sawToolInputEnd = true
		case provider.ChunkTypeToolCall:
			toolCall = chunk.ToolCall
		case provider.ChunkTypeFinish:
			finishReason = chunk.FinishReason
			finishUsage = chunk.Usage
		}
	}
	if text.String() != "Hi there" {
		t.Fatalf("text = %q, want %q", text.String(), "Hi there")
	}
	if !sawToolInputStart || !sawToolInputDelta || !sawToolInputEnd {
		t.Fatalf("missing tool-input lifecycle chunks: start=%v delta=%v end=%v", sawToolInputStart, sawToolInputDelta, sawToolInputEnd)
	}
	if toolCall == nil || toolCall.ToolName != "search" || toolCall.ID != "t1" {
		t.Fatalf("toolCall = %#v", toolCall)
	}
	if toolCall.Arguments["q"] != "cats" {
		t.Fatalf("toolCall.Arguments = %#v", toolCall.Arguments)
	}
	if finishReason != types.FinishReasonToolCalls {
		t.Fatalf("finishReason = %v, want tool-calls", finishReason)
	}
	if finishUsage == nil || finishUsage.OutputTokens == nil || *finishUsage.OutputTokens != 6 {
		t.Fatalf("finish usage = %#v", finishUsage)
	}
}

func TestDoStream_SurfacesModeledException(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frame := buildBedrockEventFrame(map[string]string{
			":message-type":   "exception",
			":exception-type": "serviceUnavailableException",
		}, []byte(`{"message":"overloaded"}`))
		w.WriteHeader(200)
		_, _ = w.Write(frame)
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck

	sawError := false
	for i := 0; i < 10; i++ {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeError {
			sawError = true
			if !strings.Contains(chunk.Text, "overloaded") {
				t.Fatalf("error text = %q, want to mention 'overloaded'", chunk.Text)
			}
		}
	}
	if !sawError {
		t.Fatal("expected an error chunk for the modeled exception")
	}

	// stream.Err() should now expose structured detail (status code + the
	// exception type as ErrorCode), not just the bare chunk text — mirrors TS
	// getAmazonBedrockStreamErrorMetadata.
	streamErr := stream.Err()
	var providerErr *providererrors.ProviderError
	if !errors.As(streamErr, &providerErr) {
		t.Fatalf("Err() = %v (%T), want a *providererrors.ProviderError", streamErr, streamErr)
	}
	if providerErr.StatusCode != 503 {
		t.Fatalf("StatusCode = %d, want 503 for serviceUnavailableException", providerErr.StatusCode)
	}
	if providerErr.ErrorCode != "serviceUnavailableException" {
		t.Fatalf("ErrorCode = %q, want serviceUnavailableException", providerErr.ErrorCode)
	}
	if !providerErr.IsRetryable() {
		t.Fatal("expected serviceUnavailableException (503) to be retryable")
	}
}

func TestBedrockStreamErrorMetadata_MatchesTS(t *testing.T) {
	tests := []struct {
		exceptionType   string
		wantStatusCode  int
		wantIsRetryable bool
	}{
		{"internalServerException", 500, true},
		{"modelStreamErrorException", 424, true},
		{"serviceUnavailableException", 503, true},
		{"throttlingException", 429, true},
		{"validationException", 400, false},
		{"somethingUnknown", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.exceptionType, func(t *testing.T) {
			statusCode, isRetryable := bedrockStreamErrorMetadata(tt.exceptionType)
			if statusCode != tt.wantStatusCode || isRetryable != tt.wantIsRetryable {
				t.Fatalf("bedrockStreamErrorMetadata(%q) = (%d, %v), want (%d, %v)", tt.exceptionType, statusCode, isRetryable, tt.wantStatusCode, tt.wantIsRetryable)
			}
		})
	}
}

func TestConverseURL_EncodesApplicationInferenceProfileARN(t *testing.T) {
	arn := "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc-123"
	model := newBedrockModelWithID(arn)
	reqURL, rawPath, err := model.converseURL("https://bedrock-runtime.us-east-1.amazonaws.com", "/converse")
	if err != nil {
		t.Fatalf("converseURL error = %v", err)
	}
	wantEncoded := "arn%3Aaws%3Abedrock%3Aus-east-1%3A123456789012%3Aapplication-inference-profile%2Fabc-123"
	wantPath := "/model/" + wantEncoded + "/converse"
	if rawPath != wantPath {
		t.Fatalf("rawPath = %q, want %q", rawPath, wantPath)
	}
	if reqURL.Opaque != wantPath {
		t.Fatalf("reqURL.Opaque = %q, want %q", reqURL.Opaque, wantPath)
	}
}

func TestConverseURL_LiteralPathSurvivesOnTheWire(t *testing.T) {
	var gotRequestURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.URL.RequestURI()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
	}))
	defer server.Close()

	modelID := "us.anthropic.claude-v2:1"
	model := newHTTPTestBedrockModel(t, server, modelID)
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	want := "/model/us.anthropic.claude-v2%3A1/converse"
	if gotRequestURI != want {
		t.Fatalf("server saw request-target %q, want %q", gotRequestURI, want)
	}
}

func TestAWSSigner_DoubleEncodesOpaquePathForCanonicalRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.URL.Opaque = "/model/anthropic.claude-v2%3A1/converse"
	req.Header.Set("Content-Type", "application/json")

	signer := NewAWSSigner("key", "secret", "", "us-east-1")
	canonical := signer.buildCanonicalRequest(req, []byte("{}"))
	wantURI := "/model/anthropic.claude-v2%253A1/converse"
	if !strings.Contains(canonical, wantURI) {
		t.Fatalf("canonical request = %q, want it to contain double-encoded URI %q", canonical, wantURI)
	}
}

func TestPrepareTools_FiltersUnsupportedWebToolsWithWarning(t *testing.T) {
	// Anthropic provider tools are identified by a namespaced Name
	// ("anthropic.<tool>"), matching pkg/providers/anthropic/tools's factories
	// (e.g. anthropictools.AnthropicTools.WebSearch20250305(...)), not by
	// Type/ProviderID.
	tools := []types.Tool{
		{Name: "anthropic.web_search_20250305", ProviderExecuted: true},
		{Type: types.ToolTypeFunction, Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-3-5-sonnet-20241022-v2:0", "", nil, nil)
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "web_search_20250305 tool" {
		t.Fatalf("warnings = %#v, want a single web_search_20250305 filter warning", result.Warnings)
	}
	if len(result.ToolConfig.Tools) != 1 {
		t.Fatalf("expected only the function tool to survive, got %#v", result.ToolConfig.Tools)
	}
}

func TestPrepareTools_StrictSchemaCompatibility(t *testing.T) {
	compatible := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]interface{}{"a": map[string]interface{}{"type": "string"}},
	}
	incompatible := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}},
	}
	if !isStrictToolSchemaCompatible(compatible) {
		t.Fatal("expected compatible schema to pass")
	}
	if isStrictToolSchemaCompatible(incompatible) {
		t.Fatal("expected incompatible schema (missing additionalProperties:false) to fail")
	}

	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "strict_tool", Strict: true, Parameters: incompatible},
	}
	// claude-3-5-sonnet is a legacy model (not in modelsWithoutStrictToolSupport),
	// so strict tool support itself is allowed, but the schema is incompatible.
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-3-5-sonnet-20241022-v2:0", "", nil, nil)
	foundWarning := false
	for _, w := range result.Warnings {
		if w.Feature == "strict" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected a strict-schema warning, got %#v", result.Warnings)
	}
	toolSpec := result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
	if _, ok := toolSpec["strict"]; ok {
		t.Fatalf("expected strict to be omitted when schema is incompatible, got %#v", toolSpec)
	}
}

func TestPrepareTools_ModelsWithoutStrictSupportOmitStrict(t *testing.T) {
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "t", Strict: true, Parameters: map[string]interface{}{"type": "object", "additionalProperties": false}},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil)
	toolSpec := result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
	if _, ok := toolSpec["strict"]; ok {
		t.Fatalf("expected strict omitted for claude-opus-5 (no strict tool support), got %#v", toolSpec)
	}
	foundWarning := false
	for _, w := range result.Warnings {
		if w.Feature == "strict" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatal("expected an unsupported strict warning")
	}
}

// TestPrepareTools_ToolSearchWireShape verifies that an Anthropic tool_search
// tool (identified by its namespaced Name, matching
// pkg/providers/anthropic/tools's factory convention) is forwarded through
// the standard Bedrock toolConfig as {toolSpec: {name, inputSchema}} — the
// same shape as any other Converse tool — using the short Bedrock tool name
// and the tool's own input schema, not the Anthropic Messages API tool wire
// shape (which has a "type" field and no separate toolSpec wrapper).
func TestPrepareTools_ToolSearchWireShape(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
		"required":   []string{"query"},
	}
	tools := []types.Tool{
		{Name: "anthropic.tool_search_bm25_20251119", Parameters: schema, ProviderExecuted: true},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil)
	if len(result.ToolConfig.Tools) != 1 {
		t.Fatalf("ToolConfig.Tools = %#v, want 1 tool", result.ToolConfig.Tools)
	}
	toolSpec, ok := result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool = %#v, want a toolSpec map", result.ToolConfig.Tools[0])
	}
	if _, hasType := toolSpec["type"]; hasType {
		t.Fatalf("toolSpec must not have a 'type' field (Bedrock toolSpec has no type), got %#v", toolSpec)
	}
	if toolSpec["name"] != "tool_search_tool_bm25" {
		t.Fatalf("toolSpec.name = %v, want tool_search_tool_bm25", toolSpec["name"])
	}
	inputSchema, ok := toolSpec["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolSpec.inputSchema = %#v, want a map", toolSpec["inputSchema"])
	}
	jsonSchema, ok := inputSchema["json"].(map[string]interface{})
	if !ok || jsonSchema["type"] != "object" {
		t.Fatalf("toolSpec.inputSchema.json = %#v, want the tool_search input schema", inputSchema["json"])
	}
}

// TestPrepareTools_BuiltinToolsForwarded verifies that Anthropic "simple"
// builtin provider tools (bash, text editors, code execution, memory,
// advisor — anything in anthropicBuiltinToolTypes, not just tool_search) are
// forwarded through the standard Bedrock toolConfig as {toolSpec: {name,
// inputSchema}}, using their own types.Tool.Parameters as the schema. Ports
// TS amazon-bedrock-prepare-tools.ts's generic anthropicTools factory-id
// lookup, which forwards any ProviderTool it finds a factory match for (not
// just tool_search) — see amazon-bedrock-prepare-tools.ts:120-141.
func TestPrepareTools_BuiltinToolsForwarded(t *testing.T) {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
		"required":   []string{"command"},
	}
	tools := []types.Tool{
		{Name: "anthropic.bash_20250124", Parameters: schema, ProviderExecuted: true},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil)
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings for a recognized builtin tool, got %#v", result.Warnings)
	}
	if len(result.ToolConfig.Tools) != 1 {
		t.Fatalf("ToolConfig.Tools = %#v, want 1 tool", result.ToolConfig.Tools)
	}
	toolSpec, ok := result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool = %#v, want a toolSpec map", result.ToolConfig.Tools[0])
	}
	if _, hasType := toolSpec["type"]; hasType {
		t.Fatalf("toolSpec must not have a 'type' field (Bedrock toolSpec has no type), got %#v", toolSpec)
	}
	if toolSpec["name"] != "bash" {
		t.Fatalf("toolSpec.name = %v, want bash", toolSpec["name"])
	}
	inputSchema, ok := toolSpec["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolSpec.inputSchema = %#v, want a map", toolSpec["inputSchema"])
	}
	if jsonSchema, ok := inputSchema["json"].(map[string]interface{}); !ok || jsonSchema["type"] != "object" {
		t.Fatalf("toolSpec.inputSchema.json = %#v, want the bash input schema", inputSchema["json"])
	}
}

// TestPrepareTools_UnrecognizedAnthropicToolWarns verifies an Anthropic
// provider tool Bedrock doesn't know how to forward at all (e.g. a
// self-serializing computer/computer_toolset tool, which needs per-instance
// config BuiltinToolAPIName's static table does not carry) produces an
// "unsupported" warning naming the tool, instead of being silently dropped or
// sent with a broken wire shape.
func TestPrepareTools_UnrecognizedAnthropicToolWarns(t *testing.T) {
	tools := []types.Tool{
		{Name: "anthropic.computer_toolset_20260801", ProviderExecuted: true},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil)
	if len(result.ToolConfig.Tools) != 0 {
		t.Fatalf("expected the unrecognized tool to be dropped, got %#v", result.ToolConfig.Tools)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "tool anthropic.computer_toolset_20260801" {
		t.Fatalf("warnings = %#v, want a single 'tool anthropic.computer_toolset_20260801' warning", result.Warnings)
	}
}

func TestModelSupport_IsAnthropicModelID(t *testing.T) {
	budget := 100
	tests := []struct {
		name     string
		modelID  string
		family   string
		budget   *int
		expected bool
	}{
		{"contains anthropic", "anthropic.claude-3-5-sonnet-20241022-v2:0", "", nil, true},
		{"modelFamily override", "some-custom-id", "anthropic", nil, true},
		{"non-anthropic no override", "amazon.nova-pro-v1:0", "", nil, false},
		{"inference profile ARN with budget", "arn:aws:bedrock:us-east-1:1:application-inference-profile/x", "", &budget, true},
		{"inference profile ARN without budget", "arn:aws:bedrock:us-east-1:1:application-inference-profile/x", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAnthropicModelID(tt.modelID, tt.family, tt.budget); got != tt.expected {
				t.Errorf("isAnthropicModelID(%q, %q, %v) = %v, want %v", tt.modelID, tt.family, tt.budget, got, tt.expected)
			}
		})
	}
}

func TestModelSupport_SupportsStrictToolsAndNativeSO(t *testing.T) {
	strictUnsupported := []string{"anthropic.claude-opus-4-7", "anthropic.claude-opus-4-8", "anthropic.claude-opus-5", "anthropic.claude-fable-5", "anthropic.claude-sonnet-5"}
	for _, id := range strictUnsupported {
		if bedrockSupportsStrictTools(id) {
			t.Errorf("bedrockSupportsStrictTools(%q) = true, want false", id)
		}
		if bedrockSupportsNativeStructuredOutput(id) {
			t.Errorf("bedrockSupportsNativeStructuredOutput(%q) = true, want false", id)
		}
	}
	extraSONotSupported := []string{"anthropic.claude-sonnet-4-6-v1:0", "anthropic.claude-haiku-4-5-20251001-v1:0"}
	for _, id := range extraSONotSupported {
		if !bedrockSupportsStrictTools(id) {
			t.Errorf("bedrockSupportsStrictTools(%q) = false, want true", id)
		}
		if bedrockSupportsNativeStructuredOutput(id) {
			t.Errorf("bedrockSupportsNativeStructuredOutput(%q) = true, want false", id)
		}
	}
	if !bedrockSupportsStrictTools("anthropic.claude-3-5-sonnet-20241022-v2:0") {
		t.Error("expected strict tools supported for a model outside the exclusion lists")
	}
	if !bedrockSupportsNativeStructuredOutput("anthropic.claude-3-5-sonnet-20241022-v2:0") {
		t.Error("expected native structured output supported for a model outside the exclusion lists")
	}
}

func TestBedrockAPIError_FormatsTypeAndMessage(t *testing.T) {
	err := bedrockAPIError(400, []byte(`{"message":"bad input","type":"ValidationException"}`), nil)
	if err.Message != "ValidationException: bad input" {
		t.Fatalf("Message = %q", err.Message)
	}
	if err.StatusCode != 400 {
		t.Fatalf("StatusCode = %d, want 400", err.StatusCode)
	}

	errNoType := bedrockAPIError(500, []byte(`{"message":"boom"}`), nil)
	if errNoType.Message != "boom" {
		t.Fatalf("Message = %q, want boom", errNoType.Message)
	}

	errUnparsable := bedrockAPIError(502, []byte("not json"), nil)
	if errUnparsable.Message != "not json" {
		t.Fatalf("Message = %q, want raw body fallback", errUnparsable.Message)
	}
}

func TestResolveAmazonBedrockBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		region string
		want   string
	}{
		{"standard partition", "us-east-1", "https://bedrock-runtime.us-east-1.amazonaws.com"},
		{"china partition", "cn-north-1", "https://bedrock-runtime.cn-north-1.amazonaws.com.cn"},
		{"us-iso partition", "us-iso-east-1", "https://bedrock-runtime.us-iso-east-1.c2s.ic.gov"},
		{"us-isob partition", "us-isob-east-1", "https://bedrock-runtime.us-isob-east-1.sc2s.sgov.gov"},
		{"eu-isoe partition", "eu-isoe-west-1", "https://bedrock-runtime.eu-isoe-west-1.cloud.adc-e.uk"},
		{"us-isof partition", "us-isof-south-1", "https://bedrock-runtime.us-isof-south-1.csp.hci.ic.gov"},
		{"eusc partition", "eusc-de-east-1", "https://bedrock-runtime.eusc-de-east-1.amazonaws.eu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
				Region:                               tt.region,
				Service:                              "bedrock-runtime",
				ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
			})
			if err != nil {
				t.Fatalf("ResolveAmazonBedrockBaseURL error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAmazonBedrockBaseURL_EnvVarPrecedence(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "https://custom-runtime.example.com/")
	t.Setenv("AWS_ENDPOINT_URL", "https://generic.example.com/")

	got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		Region:                               "us-east-1",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got != "https://custom-runtime.example.com" {
		t.Fatalf("got %q, want service-specific env var to win (trailing slash stripped)", got)
	}

	t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "")
	got2, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		Region:                               "us-east-1",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got2 != "https://generic.example.com" {
		t.Fatalf("got %q, want AWS_ENDPOINT_URL fallback", got2)
	}

	explicit, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		BaseURL:                              "https://explicit.example.com",
		Region:                               "us-east-1",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if explicit != "https://explicit.example.com" {
		t.Fatalf("got %q, want explicit BaseURL to win over env vars", explicit)
	}
}
