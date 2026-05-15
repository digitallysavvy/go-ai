package xai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestXAIProvider_WrappersAndDefaults(t *testing.T) {
	t.Parallel()

	p := CreateXai(Config{APIKey: "test-key"})
	if p == nil || p.Client() == nil {
		t.Fatal("CreateXai returned nil provider or nil client")
	}
	if p.Files() == nil {
		t.Fatal("Files() returned nil")
	}

	lm, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if lm.ModelID() != "grok-beta" {
		t.Fatalf("default responses model ID = %q, want grok-beta", lm.ModelID())
	}

	legacyLM, err := p.ChatCompletionsLanguageModel("")
	if err != nil {
		t.Fatalf("ChatCompletionsLanguageModel() error = %v", err)
	}
	if legacyLM.ModelID() != "grok-beta" {
		t.Fatalf("default chat-completions model ID = %q, want grok-beta", legacyLM.ModelID())
	}

	img, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	if img.ModelID() != "grok-image-1" {
		t.Fatalf("default image model ID = %q, want grok-image-1", img.ModelID())
	}

	video, err := p.VideoModel("")
	if err != nil {
		t.Fatalf("VideoModel() error = %v", err)
	}
	if video.ModelID() != "grok-imagine-video" {
		t.Fatalf("default video model ID = %q, want grok-imagine-video", video.ModelID())
	}

	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel expected unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
}

func TestXAIToolBuildersAndProviderExecutedBehavior(t *testing.T) {
	t.Parallel()

	date := "2026-05-01"
	enableImage := true
	enableVideo := true

	tools := []types.Tool{
		CodeExecution(),
		ViewImage(),
		XSearch(XSearchConfig{
			AllowedXHandles:          []string{"digitallysavvy"},
			ExcludedXHandles:         []string{"spam"},
			FromDate:                 &date,
			EnableImageUnderstanding: &enableImage,
			EnableVideoUnderstanding: &enableVideo,
		}),
	}

	for _, tool := range tools {
		if !tool.ProviderExecuted {
			t.Fatalf("%s must be provider-executed", tool.Name)
		}
		if tool.Execute == nil {
			t.Fatalf("%s Execute must be set", tool.Name)
		}
		_, err := tool.Execute(context.Background(), map[string]interface{}{"q": "test"}, types.ToolExecutionOptions{ToolCallID: "tc"})
		if err == nil {
			t.Fatalf("%s execute expected provider-executed noop error", tool.Name)
		}
		var te *types.ToolExecutionError
		if !errors.As(err, &te) || !te.ProviderExecuted {
			t.Fatalf("%s execute error type mismatch: %T", tool.Name, err)
		}
	}
}

func TestXAIPrepareResponsesToolsCoverage(t *testing.T) {
	t.Parallel()

	max := 3
	tools := []types.Tool{
		WebSearch(WebSearchConfig{AllowedDomains: []string{"example.com"}}),
		XSearch(XSearchConfig{AllowedXHandles: []string{"digitallysavvy"}}),
		CodeExecution(),
		ViewImage(),
		ViewXVideo(),
		FileSearch(FileSearchConfig{VectorStoreIDs: []string{"vs_1"}, MaxNumResults: max}),
		MCPServer(MCPServerConfig{
			ServerURL:         "https://mcp.example.com",
			ServerLabel:       "mcp",
			ServerDescription: "desc",
			AllowedTools:      []string{"search"},
			Headers:           map[string]string{"x": "1"},
			Authorization:     "Bearer abc",
		}),
		{
			Name:        "my_fn",
			Description: "custom",
			Parameters:  map[string]interface{}{"type": "object"},
		},
	}

	wire := prepareXAIResponsesTools(tools)
	if len(wire) != len(tools) {
		t.Fatalf("wire tools length = %d, want %d", len(wire), len(tools))
	}

	codeTool := wire[2].(map[string]interface{})
	if codeTool["type"] != "code_interpreter" {
		t.Fatalf("xai.code_execution should map to code_interpreter, got %#v", codeTool)
	}
	fnTool := wire[len(wire)-1].(map[string]interface{})
	if fnTool["type"] != "function" || fnTool["name"] != "my_fn" {
		t.Fatalf("function fallback mapping mismatch: %#v", fnTool)
	}
}

