package perplexity

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPerplexityReasoningWarning(t *testing.T) {
	// Minimal server returning a valid OpenAI-compatible response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning for unsupported reasoning, got none")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && w.Feature == "reasoning" && w.Details == "This provider does not support reasoning configuration." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TypeScript reasoning warning, got: %+v", result.Warnings)
	}
}

func TestPerplexityNoWarningWhenReasoningNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	opts := &provider.GenerateOptions{}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings when Reasoning is nil, got: %+v", result.Warnings)
	}
}

// TestPerplexityCostInMetadata verifies that when the Perplexity API returns a
// nested usage.cost object, the cost fields are surfaced in
// providerMetadata["perplexity"].Cost — matching the TS SDK structure.
func TestPerplexityCostInMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Cost is a nested object under usage.cost (matches real Perplexity API).
		_, _ = w.Write([]byte(`{
			"id":"test","model":"sonar",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],
			"usage":{
				"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,
				"citation_tokens":3,"num_search_queries":1,
				"cost":{
					"input_tokens_cost":0.001,
					"output_tokens_cost":0.002,
					"request_cost":0.0005,
					"total_cost":0.0035
				}
			}
		}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata is nil; expected perplexity metadata")
	}

	raw, ok := result.ProviderMetadata["perplexity"]
	if !ok {
		t.Fatalf("ProviderMetadata missing 'perplexity' key, got: %+v", result.ProviderMetadata)
	}

	meta, ok := raw.(PerplexityMetadata)
	if !ok {
		t.Fatalf("ProviderMetadata['perplexity'] type = %T, want PerplexityMetadata", raw)
	}

	// usage sub-fields
	if meta.Usage.CitationTokens == nil || *meta.Usage.CitationTokens != 3 {
		t.Errorf("Usage.CitationTokens = %v, want 3", meta.Usage.CitationTokens)
	}
	if meta.Usage.NumSearchQueries == nil || *meta.Usage.NumSearchQueries != 1 {
		t.Errorf("Usage.NumSearchQueries = %v, want 1", meta.Usage.NumSearchQueries)
	}

	// cost sub-object must be present and have correct values
	if meta.Cost == nil {
		t.Fatal("Cost is nil; expected cost object")
	}
	checkCost := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s is nil, want %v", name, want)
			return
		}
		if *got != want {
			t.Errorf("%s = %v, want %v", name, *got, want)
		}
	}
	checkCost("Cost.InputTokensCost", meta.Cost.InputTokensCost, 0.001)
	checkCost("Cost.OutputTokensCost", meta.Cost.OutputTokensCost, 0.002)
	checkCost("Cost.RequestCost", meta.Cost.RequestCost, 0.0005)
	checkCost("Cost.TotalCost", meta.Cost.TotalCost, 0.0035)
}

func TestPerplexityResponseMetadataMatchesTypeScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp-123","created":1770768220,"model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")
	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if result.ResponseMetadata == nil {
		t.Fatal("ResponseMetadata is nil")
	}
	if result.ResponseMetadata.ID != "resp-123" || result.ResponseMetadata.ModelID != "sonar" {
		t.Fatalf("ResponseMetadata = %+v, want id/model from response", result.ResponseMetadata)
	}
	if got := result.ResponseMetadata.Timestamp.Unix(); got != 1770768220 {
		t.Fatalf("ResponseMetadata.Timestamp = %d, want 1770768220", got)
	}
	if result.ResponseMetadata.Headers["Test-Header"] != "test-value" {
		t.Fatalf("ResponseMetadata.Headers = %+v, want Test-Header", result.ResponseMetadata.Headers)
	}
}

// TestPerplexityMetadataAlwaysPresent verifies that providerMetadata["perplexity"]
// is always set — even when no cost information is returned — matching the TS SDK
// which unconditionally sets this field.
func TestPerplexityMetadataAlwaysPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata should always be set for Perplexity responses")
	}
	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("ProviderMetadata['perplexity'] type = %T, want PerplexityMetadata", result.ProviderMetadata["perplexity"])
	}
	// Cost must be nil when not in response.
	if meta.Cost != nil {
		t.Errorf("Cost should be nil when not in response, got: %+v", meta.Cost)
	}
	// Images must be nil/empty when not in response.
	if len(meta.Images) != 0 {
		t.Errorf("Images should be empty when not in response, got: %+v", meta.Images)
	}
}

// TestPerplexityImagesInMetadata verifies that the images array from the API
// response is mapped to providerMetadata["perplexity"].Images with camelCase
// field names (imageUrl, originUrl) — matching the TS SDK public shape.
func TestPerplexityImagesInMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id":"test","model":"sonar",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],
			"images":[{"image_url":"https://img.example.com/a.jpg","origin_url":"https://example.com/a","height":100,"width":200}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
		}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("expected PerplexityMetadata, got %T", result.ProviderMetadata["perplexity"])
	}
	if len(meta.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(meta.Images))
	}
	img := meta.Images[0]
	if img.ImageUrl != "https://img.example.com/a.jpg" {
		t.Errorf("ImageUrl = %q, want %q", img.ImageUrl, "https://img.example.com/a.jpg")
	}
	if img.OriginUrl != "https://example.com/a" {
		t.Errorf("OriginUrl = %q, want %q", img.OriginUrl, "https://example.com/a")
	}
	if img.Height != 100 || img.Width != 200 {
		t.Errorf("Height/Width = %d/%d, want 100/200", img.Height, img.Width)
	}
}

func TestPerplexityStreamProviderMetadataMatchesTypeScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"resp-1\",\"created\":1710000000,\"model\":\"sonar\",\"citations\":[\"https://example.com/a\"],\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"resp-1\",\"created\":1710000000,\"model\":\"sonar\",\"images\":[{\"image_url\":\"https://img.example.com/a.jpg\",\"origin_url\":\"https://example.com/a\",\"height\":100,\"width\":200}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15,\"citation_tokens\":3,\"num_search_queries\":1,\"reasoning_tokens\":2,\"cost\":{\"input_tokens_cost\":0.001,\"output_tokens_cost\":0.002,\"request_cost\":0.0005,\"total_cost\":0.0035}},\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")
	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	var chunkTypes []provider.ChunkType
	var responseMetadata *provider.ResponseMetadata
	var finish *provider.StreamChunk
	for {
		ch, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		chunkTypes = append(chunkTypes, ch.Type)
		if ch.Type == provider.ChunkTypeSource && ch.SourceContent != nil {
			if ch.SourceContent.URL != "https://example.com/a" {
				t.Fatalf("source URL = %q, want https://example.com/a", ch.SourceContent.URL)
			}
		}
		if ch.Type == provider.ChunkTypeResponseMetadata {
			responseMetadata = ch.ResponseMetadata
		}
		if ch.Type == provider.ChunkTypeFinish {
			finish = ch
		}
	}
	if responseMetadata == nil {
		t.Fatal("expected response metadata chunk")
	}
	if responseMetadata.ID != "resp-1" || responseMetadata.ModelID != "sonar" || responseMetadata.Timestamp.Unix() != 1710000000 {
		t.Fatalf("response metadata = %+v, want first SSE id/model/created", responseMetadata)
	}
	wantPrefix := []provider.ChunkType{
		provider.ChunkTypeStreamStart,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeSource,
		provider.ChunkTypeText,
	}
	if len(chunkTypes) < len(wantPrefix) {
		t.Fatalf("chunk types = %+v, want prefix %+v", chunkTypes, wantPrefix)
	}
	for i, want := range wantPrefix {
		if chunkTypes[i] != want {
			t.Fatalf("chunkTypes[%d] = %s, want %s; all types=%+v", i, chunkTypes[i], want, chunkTypes)
		}
	}
	if finish == nil {
		t.Fatal("missing finish chunk")
	}
	if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 10 {
		t.Fatalf("finish usage = %+v, want input tokens 10", finish.Usage)
	}
	if finish.Usage.OutputDetails == nil || finish.Usage.OutputDetails.ReasoningTokens == nil || *finish.Usage.OutputDetails.ReasoningTokens != 2 {
		t.Fatalf("finish usage output details = %+v, want reasoning tokens 2", finish.Usage.OutputDetails)
	}

	var meta map[string]PerplexityMetadata
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatalf("ProviderMetadata is not valid JSON: %v", err)
	}
	perp := meta["perplexity"]
	if perp.Usage.CitationTokens == nil || *perp.Usage.CitationTokens != 3 {
		t.Fatalf("citationTokens = %v, want 3", perp.Usage.CitationTokens)
	}
	if perp.Cost == nil || perp.Cost.TotalCost == nil || *perp.Cost.TotalCost != 0.0035 {
		t.Fatalf("cost = %+v, want totalCost 0.0035", perp.Cost)
	}
	if len(perp.Images) != 1 || perp.Images[0].ImageUrl != "https://img.example.com/a.jpg" {
		t.Fatalf("images = %+v, want mapped image metadata", perp.Images)
	}
}

func TestPerplexityStreamIncludeRawChunksMatchesTypeScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"ppl-123\",\"created\":1234567890,\"model\":\"sonar\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":null}],\"citations\":[\"https://example.com\"]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"ppl-789\",\"created\":1234567890,\"model\":\"sonar\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15,\"citation_tokens\":2,\"num_search_queries\":1}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")
	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{IncludeRawChunks: true})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer stream.Close()

	var sawRawBeforeText bool
	var sawText bool
	for {
		ch, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		if ch.Type == provider.ChunkTypeRaw && !sawText {
			raw, ok := ch.Raw.(map[string]interface{})
			if ok && raw["id"] == "ppl-123" {
				sawRawBeforeText = true
			}
		}
		if ch.Type == provider.ChunkTypeText && ch.Text == "Hello" {
			sawText = true
		}
	}
	if !sawRawBeforeText || !sawText {
		t.Fatalf("raw/text ordering mismatch: sawRawBeforeText=%v sawText=%v", sawRawBeforeText, sawText)
	}
}

func TestPerplexityGenerateCitationsAndReasoningUsage(t *testing.T) {
	reasoningTokens := 2
	usage := perplexityUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		ReasoningTokens:  &reasoningTokens,
	}
	converted := convertPerplexityUsage(usage)
	if converted.InputDetails == nil || converted.InputDetails.NoCacheTokens == nil || *converted.InputDetails.NoCacheTokens != 10 {
		t.Fatalf("input details = %+v, want noCache tokens 10", converted.InputDetails)
	}
	// 2214258: Perplexity reports reasoning_tokens as ADDITIONAL to
	// completion_tokens, not a subset of it -- text tokens equal
	// completion_tokens unmodified, and the output total is completion+reasoning.
	if converted.OutputDetails == nil || converted.OutputDetails.TextTokens == nil || *converted.OutputDetails.TextTokens != 5 {
		t.Fatalf("output details = %+v, want text tokens 5", converted.OutputDetails)
	}
	if converted.OutputDetails.ReasoningTokens == nil || *converted.OutputDetails.ReasoningTokens != 2 {
		t.Fatalf("output details = %+v, want reasoning tokens 2", converted.OutputDetails)
	}
	if converted.OutputTokens == nil || *converted.OutputTokens != 7 {
		t.Fatalf("output tokens = %v, want 7 (completion+reasoning)", converted.OutputTokens)
	}
	if converted.TotalTokens == nil || *converted.TotalTokens != 17 {
		t.Fatalf("total tokens = %v, want 17 (prompt+completion+reasoning)", converted.TotalTokens)
	}
	// convertPerplexityUsage no longer populates Raw itself: Raw is now set by
	// the caller from the untransformed wire bytes (extractRawUsage), so that
	// ALL fields the API returns are preserved, not just the ones modeled by
	// perplexityUsage. Verify that behavior end-to-end via convertResponse,
	// including a field not modeled by perplexityUsage at all.
	if converted.Raw != nil {
		t.Fatalf("convertPerplexityUsage() Raw = %v, want nil (populated by caller)", converted.Raw)
	}

	rawBody := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"reasoning_tokens":2,"unmodeled_future_field":"x"}}`)
	result := NewLanguageModel(New(Config{APIKey: "test-key"}), "sonar").convertResponse(perplexityResponse{
		Citations: []string{"https://example.com/a"},
		Choices: []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{
			{
				Index:        0,
				FinishReason: "stop",
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "hello"},
			},
		},
		Usage: &usage,
	}, rawBody)
	if len(result.Content) != 1 {
		t.Fatalf("content len = %d, want 1 source", len(result.Content))
	}
	source, ok := result.Content[0].(types.SourceContent)
	if !ok || source.URL != "https://example.com/a" {
		t.Fatalf("source content = %#v, want citation source", result.Content[0])
	}
	if got := result.Usage.Raw["reasoning_tokens"]; got != float64(2) {
		t.Fatalf("raw reasoning_tokens = %v, want 2", got)
	}
	if got := result.Usage.Raw["unmodeled_future_field"]; got != "x" {
		t.Fatalf("Usage.Raw = %+v, want unmodeled_future_field preserved (full decoded usage object)", result.Usage.Raw)
	}
}

