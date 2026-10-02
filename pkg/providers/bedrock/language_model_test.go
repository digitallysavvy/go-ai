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
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

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

func TestDoGenerate_RequestMetadataForwardedToTopLevel(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"output": {"message": {"role": "assistant", "content": [{"text": "hello"}]}},
			"stopReason": "end_turn",
			"usage": {"inputTokens": 3, "outputTokens": 5, "totalTokens": 8}
		}`))
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"requestMetadata": map[string]interface{}{"team": "search", "environment": "prod"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	rm, ok := gotBody["requestMetadata"].(map[string]interface{})
	if !ok || rm["team"] != "search" || rm["environment"] != "prod" {
		t.Fatalf("requestMetadata = %#v, want top-level map", gotBody["requestMetadata"])
	}
	if additional, ok := gotBody["additionalModelRequestFields"].(map[string]interface{}); ok {
		if _, leaked := additional["requestMetadata"]; leaked {
			t.Fatalf("requestMetadata leaked into additionalModelRequestFields: %#v", additional)
		}
	}
}

func TestDoGenerate_RequestMetadataOmittedWhenNotProvided(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"output": {"message": {"role": "assistant", "content": [{"text": "hello"}]}},
			"stopReason": "end_turn",
			"usage": {"inputTokens": 3, "outputTokens": 5, "totalTokens": 8}
		}`))
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if _, ok := gotBody["requestMetadata"]; ok {
		t.Fatalf("requestMetadata = %#v, want omitted", gotBody["requestMetadata"])
	}
}

