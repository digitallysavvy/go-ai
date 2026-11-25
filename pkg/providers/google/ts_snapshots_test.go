package google

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

// Drives the vitest snapshots of google-language-model.test.ts (extracted to
// ../gemini/testdata/ts/google_snapshots.json) through the Go model: part
// order, tool names, joined tool input, finish reasons and token totals.

type googSnapPart struct {
	Type         string `json:"type"`
	ToolName     string `json:"toolName"`
	Delta        string `json:"delta"`
	FinishReason *struct {
		Raw     string `json:"raw"`
		Unified string `json:"unified"`
	} `json:"finishReason"`
	Usage *struct {
		InputTokens  *int `json:"inputTokens"`
		OutputTokens *int `json:"outputTokens"`
	} `json:"usage"`
}

type googSnap struct {
	Kind         string         `json:"kind"`
	Parts        []googSnapPart `json:"parts"`
	FinishReason *struct {
		Raw     string `json:"raw"`
		Unified string `json:"unified"`
	} `json:"finishReason"`
}

func loadGoogSnaps(t *testing.T) map[string]googSnap {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", "google_snapshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]googSnap
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func weatherTool() types.Tool {
	return types.Tool{Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}}}
}

func collapse(seq []string) []string {
	var out []string
	for _, n := range seq {
		if k := len(out); k > 0 && out[k-1] == n && (n == "text-delta" || n == "reasoning-delta" || n == "tool-input-delta") {
			continue
		}
		out = append(out, n)
	}
	return out
}

func TestTSSnapshotsDrivenThroughGo(t *testing.T) {
	snaps := loadGoogSnaps(t)
	cases := []struct {
		key, fixture string
		tools        []types.Tool
	}{
		{"doGenerate > reasoning-gemini3 > should extract reasoning with thoughtSignature", "google-reasoning-gemini3", nil},
		{"doGenerate > text > should extract text response", "google-text", nil},
		{"doGenerate > tool-call > should extract tool calls", "google-tool-call", []types.Tool{weatherTool()}},
		{"doGenerate > tool-call-gemini3 > should extract tool call with thoughtSignature", "google-tool-call-gemini3", nil},
		{"doStream > reasoning > should stream reasoning and text parts", "google-reasoning", nil},
		{"doStream > reasoning-gemini3 > should stream reasoning with thoughtSignature", "google-reasoning-gemini3", nil},
		{"doStream > text > should stream text deltas", "google-text", nil},
		{"doStream > tool-call > should stream tool call", "google-tool-call", []types.Tool{weatherTool()}},
		{"doStream > tool-call-gemini3 > should stream tool call with thoughtSignature", "google-tool-call-gemini3", nil},
		{"doStream > text > should include raw chunks when includeRawChunks is enabled", "google-text", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			snap, ok := snaps[tc.key]
			if !ok {
				t.Fatalf("no snapshot %q", tc.key)
			}
			m, closeFn := e2eModel(t, fixtureHandler(t, tc.fixture, nil), "gemini-pro")
			defer closeFn()
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: tc.tools, IncludeRawChunks: strings.Contains(tc.key, "raw chunks")}
			var want []string
			for _, p := range snap.Parts {
				n := p.Type
				if strings.HasPrefix(n, "tool-") && p.ToolName != "" {
					n += ":" + p.ToolName
				}
				want = append(want, n)
			}
			if snap.Kind == "generate" {
				res, err := m.DoGenerate(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, c := range res.Content {
					switch p := c.(type) {
					case types.TextContent:
						got = append(got, "text")
					case types.ReasoningContent:
						got = append(got, "reasoning")
					case types.ToolCallContent:
						got = append(got, "tool-call:"+p.ToolName)
					default:
						got = append(got, "?")
					}
				}
				if len(got) == 0 && res.Text != "" {
					got = []string{"text"}
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Fatalf("content\n got %v\nwant %v", got, want)
				}
				if snap.FinishReason != nil && (string(res.FinishReason) != snap.FinishReason.Unified || res.RawFinishReason != snap.FinishReason.Raw) {
					t.Fatalf("finish = %v/%q", res.FinishReason, res.RawFinishReason)
				}
				return
			}
			s, err := m.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			chunks := drainAll(t, s)
			var got []string
			for i, c := range chunks {
				n := string(c.Type)
				switch c.Type {
				case provider.ChunkTypeText:
					n = "text-delta"
				case provider.ChunkTypeReasoning:
					n = "reasoning-delta"
				case provider.ChunkTypeToolCall, provider.ChunkTypeToolInputStart:
					n += ":" + c.ToolCall.ToolName
				case provider.ChunkTypeResponseMetadata:
					if i > 0 && chunks[i-1].Type == provider.ChunkTypeResponseMetadata {
						continue
					}
				}
				if opts.IncludeRawChunks && c.Type != provider.ChunkTypeRaw {
					continue // the TS test snapshots only the raw parts
				}
				got = append(got, n)
			}
			if !opts.IncludeRawChunks { // raw chunks are not collapsed
				got, want = collapse(got), collapse(want)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("stream\n got %v\nwant %v", got, want)
			}
			var fin *provider.StreamChunk
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeFinish {
					fin = c
				}
			}
			for _, p := range snap.Parts {
				if p.Type != "finish" {
					continue
				}
				if fin == nil || string(fin.FinishReason) != p.FinishReason.Unified || fin.RawFinishReason != p.FinishReason.Raw {
					t.Fatalf("finish = %+v", fin)
				}
				if p.Usage != nil && p.Usage.InputTokens != nil && int(*fin.Usage.InputTokens) != *p.Usage.InputTokens {
					t.Errorf("input tokens = %d want %d", *fin.Usage.InputTokens, *p.Usage.InputTokens)
				}
				if p.Usage != nil && p.Usage.OutputTokens != nil && int(*fin.Usage.OutputTokens) != *p.Usage.OutputTokens {
					t.Errorf("output tokens = %d want %d", *fin.Usage.OutputTokens, *p.Usage.OutputTokens)
				}
			}
		})
	}
}