func TestPerplexityMissingUsageMatchesTypeScriptUndefinedUsage(t *testing.T) {
	result := NewLanguageModel(New(Config{APIKey: "test-key"}), "sonar").convertResponse(perplexityResponse{
		Choices: []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{
			{
				Index:        0,
				FinishReason: "stop",
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "hello"},
			},
		},
	}, nil)
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.TotalTokens != nil || result.Usage.Raw != nil {
		t.Fatalf("usage = %+v, want empty usage when provider omits usage", result.Usage)
	}
	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("metadata type = %T, want PerplexityMetadata", result.ProviderMetadata["perplexity"])
	}
	if meta.Usage.CitationTokens != nil || meta.Usage.NumSearchQueries != nil || meta.Cost != nil || meta.Images != nil {
		t.Fatalf("metadata = %+v, want null/empty fields for omitted usage/images", meta)
	}
}

func TestPerplexityUsageIncludesZeroReasoningDetailsLikeTypeScript(t *testing.T) {
	usage := convertPerplexityUsage(perplexityUsage{
		PromptTokens:     11,
		CompletionTokens: 392,
		TotalTokens:      403,
	})
	if usage.InputDetails == nil || usage.InputDetails.NoCacheTokens == nil || *usage.InputDetails.NoCacheTokens != 11 {
		t.Fatalf("InputDetails = %+v, want noCache=11", usage.InputDetails)
	}
	if usage.OutputDetails == nil || usage.OutputDetails.TextTokens == nil || *usage.OutputDetails.TextTokens != 392 {
		t.Fatalf("OutputDetails = %+v, want text=392", usage.OutputDetails)
	}
	if usage.OutputDetails.ReasoningTokens == nil || *usage.OutputDetails.ReasoningTokens != 0 {
		t.Fatalf("ReasoningTokens = %v, want 0", usage.OutputDetails.ReasoningTokens)
	}
}

