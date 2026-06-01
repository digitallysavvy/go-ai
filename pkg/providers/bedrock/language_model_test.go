package bedrock

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

func TestResolveCredentialsEnvWins(t *testing.T) {
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
	if creds.AccessKeyID != "env-key" || creds.SecretAccessKey != "env-secret" || creds.SessionToken != "env-session" {
		t.Fatalf("creds = %#v, want env credentials", creds)
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

func TestBuildClaudeRequest_DropsUnsignedReasoningBlocks(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "visible"},
					types.ReasoningContent{Text: "foreign reasoning without signature"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 1 || content[0]["text"] != "visible" {
		t.Fatalf("content = %#v, want only visible text block", messages[0]["content"])
	}
}

func TestBuildClaudeRequest_SystemMessageCachePoint(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		AllowSystemMessages: true,
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleSystem,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "System Prompt",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{
								"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"},
							},
						},
					},
				},
			},
			{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "hello"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	system, ok := body["system"].([]map[string]interface{})
	if !ok {
		t.Fatalf("system = %T, want block slice: %#v", body["system"], body["system"])
	}
	if len(system) != 2 {
		t.Fatalf("system length = %d, want text/cachePoint: %#v", len(system), system)
	}
	if system[0]["text"] != "System Prompt" {
		t.Fatalf("system text block = %#v", system[0])
	}
	cachePoint := system[1]["cachePoint"].(map[string]interface{})
	if cachePoint["type"] != "default" || cachePoint["ttl"] != "5m" {
		t.Fatalf("system cachePoint = %#v", cachePoint)
	}
	messages := body["messages"].([]map[string]interface{})
	if len(messages) != 1 || messages[0]["role"] != types.RoleUser {
		t.Fatalf("messages = %#v, want only user message", messages)
	}
}

func TestBuildClaudeRequest_TopLevelSystemUsesContentBlocks(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "System Prompt",
			Messages: []types.Message{
				{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "Hello"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	system, ok := body["system"].([]map[string]interface{})
	if !ok {
		t.Fatalf("system = %T, want block slice: %#v", body["system"], body["system"])
	}
	if len(system) != 1 || system[0]["text"] != "System Prompt" {
		t.Fatalf("system = %#v, want text block", system)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 || content[0]["text"] != "Hello" {
		t.Fatalf("messages = %#v, want user text block", messages)
	}
}

func TestBuildClaudeRequest_ReplaysOnlySignedReasoningBlocks(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "visible"},
					types.ReasoningContent{Text: "signed reasoning", Signature: "sig-1"},
					types.ReasoningContent{Text: "unsigned reasoning"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content length = %d, want text + signed reasoning only: %#v", len(content), content)
	}
	rc := content[1]["reasoningContent"].(map[string]interface{})
	rt := rc["reasoningText"].(map[string]interface{})
	if rt["text"] != "signed reasoning" || rt["signature"] != "sig-1" {
		t.Fatalf("reasoning block = %#v", content[1])
	}
}

func TestBuildClaudeRequest_ReplaysBedrockReasoningProviderOptions(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "provider option signature",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{"signature": "sig-provider"},
						},
					},
					types.ReasoningContent{
						Text: "foreign signature",
						ProviderOptions: map[string]interface{}{
							"anthropic": map[string]interface{}{"signature": "foreign-sig"},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("content length = %d, want only Bedrock-signed reasoning: %#v", len(content), content)
	}
	rc := content[0]["reasoningContent"].(map[string]interface{})
	rt := rc["reasoningText"].(map[string]interface{})
	if rt["text"] != "provider option signature" || rt["signature"] != "sig-provider" {
		t.Fatalf("reasoning block = %#v", content[0])
	}
}

func TestBuildClaudeRequest_ReplaysEmptyRedactedReasoningProviderOption(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{"redactedData": ""},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("content length = %d, want redacted reasoning: %#v", len(content), content)
	}
	rc := content[0]["reasoningContent"].(map[string]interface{})
	redacted := rc["redactedReasoning"].(map[string]interface{})
	if redacted["data"] != "" {
		t.Fatalf("redacted reasoning = %#v, want empty data string", redacted)
	}
}

