package cerebras

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of packages/cerebras/src/cerebras-chat-language-model.test.ts
// ("finish reason normalization") over the TS fixtures (gzipped).

func readCerebrasFixture(t *testing.T, name string) string {
	t.Helper()
	f, err := os.Open("testdata/fixtures/" + name + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func cerebrasFixtureModel(t *testing.T, contentType, body string) provider.LanguageModel {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	m, err := New(Config{APIKey: "test-api-key", BaseURL: srv.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func cerebrasContentKinds(content []types.ContentPart) []string {
	var kinds []string
	for _, p := range content {
		kinds = append(kinds, p.ContentType())
	}
	return kinds
}

func TestCerebrasFixtureFirstToolCallStepIsPreserved(t *testing.T) {
	m := cerebrasFixtureModel(t, "application/json", readCerebrasFixture(t, "cerebras-structured-output-tools.1.json"))
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cerebrasContentKinds(res.Content), ","); got != "reasoning" {
		t.Errorf("content = %s, want reasoning", got)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "nonUsefulTool" || res.ToolCalls[0].RawArguments != "{}" {
		t.Errorf("tool calls = %+v", res.ToolCalls)
	}
	if res.FinishReason != types.FinishReasonToolCalls || res.RawFinishReason != "tool_calls" {
		t.Errorf("finish = %s/%s", res.FinishReason, res.RawFinishReason)
	}
}

func TestCerebrasFixtureRepeatedToolCallDroppedWithStructuredOutputText(t *testing.T) {
	body := readCerebrasFixture(t, "cerebras-structured-output-tools.2.json")
	m := cerebrasFixtureModel(t, "application/json", body)
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"result":"2026"}` {
		t.Errorf("text = %q", res.Text)
	}
	if got := strings.Join(cerebrasContentKinds(res.Content), ","); got != "reasoning" {
		t.Errorf("content = %s, want reasoning (no tool-call part)", got)
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("tool calls = %+v, want none", res.ToolCalls)
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "tool_calls" {
		t.Errorf("finish = %s/%s, want stop/tool_calls", res.FinishReason, res.RawFinishReason)
	}

	// Without a JSON response format the mixed response is kept as is.
	res, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"result":"2026"}` || len(res.ToolCalls) != 1 {
		t.Errorf("text = %q, calls = %d, want the mixed response kept", res.Text, len(res.ToolCalls))
	}
	if res.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish = %s, want tool-calls", res.FinishReason)
	}

	// Only `json` triggers the normalization; a text response format does not.
	res, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 {
		t.Errorf("text format: finish = %s, calls = %d, want tool-calls/1", res.FinishReason, len(res.ToolCalls))
	}
}

func TestCerebrasFixtureStreamedStructuredOutputNormalizesFinishAndDropsToolCalls(t *testing.T) {
	var sse strings.Builder
	// The TS fixture concatenates two responses; the second one (after the
	// first finish_reason chunk) is the mixed structured-output stream.
	lines := strings.Split(readCerebrasFixture(t, "cererebras-structured-output-tools.1.chunks.txt"), "\n")
	for i, line := range lines {
		if i < 35 {
			continue
		}
		if strings.TrimSpace(line) != "" {
			sse.WriteString("data: " + line + "\n\n")
		}
	}
	sse.WriteString("data: [DONE]\n\n")
	m := cerebrasFixtureModel(t, "text/event-stream", sse.String())
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	toolCalls := 0
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		switch chunk.Type {
		case provider.ChunkTypeToolCall:
			toolCalls++
		case provider.ChunkTypeFinish:
			finish = chunk
		}
	}
	if toolCalls != 0 {
		t.Errorf("tool-call chunks = %d, want 0", toolCalls)
	}
	if finish == nil {
		t.Fatal("no finish chunk")
	}
	if finish.FinishReason != types.FinishReasonStop {
		t.Errorf("finish reason = %s, want stop (raw tool_calls)", finish.FinishReason)
	}
	if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 433 {
		t.Errorf("usage = %+v", finish.Usage)
	}
}