func TestPerplexityReasoningNotAddedToBody(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildRequestBody(opts, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("Perplexity should not set reasoning_effort in body; warning is emitted instead")
	}
}

func TestPerplexityRequestBodyMatchesTypeScriptProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	topK := 5
	frequencyPenalty := 0.2
	presencePenalty := 0.3
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "Hello"},
		TopK:             &topK,
		FrequencyPenalty: &frequencyPenalty,
		PresencePenalty:  &presencePenalty,
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"search_recency_filter": "month",
				"return_images":         true,
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	if _, ok := body["stream"]; ok {
		t.Fatalf("non-streaming request body must omit stream=false to match TypeScript, got %v", body["stream"])
	}
	if body["search_recency_filter"] != "month" || body["return_images"] != true {
		t.Fatalf("provider options were not passed through: %+v", body)
	}
	if body["top_k"] != 5 || body["frequency_penalty"] != 0.2 || body["presence_penalty"] != 0.3 {
		t.Fatalf("standardized settings missing from body: %+v", body)
	}
	if _, ok := body["messages"]; !ok {
		t.Fatalf("messages missing from body: %+v", body)
	}
}

func TestPerplexityJSONResponseFormatOmitsNilSchemaLikeTypeScript(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	responseFormat := body["response_format"].(map[string]interface{})
	jsonSchema := responseFormat["json_schema"].(map[string]interface{})
	if _, ok := jsonSchema["schema"]; ok {
		t.Fatalf("json_schema should omit nil schema to match TypeScript JSON.stringify behavior, got %+v", jsonSchema)
	}

	schema := map[string]interface{}{"type": "object"}
	body, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() with schema error = %v", err)
	}
	responseFormat = body["response_format"].(map[string]interface{})
	jsonSchema = responseFormat["json_schema"].(map[string]interface{})
	if jsonSchema["schema"] == nil {
		t.Fatalf("json_schema should include non-nil schema, got %+v", jsonSchema)
	}
}

