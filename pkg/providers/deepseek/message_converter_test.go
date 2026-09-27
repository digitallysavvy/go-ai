package deepseek

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func deepseekOpt(kv ...interface{}) map[string]interface{} {
	inner := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		inner[kv[i].(string)] = kv[i+1]
	}
	return map[string]interface{}{"deepseek": inner}
}

// testGenerateOptions builds a minimal GenerateOptions with a simple text
// prompt and the given deepseek providerOptions block (as produced by
// deepseekOpt).
func testGenerateOptions(providerOptions map[string]interface{}) provider.GenerateOptions {
	return provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: providerOptions,
	}
}

func TestDeepSeekIsV4Model(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"deepseek-v4-pro", true},
		{"deepseek-v4-flash", true},
		{"deepseek-v4-flash-vision-exp", true},
		{"deepseek-flash", true},
		{"deepseek-flash-preview", true},
		{"deepseek-pro", true},
		{"deepseek-pro-mini", true},
		{"deepseek-chat", false},
		{"deepseek-reasoner", false},
	}
	for _, tt := range tests {
		if got := isDeepSeekV4Model(tt.modelID); got != tt.want {
			t.Errorf("isDeepSeekV4Model(%q) = %v, want %v", tt.modelID, got, tt.want)
		}
	}
}

func TestDeepSeekUserMessageImageURL(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "what is this?"},
			types.FileContent{URL: "https://example.com/image.png", MediaType: "image/png"},
		}},
	}
	converted, warnings, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	parts, ok := converted[0]["content"].([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v, want 2 parts", converted[0]["content"])
	}
	if parts[1]["type"] != "image_url" {
		t.Fatalf("part[1] type = %v, want image_url", parts[1]["type"])
	}
	imageURL := parts[1]["image_url"].(map[string]interface{})
	if imageURL["url"] != "https://example.com/image.png" {
		t.Fatalf("image url = %v", imageURL["url"])
	}
}

func TestDeepSeekUserMessageImageDataURL(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/jpg"},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	parts := converted[0]["content"].([]map[string]interface{})
	imageURL := parts[0]["image_url"].(map[string]interface{})
	// image/jpg must be normalized to image/jpeg in the data URL.
	if want := "data:image/jpeg;base64,AQID"; imageURL["url"] != want {
		t.Fatalf("image url = %v, want %v", imageURL["url"], want)
	}
}

func TestDeepSeekUserMessageUnsupportedImageMediaType(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: []byte{1, 2, 3}, MediaType: "image/tiff"},
		}},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error for unsupported image media type")
	}
}

func TestDeepSeekUserMessageImageURLTooLong(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	longURL := "https://example.com/" + string(make([]byte, 8200))
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{URL: longURL, MediaType: "image/png"},
		}},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error for overlong image URL")
	}
}

func TestDeepSeekUserMessageImageReference(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Reference: "file-abc123", MediaType: "image/png"},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	parts := converted[0]["content"].([]map[string]interface{})
	if parts[0]["type"] != "file" || parts[0]["file_id"] != "file-abc123" {
		t.Fatalf("part = %#v, want file/file_id", parts[0])
	}
}

func TestDeepSeekImageDetailOption(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{URL: "https://example.com/image.png", MediaType: "image/png", ProviderOptions: deepseekOpt("imageDetail", "high")},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	parts := converted[0]["content"].([]map[string]interface{})
	imageURL := parts[0]["image_url"].(map[string]interface{})
	if imageURL["detail"] != "high" {
		t.Fatalf("detail = %v, want high", imageURL["detail"])
	}
}

// TestDeepSeekImageDetailOptionRejectsInvalidValue guards TS's
// deepseekFilePartProviderOptions zod enum (low/high/original/auto):
// anything else must be rejected, not forwarded verbatim to image_url.detail.
func TestDeepSeekImageDetailOptionRejectsInvalidValue(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{URL: "https://example.com/image.png", MediaType: "image/png", ProviderOptions: deepseekOpt("imageDetail", "ultra")},
		}},
	}
	_, _, err := m.convertMessages(messages)
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("convertMessages error = %v, want InvalidArgumentError", err)
	}
}

func TestDeepSeekFileDataOption(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: []byte{1, 2, 3}, MediaType: "image/png", Filename: "pic.png", ProviderOptions: deepseekOpt("fileData", true)},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	parts := converted[0]["content"].([]map[string]interface{})
	if parts[0]["type"] != "file" {
		t.Fatalf("type = %v, want file", parts[0]["type"])
	}
	if parts[0]["filename"] != "pic.png" {
		t.Fatalf("filename = %v", parts[0]["filename"])
	}
	if fd, ok := parts[0]["file_data"].(string); !ok || fd == "" {
		t.Fatalf("file_data missing: %#v", parts[0])
	}
}

