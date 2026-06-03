package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderDefaultsAndFactories(t *testing.T) {
	p := New(Config{
		APIKey:       "k",
		ResourceName: "my-resource",
		DeploymentID: "dep-1",
	})
	if p.Name() != "azure-openai" {
		t.Fatalf("Name = %q", p.Name())
	}
	if p.APIVersion() != "v1" {
		t.Fatalf("default APIVersion = %q", p.APIVersion())
	}

	if _, err := p.LanguageModel(""); err != nil {
		t.Fatalf("LanguageModel fallback deployment should work: %v", err)
	}
	if _, err := p.CompletionModel(""); err != nil {
		t.Fatalf("CompletionModel fallback deployment should work: %v", err)
	}
	if _, err := p.EmbeddingModel(""); err != nil {
		t.Fatalf("EmbeddingModel fallback deployment should work: %v", err)
	}
	if _, err := p.ImageModel(""); err != nil {
		t.Fatalf("ImageModel fallback deployment should work: %v", err)
	}
	if _, err := p.SpeechModel(""); err != nil {
		t.Fatalf("SpeechModel fallback deployment should work: %v", err)
	}
	if _, err := p.TranscriptionModel(""); err != nil {
		t.Fatalf("TranscriptionModel fallback deployment should work: %v", err)
	}
	if rm, err := p.RerankingModel("x"); rm != nil || err == nil {
		t.Fatalf("RerankingModel expected unsupported error, got model=%v err=%v", rm, err)
	}
}

func TestProviderRequiresDeploymentWhenUnset(t *testing.T) {
	p := New(Config{APIKey: "k", ResourceName: "r"})
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected deployment error")
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("expected deployment error")
	}
	if _, err := p.ImageModel(""); err == nil {
		t.Fatal("expected deployment error")
	}
	if _, err := p.CompletionModel(""); err == nil {
		t.Fatal("expected deployment error")
	}
}

func TestCompletionModelUsesAzureCompletionURLHeadersAndOptions(t *testing.T) {
	var capturedPath string
	var capturedQuery string
	var capturedAPIKey string
	var capturedAuthorization string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		capturedAPIKey = r.Header.Get("api-key")
		capturedAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"cmpl_azure",
			"created":1711363706,
			"model":"deployment-1",
			"choices":[{"text":"azure completion","finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:     "azure-key",
		BaseURL:    server.URL + "/openai",
		APIVersion: "2025-04-01-preview",
	})
	model, err := p.CompletionModel("deployment-1")
	if err != nil {
		t.Fatalf("CompletionModel: %v", err)
	}
	if model.Provider() != "azure.completion" {
		t.Fatalf("Provider = %q", model.Provider())
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"user": "openai-user"},
			"azure":  map[string]interface{}{"user": "azure-user"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/openai/v1/completions" {
		t.Fatalf("path = %q", capturedPath)
	}
	if capturedQuery != "api-version=2025-04-01-preview" {
		t.Fatalf("query = %q", capturedQuery)
	}
	if capturedAPIKey != "azure-key" {
		t.Fatalf("api-key = %q", capturedAPIKey)
	}
	if capturedAuthorization != "" {
		t.Fatalf("Authorization = %q, want empty", capturedAuthorization)
	}
	if capturedBody["model"] != "deployment-1" || capturedBody["user"] != "azure-user" {
		t.Fatalf("body = %#v", capturedBody)
	}
	if result.Text != "azure completion" {
		t.Fatalf("Text = %q", result.Text)
	}

	serializable, ok := model.(provider.SerializableModel)
	if !ok {
		t.Fatal("Azure completion model should be serializable")
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "azure.completion" || serialized.ModelID != "deployment-1" {
		t.Fatalf("serialized = %#v", serialized)
	}
	restored, err := deserializeCompletionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeCompletionModel: %v", err)
	}
	if restored.Provider() != "azure.completion" || restored.ModelID() != "deployment-1" {
		t.Fatalf("restored = %s/%s", restored.Provider(), restored.ModelID())
	}
}