func TestPerplexityMessageConversionMatchesTypeScript(t *testing.T) {
	messages, err := convertToPerplexityMessages([]types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello "},
				types.TextContent{Text: "World"},
			},
		},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v", err)
	}
	if messages[0]["content"] != "Hello World" {
		t.Fatalf("content = %#v, want concatenated text", messages[0]["content"])
	}

	messages, err = convertToPerplexityMessages([]types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Analyze this PDF"},
				types.FileContent{Data: []byte("pdf"), MediaType: "application/pdf", Filename: "test.pdf"},
			},
		},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() PDF error = %v", err)
	}
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("PDF content = %#v, want multipart array", messages[0]["content"])
	}
	if content[1]["type"] != "file_url" || content[1]["file_name"] != "test.pdf" {
		t.Fatalf("PDF part = %#v, want TypeScript file_url shape", content[1])
	}

	messages, err = convertToPerplexityMessages([]types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{Data: []byte{0, 1, 2, 3}, MediaType: "image/png"},
			},
		},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() image error = %v", err)
	}
	content = messages[0]["content"].([]map[string]interface{})
	imageURL := content[0]["image_url"].(map[string]interface{})["url"].(string)
	if imageURL != "data:image/png;base64,AAECAw==" {
		t.Fatalf("image url = %q, want TypeScript data URL", imageURL)
	}

	_, err = convertToPerplexityMessages([]types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"perplexity": "file-ref-123"}, MediaType: "image/png"}},
			},
		},
	})
	if err == nil {
		t.Fatal("expected provider-reference file parts to be rejected")
	}
}