func TestBuildClaudeRequest_ReplaysEmptySignatureReasoningProviderOption(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "reasoning",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{"signature": ""},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("content length = %d, want signed reasoning block: %#v", len(content), content)
	}
	rc := content[0]["reasoningContent"].(map[string]interface{})
	rt := rc["reasoningText"].(map[string]interface{})
	if rt["text"] != "reasoning" || rt["signature"] != "" {
		t.Fatalf("reasoning block = %#v, want empty signature replayed", content[0])
	}
}

func TestBuildClaudeRequest_ReasoningDoesNotFallBackWhenAmazonBedrockPresent(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ReasoningContent{
						Text: "bedrock signature should not be used",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{},
							"bedrock":       map[string]interface{}{"signature": "sig-bedrock"},
						},
					},
					types.TextContent{Text: "answer"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 || content[0]["text"] != "answer" {
		t.Fatalf("content = %#v, want unsigned reasoning skipped without bedrock fallback", content)
	}
}

func TestBuildClaudeRequest_PartLevelCachePointAfterContentBlock(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "cache me",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{"cachePoint": true},
						},
					},
					types.TextContent{Text: "plain"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("content length = %d, want text/cachePoint/text: %#v", len(content), content)
	}
	if content[0]["text"] != "cache me" {
		t.Fatalf("first block = %#v", content[0])
	}
	cachePoint, ok := content[1]["cachePoint"].(map[string]interface{})
	if !ok || cachePoint["type"] != "default" {
		t.Fatalf("cachePoint block = %#v", content[1])
	}
	if content[2]["text"] != "plain" {
		t.Fatalf("third block = %#v", content[2])
	}
}

func TestBuildClaudeRequest_PreservesCachePointConfigAndMessageCachePoint(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "part cache",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{
								"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"},
							},
						},
					},
				},
				ProviderOptions: map[string]interface{}{
					"amazonBedrock": map[string]interface{}{
						"cachePoint": map[string]interface{}{"type": "default", "ttl": "1h"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("content length = %d, want text/part cache/message cache: %#v", len(content), content)
	}
	partCache := content[1]["cachePoint"].(map[string]interface{})
	if partCache["type"] != "default" || partCache["ttl"] != "5m" {
		t.Fatalf("part cachePoint = %#v", partCache)
	}
	messageCache := content[2]["cachePoint"].(map[string]interface{})
	if messageCache["type"] != "default" || messageCache["ttl"] != "1h" {
		t.Fatalf("message cachePoint = %#v", messageCache)
	}
}

func TestBuildClaudeRequest_CachePointFallsBackToLegacyBedrock(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "cache me",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{"guardContent": true},
							"bedrock": map[string]interface{}{
								"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content length = %d, want text/cachePoint: %#v", len(content), content)
	}
	cachePoint := content[1]["cachePoint"].(map[string]interface{})
	if cachePoint["type"] != "default" || cachePoint["ttl"] != "5m" {
		t.Fatalf("cachePoint = %#v", cachePoint)
	}
}

func TestBuildClaudeRequest_CachePointFalseDoesNotFallBack(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "no cache",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{"cachePoint": false},
							"bedrock": map[string]interface{}{
								"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("content = %#v, want no fallback cachePoint", content)
	}
}