func TestXAIModelMetadataAndErrorHelpers(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "test-key"})
	lm := NewLanguageModel(p, "grok-3")
	if lm.SpecificationVersion() != "v3" {
		t.Fatalf("language spec = %q", lm.SpecificationVersion())
	}
	if lm.Provider() != "xai" {
		t.Fatalf("language provider = %q", lm.Provider())
	}
	if lm.ModelID() != "grok-3" {
		t.Fatalf("language model ID = %q", lm.ModelID())
	}
	if !lm.SupportsTools() || !lm.SupportsStructuredOutput() || lm.SupportsImageInput() {
		t.Fatal("language model capabilities mismatch")
	}
	lerr := lm.handleError(errors.New("chat fail"))
	var lpErr *providererrors.ProviderError
	if !errors.As(lerr, &lpErr) || lpErr.Provider != "xai" {
		t.Fatalf("language handleError mismatch: %v", lerr)
	}

	im := NewImageModel(p, "grok-image-1")
	if im.SpecificationVersion() != "v3" {
		t.Fatalf("image spec = %q", im.SpecificationVersion())
	}
	if im.Provider() != "xai" {
		t.Fatalf("image provider = %q", im.Provider())
	}
	if im.ModelID() != "grok-image-1" {
		t.Fatalf("image model ID = %q", im.ModelID())
	}
	ierr := im.handleError(errors.New("image fail"))
	var ipErr *providererrors.ProviderError
	if !errors.As(ierr, &ipErr) || ipErr.Provider != "xai" {
		t.Fatalf("image handleError mismatch: %v", ierr)
	}
}

func TestXAIResponsesToolNameResolutionHelpers(t *testing.T) {
	t.Parallel()

	toolNames := resolveProviderToolNames([]types.Tool{
		{Name: "xai.web_search"},
		{Name: "xai.code_execution"},
		{Name: "custom-tool"},
	})
	if toolNames["xai.web_search"] != "xai.web_search" {
		t.Fatalf("resolveProviderToolNames missing web_search: %#v", toolNames)
	}

	if got := resolvedToolName("web_search_call", "", toolNames); got != "xai.web_search" {
		t.Fatalf("resolvedToolName(web_search_call) = %q", got)
	}
	if got := resolvedToolName("x_search_call", "x_keyword_search", toolNames); got != "xai.x_search" {
		t.Fatalf("resolvedToolName(x_search subtool) = %q", got)
	}
	if got := resolvedToolName("code_execution_call", "", toolNames); got != "xai.code_execution" {
		t.Fatalf("resolvedToolName(code_execution_call) = %q", got)
	}
	if got := resolvedToolName("unknown_call", "", toolNames); got != "unknown_call" {
		t.Fatalf("resolvedToolName(default) = %q", got)
	}
	if got := providerToolNameFromType("view_image_call"); got != "xai.view_image" {
		t.Fatalf("providerToolNameFromType(view_image_call) = %q", got)
	}
}

func TestXAIStreamHelpersAndLastAssistantText(t *testing.T) {
	t.Parallel()

	// newXAIStream + Next/Err/Close coverage with immediate done stream.
	stream := newXAIStream(io.NopCloser(strings.NewReader("data: [DONE]\n\n")), "")
	_, err := stream.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("newXAIStream Next() err = %v, want EOF", err)
	}
	if stream.Err() != nil {
		t.Fatalf("xaiStream.Err() = %v, want nil after EOF", stream.Err())
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("xaiStream.Close() error = %v", err)
	}

	respStream := newXAIResponsesStream(io.NopCloser(strings.NewReader("data: [DONE]\n\n")))
	_, err = respStream.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("xaiResponsesStream Next() err = %v, want EOF", err)
	}
	if respStream.Err() != nil {
		t.Fatalf("xaiResponsesStream.Err() = %v, want nil after EOF", respStream.Err())
	}
	if err := respStream.Close(); err != nil {
		t.Fatalf("xaiResponsesStream.Close() error = %v", err)
	}

	// lastAssistantText branch coverage.
	if got := lastAssistantText(&provider.GenerateOptions{Prompt: types.Prompt{Text: "simple"}}); got != "" {
		t.Fatalf("lastAssistantText(simple prompt) = %q, want empty", got)
	}
	if got := lastAssistantText(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "u"}}},
	}}}); got != "" {
		t.Fatalf("lastAssistantText(last user) = %q, want empty", got)
	}
	if got := lastAssistantText(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "assistant text"}}},
	}}}); got != "assistant text" {
		t.Fatalf("lastAssistantText(assistant) = %q", got)
	}

	rm := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-3")
	wrapped := rm.wrapErr(errors.New("boom"))
	var pErr *providererrors.ProviderError
	if !errors.As(wrapped, &pErr) || pErr.Provider != "xai.responses" {
		t.Fatalf("wrapErr mismatch: %v", wrapped)
	}
}