func TestPerplexityWarningsMatchTypeScript(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	topK := 5
	seed := 123
	reasoning := types.ReasoningDefault
	warnings := model.checkWarnings(&provider.GenerateOptions{
		TopK:          &topK,
		Seed:          &seed,
		StopSequences: []string{"stop"},
		Reasoning:     &reasoning,
	})
	if len(warnings) != 3 {
		t.Fatalf("warnings = %+v, want topK, stopSequences, seed only", warnings)
	}

	reasoning = types.ReasoningHigh
	warnings = model.checkWarnings(&provider.GenerateOptions{Reasoning: &reasoning})
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "reasoning" {
		t.Fatalf("reasoning warning = %+v, want TypeScript unsupported reasoning warning", warnings)
	}
}

// pngBase64 decodes to the PNG signature bytes (0x89 P N G \r \n \x1a \n),
// matching the fixture used by the TS SDK's convert-to-perplexity-messages
// test suite ("top-level-only media type resolution").
const pngBase64 = "iVBORw0KGgo="

// pdfBase64 decodes to "%PDF-1.4", matching the TS test fixture.
const pdfBase64 = "JVBERi0xLjQ="

func TestPerplexityFilePartFullImageMediaTypePassesThroughUnchanged(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(pngBase64)
	messages, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: data, MediaType: "image/png"},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v", err)
	}
	content := messages[0]["content"].([]map[string]interface{})
	url := content[0]["image_url"].(map[string]interface{})["url"].(string)
	if url != "data:image/png;base64,"+pngBase64 {
		t.Fatalf("url = %q, want data:image/png;base64,%s", url, pngBase64)
	}
}

func TestPerplexityFilePartBareImageMediaTypeSniffsSubtypeFromBytes(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(pngBase64)
	messages, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: data, MediaType: "image"},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v", err)
	}
	content := messages[0]["content"].([]map[string]interface{})
	url := content[0]["image_url"].(map[string]interface{})["url"].(string)
	if url != "data:image/png;base64,"+pngBase64 {
		t.Fatalf("url = %q, want data:image/png;base64,%s", url, pngBase64)
	}
}

func TestPerplexityFilePartBareImageMediaTypeWithURLSourcePassesThrough(t *testing.T) {
	messages, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/x.png", MediaType: "image"}},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v, want no error for URL-sourced top-level-only image", err)
	}
	content := messages[0]["content"].([]map[string]interface{})
	url := content[0]["image_url"].(map[string]interface{})["url"].(string)
	if url != "https://example.com/x.png" {
		t.Fatalf("url = %q, want raw URL passthrough (no media type resolution needed)", url)
	}
}

func TestPerplexityFilePartBareApplicationMediaTypeSniffsToPDF(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(pdfBase64)
	messages, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: data, MediaType: "application", Filename: "doc.pdf"},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v", err)
	}
	content := messages[0]["content"].([]map[string]interface{})
	part := content[0]
	if part["type"] != "file_url" {
		t.Fatalf("part type = %v, want file_url", part["type"])
	}
	if url := part["file_url"].(map[string]interface{})["url"].(string); url != pdfBase64 {
		t.Fatalf("url = %q, want %q", url, pdfBase64)
	}
	if part["file_name"] != "doc.pdf" {
		t.Fatalf("file_name = %v, want doc.pdf", part["file_name"])
	}
}