func TestCompletionModelUsesDeploymentBasedAzureURL(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"text":"","finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:                 "azure-key",
		BaseURL:                server.URL + "/openai",
		APIVersion:             "v1",
		UseDeploymentBasedURLs: true,
	})
	model, err := p.CompletionModel("deployment-1")
	if err != nil {
		t.Fatalf("CompletionModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/openai/deployments/deployment-1/completions" {
		t.Fatalf("path = %q", capturedPath)
	}
}

func TestLanguageBuildRequestBodyAndUsageConversion(t *testing.T) {
	p := New(Config{APIKey: "k", ResourceName: "r", DeploymentID: "d"})
	m := NewLanguageModel(p, "dep-1")
	max := 77
	temp := 0.2

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Text:   "hello",
			System: "sys",
		},
		MaxTokens:   &max,
		Temperature: &temp,
	}, true)

	if body["stream"] != true {
		t.Fatalf("stream = %#v", body["stream"])
	}
	msgs, ok := body["messages"].([]map[string]interface{})
	if !ok || len(msgs) == 0 {
		t.Fatalf("messages missing: %#v", body["messages"])
	}
	if msgs[0]["role"] != "system" {
		t.Fatalf("expected system message first, got %#v", msgs[0])
	}
	if body["max_tokens"] != max || body["temperature"] != temp {
		t.Fatalf("optional params missing: %#v", body)
	}

	cached := 7
	textTokens := 33
	imageTokens := 4
	reasoning := 10
	usage := convertAzureUsage(azureUsage{
		PromptTokens:     100,
		CompletionTokens: 40,
		TotalTokens:      140,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{
			CachedTokens: &cached,
			TextTokens:   &textTokens,
			ImageTokens:  &imageTokens,
		},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{
			ReasoningTokens: &reasoning,
		},
	})
	if usage.InputTokens == nil || *usage.InputTokens != 100 {
		t.Fatalf("input tokens = %#v", usage.InputTokens)
	}
	if usage.InputDetails == nil || usage.OutputDetails == nil {
		t.Fatalf("expected input/output details, got %#v %#v", usage.InputDetails, usage.OutputDetails)
	}
}

func TestEmbeddingAndTranscriptionHelpers(t *testing.T) {
	p := New(Config{APIKey: "k", ResourceName: "r", DeploymentID: "d"})
	emb := NewEmbeddingModel(p, "dep-1")
	if emb.MaxEmbeddingsPerCall() != 2048 {
		t.Fatalf("MaxEmbeddingsPerCall = %d", emb.MaxEmbeddingsPerCall())
	}
	if !emb.SupportsParallelCalls() {
		t.Fatal("SupportsParallelCalls should be true")
	}
	if optsHeaders(nil) != nil {
		t.Fatal("optsHeaders(nil) should be nil")
	}
	if h := optsHeaders(&provider.EmbedModelOptions{Headers: map[string]string{"X-A": "B"}}); h["X-A"] != "B" {
		t.Fatalf("optsHeaders mismatch: %#v", h)
	}

	tm := NewTranscriptionModel(p, "dep-1")
	body, ct, err := tm.buildMultipartBody(&provider.TranscriptionOptions{
		Audio:      []byte("abc"),
		Language:   "fr",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("buildMultipartBody: %v", err)
	}
	if !strings.Contains(ct, "multipart/form-data") {
		t.Fatalf("unexpected content type: %s", ct)
	}
	raw, _ := io.ReadAll(body)
	blob := string(raw)
	if !strings.Contains(blob, `name="language"`) || !strings.Contains(blob, "fr") {
		t.Fatalf("language field missing: %s", blob)
	}
	if !strings.Contains(blob, `name="response_format"`) || !strings.Contains(blob, "verbose_json") {
		t.Fatalf("response_format missing: %s", blob)
	}

	verbose := []byte(`{"text":"hello","duration":2.5,"segments":[{"text":"hello","start":0.0,"end":2.5}]}`)
	vr, err := tm.convertResponse(verbose, true)
	if err != nil || vr.Text != "hello" || len(vr.Timestamps) != 1 {
		t.Fatalf("verbose convertResponse result=%#v err=%v", vr, err)
	}
	simple := []byte(`{"text":"hola"}`)
	sr, err := tm.convertResponse(simple, false)
	if err != nil || sr.Text != "hola" {
		t.Fatalf("simple convertResponse result=%#v err=%v", sr, err)
	}
}

func TestImageAndSpeechBuildHelpers(t *testing.T) {
	p := New(Config{APIKey: "k", ResourceName: "r", DeploymentID: "d"})
	im := NewImageModel(p, "img-dep")
	n := 2
	ib := im.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:  "draw cat",
		N:       &n,
		Size:    "1024x1024",
		Quality: "hd",
		Style:   "vivid",
	})
	if ib["n"] != 2 || ib["size"] != "1024x1024" {
		t.Fatalf("image request body mismatch: %#v", ib)
	}
	if _, err := im.convertResponse([]byte(`{"data":[]}`)); err == nil {
		t.Fatal("convertResponse should fail when no images are returned")
	}
	okResp := []byte(`{"data":[{"url":"https://example.com/i.png"}]}`)
	ir, err := im.convertResponse(okResp)
	if err != nil || ir.URL == "" {
		t.Fatalf("convertResponse result=%#v err=%v", ir, err)
	}

	sm := NewSpeechModel(p, "speech-dep")
	speed := 1.2
	sb := sm.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "nova",
		Speed: &speed,
	})
	if sb["voice"] != "nova" || sb["response_format"] != "mp3" {
		t.Fatalf("speech body mismatch: %#v", sb)
	}
}