func TestBuildClaudeRequest_CachePointAfterSkippedAssistantParts(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "\n\n",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"}},
						},
					},
					types.ReasoningContent{
						Text: "unsigned reasoning",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default", "ttl": "1h"}},
						},
					},
					types.TextContent{Text: "answer"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 4 {
		t.Fatalf("content = %#v, want text/cache/cache/text", content)
	}
	if content[0]["text"] != "\n\n" {
		t.Fatalf("first block = %#v, want preserved empty text because reasoning is present", content[0])
	}
	firstCache := content[1]["cachePoint"].(map[string]interface{})
	if firstCache["ttl"] != "5m" {
		t.Fatalf("first cachePoint = %#v, want ttl 5m", firstCache)
	}
	secondCache := content[2]["cachePoint"].(map[string]interface{})
	if secondCache["ttl"] != "1h" {
		t.Fatalf("second cachePoint = %#v, want ttl 1h", secondCache)
	}
	if content[3]["text"] != "answer" {
		t.Fatalf("fourth block = %#v, want answer text", content[3])
	}
}

func TestBuildClaudeRequest_CachePointAfterSkippedEmptyAssistantText(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{
						Text: "  ",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default", "ttl": "5m"}},
						},
					},
					types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 2 {
		t.Fatalf("content = %#v, want cache/toolUse", content)
	}
	cachePoint := content[0]["cachePoint"].(map[string]interface{})
	if cachePoint["ttl"] != "5m" {
		t.Fatalf("cachePoint = %#v, want ttl 5m", cachePoint)
	}
	if _, ok := content[1]["toolUse"]; !ok {
		t.Fatalf("second block = %#v, want toolUse", content[1])
	}
}

func TestBuildClaudeRequest_NormalizesMistralToolCallIDs(t *testing.T) {
	model := newTestBedrockModelWithID(ModelMistralLarge2402V1)
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.ToolCallContent{
						ToolCallID: "tooluse_xyz123ABC456-def",
						ToolName:   "test-tool",
						Arguments:  map[string]interface{}{"query": "test"},
					},
				},
			},
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "tooluse_bpe71yCfRu2b5i-nKGDr5g",
						ToolName:   "calculator",
						Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "The result is 42"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	assistantContent := messages[0]["content"].([]map[string]interface{})
	toolUse := assistantContent[0]["toolUse"].(map[string]interface{})
	if toolUse["toolUseId"] != "toolusexy" {
		t.Fatalf("toolUseId = %v, want toolusexy", toolUse["toolUseId"])
	}
	toolContent := messages[1]["content"].([]map[string]interface{})
	toolResult := toolContent[0]["toolResult"].(map[string]interface{})
	if toolResult["toolUseId"] != "toolusebp" {
		t.Fatalf("toolResult toolUseId = %v, want toolusebp", toolResult["toolUseId"])
	}
}

func TestBuildClaudeRequest_PreservesNonMistralToolCallIDs(t *testing.T) {
	model := newTestBedrockModel()
	originalID := "tooluse_bpe71yCfRu2b5i-nKGDr5g"
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: originalID,
						ToolName:   "calculator",
						Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "The result is 42"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	toolResult := content[0]["toolResult"].(map[string]interface{})
	if toolResult["toolUseId"] != originalID {
		t.Fatalf("toolResult toolUseId = %v, want %s", toolResult["toolUseId"], originalID)
	}
}

func TestBuildClaudeRequest_RejectsUnsupportedImageMimeType(t *testing.T) {
	model := newTestBedrockModel()
	_, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{Image: []byte("bmp"), MimeType: "image/bmp"},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "Unsupported image mime type") && !strings.Contains(err.Error(), "unsupported image mime type") {
		t.Fatalf("buildClaudeRequest error = %v, want unsupported image mime type", err)
	}
}

func TestBuildClaudeRequest_RejectsImageURLData(t *testing.T) {
	model := newTestBedrockModel()
	_, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{URL: "https://example.com/image.png", MimeType: "image/png"},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "image URL data") {
		t.Fatalf("buildClaudeRequest error = %v, want unsupported image URL data", err)
	}
}

func TestBuildClaudeRequest_RejectsUnsupportedFileMimeType(t *testing.T) {
	model := newTestBedrockModel()
	_, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{Data: []byte("data"), MediaType: "application/json", Filename: "data.json"},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "Unsupported file mime type") && !strings.Contains(err.Error(), "unsupported file mime type") {
		t.Fatalf("buildClaudeRequest error = %v, want unsupported file mime type", err)
	}
}

