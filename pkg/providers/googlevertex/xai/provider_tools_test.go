package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/require"
)

// TS openai-compatible prepareTools drops provider-defined tools with an
// `unsupported` warning `provider-defined tool <id>`.
func TestDoGenerate_DropsProviderDefinedTools(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	prov, err := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: server.URL})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)

	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Type: "function", Name: "f", Parameters: map[string]interface{}{"type": "object"}},
			{Type: "provider", ProviderID: "xai.web_search", Name: "web_search"},
		},
	})
	require.NoError(t, err)
	tools, _ := captured["tools"].([]interface{})
	require.Len(t, tools, 1)
	require.Contains(t, res.Warnings, types.Warning{Type: "unsupported", Feature: "provider-defined tool xai.web_search"})
}

func newTestModel(t *testing.T, h http.HandlerFunc) (provider.LanguageModel, func()) {
	t.Helper()
	server := httptest.NewServer(h)
	prov, err := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: server.URL})
	require.NoError(t, err)
	model, err := prov.LanguageModel(ModelGrok41FastReasoning)
	require.NoError(t, err)
	return model, server.Close
}

// TS openai-compatible doGenerate: content arrays, reasoning, generated tool
// call ids, raw arguments, providerMetadata under googleVertex, call headers.
func TestDoGenerate_CompatChatResponseShape(t *testing.T) {
	var hdr string
	model, closeFn := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Get("X-Call")
		_, _ = w.Write([]byte(`{"id":"r1","model":"grok","created":5,"choices":[{"message":{"content":[{"type":"text","text":"hi"}],"reasoning_content":"why","tool_calls":[{"function":{"name":"f","arguments":"{\"b\":1, \"a\":2}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"cost":3}}`))
	})
	defer closeFn()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{"X-Call": "1"},
	})
	require.NoError(t, err)
	require.Equal(t, "1", hdr)
	require.Equal(t, "hi", res.Text)
	require.Len(t, res.Content, 1)
	require.Len(t, res.ToolCalls, 1)
	require.NotEmpty(t, res.ToolCalls[0].ID)
	require.Equal(t, `{"b":1, "a":2}`, res.ToolCalls[0].RawArguments)
	require.Equal(t, "r1", res.ResponseMetadata.ID)
	require.Contains(t, res.ProviderMetadata, "googleVertex")
	require.EqualValues(t, 3, res.Usage.Raw["cost"])
}

func TestDoGenerate_NoChoicesIsInvalidResponse(t *testing.T) {
	model, closeFn := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})
	defer closeFn()
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	require.ErrorContains(t, err, "Response did not contain any choices.")
}

// TS applies convertUsage to streamed usage too: reasoning tokens are added to
// the output total, and the finish part carries providerMetadata.googleVertex.
func TestDoStream_UsesVertexUsageAndMetadata(t *testing.T) {
	var hdr string
	model, closeFn := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Get("X-Call")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"completion_tokens_details\":{\"reasoning_tokens\":7}}}\n\ndata: [DONE]\n\n"))
	})
	defer closeFn()
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{"X-Call": "1"}})
	require.NoError(t, err)
	defer stream.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	require.Equal(t, "1", hdr)
	require.NotNil(t, finish)
	require.EqualValues(t, 12, *finish.Usage.OutputTokens)
	require.EqualValues(t, 5, *finish.Usage.OutputDetails.TextTokens)
	require.NotEmpty(t, finish.ProviderMetadata)
}