func TestPerplexityFilePartUnsupportedMediaTypeErrorsInsteadOfDropping(t *testing.T) {
	_, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: []byte("ID3\x04\x00\x00\x00\x00\x00\x0f"), MediaType: "audio/mpeg", Filename: "clip.mp3"},
		}},
	})
	if err == nil {
		t.Fatal("convertToPerplexityMessages() error = nil, want UnsupportedFunctionalityError for audio/mpeg")
	}
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %v (%T), want UnsupportedFunctionalityError", err, err)
	}
}

func TestPerplexityFilePartImageWildcardSubtypeIsDetected(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(pngBase64)
	messages, err := convertToPerplexityMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: data, MediaType: "image/*"},
		}},
	})
	if err != nil {
		t.Fatalf("convertToPerplexityMessages() error = %v", err)
	}
	content := messages[0]["content"].([]map[string]interface{})
	url := content[0]["image_url"].(map[string]interface{})["url"].(string)
	if url != "data:image/png;base64,"+pngBase64 {
		t.Fatalf("url = %q, want data:image/png;base64,%s", url, pngBase64)
	}
}

func TestPerplexityLanguageModelOptionsValidation(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	// Invalid enum value should error rather than being forwarded verbatim.
	_, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"search_mode": "not-a-real-mode",
			},
		},
	}, false)
	if err == nil {
		t.Fatal("buildRequestBody() error = nil, want validation error for invalid search_mode")
	}

	// Valid nested web_search_options enum should pass through.
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"reasoning_effort": "high",
				"web_search_options": map[string]interface{}{
					"search_context_size": "medium",
				},
				"unmodeled_future_option": "passthrough",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v, want high", body["reasoning_effort"])
	}
	wso, ok := body["web_search_options"].(map[string]interface{})
	if !ok || wso["search_context_size"] != "medium" {
		t.Fatalf("web_search_options = %+v, want search_context_size=medium", body["web_search_options"])
	}
	if body["unmodeled_future_option"] != "passthrough" {
		t.Fatalf("unmodeled_future_option = %v, want passthrough (loose-object forwarding)", body["unmodeled_future_option"])
	}

	// Invalid nested enum should also error.
	_, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"web_search_options": map[string]interface{}{
					"search_type": "bogus",
				},
			},
		},
	}, false)
	if err == nil {
		t.Fatal("buildRequestBody() error = nil, want validation error for invalid web_search_options.search_type")
	}

	// Valid doubly-nested web_search_options.user_location should pass through.
	body, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"web_search_options": map[string]interface{}{
					"user_location": map[string]interface{}{
						"latitude":  37.7749,
						"longitude": -122.4194,
						"city":      "San Francisco",
					},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	wso, ok = body["web_search_options"].(map[string]interface{})
	if !ok {
		t.Fatalf("web_search_options missing: %+v", body)
	}
	userLocation, ok := wso["user_location"].(map[string]interface{})
	if !ok || userLocation["city"] != "San Francisco" {
		t.Fatalf("user_location = %+v, want city=San Francisco", wso["user_location"])
	}

	// Invalid web_search_options.user_location.latitude type should error.
	_, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"web_search_options": map[string]interface{}{
					"user_location": map[string]interface{}{
						"latitude": "not-a-number",
					},
				},
			},
		},
	}, false)
	if err == nil {
		t.Fatal("buildRequestBody() error = nil, want validation error for invalid web_search_options.user_location.latitude")
	}

	// Valid media_response.overrides.return_videos should pass through.
	body, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"media_response": map[string]interface{}{
					"overrides": map[string]interface{}{
						"return_videos": true,
					},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	mediaResponse, ok := body["media_response"].(map[string]interface{})
	if !ok {
		t.Fatalf("media_response missing: %+v", body)
	}
	overrides, ok := mediaResponse["overrides"].(map[string]interface{})
	if !ok || overrides["return_videos"] != true {
		t.Fatalf("media_response.overrides = %+v, want return_videos=true", mediaResponse["overrides"])
	}

	// Invalid media_response.overrides.return_videos type should error.
	_, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"media_response": map[string]interface{}{
					"overrides": map[string]interface{}{
						"return_videos": "yes",
					},
				},
			},
		},
	}, false)
	if err == nil {
		t.Fatal("buildRequestBody() error = nil, want validation error for invalid media_response.overrides.return_videos")
	}
}