func TestDeepSeekFileDataWithURLErrors(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{URL: "https://example.com/image.png", MediaType: "image/png", ProviderOptions: deepseekOpt("fileData", true)},
		}},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error combining fileData with a URL image")
	}
}

func TestDeepSeekFileDataWithImageDetailErrors(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: []byte{1, 2, 3}, MediaType: "image/png", ProviderOptions: deepseekOpt("fileData", true, "imageDetail", "high")},
		}},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error combining fileData with imageDetail")
	}
}

func TestDeepSeekToolResultImageContent(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "call-1",
				ToolName:   "render",
				Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.TextContentBlock{Text: "here is the image"},
						types.FileContentBlock{Data: []byte{1, 2, 3}, MediaType: "image/png"},
					},
				},
			},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	if converted[0]["role"] != "tool" || converted[0]["tool_call_id"] != "call-1" {
		t.Fatalf("tool message header mismatch: %#v", converted[0])
	}
	parts, ok := converted[0]["content"].([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v, want 2 parts", converted[0]["content"])
	}
	if parts[0]["type"] != "text" {
		t.Fatalf("part[0] = %#v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Fatalf("part[1] = %#v, want image_url", parts[1])
	}
}

// TestDeepSeekImageProviderReferenceUsesDeepSeekKey guards against resolving
// the wrong provider's file_id from a multi-provider reference map: TS
// resolveProviderReference({reference, provider: 'deepseek'}) looks up the
// "deepseek" key specifically, not the first non-empty key across all
// providers (convert-to-deepseek-chat-messages.test.ts "should convert an
// image provider reference to a file content part").
func TestDeepSeekImageProviderReferenceUsesDeepSeekKey(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "Hello"},
			types.FileContent{
				FileData: types.FileData{
					Type: types.FileDataTypeReference,
					Reference: types.ProviderReference{
						"deepseek": "file-api-deepseek",
						"openai":   "file-openai",
					},
				},
				MediaType: "image/png",
			},
		}},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	parts, ok := converted[0]["content"].([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v, want 2 parts", converted[0]["content"])
	}
	if parts[1]["type"] != "file" || parts[1]["file_id"] != "file-api-deepseek" {
		t.Fatalf("part[1] = %#v, want file_id file-api-deepseek", parts[1])
	}
}

// TestDeepSeekImageProviderReferenceMissingDeepSeekKeyErrors guards TS's
// "should throw when an image reference has no DeepSeek identifier": a
// reference map with no "deepseek" key must raise a typed error, not
// silently fall through to treating the part as inline image data.
func TestDeepSeekImageProviderReferenceMissingDeepSeekKeyErrors(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4-flash-vision-exp")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{
				FileData: types.FileData{
					Type:      types.FileDataTypeReference,
					Reference: types.ProviderReference{"openai": "file-openai"},
				},
				MediaType: "image/png",
			},
		}},
	}
	_, _, err := m.convertMessages(messages)
	var refErr *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &refErr) {
		t.Fatalf("convertMessages error = %v, want NoSuchProviderReferenceError", err)
	}
}

func TestDeepSeekMessageNameOption(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "sys"}}, ProviderOptions: deepseekOpt("name", "narrator")},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}, ProviderOptions: deepseekOpt("name", "alice")},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	if converted[0]["name"] != "narrator" {
		t.Fatalf("system name = %v", converted[0]["name"])
	}
	if converted[1]["name"] != "alice" {
		t.Fatalf("user name = %v", converted[1]["name"])
	}
}

func TestDeepSeekToolMessageNameWarns(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}},
			},
			ProviderOptions: deepseekOpt("name", "should-warn"),
		},
	}
	_, warnings, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	if len(warnings) != 1 || warnings[0].Feature != "message name on tool messages" {
		t.Fatalf("warnings = %#v, want one 'message name on tool messages' warning", warnings)
	}
}

func TestDeepSeekAssistantPrefixCompletion(t *testing.T) {
	prov := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/beta"})
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "continue: "}}},
		{
			Role:            types.RoleAssistant,
			Content:         []types.ContentPart{types.TextContent{Text: "partial"}},
			ProviderOptions: deepseekOpt("prefix", true),
		},
	}
	converted, _, err := m.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error = %v", err)
	}
	if converted[1]["prefix"] != true {
		t.Fatalf("assistant prefix = %v, want true", converted[1]["prefix"])
	}
}

func TestDeepSeekAssistantPrefixRequiresBeta(t *testing.T) {
	prov := New(Config{APIKey: "k"}) // no /beta suffix
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{
			Role:            types.RoleAssistant,
			Content:         []types.ContentPart{types.TextContent{Text: "partial"}},
			ProviderOptions: deepseekOpt("prefix", true),
		},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error: assistant prefix completion requires a beta base URL")
	}
}