func TestSerializeDeserializeLanguageModel(t *testing.T) {
	p := New(Config{
		APIKey:       "k",
		ResourceName: "r",
		DeploymentID: "dep",
		APIVersion:   "2024-10-21",
	})
	mAny, err := p.ChatModel("dep-a")
	if err != nil {
		t.Fatalf("ChatModel: %v", err)
	}
	lm := mAny.(*LanguageModel)
	s := lm.Serialize()
	restored, err := deserializeModel(s)
	if err != nil {
		t.Fatalf("deserializeModel: %v", err)
	}
	if restored.Provider() != "azure.chat" || restored.ModelID() != "dep-a" {
		t.Fatalf("restored model mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestResponsesModelMatchesAzureResponsesRequest(t *testing.T) {
	var capturedPath string
	var capturedQuery string
	var capturedHeaders http.Header
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		capturedHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_123",
			"model":"test-deployment",
			"created_at":123,
			"output":[{"type":"message","id":"msg_123","role":"assistant","content":[{"type":"output_text","text":"done"}]}],
			"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
		}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:     "test-api-key",
		BaseURL:    server.URL + "/openai",
		APIVersion: "v1",
		Headers: map[string]string{
			"X-Provider": "provider",
		},
	})
	model, err := p.LanguageModel("test-deployment")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if model.Provider() != "azure.responses" {
		t.Fatalf("Provider = %q, want azure.responses", model.Provider())
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Headers: map[string]string{
			"X-Request": "request",
		},
		ProviderOptions: map[string]interface{}{
			"azure": map[string]interface{}{
				"serviceTier": "priority",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("Text = %q, want done", result.Text)
	}
	if capturedPath != "/openai/v1/responses" {
		t.Fatalf("path = %q, want /openai/v1/responses", capturedPath)
	}
	if capturedQuery != "api-version=v1" {
		t.Fatalf("query = %q, want api-version=v1", capturedQuery)
	}
	if capturedHeaders.Get("api-key") != "test-api-key" {
		t.Fatalf("api-key header missing: %#v", capturedHeaders)
	}
	if capturedHeaders.Get("Authorization") != "" {
		t.Fatalf("Authorization header should be absent, got %q", capturedHeaders.Get("Authorization"))
	}
	if capturedHeaders.Get("X-Provider") != "provider" || capturedHeaders.Get("X-Request") != "request" {
		t.Fatalf("custom headers missing: %#v", capturedHeaders)
	}
	if capturedBody["model"] != "test-deployment" {
		t.Fatalf("model body = %#v", capturedBody["model"])
	}
	if capturedBody["service_tier"] != "priority" {
		t.Fatalf("service_tier body = %#v, want priority", capturedBody["service_tier"])
	}
	if _, ok := capturedBody["input"]; !ok {
		t.Fatalf("responses body missing input: %#v", capturedBody)
	}
	if len(result.Content) == 0 {
		t.Fatalf("result content missing")
	}
	text, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want TextContent", result.Content[0])
	}
	if _, ok := text.ProviderOptions["azure"]; !ok {
		t.Fatalf("provider options = %#v, want azure metadata key", text.ProviderOptions)
	}
}

func TestResponsesModelUseDeploymentBasedURLs(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_123",
			"model":"test-deployment",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],
			"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
		}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:                 "test-api-key",
		BaseURL:                server.URL + "/openai",
		UseDeploymentBasedURLs: true,
	})
	model, err := p.ResponsesModel("test-deployment")
	if err != nil {
		t.Fatalf("ResponsesModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/openai/deployments/test-deployment/responses" {
		t.Fatalf("path = %q, want deployment-based responses path", capturedPath)
	}
}

func TestConvertResponseMapsToolCalls(t *testing.T) {
	p := New(Config{APIKey: "k", ResourceName: "r", DeploymentID: "d"})
	m := NewLanguageModel(p, "dep-1")
	resp := azureResponse{
		Choices: []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				FinishReason: "tool_calls",
				Message: struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				}{
					Content: "",
					ToolCalls: []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}{
						{
							ID: "call_1",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "lookup",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	}

	got := m.convertResponse(resp)
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].ToolName != "lookup" {
		t.Fatalf("tool name = %q", got.ToolCalls[0].ToolName)
	}
	if got.ToolCalls[0].Arguments["city"] != "Paris" {
		t.Fatalf("tool args = %#v", got.ToolCalls[0].Arguments)
	}
}

var _ = types.Warning{} // keep types import in this test package for parity compile coverage