func TestBuildClaudeRequest_RejectsUnsupportedFileDataKinds(t *testing.T) {
	tests := []struct {
		name    string
		part    types.FileContent
		wantErr string
	}{
		{
			name:    "url",
			part:    types.FileContent{URL: "https://example.com/file.pdf", MediaType: "application/pdf"},
			wantErr: "file URL data",
		},
		{
			name:    "reference",
			part:    types.FileContent{Reference: "file-123", MediaType: "application/pdf"},
			wantErr: "provider references",
		},
		{
			name:    "tagged url",
			part:    types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/file.pdf", MediaType: "application/pdf"}},
			wantErr: "file URL data",
		},
		{
			name:    "tagged reference",
			part:    types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"bedrock": "file-123"}, MediaType: "application/pdf"}},
			wantErr: "provider references",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestBedrockModel()
			_, err := model.buildClaudeRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					{
						Role:    types.RoleUser,
						Content: []types.ContentPart{tt.part},
					},
				}},
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("buildClaudeRequest error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildClaudeRequest_DocumentCitationsProviderOption(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Data:      []byte{0, 1, 2, 3},
						MediaType: "application/pdf",
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{
								"citations": map[string]interface{}{"enabled": true},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	document := content[0]["document"].(map[string]interface{})
	citations, ok := document["citations"].(map[string]interface{})
	if !ok || citations["enabled"] != true {
		t.Fatalf("document citations = %#v, want enabled true", document["citations"])
	}
}

func TestBuildClaudeRequest_ToolResultDocumentFileContent(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-123",
						ToolName:   "document-reader",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.FileContentBlock{
									MediaType: "application/pdf",
									Filename:  "tool-result.pdf",
									FileData: types.FileData{
										Type:       types.FileDataTypeData,
										DataString: "base64data",
									},
								},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	toolResult := content[0]["toolResult"].(map[string]interface{})
	blocks := toolResult["content"].([]map[string]interface{})
	document := blocks[0]["document"].(map[string]interface{})
	if document["format"] != "pdf" || document["name"] != "tool-result" {
		t.Fatalf("document = %#v", document)
	}
	source := document["source"].(map[string]interface{})
	if source["bytes"] != "base64data" {
		t.Fatalf("document source = %#v, want bytes base64data", source)
	}
}

func TestBuildClaudeRequest_CitationsDoNotFallBackWhenAmazonBedrockPresent(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						Data:      []byte{0, 1, 2, 3},
						MediaType: "application/pdf",
						ProviderOptions: map[string]interface{}{
							"amazonBedrock": map[string]interface{}{},
							"bedrock": map[string]interface{}{
								"citations": map[string]interface{}{"enabled": true},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	document := content[0]["document"].(map[string]interface{})
	if _, ok := document["citations"]; ok {
		t.Fatalf("document citations = %#v, want no bedrock fallback when amazonBedrock is present", document["citations"])
	}
}

func TestBuildClaudeRequest_DocumentNamesMatchTypeScript(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{Data: []byte("a"), MediaType: "application/pdf", Filename: "archive.tar.gz"},
					types.FileContent{Data: []byte("b"), MediaType: "text/plain"},
					types.FileContent{Data: []byte("c"), MediaType: "text/plain"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	names := []string{}
	for _, block := range content {
		document := block["document"].(map[string]interface{})
		names = append(names, document["name"].(string))
	}
	want := []string{"archive", "document-1", "document-2"}
	if len(names) != len(want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %#v, want %#v", names, want)
		}
	}
}

func TestBuildClaudeRequest_PreservesExplicitEmptyInlineData(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{Image: []byte{}, MimeType: "image/png"},
					types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{}, MediaType: "application/pdf"}},
					types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "", MediaType: "text/plain"}},
					types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AA", MediaType: "application/pdf"}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 4 {
		t.Fatalf("content length = %d, want 4 blocks: %#v", len(content), content)
	}

	image := content[0]["image"].(map[string]interface{})
	imageSource := image["source"].(map[string]interface{})
	if imageSource["bytes"] != "" {
		t.Fatalf("image bytes = %#v, want empty base64 string", imageSource["bytes"])
	}

	dataDocument := content[1]["document"].(map[string]interface{})
	dataSource := dataDocument["source"].(map[string]interface{})
	if dataSource["bytes"] != "" {
		t.Fatalf("data document bytes = %#v, want empty base64 string", dataSource["bytes"])
	}

	textDocument := content[2]["document"].(map[string]interface{})
	textSource := textDocument["source"].(map[string]interface{})
	if textSource["bytes"] != "" {
		t.Fatalf("text document bytes = %#v, want empty base64 string", textSource["bytes"])
	}

	stringDocument := content[3]["document"].(map[string]interface{})
	stringSource := stringDocument["source"].(map[string]interface{})
	if stringSource["bytes"] != "AA" {
		t.Fatalf("string document bytes = %#v, want DataString passed through", stringSource["bytes"])
	}
}