func TestDeepSeekAssistantPrefixMustBeFinalMessage(t *testing.T) {
	prov := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/beta"})
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{
			Role:            types.RoleAssistant,
			Content:         []types.ContentPart{types.TextContent{Text: "partial"}},
			ProviderOptions: deepseekOpt("prefix", true),
		},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "more"}}},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error: prefix must be on the final message")
	}
}

func TestDeepSeekPrefixOnNonAssistantErrors(t *testing.T) {
	prov := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/beta"})
	m := NewLanguageModel(prov, "deepseek-v4")

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}, ProviderOptions: deepseekOpt("prefix", true)},
	}
	if _, _, err := m.convertMessages(messages); err == nil {
		t.Fatal("expected error: prefix requires an assistant message")
	}
}

func TestDeepSeekStrictToolCallsRequireBeta(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-v4")

	tools := []types.Tool{{Name: "search", Parameters: map[string]interface{}{}, Strict: true}}
	if _, _, _, err := m.prepareDeepSeekTools(tools, types.ToolChoice{}); err == nil {
		t.Fatal("expected error: strict tool calls require the beta endpoint")
	}
}

func TestDeepSeekMixedStrictToolCallsRejected(t *testing.T) {
	prov := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/beta"})
	m := NewLanguageModel(prov, "deepseek-v4")

	tools := []types.Tool{
		{Name: "search", Parameters: map[string]interface{}{}, Strict: true},
		{Name: "lookup", Parameters: map[string]interface{}{}, Strict: false},
	}
	if _, _, _, err := m.prepareDeepSeekTools(tools, types.ToolChoice{}); err == nil {
		t.Fatal("expected error: mixed strict/non-strict tool calls rejected")
	}
}

func TestDeepSeekStrictToolCallsAllowedOnBeta(t *testing.T) {
	prov := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/beta"})
	m := NewLanguageModel(prov, "deepseek-v4")

	tools := []types.Tool{{Name: "search", Parameters: map[string]interface{}{}, Strict: true}}
	deepseekTools, _, warnings, err := m.prepareDeepSeekTools(tools, types.ToolChoice{})
	if err != nil {
		t.Fatalf("prepareDeepSeekTools error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(deepseekTools) != 1 || deepseekTools[0]["function"].(map[string]interface{})["strict"] != true {
		t.Fatalf("tools = %#v, want strict function tool", deepseekTools)
	}
}

func TestDeepSeekUserIDOption(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	opts := testGenerateOptions(deepseekOpt("userId", "user_123"))
	body, warnings, err := m.buildRequestBodyWithWarnings(&opts, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if body["user_id"] != "user_123" {
		t.Fatalf("user_id = %v, want user_123", body["user_id"])
	}
}

func TestDeepSeekUserIDOptionInvalid(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	opts := testGenerateOptions(deepseekOpt("userId", "not valid!"))
	if _, _, err := m.buildRequestBodyWithWarnings(&opts, false); err == nil {
		t.Fatal("expected error for invalid userId")
	}
}

func TestDeepSeekUserIDOptionTooLong(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	long := make([]byte, 513)
	for i := range long {
		long[i] = 'a'
	}
	opts := testGenerateOptions(deepseekOpt("userId", string(long)))
	if _, _, err := m.buildRequestBodyWithWarnings(&opts, false); err == nil {
		t.Fatal("expected error for overlong userId")
	}
}

func TestDeepSeekLogprobsOption(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	opts := testGenerateOptions(deepseekOpt("logprobs", true))
	body, _, err := m.buildRequestBodyWithWarnings(&opts, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}
	if body["logprobs"] != true {
		t.Fatalf("logprobs = %v, want true", body["logprobs"])
	}
	if _, ok := body["top_logprobs"]; ok {
		t.Fatalf("top_logprobs should be omitted, got %v", body["top_logprobs"])
	}
}

func TestDeepSeekTopLogprobsOptionSetsLogprobs(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	opts := testGenerateOptions(deepseekOpt("topLogprobs", 5))
	body, _, err := m.buildRequestBodyWithWarnings(&opts, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}
	if body["logprobs"] != true {
		t.Fatalf("logprobs = %v, want true", body["logprobs"])
	}
	if body["top_logprobs"] != 5 {
		t.Fatalf("top_logprobs = %v, want 5", body["top_logprobs"])
	}
}

func TestDeepSeekTopLogprobsOptionOutOfRange(t *testing.T) {
	prov := New(Config{APIKey: "k"})
	m := NewLanguageModel(prov, "deepseek-chat")

	opts := testGenerateOptions(deepseekOpt("topLogprobs", 21))
	if _, _, err := m.buildRequestBodyWithWarnings(&opts, false); err == nil {
		t.Fatal("expected error for topLogprobs > 20")
	}
}
