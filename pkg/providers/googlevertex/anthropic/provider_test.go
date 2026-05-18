package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropicprovider "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"golang.org/x/oauth2"
)

func TestLanguageModelNonStreamingRequest(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("anthropic-version") != "" {
			t.Fatalf("anthropic-version header = %q, want empty", r.Header.Get("anthropic-version"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer server.Close()

	p := NewGoogleVertexAnthropicProvider(Options{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "test-token", nil
		},
		HTTPClient: server.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}

	if gotPath != "/claude-sonnet-4-6:rawPredict" {
		t.Fatalf("path = %q, want model rawPredict path", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if _, ok := gotBody["model"]; ok {
		t.Fatal("request body should not include model")
	}
	if gotBody["anthropic_version"] != DefaultVertexAPIVersion {
		t.Fatalf("anthropic_version = %v, want %s", gotBody["anthropic_version"], DefaultVertexAPIVersion)
	}
	if gotBody["stream"] != false {
		t.Fatalf("stream = %v, want false", gotBody["stream"])
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q, want ok", result.Text)
	}
}

func TestLanguageModelStreamingRequest(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	p := New(Options{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "stream-token", nil
		},
		HTTPClient: server.Client(),
	})
	model, err := p.LanguageModel(string(Claude3_5SonnetV2_20241022))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close()

	var text string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream Next error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeText {
			text += chunk.Text
		}
	}

	if gotPath != "/claude-3-5-sonnet-v2@20241022:streamRawPredict" {
		t.Fatalf("path = %q, want streamRawPredict path", gotPath)
	}
	if gotBody["model"] != nil {
		t.Fatal("request body should not include model")
	}
	if gotBody["stream"] != true {
		t.Fatalf("stream = %v, want true", gotBody["stream"])
	}
	if text != "hi" {
		t.Fatalf("stream text = %q, want hi", text)
	}
}

func TestBaseURLFromProjectAndLocation(t *testing.T) {
	p := New(Options{Project: "test-project", Location: "us-east5", AuthToken: staticAuthToken("token")})
	got, err := p.baseURL()
	if err != nil {
		t.Fatalf("baseURL error = %v", err)
	}
	want := "https://us-east5-aiplatform.googleapis.com/v1/projects/test-project/locations/us-east5/publishers/anthropic/models"
	if got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
}

func TestBaseURLGlobalLocation(t *testing.T) {
	p := New(Options{Project: "test-project", Location: "global", AuthToken: staticAuthToken("token")})
	got, err := p.baseURL()
	if err != nil {
		t.Fatalf("baseURL error = %v", err)
	}
	want := "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/publishers/anthropic/models"
	if got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
}

func TestEnvProjectAndLocationDefaults(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "env-project")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	p := New(Options{AuthToken: staticAuthToken("token")})
	got, err := p.baseURL()
	if err != nil {
		t.Fatalf("baseURL error = %v", err)
	}
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/env-project/locations/us-central1/publishers/anthropic/models"
	if got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
}

func TestUserAuthorizationHeaderOverridesGeneratedToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p := New(Options{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "generated", nil
		},
		Headers:    map[string]string{"Authorization": "Bearer user-override"},
		HTTPClient: server.Client(),
	})
	model, err := p.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotAuth != "Bearer user-override" {
		t.Fatalf("Authorization = %q, want user override", gotAuth)
	}
}

func TestTokenSourceProvidesGeneratedAuthorization(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	source := &countingTokenSource{token: "source-token"}
	p := New(Options{
		BaseURL:     server.URL,
		TokenSource: source,
		HTTPClient:  server.Client(),
	})
	model, err := p.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if gotAuth != "Bearer source-token" {
		t.Fatalf("Authorization = %q, want TokenSource bearer", gotAuth)
	}
	if source.calls != 1 {
		t.Fatalf("TokenSource calls = %d, want 1", source.calls)
	}
}