func TestBuildClaudeRequest_ResolvesTopLevelMediaTypesFromInlineData(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x00}, MediaType: "image"},
					types.FileContent{Data: []byte("%PDF-1.7"), MediaType: "application"},
					types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "hello", MediaType: "text"}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	image := content[0]["image"].(map[string]interface{})
	if image["format"] != "png" {
		t.Fatalf("image block = %#v, want png format", image)
	}
	pdfDocument := content[1]["document"].(map[string]interface{})
	if pdfDocument["format"] != "pdf" {
		t.Fatalf("document block = %#v, want pdf format", pdfDocument)
	}
	textDocument := content[2]["document"].(map[string]interface{})
	if textDocument["format"] != "txt" {
		t.Fatalf("text document block = %#v, want txt format", textDocument)
	}
}

func TestBuildClaudeRequest_AssistantTextFilteringAndTrim(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "hello"}},
			},
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "\n\n"},
					types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{}},
					types.TextContent{Text: "answer  "},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[1]["content"].([]map[string]interface{})
	if len(content) != 2 {
		t.Fatalf("assistant content length = %d, want toolUse/text: %#v", len(content), content)
	}
	if _, ok := content[0]["toolUse"]; !ok {
		t.Fatalf("first assistant block = %#v, want toolUse", content[0])
	}
	if content[1]["text"] != "answer" {
		t.Fatalf("assistant final text = %#v, want trimmed answer", content[1])
	}
}

func TestBuildClaudeRequest_KeepsAssistantTrailingWhitespaceBeforeUser(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role:    types.RoleAssistant,
				Content: []types.ContentPart{types.TextContent{Text: "assistant  "}},
			},
			{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "next"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	if len(content) != 1 || content[0]["text"] != "assistant  " {
		t.Fatalf("assistant content = %#v, want trailing whitespace preserved", messages[0]["content"])
	}
}

func TestBuildClaudeRequest_GroupsConsecutiveMessages(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi!"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "World"}}},
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{types.ToolResultContent{
					ToolCallID: "call-1",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "tool result"},
				}},
			},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	if len(messages) != 3 {
		t.Fatalf("messages length = %d, want user/assistant/user groups: %#v", len(messages), messages)
	}
	if messages[0]["role"] != types.RoleUser || messages[1]["role"] != types.RoleAssistant || messages[2]["role"] != types.RoleUser {
		t.Fatalf("messages roles = %#v", messages)
	}
	assistantContent := messages[1]["content"].([]map[string]interface{})
	if len(assistantContent) != 2 || assistantContent[0]["text"] != "Hello" || assistantContent[1]["text"] != "World" {
		t.Fatalf("assistant group content = %#v", assistantContent)
	}
	userContent := messages[2]["content"].([]map[string]interface{})
	if len(userContent) != 2 {
		t.Fatalf("user/tool group content = %#v, want toolResult + text", userContent)
	}
	if _, ok := userContent[0]["toolResult"]; !ok || userContent[1]["text"] != "again" {
		t.Fatalf("user/tool group content = %#v", userContent)
	}
}

