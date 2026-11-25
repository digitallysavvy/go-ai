package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func anthropicMeta(t *testing.T, raw interface{}) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("metadata %s: %v", b, err)
	}
	return m["anthropic"]
}

func jsonString(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func generateFixture(t *testing.T, fixture string, opts *provider.GenerateOptions) *types.GenerateResult {
	t.Helper()
	m, closeFn := fixtureModel(t, serveJSONFixture(fixture, t))
	defer closeFn()
	if opts == nil {
		opts = &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	}
	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func streamFixture(t *testing.T, fixture string) []*provider.StreamChunk {
	t.Helper()
	m, closeFn := fixtureModel(t, serveChunksFixture(fixture, t))
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	return drain(t, s)
}

func finishOf(chunks []*provider.StreamChunk) *provider.StreamChunk {
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			return c
		}
	}
	return nil
}

func textById(chunks []*provider.StreamChunk, typ provider.ChunkType, id string) string {
	var b strings.Builder
	for _, c := range chunks {
		if c.Type == typ && c.ID == id {
			b.WriteString(c.Text)
			b.WriteString(c.Reasoning)
		}
	}
	return b.String()
}

// TS "should parse compaction response with iterations and compaction content".
func TestFixtureCompactionGenerate(t *testing.T) {
	res := generateFixture(t, "anthropic-compaction.1", nil)
	if len(res.Content) != 2 {
		t.Fatalf("content = %+v", res.Content)
	}
	first, ok1 := res.Content[0].(types.TextContent)
	second, ok2 := res.Content[1].(types.TextContent)
	if !ok1 || !ok2 || !strings.Contains(first.Text, "## Summary of Conversation") || !strings.Contains(second.Text, "Based on our conversation history, you had asked me") {
		t.Fatalf("content = %+v", res.Content)
	}
	if string(first.ProviderMetadata) != `{"anthropic":{"type":"compaction"}}` || len(second.ProviderMetadata) != 0 {
		t.Fatalf("metadata = %s / %s", first.ProviderMetadata, second.ProviderMetadata)
	}
	want := `[{"inputTokens":60385,"outputTokens":592,"type":"compaction"},{"inputTokens":682,"outputTokens":1320,"type":"message"}]`
	if got := jsonString(t, anthropicMeta(t, res.ProviderMetadata)["iterations"]); got != want {
		t.Fatalf("iterations\n got %s\nwant %s", got, want)
	}
	if *res.Usage.InputTokens != 60385+682 || *res.Usage.OutputTokens != 592+1320 {
		t.Fatalf("usage = %d/%d", *res.Usage.InputTokens, *res.Usage.OutputTokens)
	}
}

// TS "should stream compaction content blocks with provider metadata" and
// "should parse iterations from streaming compaction response".
func TestFixtureCompactionStream(t *testing.T) {
	chunks := streamFixture(t, "anthropic-compaction.1")
	var start *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeTextStart && c.ID == "0" {
			start = c
		}
	}
	if start == nil || string(start.ProviderMetadata) != `{"anthropic":{"type":"compaction"}}` {
		t.Fatalf("compaction text-start = %+v", start)
	}
	text := textById(chunks, provider.ChunkTypeText, "0")
	if !strings.Contains(text, "## Summary of Conversation") || !strings.Contains(text, "React vs Vue.js") {
		t.Fatalf("compaction text = %q", text)
	}
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeTextStart && c.ID == "1" && len(c.ProviderMetadata) != 0 {
			t.Fatalf("regular text-start carries metadata: %s", c.ProviderMetadata)
		}
	}
	fin := finishOf(chunks)
	want := `[{"inputTokens":60385,"outputTokens":522,"type":"compaction"},{"inputTokens":612,"outputTokens":2819,"type":"message"}]`
	if got := jsonString(t, anthropicMeta(t, fin.ProviderMetadata)["iterations"]); got != want {
		t.Fatalf("iterations\n got %s\nwant %s", got, want)
	}
	if *fin.Usage.InputTokens != 60385+612 || *fin.Usage.OutputTokens != 522+2819 {
		t.Fatalf("usage = %d/%d", *fin.Usage.InputTokens, *fin.Usage.OutputTokens)
	}
}