func TestDoStream_RequestMetadataForwardedToTopLevel(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		body := buildBedrockEventStreamBody([][2]string{
			{"messageStop", `{"stopReason":"end_turn"}`},
		})
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"requestMetadata": map[string]interface{}{"team": "search", "environment": "prod"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	for {
		if _, err := stream.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
	}
	rm, ok := gotBody["requestMetadata"].(map[string]interface{})
	if !ok || rm["team"] != "search" || rm["environment"] != "prod" {
		t.Fatalf("requestMetadata = %#v, want top-level map", gotBody["requestMetadata"])
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

// TestDoStream_ModelStreamErrorExceptionIsRetryable is a P1-1c part 2
// regression test: modelStreamErrorException maps to HTTP 424, which is
// outside ProviderError.IsRetryable()'s generic 429/5xx default, but TS's
// getAmazonBedrockStreamErrorMetadata still marks it retryable. Both the
// final stream.Err() (*providererrors.ProviderError.Retryable override) and
// the mid-stream ChunkTypeError chunk's Err field (a
// *providererrors.StreamProviderError, so pkg/ai's streamRetries logic sees
// the same verdict without waiting for the stream to end) must reflect that.
func TestDoStream_ModelStreamErrorExceptionIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frame := buildBedrockEventFrame(map[string]string{
			":message-type":   "exception",
			":exception-type": "modelStreamErrorException",
		}, []byte(`{"message":"model stream interrupted"}`))
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

	var errChunk *provider.StreamChunk
	for i := 0; i < 10; i++ {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeError {
			errChunk = chunk
		}
	}
	if errChunk == nil {
		t.Fatal("expected an error chunk for the modeled exception")
	}

	// The chunk's own Err field must already carry a StreamProviderError
	// with the 424/retryable verdict (P1-1c part 2: structured stream error
	// payload), not just Text.
	var streamProviderErr *providererrors.StreamProviderError
	if !errors.As(errChunk.Err, &streamProviderErr) {
		t.Fatalf("chunk.Err = %v (%T), want a *providererrors.StreamProviderError", errChunk.Err, errChunk.Err)
	}
	if streamProviderErr.StatusCode == nil || *streamProviderErr.StatusCode != 424 {
		t.Fatalf("chunk.Err.StatusCode = %v, want 424", streamProviderErr.StatusCode)
	}
	if !streamProviderErr.IsRetryable {
		t.Fatal("chunk.Err.IsRetryable = false, want true for modelStreamErrorException")
	}
	if streamProviderErr.Type != "modelStreamErrorException" {
		t.Fatalf("chunk.Err.Type = %q, want modelStreamErrorException", streamProviderErr.Type)
	}

	// stream.Err() after the stream ends must carry the same override on
	// ProviderError.Retryable (this is the field IsRetryable() consults).
	streamErr := stream.Err()
	var providerErr *providererrors.ProviderError
	if !errors.As(streamErr, &providerErr) {
		t.Fatalf("Err() = %v (%T), want a *providererrors.ProviderError", streamErr, streamErr)
	}
	if providerErr.StatusCode != 424 {
		t.Fatalf("StatusCode = %d, want 424 for modelStreamErrorException", providerErr.StatusCode)
	}
	if providerErr.Retryable == nil || !*providerErr.Retryable {
		t.Fatal("expected modelStreamErrorException (424) to be explicitly marked Retryable")
	}
	if !providerErr.IsRetryable() {
		t.Fatal("expected IsRetryable() to honor the 424 override and return true")
	}
}

// TestDoStream_ModeledExceptionStillEmitsFinishChunk is a regression test:
// TS's enqueueError (amazon-bedrock-chat-language-model.ts) records the error
// and sets finishReason but relies on the ReadableStream's own flush() when
// the connection closes, so a modeled exception is always followed by a
// terminal 'finish' chunk (finishReason:'error', whatever usage/
// providerMetadata had accrued) — the stream is never truncated right after
// the error. Previously Go set s.done=true in the exception branch, so
// s.flush() (and its ChunkTypeFinish chunk) was never reached.
func TestDoStream_ModeledExceptionStillEmitsFinishChunk(t *testing.T) {
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

	var sawFinish bool
	var finishReason types.FinishReason
	for i := 0; i < 10; i++ {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			sawFinish = true
			finishReason = chunk.FinishReason
		}
	}
	if !sawFinish {
		t.Fatal("expected a terminal finish chunk after the modeled exception, matching TS's always-flush contract")
	}
	if finishReason != types.FinishReasonError {
		t.Fatalf("finish chunk finishReason = %v, want error", finishReason)
	}
}

// TestDoStream_UnparseableEventSetsFinishReasonError is a regression test:
// TS's !chunk.success branch sets finishReason to 'error' (in addition to
// emitting an error part) so that if the stream ends without a later
// messageStop overwriting it, flush() still reports finishReason:'error'
// rather than a stale default.
func TestDoStream_UnparseableEventSetsFinishReasonError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frame := buildBedrockEventFrame(map[string]string{
			":message-type": "event",
			":event-type":   "contentBlockDelta",
		}, []byte(`not valid json`))
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

	var sawFinish bool
	var finishReason types.FinishReason
	for i := 0; i < 10; i++ {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			sawFinish = true
			finishReason = chunk.FinishReason
		}
	}
	if !sawFinish {
		t.Fatal("expected a terminal finish chunk")
	}
	if finishReason != types.FinishReasonError {
		t.Fatalf("finish chunk finishReason = %v, want error", finishReason)
	}
}

// TestDoStream_ResponseMetadataIncludesTimestamp verifies the streaming
// response-metadata chunk parses the "date" response header the same way
// DoGenerate does, instead of always leaving Timestamp zero.
func TestDoStream_ResponseMetadataIncludesTimestamp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("date", "Tue, 12 May 2026 10:30:00 GMT")
		frame := buildBedrockEventFrame(map[string]string{
			":message-type": "event",
			":event-type":   "messageStop",
		}, []byte(`{"stopReason":"end_turn"}`))
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

	var gotTimestamp time.Time
	for i := 0; i < 10; i++ {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeResponseMetadata {
			gotTimestamp = chunk.ResponseMetadata.Timestamp
		}
	}
	want, _ := time.Parse(time.RFC1123, "Tue, 12 May 2026 10:30:00 GMT")
	if !gotTimestamp.Equal(want) {
		t.Fatalf("ResponseMetadata.Timestamp = %v, want %v", gotTimestamp, want)
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
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-3-5-sonnet-20241022-v2:0", "", nil, nil, false)
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "web_search_20250305 tool" {
		t.Fatalf("warnings = %#v, want a single web_search_20250305 filter warning", result.Warnings)
	}
	if len(result.ToolConfig.Tools) != 1 {
		t.Fatalf("expected only the function tool to survive, got %#v", result.ToolConfig.Tools)
	}
}