func TestBuildClaudeRequest_RejectsSystemAfterUser(t *testing.T) {
	model := newTestBedrockModel()
	_, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "late system"}}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "system messages") {
		t.Fatalf("buildClaudeRequest error = %v, want separated system message error", err)
	}
}

func TestBuildClaudeRequest_NonImageToolResultFileBecomesDocument(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.FileContentBlock{MediaType: "application/pdf", Data: []byte("pdf")},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	toolResult := content[0]["toolResult"].(map[string]interface{})
	blocks := toolResult["content"].([]map[string]interface{})
	document := blocks[0]["document"].(map[string]interface{})
	if document["format"] != "pdf" || document["name"] != "document-1" {
		t.Fatalf("document = %#v", document)
	}
}

func TestBuildClaudeRequest_PreservesExplicitEmptyToolResultFileData(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.FileContentBlock{MediaType: "image/png", Data: []byte{}},
								types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{}, MediaType: "image/jpeg"}},
								types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AA", MediaType: "image/webp"}},
								types.FileContentBlock{MediaType: "image", Data: []byte{0x47, 0x49, 0x46}},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content := messages[0]["content"].([]map[string]interface{})
	toolResult := content[0]["toolResult"].(map[string]interface{})
	toolContent := toolResult["content"].([]map[string]interface{})
	if len(toolContent) != 4 {
		t.Fatalf("tool result content length = %d, want 4: %#v", len(toolContent), toolContent)
	}
	for i, block := range toolContent {
		image := block["image"].(map[string]interface{})
		source := image["source"].(map[string]interface{})
		want := ""
		if i == 2 {
			want = "AA"
		} else if i == 3 {
			want = "R0lG"
		}
		if i == 3 && image["format"] != "gif" {
			t.Fatalf("tool result image = %#v, want gif format", image)
		}
		if source["bytes"] != want {
			t.Fatalf("tool result image bytes = %#v, want %q", source["bytes"], want)
		}
	}
}

func TestBuildClaudeRequest_RejectsUnsupportedToolResultContentBlock(t *testing.T) {
	model := newTestBedrockModel()
	_, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.CustomContentBlock{ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{}}},
							},
						},
					},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported tool content part type") {
		t.Fatalf("buildClaudeRequest error = %v, want unsupported tool content part type", err)
	}
}

