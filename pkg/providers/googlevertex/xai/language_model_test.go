package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockChatCompletionsResponse() map[string]interface{} {
	return map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"finish_reason": "stop",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello!",
				},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     663,
			"completion_tokens": 50,
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": 654,
			},
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": 124,
			},
		},
	}
}

func TestLanguageModel_Metadata(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	assert.Equal(t, "v4", model.SpecificationVersion())
	assert.Equal(t, "googleVertex.xai", model.Provider())
	assert.Equal(t, ModelGrok41FastReasoning, model.ModelID())
	assert.True(t, model.SupportsTools())
	assert.True(t, model.SupportsStructuredOutput())
	assert.True(t, model.SupportsImageInput())
}

// TestLanguageModel_SupportedURLs mirrors TS "should support HTTP image URLs".
func TestLanguageModel_SupportedURLs(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	supported, ok := model.(*LanguageModel)
	require.True(t, ok)
	patterns := supported.SupportedURLs()
	require.Contains(t, patterns, "image/*")
	require.Len(t, patterns["image/*"], 1)
	assert.Equal(t, `^https?://.*$`, patterns["image/*"][0])
}

// TestLanguageModel_StripsReasoningEffort mirrors TS "should strip
// reasoning_effort from request bodies".
func TestLanguageModel_StripsReasoningEffort(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "global",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	reasoning := types.ReasoningHigh
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
	})
	require.NoError(t, err)
	_, hasReasoningEffort := capturedBody["reasoning_effort"]
	assert.False(t, hasReasoningEffort, "reasoning_effort must be stripped for the Vertex xai sub-provider")

	// Also verify explicit provider-option reasoning_effort is stripped.
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"reasoningEffort": "high"},
		},
	})
	require.NoError(t, err)
	_, hasReasoningEffort = capturedBody["reasoning_effort"]
	assert.False(t, hasReasoningEffort)
}

// TestLanguageModel_RequestShape verifies the model ID, path, and auth
// header sent to the Vertex MaaS endpoint.
func TestLanguageModel_RequestShape(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedAuth string
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "global",
		AccessToken: "vertex-oauth-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello!", result.Text)

	assert.Equal(t, "Bearer vertex-oauth-token", capturedAuth)
	assert.Equal(t, "/chat/completions", capturedPath)
	assert.Equal(t, ModelGrok41FastReasoning, capturedBody["model"])
}

// TestLanguageModel_Usage mirrors TS "should count Grok reasoning tokens
// separately from completion tokens" exactly (same fixture and expected
// output as the TS inline snapshot).
func TestLanguageModel_Usage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "global",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	require.NoError(t, err)

	require.NotNil(t, result.Usage.InputTokens)
	assert.Equal(t, int64(663), *result.Usage.InputTokens)
	require.NotNil(t, result.Usage.InputDetails)
	require.NotNil(t, result.Usage.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(654), *result.Usage.InputDetails.CacheReadTokens)
	require.NotNil(t, result.Usage.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(9), *result.Usage.InputDetails.NoCacheTokens)

	require.NotNil(t, result.Usage.OutputTokens)
	assert.Equal(t, int64(174), *result.Usage.OutputTokens)
	require.NotNil(t, result.Usage.OutputDetails)
	require.NotNil(t, result.Usage.OutputDetails.TextTokens)
	assert.Equal(t, int64(50), *result.Usage.OutputDetails.TextTokens)
	require.NotNil(t, result.Usage.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(124), *result.Usage.OutputDetails.ReasoningTokens)
}

func TestLanguageModel_HeadersForwarded_ViaProviderConfig(t *testing.T) {
	var capturedCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCustom = r.Header.Get("Custom-Provider-Header")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "global",
		AccessToken: "test-token",
		BaseURL:     server.URL,
		Headers:     map[string]string{"Custom-Provider-Header": "provider-header-value"},
	})
	require.NoError(t, err)

	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "provider-header-value", capturedCustom)
}

func TestLanguageModel_TopKWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "global",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	topK := 5
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		TopK:   &topK,
	})
	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "unsupported", result.Warnings[0].Type)
	assert.Equal(t, "topK", result.Warnings[0].Feature)
}

// TS createJsonResponseHandler(openai-compatible chat schema): a 200 body that
// does not match is an APICallError "Invalid JSON response".
func TestLanguageModel_DoGenerate_InvalidResponseSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":"nope"}`))
	}))
	defer server.Close()
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token", BaseURL: server.URL})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var perr *providererrors.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "Invalid JSON response", perr.Message)
	assert.Equal(t, 200, perr.StatusCode)
}

// TS openai-compatible getArgs: strict defaults to true, unknown
// providerOptions[googleVertex] keys are spread into the body, extra keys
// under the deprecated "xai" key are not spread, and reasoning_effort is
// stripped by transformRequestBody.
func TestLanguageModel_ProviderOptionsLikeTS(t *testing.T) {
	prov, err := New(Config{Project: "p", AccessToken: "t", BaseURL: "http://unused"})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	lm := model.(*LanguageModel)
	body, _ := lm.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"customFlag": true, "user": "u1", "reasoningEffort": "low"},
			"xai":          map[string]interface{}{"otherFlag": 1},
		},
	}, false)
	assert.Equal(t, true, body["customFlag"])
	assert.Equal(t, "u1", body["user"])
	assert.NotContains(t, body, "otherFlag")
	assert.NotContains(t, body, "reasoning_effort")
	rf := body["response_format"].(map[string]interface{})
	js := rf["json_schema"].(map[string]interface{})
	assert.Equal(t, true, js["strict"])
}

// The Go-only "xai" options key read by v0.5.2 still fills the chat option
// fields (googleVertex wins) and returns a deprecated warning.
func TestLanguageModel_DeprecatedXaiOptionsKey(t *testing.T) {
	prov, err := New(Config{Project: "p", AccessToken: "t", BaseURL: "http://unused"})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	lm := model.(*LanguageModel)

	body, warnings := lm.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"user": "legacy", "textVerbosity": "low", "otherFlag": 1},
		},
	}, false)
	assert.Equal(t, "legacy", body["user"])
	assert.Equal(t, "low", body["verbosity"])
	assert.NotContains(t, body, "otherFlag")
	found := false
	for _, w := range warnings {
		if w.Type == "deprecated" && w.Setting == "providerOptions key 'xai'" {
			found = true
		}
	}
	assert.True(t, found, "deprecated warning for the xai key, got %#v", warnings)

	body, _ = lm.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"user": "new"},
			"xai":          map[string]interface{}{"user": "legacy"},
		},
	}, false)
	assert.Equal(t, "new", body["user"])

	_, warnings = lm.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"googleVertex": map[string]interface{}{"user": "new"}},
	}, false)
	for _, w := range warnings {
		assert.NotEqual(t, "providerOptions key 'xai'", w.Setting)
	}
}

// TestLanguageModel_GoogleTokenOverridesCallerAuthorization matches the TS
// node wrapper: the generated bearer token is spread after user headers, so a
// caller Authorization header (config or per-call) never wins.
func TestLanguageModel_GoogleTokenOverridesCallerAuthorization(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockChatCompletionsResponse())
	}))
	defer server.Close()

	prov, err := New(Config{
		Project: "p", Location: "global", AccessToken: "google-token", BaseURL: server.URL,
		Headers: map[string]string{"Authorization": "Bearer config-header"},
	})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hi"},
		Headers: map[string]string{"Authorization": "Bearer call-header"},
	})
	require.NoError(t, err)
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"Bearer google-token", "Bearer google-token"}, got)
}
