package bedrock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Drives the TS inline snapshots of amazon-bedrock-chat-language-model.test.ts
// for the JSON-response-format fixtures (extracted to
// testdata/ts/inline_snapshots.json): part order, tool names and finish.

type inlineSnapshot struct {
	Fixture  string `json:"fixture"`
	Line     int    `json:"line"`
	Analysis struct {
		Kind  string `json:"kind"`
		Parts []struct {
			Type     string `json:"type"`
			ToolName string `json:"toolName"`
			ID       string `json:"id"`
		} `json:"parts"`
	} `json:"analysis"`
}

func loadInline(t *testing.T) []inlineSnapshot {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ts", "inline_snapshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []inlineSnapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func fn(name string, props map[string]interface{}) types.Tool {
	return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object", "properties": props}}
}

func jsonOpts(tl ...types.Tool) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Test"},
		Tools:  tl,
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"},
		}},
	}
}

func toolsFor(fixture string) []types.Tool {
	s := map[string]interface{}{"x": map[string]interface{}{"type": "string"}}
	switch {
	case strings.Contains(fixture, "json-with-tools"):
		return []types.Tool{fn("tool-a", s), fn("tool-b", s)}
	case strings.Contains(fixture, "json-with-tool"):
		return []types.Tool{fn("get-weather", s)}
	case strings.Contains(fixture, "text-then-weather"):
		return []types.Tool{fn("weather", s)}
	}
	return nil
}

func TestTSInlineSnapshotsJSONFixtures(t *testing.T) {
	for _, snap := range loadInline(t) {
		snap := snap
		if len(snap.Analysis.Parts) == 0 {
			continue
		}
		name := snap.Fixture + "/" + snap.Analysis.Kind
		t.Run(name, func(t *testing.T) {
			srv := fixtureServer(t, snap.Fixture, nil)
			defer srv.Close()
			m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
			opts := jsonOpts(toolsFor(snap.Fixture)...)
			var want []string
			for _, p := range snap.Analysis.Parts {
				n := p.Type
				if n == "text-delta" {
					n = "text"
				}
				if strings.HasPrefix(n, "tool-") && p.ToolName != "" {
					n += ":" + p.ToolName
				}
				if snap.Analysis.Kind == "stream" && len(want) > 0 && want[len(want)-1] == n && (n == "text" || n == "tool-input-delta") {
					continue
				}
				want = append(want, n)
			}
			if snap.Analysis.Kind == "stream" {
				s, err := m.DoStream(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, c := range drainChunks(t, s) {
					n := string(c.Type)
					if c.Type == provider.ChunkTypeToolCall || c.Type == provider.ChunkTypeToolInputStart {
						n += ":" + c.ToolCall.ToolName
					}
					if len(got) > 0 && got[len(got)-1] == n && (n == "text" || n == "tool-input-delta") {
						continue
					}
					got = append(got, n)
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Fatalf("stream\n got %v\nwant %v", got, want)
				}
				return
			}
			res, err := m.DoGenerate(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			content := res.Content
			if len(content) == 0 && res.Text != "" {
				// Text-only responses carry their text in res.Text (the core
				// builds the text parts from it); compare the joined text.
				allText := true
				for _, w := range want {
					allText = allText && w == "text"
				}
				if !allText {
					t.Fatalf("content missing, want %v", want)
				}
				want = []string{"text"}
				content = []types.ContentPart{types.TextContent{Text: res.Text}}
			}
			for _, c := range content {
				switch p := c.(type) {
				case types.TextContent:
					got = append(got, "text")
				case types.ToolCallContent:
					got = append(got, "tool-call:"+p.ToolName)
				default:
					got = append(got, "?")
				}
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("generate\n got %v\nwant %v", got, want)
			}
			if res.FinishReason != types.FinishReasonStop && !strings.Contains(snap.Fixture, "tool-no-args") {
				t.Fatalf("finish = %v", res.FinishReason)
			}
		})
	}
}

// TS snapshot "doGenerate > reasoning": the signed reasoning part carries
// providerMetadata under both the amazonBedrock and bedrock keys.
func TestFixtureReasoningGenerateProviderMetadata(t *testing.T) {
	srv := fixtureServer(t, "amazon-bedrock-reasoning", nil)
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	rc, ok := res.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
	var meta map[string]map[string]string
	if err := json.Unmarshal(rc.ProviderMetadata, &meta); err != nil || meta["amazonBedrock"]["signature"] == "" || meta["bedrock"]["signature"] != meta["amazonBedrock"]["signature"] || meta["bedrock"]["signature"] != rc.Signature {
		t.Fatalf("reasoning providerMetadata = %s (%v)", rc.ProviderMetadata, err)
	}
}