func TestBuildClaudeRequest_ToolResultCachePoint(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call-1",
						Output: &types.ToolResultOutput{
							Type:  types.ToolResultOutputText,
							Value: "tool output",
						},
						ProviderOptions: map[string]interface{}{
							"bedrock": map[string]interface{}{
								"cachePoint": map[string]interface{}{"type": "default", "ttl": "1h"},
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	if messages[0]["role"] != types.RoleUser {
		t.Fatalf("tool result message role = %v, want user", messages[0]["role"])
	}
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content length = %d, want toolResult/cachePoint: %#v", len(content), content)
	}
	toolResult := content[0]["toolResult"].(map[string]interface{})
	if toolResult["toolUseId"] != "call-1" {
		t.Fatalf("toolResult = %#v", toolResult)
	}
	cachePoint := content[1]["cachePoint"].(map[string]interface{})
	if cachePoint["type"] != "default" || cachePoint["ttl"] != "1h" {
		t.Fatalf("cachePoint = %#v", cachePoint)
	}
}

func TestBuildClaudeRequest_NoCachePointWhenPartOptionAbsent(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{
						Image:    []byte("png"),
						MimeType: "image/png",
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %T, want block slice: %#v", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("content length = %d, want only image block: %#v", len(content), content)
	}
	if _, hasCache := content[0]["cachePoint"]; hasCache {
		t.Fatalf("unexpected cachePoint: %#v", content)
	}
}

// getToolSpec extracts the toolSpec map from the first entry in the tools array.
func getToolSpec(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	toolsRaw, ok := body["tools"]
	if !ok {
		t.Fatal("tools key not found in request body")
	}
	tools, ok := toolsRaw.([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected non-empty tools slice, got %T %v", toolsRaw, toolsRaw)
	}
	entry, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for tools[0], got %T", tools[0])
	}
	spec, ok := entry["toolSpec"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected toolSpec map, got %T", entry["toolSpec"])
	}
	return spec
}

// TestBuildClaudeRequest_ToolsForwardedWithStrictMode verifies that tools are
// forwarded in Bedrock's toolSpec format and that strict=true is included when
// the tool has Strict set (#12893).
func TestBuildClaudeRequest_ToolsForwardedWithStrictMode(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{
				Name:        "get_weather",
				Description: "Returns current weather",
				Parameters:  map[string]interface{}{"type": "object"},
				Strict:      true,
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	spec := getToolSpec(t, body)

	if spec["name"] != "get_weather" {
		t.Errorf("name = %v, want get_weather", spec["name"])
	}
	if spec["strict"] != true {
		t.Errorf("strict = %v, want true", spec["strict"])
	}

	inputSchema, ok := spec["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inputSchema map, got %T", spec["inputSchema"])
	}
	if inputSchema["json"] == nil {
		t.Error("inputSchema.json must not be nil")
	}
}

// TestBuildClaudeRequest_ToolChoiceAuto verifies that ToolChoiceAuto maps to
// Bedrock's { "auto": {} } format.
func TestBuildClaudeRequest_ToolChoiceAuto(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	if _, hasAuto := tc["auto"]; !hasAuto {
		t.Errorf("toolChoice should have 'auto' key, got %v", tc)
	}
}

// TestBuildClaudeRequest_ToolChoiceRequired verifies that ToolChoiceRequired
// maps to Bedrock's { "any": {} } format.
func TestBuildClaudeRequest_ToolChoiceRequired(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	if _, hasAny := tc["any"]; !hasAny {
		t.Errorf("toolChoice should have 'any' key for required, got %v", tc)
	}
}

// TestBuildClaudeRequest_ToolChoiceTool_FiltersAndMapsCorrectly verifies that
// ToolChoiceTool filters tools to the named tool and sets
// { "tool": { "name": "..." } } (#12854).
func TestBuildClaudeRequest_ToolChoiceTool_FiltersAndMapsCorrectly(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{Name: "search", Parameters: map[string]interface{}{}},
			{Name: "calculator", Parameters: map[string]interface{}{}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "calculator"},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	// Only the named tool should be in the tools array.
	toolsRaw := body["tools"].([]interface{})
	if len(toolsRaw) != 1 {
		t.Errorf("expected 1 tool after filtering, got %d", len(toolsRaw))
	}
	spec := getToolSpec(t, body)
	if spec["name"] != "calculator" {
		t.Errorf("tool name = %v, want calculator", spec["name"])
	}

	// toolChoice should be { "tool": { "name": "calculator" } }.
	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	toolEntry, ok := tc["tool"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice.tool is %T, want map", tc["tool"])
	}
	if toolEntry["name"] != "calculator" {
		t.Errorf("toolChoice.tool.name = %v, want calculator", toolEntry["name"])
	}
}

// TestBuildClaudeRequest_ToolChoiceNone_NoToolsInBody verifies that when
// ToolChoiceNone is set no tools or toolChoice are added to the request body.
func TestBuildClaudeRequest_ToolChoiceNone_NoToolsInBody(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	if _, ok := body["tools"]; ok {
		t.Error("tools must not be present when toolChoice is none")
	}
	if _, ok := body["toolChoice"]; ok {
		t.Error("toolChoice must not be present when toolChoice is none")
	}
}

// TestBuildClaudeRequest_StrictFalse_OmittedFromSpec verifies that strict is
// not set in the toolSpec when Strict is false (omitempty behaviour).
func TestBuildClaudeRequest_StrictFalse_OmittedFromSpec(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{Name: "bar", Parameters: map[string]interface{}{}, Strict: false},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	spec := getToolSpec(t, body)
	if _, ok := spec["strict"]; ok {
		t.Errorf("strict must be absent when Strict=false, got %v", spec["strict"])
	}
}

func TestBuildClaudeRequest_AnthropicToolSearchProviderTools(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{
				Type:       types.ToolTypeProviderDefined,
				ProviderID: "anthropic.tool_search_bm25_20251119",
				Name:       "tool_search",
			},
			{
				Type:       types.ToolTypeProviderDefined,
				ProviderID: "anthropic.tool_search_regex_20251119",
				Name:       "tool_search",
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v, want 2 provider tools", body["tools"])
	}
	first := tools[0].(map[string]interface{})
	second := tools[1].(map[string]interface{})
	if first["type"] != "tool_search_tool_bm25_20251119" || first["name"] != "tool_search_tool_bm25" {
		t.Errorf("bm25 tool = %#v", first)
	}
	if second["type"] != "tool_search_tool_regex_20251119" || second["name"] != "tool_search_tool_regex" {
		t.Errorf("regex tool = %#v", second)
	}
}

func TestBuildClaudeRequest_ServiceTier(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"bedrock": map[string]interface{}{"serviceTier": "priority"},
		},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	serviceTier, ok := body["serviceTier"].(map[string]interface{})
	if !ok {
		t.Fatalf("serviceTier = %#v, want map", body["serviceTier"])
	}
	if serviceTier["type"] != "priority" {
		t.Errorf("serviceTier.type = %v, want priority", serviceTier["type"])
	}
}