// TestPrepareTools_RejectsForcedToolUse ports the TS "models that reject
// forced tool use" describe block (amazon-bedrock-chat-language-model.test.ts):
// models with model_capabilities.RejectsForcedToolUse (e.g. Claude Opus 5.5)
// fall a forced tool_choice ('required' or a named tool) back to 'auto' with
// an "unsupported" warning, instead of sending the forced choice as-is.
func TestPrepareTools_RejectsForcedToolUse(t *testing.T) {
	weatherTool := types.Tool{Type: types.ToolTypeFunction, Name: "getWeather", Description: "Get weather", Parameters: map[string]interface{}{"type": "object"}}
	timeTool := types.Tool{Type: types.ToolTypeFunction, Name: "getTime", Description: "Get time", Parameters: map[string]interface{}{"type": "object"}}

	t.Run("should build an auto tool choice for required choice", func(t *testing.T) {
		for _, modelID := range []string{
			"anthropic.claude-opus-5-5",
			"us.anthropic.claude-opus-5-5",
			"global.anthropic.claude-opus-5-5",
		} {
			t.Run(modelID, func(t *testing.T) {
				result := prepareBedrockTools([]types.Tool{weatherTool}, types.RequiredToolChoice(), true, modelID, "", nil, nil, true)
				if result.ToolConfig.ToolChoice == nil || result.ToolConfig.ToolChoice["auto"] == nil {
					t.Fatalf("toolConfig.toolChoice = %#v, want {auto: {}}", result.ToolConfig.ToolChoice)
				}
				wantWarning := types.Warning{
					Type:    "unsupported",
					Feature: "toolChoice",
					Details: "toolChoice 'required' is not supported by this model because it rejects forced tool use. Using 'auto' instead. Instruct the model to use a tool in the prompt and verify that a tool call was made.",
				}
				if len(result.Warnings) != 1 || result.Warnings[0] != wantWarning {
					t.Fatalf("warnings = %#v, want [%#v]", result.Warnings, wantWarning)
				}
			})
		}
	})

	t.Run("should build an auto choice containing only the named tool", func(t *testing.T) {
		result := prepareBedrockTools([]types.Tool{weatherTool, timeTool}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "getWeather"}, true, "anthropic.claude-opus-5-5", "", nil, nil, true)
		if result.ToolConfig.ToolChoice == nil || result.ToolConfig.ToolChoice["auto"] == nil {
			t.Fatalf("toolConfig.toolChoice = %#v, want {auto: {}}", result.ToolConfig.ToolChoice)
		}
		if len(result.ToolConfig.Tools) != 1 || result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})["name"] != "getWeather" {
			t.Fatalf("toolConfig.tools = %#v, want only getWeather", result.ToolConfig.Tools)
		}
		wantWarning := types.Warning{
			Type:    "unsupported",
			Feature: "toolChoice",
			Details: "toolChoice 'tool' is not supported by this model because it rejects forced tool use. Only the 'getWeather' tool is sent with 'auto' tool choice. Instruct the model to use the tool in the prompt and verify that a tool call was made.",
		}
		if len(result.Warnings) != 1 || result.Warnings[0] != wantWarning {
			t.Fatalf("warnings = %#v, want [%#v]", result.Warnings, wantWarning)
		}
	})

	t.Run("should build an Anthropic auto choice when parallel tool use is disabled", func(t *testing.T) {
		disable := true
		result := prepareBedrockTools([]types.Tool{weatherTool}, types.RequiredToolChoice(), true, "anthropic.claude-opus-5-5", "", nil, &disable, true)
		wantChoice := map[string]interface{}{"type": "auto", "disable_parallel_tool_use": true}
		toolChoiceField, ok := result.AdditionalTools["tool_choice"].(map[string]interface{})
		if !ok {
			t.Fatalf("additionalTools.tool_choice = %#v, want a map", result.AdditionalTools["tool_choice"])
		}
		if toolChoiceField["type"] != wantChoice["type"] || toolChoiceField["disable_parallel_tool_use"] != wantChoice["disable_parallel_tool_use"] {
			t.Fatalf("additionalTools.tool_choice = %#v, want %#v", toolChoiceField, wantChoice)
		}
		if result.ToolConfig.ToolChoice != nil {
			t.Fatalf("toolConfig.toolChoice = %#v, want nil (forced choice moved to additionalModelRequestFields)", result.ToolConfig.ToolChoice)
		}
	})

	// TestPrepareTools_RejectsForcedToolUse/should_drop_provider_tools_that_are_not_the_named_tool
	// ports TS "should drop provider tools that are not the named tool for
	// tool choice 'tool'": a mix of a function tool and an Anthropic
	// provider-defined tool, with toolChoice targeting the function tool,
	// drops the (non-matching) provider tool entirely and sends only the
	// named function tool with 'auto' tool choice.
	t.Run("should drop provider tools that are not the named tool for tool choice 'tool'", func(t *testing.T) {
		bashTool := types.Tool{Name: "anthropic.bash_20250124", ProviderExecuted: true}
		result := prepareBedrockTools([]types.Tool{weatherTool, bashTool}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "getWeather"}, true, "us.anthropic.claude-opus-5-5", "", nil, nil, true)
		if result.ToolConfig.ToolChoice == nil || result.ToolConfig.ToolChoice["auto"] == nil {
			t.Fatalf("toolConfig.toolChoice = %#v, want {auto: {}}", result.ToolConfig.ToolChoice)
		}
		if len(result.ToolConfig.Tools) != 1 || result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})["name"] != "getWeather" {
			t.Fatalf("toolConfig.tools = %#v, want only getWeather", result.ToolConfig.Tools)
		}
		if result.AdditionalTools != nil {
			t.Fatalf("additionalTools = %#v, want nil", result.AdditionalTools)
		}
	})

	// TestPrepareTools_RejectsForcedToolUse/should_forward_rejectsForcedToolUse_to_the_Anthropic_provider_tool_path
	// ports TS "should pass the forced-tool capability to Anthropic provider
	// tool preparation": when the forced tool choice targets an Anthropic
	// provider-defined tool (not a function tool), the usingAnthropicTools
	// branch (bedrockAnthropicToolChoice) must itself receive
	// rejectsForcedToolUse and fall back to 'auto', not just the
	// function-tool branch exercised by the subtests above.
	t.Run("should forward rejectsForcedToolUse to the Anthropic provider tool path", func(t *testing.T) {
		bashTool := types.Tool{Name: "anthropic.bash_20250124", ProviderExecuted: true}
		result := prepareBedrockTools([]types.Tool{bashTool}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "bash"}, true, "us.anthropic.claude-opus-5-5", "", nil, nil, true)
		toolChoiceField, ok := result.AdditionalTools["tool_choice"].(map[string]interface{})
		if !ok || toolChoiceField["type"] != "auto" {
			t.Fatalf("additionalTools.tool_choice = %#v, want {type: auto}", result.AdditionalTools["tool_choice"])
		}
		wantWarning := types.Warning{
			Type:    "unsupported",
			Feature: "toolChoice",
			Details: "toolChoice 'tool' is not supported by this model because it rejects forced tool use. Only the 'bash' tool is sent with 'auto' tool choice. Instruct the model to use the tool in the prompt and verify that a tool call was made.",
		}
		if len(result.Warnings) != 1 || result.Warnings[0] != wantWarning {
			t.Fatalf("warnings = %#v, want [%#v]", result.Warnings, wantWarning)
		}
	})

	t.Run("should keep building forced tool choices for models that support them", func(t *testing.T) {
		// claude-opus-5 (not 5-5) does not set RejectsForcedToolUse.
		result := prepareBedrockTools([]types.Tool{weatherTool}, types.RequiredToolChoice(), true, "anthropic.claude-opus-5", "", nil, nil, false)
		if result.ToolConfig.ToolChoice == nil || result.ToolConfig.ToolChoice["any"] == nil {
			t.Fatalf("toolConfig.toolChoice = %#v, want {any: {}}", result.ToolConfig.ToolChoice)
		}
		if len(result.Warnings) != 0 {
			t.Fatalf("warnings = %#v, want none", result.Warnings)
		}
	})
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
		{Type: types.ToolTypeFunction, Name: "strict_tool", Strict: types.BoolPtr(true), Parameters: incompatible},
	}
	// claude-3-5-sonnet is a legacy model (not in modelsWithoutStrictToolSupport),
	// so strict tool support itself is allowed, but the schema is incompatible.
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-3-5-sonnet-20241022-v2:0", "", nil, nil, false)
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
		{Type: types.ToolTypeFunction, Name: "t", Strict: types.BoolPtr(true), Parameters: map[string]interface{}{"type": "object", "additionalProperties": false}},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil, false)
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
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil, false)
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
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil, false)
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
// provider tool built as a raw types.Tool literal (no ProviderOptions, e.g. a
// caller-constructed value rather than one of the
// pkg/providers/anthropic/tools constructors) produces an "unsupported"
// warning naming the tool, instead of being silently dropped or sent with a
// broken wire shape. Real self-serializing tools (see
// TestPrepareTools_SelfSerializingAnthropicToolsForwarded) carry ProviderOptions
// that implement anthropicAPIMapper and ARE forwarded — this test's tool
// intentionally omits ProviderOptions to exercise the "can't derive a wire
// name at all" fallback.
func TestPrepareTools_UnrecognizedAnthropicToolWarns(t *testing.T) {
	tools := []types.Tool{
		{Name: "anthropic.computer_toolset_20260801", ProviderExecuted: true},
	}
	result := prepareBedrockTools(tools, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil, false)
	if len(result.ToolConfig.Tools) != 0 {
		t.Fatalf("expected the unrecognized tool to be dropped, got %#v", result.ToolConfig.Tools)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "tool anthropic.computer_toolset_20260801" {
		t.Fatalf("warnings = %#v, want a single 'tool anthropic.computer_toolset_20260801' warning", result.Warnings)
	}
}

// TestPrepareTools_SelfSerializingAnthropicToolsForwarded ports TS
// amazon-bedrock-prepare-tools.ts's generic anthropicTools factory-id lookup
// for the self-serializing Anthropic provider tools (computer_*,
// computer_toolset_20260801, text_editor_20250728), which need per-instance
// config and so implement ToAnthropicAPIMap on their ProviderOptions instead
// of appearing in anthropic.BuiltinToolAPIName's static table. Was previously
// a documented gap (bedrockAnthropicProviderTool returned nil for all of
// these, producing an "unsupported" warning instead of a toolSpec).
func TestPrepareTools_SelfSerializingAnthropicToolsForwarded(t *testing.T) {
	tests := []struct {
		name     string
		tool     types.Tool
		wantName string
	}{
		{
			name:     "computer_20251124",
			tool:     anthropictools.Computer20251124(anthropictools.Computer20251124Args{DisplayWidthPx: 1024, DisplayHeightPx: 768}),
			wantName: "computer",
		},
		{
			name:     "computer_toolset_20260801",
			tool:     anthropictools.ComputerToolset20260801(anthropictools.ComputerToolset20260801Config{}),
			wantName: "computer",
		},
		{
			name:     "text_editor_20250728",
			tool:     anthropictools.TextEditor20250728(anthropictools.TextEditor20250728Args{}),
			wantName: "str_replace_based_edit_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prepareBedrockTools([]types.Tool{tt.tool}, types.ToolChoice{}, false, "anthropic.claude-opus-5", "", nil, nil, false)
			if len(result.Warnings) != 0 {
				t.Fatalf("expected no warnings, got %#v", result.Warnings)
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
			if toolSpec["name"] != tt.wantName {
				t.Fatalf("toolSpec.name = %v, want %v", toolSpec["name"], tt.wantName)
			}
			inputSchema, ok := toolSpec["inputSchema"].(map[string]interface{})
			if !ok {
				t.Fatalf("toolSpec.inputSchema = %#v, want a map", toolSpec["inputSchema"])
			}
			jsonSchema, ok := inputSchema["json"].(map[string]interface{})
			if !ok || jsonSchema["type"] != "object" {
				t.Fatalf("toolSpec.inputSchema.json = %#v, want the tool's own JSON schema", inputSchema["json"])
			}
		})
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

// TestBedrockAPIError_CarriesHeadersAndBody verifies bedrockAPIError populates
// ProviderError.ResponseHeaders/ResponseBody like TS APICallError (every TS
// createJsonErrorResponseHandler call attaches responseHeaders and
// responseBody from the failed HTTP response; the Go integrator follow-up
// noted bedrockAPIError ignored its headers parameter and every caller
// passed nil).
func TestBedrockAPIError_CarriesHeadersAndBody(t *testing.T) {
	headers := map[string]string{"x-amzn-requestid": "req-123"}
	body := []byte(`{"message":"bad input","type":"ValidationException"}`)

	err := bedrockAPIError(400, body, headers)

	if err.ResponseBody != string(body) {
		t.Fatalf("ResponseBody = %q, want %q", err.ResponseBody, string(body))
	}
	if err.ResponseHeaders["x-amzn-requestid"] != "req-123" {
		t.Fatalf("ResponseHeaders = %#v, want x-amzn-requestid=req-123", err.ResponseHeaders)
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

// TestResolveAmazonBedrockBaseURL_RejectsRegionThatWouldRewriteHost ports TS
// resolve-amazon-bedrock-base-url.test.ts "rejects region %j because it
// would rewrite the request host" (TS #21842): region is interpolated
// directly into the request host, so a non-DNS-label value must be
// rejected with an InvalidArgumentError instead of silently redirecting the
// request.
func TestResolveAmazonBedrockBaseURL_RejectsRegionThatWouldRewriteHost(t *testing.T) {
	for _, region := range []string{
		"evil.example.com/#",
		"user@internal:8080/#",
		"169.254.169.254:80/x#",
		"us-east-1/../..",
		"us east 1",
		"",
	} {
		t.Run(region, func(t *testing.T) {
			_, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
				Region:                               region,
				Service:                              "bedrock-runtime",
				ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
			})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("ResolveAmazonBedrockBaseURL(region=%q) error = %v, want InvalidArgumentError", region, err)
			}
			if invalid.Field != "region" {
				t.Fatalf("Field = %q, want region", invalid.Field)
			}
		})
	}
}

// TestResolveAmazonBedrockBaseURL_DoesNotValidateUnusedRegion ports TS
// "does not reject the region when an explicit endpoint is configured" /
// "does not validate an unused region with %s": region is irrelevant once
// an explicit BaseURL or endpoint env var is set, so a garbage region must
// not block the request.
func TestResolveAmazonBedrockBaseURL_DoesNotValidateUnusedRegion(t *testing.T) {
	got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		BaseURL:                              "https://proxy.example/",
		Region:                               "user@internal/#",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("error = %v, want success (region unused with explicit BaseURL)", err)
	}
	if got != "https://proxy.example" {
		t.Fatalf("got %q, want the explicit BaseURL", got)
	}

	t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "https://proxy.example/")
	got2, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		Region:                               "user@internal/#",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("error = %v, want success (region unused with endpoint env var)", err)
	}
	if got2 != "https://proxy.example" {
		t.Fatalf("got %q, want the endpoint env var URL", got2)
	}
}
