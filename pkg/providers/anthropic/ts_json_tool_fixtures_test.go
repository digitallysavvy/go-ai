package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// haikuModel is the TS test default (claude-3-haiku-20240307): no native
// structured output, so response formats go through the json tool.
func haikuModel(t *testing.T, body func(w http.ResponseWriter, r *http.Request)) (*LanguageModel, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(body))
	return NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: srv.URL}), "claude-3-haiku-20240307", nil), srv.Close
}

func jsonFormat(props map[string]interface{}, required ...string) *provider.ResponseFormat {
	req := make([]interface{}, len(required))
	for i, r := range required {
		req[i] = r
	}
	return &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{
		"type": "object", "properties": props, "required": req, "additionalProperties": false,
	}}
}

func streamText(chunks []*provider.StreamChunk) (string, *provider.StreamChunk) {
	var b strings.Builder
	var fin *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			b.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			fin = c
		}
	}
	return b.String(), fin
}

// TS: "json schema response format" fixtures. The model answers through the
// synthetic json tool; the call surfaces as text and the finish reason is stop.
func TestFixtureJSONToolGenerateAndStream(t *testing.T) {
	format := jsonFormat(map[string]interface{}{"elements": map[string]interface{}{"type": "array"}}, "elements")
	model, closeFn := haikuModel(t, serveJSONFixture("anthropic-json-tool.1", t))
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ResponseFormat: format})
	closeFn()
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "tool_use" || !strings.Contains(res.Text, `"elements"`) || len(res.ToolCalls) != 0 {
		t.Fatalf("generate: finish=%v/%q text=%q calls=%d", res.FinishReason, res.RawFinishReason, res.Text, len(res.ToolCalls))
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res.Text), &parsed); err != nil {
		t.Fatalf("text is not JSON: %v", err)
	}

	for _, fixture := range []string{"anthropic-json-tool.1", "anthropic-json-tool.2"} {
		m, closeFn := haikuModel(t, serveChunksFixture(fixture, t))
		s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ResponseFormat: format})
		if err != nil {
			t.Fatal(err)
		}
		chunks := drain(t, s)
		closeFn()
		text, fin := streamText(chunks)
		if fin == nil || fin.FinishReason != types.FinishReasonStop || fin.RawFinishReason != "tool_use" {
			t.Fatalf("%s: finish = %+v", fixture, fin)
		}
		if !json.Valid([]byte(text)) {
			t.Fatalf("%s: streamed text is not JSON: %q", fixture, text)
		}
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall {
				t.Fatalf("%s: json tool leaked as a tool call", fixture)
			}
		}
	}
}

// TS "json schema response format with other tool response (unsupported
// model)": a real tool call alongside the json tool stays a tool call.
func TestFixtureJSONOtherToolGenerateAndStream(t *testing.T) {
	format := jsonFormat(map[string]interface{}{"weather": map[string]interface{}{"type": "string"}, "temperature": map[string]interface{}{"type": "number"}}, "weather", "temperature")
	tool := types.Tool{Name: "get-weather", Description: "Get the weather in a location", Parameters: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}, "required": []interface{}{"location"},
	}}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ResponseFormat: format, Tools: []types.Tool{tool}}
	model, closeFn := haikuModel(t, serveJSONFixture("anthropic-json-other-tool.1", t))
	res, err := model.DoGenerate(context.Background(), opts)
	closeFn()
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "weather" {
		t.Fatalf("generate: finish=%v calls=%+v", res.FinishReason, res.ToolCalls)
	}

	m, closeFn := haikuModel(t, serveChunksFixture("anthropic-json-other-tool.1", t))
	defer closeFn()
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	chunks := drain(t, s)
	calls := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			calls++
			if c.ToolCall.ToolName != "weather" {
				t.Fatalf("tool call = %+v", c.ToolCall)
			}
		}
	}
	if _, fin := streamText(chunks); calls != 1 || fin == nil || fin.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("stream: calls=%d finish=%+v", calls, fin)
	}
}