func TestBuildClaudeRequest_PartialReasoningConfigMerge(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "test-key", AWSSecretAccessKey: "test-secret", Region: "us-east-1"})
	model := NewLanguageModel(p, "anthropic.claude-3-5-sonnet-20241022-v2:0", &ModelOptions{
		ReasoningConfig: &ReasoningConfig{Display: "summarized"},
	})
	level := types.ReasoningHigh

	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &level,
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	rc, ok := body["reasoningConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoningConfig = %#v, want map", body["reasoningConfig"])
	}
	if rc["type"] != "enabled" || rc["budgetTokens"] != 16000 || rc["display"] != "summarized" {
		t.Errorf("reasoningConfig = %#v, want derived type/budget plus display", rc)
	}
}

func TestBuildClaudeRequest_OutputObjectSupport(t *testing.T) {
	model := newTestBedrockModel()
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	}

	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config = %#v, want map", body["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config.format = %#v, want map", outputConfig["format"])
	}
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("format = %#v, want json_schema with schema", format)
	}
}

func TestBuildClaudeRequest_DisablesNativeStructuredOutputForClaudeOpus47(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "test-key", AWSSecretAccessKey: "test-secret", Region: "us-east-1"})
	model := NewLanguageModel(p, "anthropic.claude-opus-4-7-20260219-v1:0")
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	if _, ok := body["output_config"]; ok {
		t.Fatalf("output_config = %#v, want omitted for claude-opus-4-7", body["output_config"])
	}
}

func TestBuildClaudeRequest_DisablesNativeStructuredOutputForClaudeOpus48(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "test-key", AWSSecretAccessKey: "test-secret", Region: "us-east-1"})
	model := NewLanguageModel(p, ModelAnthropicClaudeOpus4_8)
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json_schema",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	if _, ok := body["output_config"]; ok {
		t.Fatalf("output_config = %#v, want omitted for claude-opus-4-8", body["output_config"])
	}
}