// TS "clear_tool_uses / clear_thinking / combined" generate and stream.
func TestFixtureContextEditingGenerateAndStream(t *testing.T) {
	cm := func(meta interface{}) string { return jsonString(t, anthropicMeta(t, meta)["contextManagement"]) }
	const empty = `{"appliedEdits":[]}`

	res := generateFixture(t, "anthropic-clear-tool-uses.1", nil)
	if !strings.Contains(res.Text, "Here's a comparison of the weather in both cities") || cm(res.ProviderMetadata) != empty || *res.Usage.InputTokens != 859 || *res.Usage.OutputTokens != 132 {
		t.Fatalf("clear-tool-uses generate: %q %s %+v", res.Text, cm(res.ProviderMetadata), res.Usage)
	}
	chunks := streamFixture(t, "anthropic-clear-tool-uses.1")
	fin := finishOf(chunks)
	if !strings.Contains(textById(chunks, provider.ChunkTypeText, "0"), "Here's a comparison of the weather in both cities") || cm(fin.ProviderMetadata) != empty || *fin.Usage.InputTokens != 859 || *fin.Usage.OutputTokens != 122 {
		t.Fatalf("clear-tool-uses stream: %s %+v", cm(fin.ProviderMetadata), fin.Usage)
	}

	res = generateFixture(t, "anthropic-clear-thinking.1", nil)
	reasoning, _ := res.Content[0].(types.ReasoningContent)
	if reasoning.Text != "925 divided by 5 = 185" || !strings.Contains(string(reasoning.ProviderMetadata), `"signature"`) || res.Text != "925 ÷ 5 = 185" || cm(res.ProviderMetadata) != empty || *res.Usage.InputTokens != 69 || *res.Usage.OutputTokens != 33 {
		t.Fatalf("clear-thinking generate: %+v / %q / %s", reasoning, res.Text, cm(res.ProviderMetadata))
	}
	chunks = streamFixture(t, "anthropic-clear-thinking.1")
	fin = finishOf(chunks)
	reasoningText := textById(chunks, provider.ChunkTypeReasoning, "0")
	sigParts := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning && len(c.ProviderMetadata) != 0 {
			sigParts++
		}
	}
	if !strings.Contains(reasoningText, "925") || !strings.Contains(reasoningText, "185") || sigParts != 1 ||
		textById(chunks, provider.ChunkTypeText, "1") != "925 ÷ 5 = 185" || cm(fin.ProviderMetadata) != empty || *fin.Usage.InputTokens != 69 || *fin.Usage.OutputTokens != 53 {
		t.Fatalf("clear-thinking stream: sig parts %d, reasoning %q, fin %+v", sigParts, reasoningText, fin.Usage)
	}

	res = generateFixture(t, "anthropic-combined-context-editing.1", nil)
	reasoning, _ = res.Content[0].(types.ReasoningContent)
	if !strings.Contains(reasoning.Text, "25 * 37") || !strings.Contains(res.Text, "925") || cm(res.ProviderMetadata) != empty {
		t.Fatalf("combined generate: %+v / %q / %s", reasoning, res.Text, cm(res.ProviderMetadata))
	}
	chunks = streamFixture(t, "anthropic-combined-context-editing.1")
	fin = finishOf(chunks)
	sigParts = 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning && len(c.ProviderMetadata) != 0 {
			sigParts++
		}
	}
	rt := textById(chunks, provider.ChunkTypeReasoning, "0")
	if !strings.Contains(rt, "25 * 37") || !strings.Contains(rt, "925") || sigParts != 1 || !strings.Contains(textById(chunks, provider.ChunkTypeText, "1"), "925") || cm(fin.ProviderMetadata) != empty {
		t.Fatalf("combined stream: %d %q %s", sigParts, rt, cm(fin.ProviderMetadata))
	}
}
