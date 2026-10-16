package openai

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

func chatTestPrompt() types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
}

func collectChat(t *testing.T, sse string, cfg Config) ([]*provider.StreamChunk, map[string]interface{}) {
	t.Helper()
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	cfg.APIKey, cfg.BaseURL = "k", srv.URL
	m, err := New(cfg).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}
	return chunks, body
}

func TestChatStreamMergesTrailingUsageAndMetadata(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{"content":"Hi","annotations":[{"type":"url_citation","url_citation":{"start_index":0,"end_index":2,"url":"https://a.test","title":"A"}}]},"finish_reason":null,"logprobs":{"content":[{"token":"Hi","logprob":0,"top_logprobs":[]}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":1,"accepted_prediction_tokens":2,"rejected_prediction_tokens":0}}}`,
		`data: [DONE]`, ``,
	}, "\n\n")
	chunks, body := collectChat(t, sse, Config{})
	if so, _ := body["stream_options"].(map[string]interface{}); so["include_usage"] != true {
		t.Fatalf("stream_options = %v", body["stream_options"])
	}
	var text string
	var sources int
	var finish *provider.StreamChunk
	for i, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeSource:
			sources++
		case provider.ChunkTypeFinish:
			if finish != nil || i != len(chunks)-1 {
				t.Fatalf("finish must be single and last: %d/%d", i, len(chunks))
			}
			finish = c
		}
	}
	if text != "Hi!" || sources != 1 || finish == nil {
		t.Fatalf("text=%q sources=%d finish=%v", text, sources, finish)
	}
	if finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || *finish.Usage.InputTokens != 5 || *finish.Usage.OutputDetails.ReasoningTokens != 1 {
		t.Fatalf("finish = %+v usage=%+v", finish, finish.Usage)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["openai"]["acceptedPredictionTokens"] != float64(2) || meta["openai"]["rejectedPredictionTokens"] != float64(0) || meta["openai"]["logprobs"] == nil {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestChatStreamCompatReasoningAndUsageOptIn(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"ans"},"finish_reason":"stop"}]}`,
		`data: [DONE]`, ``,
	}, "\n\n")
	chunks, body := collectChat(t, sse, Config{OpenAICompatible: true, Name: "My-Host"})
	if _, ok := body["stream_options"]; ok {
		t.Fatalf("compat without IncludeUsage must not send stream_options: %v", body["stream_options"])
	}
	var kinds []provider.ChunkType
	for _, c := range chunks {
		if c.Type != provider.ChunkTypeStreamStart {
			kinds = append(kinds, c.Type)
		}
	}
	want := []provider.ChunkType{provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoning, provider.ChunkTypeReasoningEnd, provider.ChunkTypeText, provider.ChunkTypeFinish}
	if len(kinds) != len(want) {
		t.Fatalf("chunks = %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("chunks = %v want %v", kinds, want)
		}
	}
	_, body = collectChat(t, `data: [DONE]`+"\n\n", Config{OpenAICompatible: true, IncludeUsage: true})
	if so, _ := body["stream_options"].(map[string]interface{}); so["include_usage"] != true {
		t.Fatalf("IncludeUsage should send stream_options: %v", body["stream_options"])
	}
}

func TestChatGenerateMetadataReasoningSourcesAndToolID(t *testing.T) {
	respJSON := `{"id":"c1","model":"m","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"hi","reasoning_content":"why","annotations":[{"type":"url_citation","url_citation":{"start_index":0,"end_index":2,"url":"https://a.test","title":"A"}}],"tool_calls":[{"type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls","logprobs":{"content":[{"token":"hi","logprob":0,"top_logprobs":[]}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"completion_tokens_details":{"accepted_prediction_tokens":4}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respJSON))
	}))
	defer srv.Close()
	run := func(cfg Config) *types.GenerateResult {
		cfg.APIKey, cfg.BaseURL = "k", srv.URL
		m, _ := New(cfg).ChatModel("gpt-4o")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	native := run(Config{})
	if native.ToolCalls[0].ID == "" {
		t.Error("missing tool call id must be generated")
	}
	meta := native.ProviderMetadata["openai"].(map[string]interface{})
	if meta["acceptedPredictionTokens"] != 4 || meta["logprobs"] == nil {
		t.Errorf("native metadata = %v", meta)
	}
	var sources, reasoning int
	for _, p := range native.Content {
		switch p.(type) {
		case types.SourceContent:
			sources++
		case types.ReasoningContent:
			reasoning++
		}
	}
	if sources != 1 || reasoning != 0 {
		t.Errorf("native sources=%d reasoning=%d", sources, reasoning)
	}
	compat := run(Config{OpenAICompatible: true, Name: "my-host"})
	// TS resolveProviderOptionsKey: the raw provider name unless the caller
	// used the camelCase key in providerOptions.
	if _, ok := compat.ProviderMetadata["my-host"]; !ok {
		t.Errorf("compat metadata keys = %v", compat.ProviderMetadata)
	}
	sources, reasoning = 0, 0
	for _, p := range compat.Content {
		switch p.(type) {
		case types.SourceContent:
			sources++
		case types.ReasoningContent:
			reasoning++
		}
	}
	if sources != 0 || reasoning != 1 {
		t.Errorf("compat sources=%d reasoning=%d", sources, reasoning)
	}
}
