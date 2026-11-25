package anthropic

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
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// modelWithID is fixtureModel for a specific model id, capturing the request
// body when capture is non-nil.
func modelWithID(t *testing.T, id string, body func(w http.ResponseWriter, r *http.Request), capture *map[string]interface{}) (*LanguageModel, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, capture)
		}
		body(w, r)
	}))
	return NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: srv.URL}), id, nil), srv.Close
}

func advisorTool(maxTokens *int) types.Tool {
	return tools.Advisor20260301(tools.Advisor20260301Args{Model: "claude-opus-4-7", MaxTokens: maxTokens})
}

func iterationSummary(t *testing.T, meta map[string]interface{}) string {
	t.Helper()
	am, _ := meta["anthropic"].(map[string]interface{})
	b, err := json.Marshal(am["iterations"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS "advisor tool > with fixture" (generate) and the stop-reasons fixture.
func TestFixtureAdvisorGenerate(t *testing.T) {
	maxTokens := 2048
	var body map[string]interface{}
	m, closeFn := modelWithID(t, "claude-sonnet-4-6", serveJSONFixture("anthropic-advisor-20260301.1", t), &body)
	defer closeFn()
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: []types.Tool{advisorTool(&maxTokens)}})
	if err != nil {
		t.Fatal(err)
	}
	if body["max_tokens"] != float64(128000) {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
	tl := body["tools"].([]interface{})[0].(map[string]interface{})
	if tl["type"] != "advisor_20260301" || tl["name"] != "advisor" || tl["model"] != "claude-opus-4-7" || tl["max_tokens"] != float64(2048) {
		t.Fatalf("tool = %v", tl)
	}
	var call, result bool
	for _, c := range res.Content {
		switch p := c.(type) {
		case types.ToolCallContent:
			if p.ToolName == "anthropic.advisor_20260301" && p.ToolCallID == "srvtoolu_01H51YYF9DTtdAHfcxxTs1NV" && p.ProviderExecuted && len(p.Arguments) == 0 {
				call = true
			}
		case types.ToolResultContent:
			r, _ := p.Result.(map[string]interface{})
			if p.ToolCallID == "srvtoolu_01H51YYF9DTtdAHfcxxTs1NV" && r["type"] == "advisor_result" && strings.Contains(r["text"].(string), "Deliver design outline first") {
				result = true
			}
		}
	}
	if !call || !result {
		t.Fatalf("call=%v result=%v content=%+v", call, result, res.Content)
	}
	want := `[{"inputTokens":1051,"outputTokens":35,"type":"message"},{"inputTokens":2728,"model":"claude-opus-4-7","outputTokens":874,"type":"advisor_message"},{"inputTokens":1363,"outputTokens":3165,"type":"message"}]`
	if got := iterationSummary(t, res.ProviderMetadata); got != want {
		t.Fatalf("iterations\n got %s\nwant %s", got, want)
	}
}

func advisorStopReasonResults(t *testing.T, results []*types.ToolResult) {
	t.Helper()
	if len(results) != 2 {
		t.Fatalf("results = %d", len(results))
	}
	a, _ := results[0].Result.(map[string]interface{})
	b, _ := results[1].Result.(map[string]interface{})
	if results[0].ToolCallID != "srvtoolu_advisor_plaintext" || a["type"] != "advisor_result" || a["stopReason"] != "max_tokens" || a["text"] != "Partial plaintext advice." {
		t.Fatalf("plaintext = %+v", results[0])
	}
	if results[1].ToolCallID != "srvtoolu_advisor_redacted" || b["type"] != "advisor_redacted_result" || b["stopReason"] != "end_turn" || b["encryptedContent"] != "opaque-encrypted-advice" {
		t.Fatalf("redacted = %+v", results[1])
	}
}

func TestFixtureAdvisorStopReasonsGenerateAndStream(t *testing.T) {
	maxTokens := 2048
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: []types.Tool{advisorTool(&maxTokens)}}
	m, closeFn := modelWithID(t, "claude-sonnet-4-6", serveJSONFixture("anthropic-advisor-stop-reasons", t), nil)
	res, err := m.DoGenerate(context.Background(), opts)
	closeFn()
	if err != nil {
		t.Fatal(err)
	}
	var results []*types.ToolResult
	for _, c := range res.Content {
		if p, ok := c.(types.ToolResultContent); ok {
			results = append(results, &types.ToolResult{ToolCallID: p.ToolCallID, Result: p.Result})
		}
	}
	advisorStopReasonResults(t, results)

	m, closeFn = modelWithID(t, "claude-sonnet-4-6", serveChunksFixture("anthropic-advisor-stop-reasons", t), nil)
	defer closeFn()
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	results = nil
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeToolResult {
			results = append(results, c.ToolResult)
		}
	}
	advisorStopReasonResults(t, results)
}

// TS "advisor tool" (stream): provider-executed call, the fully formed result
// before executor text resumes, and the iterations on the finish part.
func TestFixtureAdvisorStream(t *testing.T) {
	m, closeFn := modelWithID(t, "claude-sonnet-4-6", serveChunksFixture("anthropic-advisor-20250301.1", t), nil)
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: []types.Tool{advisorTool(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drain(t, s)
	resIdx, textAfter := -1, -1
	var fin *provider.StreamChunk
	for i, c := range chunks {
		switch {
		case c.Type == provider.ChunkTypeToolCall && strings.Contains(c.ToolCall.ToolName, "advisor"):
			if c.ToolCall.ID != "srvtoolu_01R6zRtm9VnRaSJUVkGK9zvM" || !c.ToolCall.ProviderExecuted {
				t.Fatalf("call = %+v", c.ToolCall)
			}
		case c.Type == provider.ChunkTypeToolResult && strings.Contains(c.ToolResult.ToolName, "advisor"):
			resIdx = i
			r, _ := c.ToolResult.Result.(map[string]interface{})
			if r["type"] != "advisor_result" || !strings.Contains(r["text"].(string), "Concurrent Worker Pool in Go") {
				t.Fatalf("result = %+v", c.ToolResult)
			}
		case c.Type == provider.ChunkTypeTextStart && resIdx >= 0 && textAfter < 0:
			textAfter = i
		case c.Type == provider.ChunkTypeFinish:
			fin = c
		}
	}
	if resIdx < 0 || textAfter <= resIdx || fin == nil {
		t.Fatalf("result at %d, next text-start at %d, finish %v", resIdx, textAfter, fin)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(fin.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	want := `[{"inputTokens":1051,"outputTokens":35,"type":"message"},{"inputTokens":2728,"model":"claude-opus-4-7","outputTokens":3880,"type":"advisor_message"},{"inputTokens":3676,"outputTokens":3356,"type":"message"}]`
	if got := iterationSummary(t, meta); got != want {
		t.Fatalf("iterations\n got %s\nwant %s", got, want)
	}
}