func TestDynamicHeadersResolvedPerRequest(t *testing.T) {
	var gotAuth, gotDynamic string
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDynamic = r.Header.Get("X-Dynamic")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p := New(Options{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "generated", nil
		},
		Headers: map[string]string{"Authorization": "Bearer static"},
		HeadersResolver: func(context.Context) (map[string]string, error) {
			calls++
			return map[string]string{
				"Authorization": "Bearer dynamic",
				"X-Dynamic":     "resolved",
			}, nil
		},
		HTTPClient: server.Client(),
	})
	model, err := p.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("HeadersResolver calls = %d, want 1", calls)
	}
	if gotAuth != "Bearer dynamic" {
		t.Fatalf("Authorization = %q, want dynamic override", gotAuth)
	}
	if gotDynamic != "resolved" {
		t.Fatalf("X-Dynamic = %q, want resolved", gotDynamic)
	}
}

func TestModelIDsMatchTypeScriptUnion(t *testing.T) {
	got := []GoogleVertexAnthropicModelID{
		ClaudeOpus4_7,
		ClaudeOpus4_6,
		ClaudeSonnet4_6,
		ClaudeOpus4_5_20251101,
		ClaudeSonnet4_5_20250929,
		ClaudeOpus4_1_20250805,
		ClaudeOpus4_20250514,
		ClaudeSonnet4_20250514,
		Claude3_7Sonnet_20250219,
		Claude3_5SonnetV2_20241022,
		Claude3_5Haiku_20241022,
		Claude3_5Sonnet_20240620,
		Claude3Haiku_20240307,
		Claude3Sonnet_20240229,
		Claude3Opus_20240229,
	}
	want := []string{
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-opus-4-5@20251101",
		"claude-sonnet-4-5@20250929",
		"claude-opus-4-1@20250805",
		"claude-opus-4@20250514",
		"claude-sonnet-4@20250514",
		"claude-3-7-sonnet@20250219",
		"claude-3-5-sonnet-v2@20241022",
		"claude-3-5-haiku@20241022",
		"claude-3-5-sonnet@20240620",
		"claude-3-haiku@20240307",
		"claude-3-sonnet@20240229",
		"claude-3-opus@20240229",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d IDs, want %d", len(got), len(want))
	}
	for i := range got {
		if string(got[i]) != want[i] {
			t.Fatalf("model ID %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVertexAnthropicForcesJSONToolStructuredOutput(t *testing.T) {
	p := New(Options{BaseURL: "https://example.invalid"})
	model, err := p.LanguageModelWithOptions(string(ClaudeSonnet4_6), &anthropicprovider.ModelOptions{})
	if err != nil {
		t.Fatalf("LanguageModelWithOptions error = %v", err)
	}
	if model.SupportsStructuredOutput() {
		t.Fatal("SupportsStructuredOutput() = true, want false")
	}
}

func TestVertexAnthropicReportsImageInputSupportForVertexModelIDs(t *testing.T) {
	p := New(Options{BaseURL: "https://example.invalid"})
	model, err := p.LanguageModel(string(Claude3Haiku_20240307))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if !model.SupportsImageInput() {
		t.Fatal("SupportsImageInput() = false, want true for Vertex Claude model IDs")
	}
}

func TestVertexAnthropicStrictToolWarningAndOmission(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p := New(Options{
		BaseURL:    server.URL,
		AuthToken:  staticAuthToken("token"),
		HTTPClient: server.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{{
			Name:        "strict_tool",
			Description: "strict",
			Parameters:  map[string]interface{}{"type": "object"},
			Strict:      true,
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one strict unsupported warning", result.Warnings)
	}
	if result.Warnings[0].Type != "unsupported" || result.Warnings[0].Feature != "strict" {
		t.Fatalf("warning = %#v, want unsupported strict warning", result.Warnings[0])
	}
	tools, ok := gotBody["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", gotBody["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool = %#v, want object", tools[0])
	}
	if _, ok := tool["strict"]; ok {
		t.Fatalf("strict should be omitted for Vertex Anthropic tool: %#v", tool)
	}
}

func TestGoogleVertexAnthropicToolsSubset(t *testing.T) {
	tools := []types.Tool{
		GoogleVertexAnthropicTools.Bash20241022(),
		GoogleVertexAnthropicTools.Bash20250124(),
		GoogleVertexAnthropicTools.TextEditor20241022(),
		GoogleVertexAnthropicTools.TextEditor20250124(),
		GoogleVertexAnthropicTools.TextEditor20250429(),
		GoogleVertexAnthropicTools.TextEditor20250728(TextEditor20250728Args{}),
		GoogleVertexAnthropicTools.Computer20241022(Computer20241022Args{}),
		GoogleVertexAnthropicTools.WebSearch20250305(WebSearch20250305Config{}),
		GoogleVertexAnthropicTools.ToolSearchRegex20251119(),
		GoogleVertexAnthropicTools.ToolSearchBm25_20251119(),
		ToolSearchBm25_20251119(),
	}
	want := []string{
		"anthropic.bash_20241022",
		"anthropic.bash_20250124",
		"anthropic.text_editor_20241022",
		"anthropic.text_editor_20250124",
		"anthropic.text_editor_20250429",
		"anthropic.text_editor_20250728",
		"anthropic.computer_20241022",
		"anthropic.web_search_20250305",
		"anthropic.tool_search_regex_20251119",
		"anthropic.tool_search_bm25_20251119",
		"anthropic.tool_search_bm25_20251119",
	}
	for i := range tools {
		if tools[i].Name != want[i] {
			t.Fatalf("tool %d name = %q, want %q", i, tools[i].Name, want[i])
		}
	}
}

func TestLanguageModelCreationDoesNotRequireCredentials(t *testing.T) {
	p := New(Options{BaseURL: "https://example.invalid"})
	if _, err := p.LanguageModel(string(ClaudeSonnet4_6)); err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
}

func TestDefaultProviderAliases(t *testing.T) {
	if GoogleVertexAnthropic == nil {
		t.Fatal("expected default provider")
	}
	if VertexAnthropic != GoogleVertexAnthropic {
		t.Fatal("deprecated alias should point at default provider")
	}
	if GoogleVertexAnthropic.Name() != "google-vertex-anthropic" {
		t.Fatalf("Name() = %q", GoogleVertexAnthropic.Name())
	}
	if CreateVertexAnthropic(Options{BaseURL: "https://example.invalid"}) == nil {
		t.Fatal("deprecated constructor alias returned nil")
	}
}

func TestProviderModelAliasesAndUnsupportedModels(t *testing.T) {
	p := New(Options{BaseURL: "https://example.invalid"})
	if _, err := p.Chat("claude-sonnet-4-6"); err != nil {
		t.Fatalf("Chat error = %v", err)
	}
	if _, err := p.Messages("claude-sonnet-4-6"); err != nil {
		t.Fatalf("Messages error = %v", err)
	}
	if _, err := p.EmbeddingModel("embed"); err == nil {
		t.Fatal("EmbeddingModel expected error")
	}
	if _, err := p.TextEmbeddingModel("embed"); err == nil {
		t.Fatal("TextEmbeddingModel expected error")
	}
	if _, err := p.ImageModel("image"); err == nil {
		t.Fatal("ImageModel expected error")
	}
}

func TestIntegrationVertexAnthropicGenerate(t *testing.T) {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" || os.Getenv("GOOGLE_VERTEX_PROJECT") == "" {
		t.Skip("GOOGLE_APPLICATION_CREDENTIALS and GOOGLE_VERTEX_PROJECT are required")
	}
	if os.Getenv("GOOGLE_VERTEX_LOCATION") == "" {
		t.Setenv("GOOGLE_VERTEX_LOCATION", "us-east5")
	}
	p := New(Options{})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	maxTokens := 16
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Reply with exactly: ok"},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result == nil || result.Text == "" {
		t.Fatal("expected non-empty Vertex Anthropic response")
	}
}

func staticAuthToken(token string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return token, nil
	}
}

type countingTokenSource struct {
	token string
	calls int
}

func (s *countingTokenSource) Token() (*oauth2.Token, error) {
	s.calls++
	return &oauth2.Token{AccessToken: s.token}, nil
}
