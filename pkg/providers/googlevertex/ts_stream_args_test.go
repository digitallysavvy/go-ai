package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// google-language-model.test.ts: the streamed function-call-argument fixtures,
// run through the Vertex model.

func vertexFixtureModel(t *testing.T, fixture string) provider.LanguageModel {
	t.Helper()
	rc := &recorder{fixture: geminiFixtureFor(t, fixture)}
	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "tok", HTTPClient: &http.Client{Transport: rc}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("gemini-pro")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func objTool(name string, props map[string]interface{}) types.Tool {
	return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object", "properties": props}}
}

func streamAll(t *testing.T, m provider.LanguageModel, opts *provider.GenerateOptions) []*provider.StreamChunk {
	t.Helper()
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

type tsGoogleSnap struct {
	Parts []struct {
		Type     string `json:"type"`
		ToolName string `json:"toolName"`
		Delta    string `json:"delta"`
	} `json:"parts"`
}

func googleSnap(t *testing.T, key string) tsGoogleSnap {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", "google_snapshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]tsGoogleSnap
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}

func toolSeq(chunks []*provider.StreamChunk) []string {
	var out []string
	for _, c := range chunks {
		n := string(c.Type)
		switch c.Type {
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolCall:
			n += ":" + c.ToolCall.ToolName
		case provider.ChunkTypeToolInputDelta:
			if k := len(out); k > 0 && out[k-1] == n {
				continue
			}
		case provider.ChunkTypeStreamStart, provider.ChunkTypeFinish, provider.ChunkTypeToolInputEnd:
		default:
			continue
		}
		out = append(out, n)
	}
	return out
}

func snapSeq(s tsGoogleSnap) []string {
	var out []string
	for _, p := range s.Parts {
		n := p.Type
		switch p.Type {
		case "tool-input-start", "tool-call":
			n += ":" + p.ToolName
		case "tool-input-delta":
			if k := len(out); k > 0 && out[k-1] == n {
				continue
			}
		case "stream-start", "finish", "tool-input-end":
		default:
			continue
		}
		out = append(out, n)
	}
	return out
}

func TestTSStreamingToolCallArgumentsSnapshots(t *testing.T) {
	str := map[string]interface{}{"type": "string"}
	cases := []struct {
		key, fixture string
		tool         types.Tool
	}{
		{"doStream > streaming-tool-call-arguments > should stream partial function call arguments with parallel tool calls", "google-stream-tool-call-arguments", objTool("getWeather", map[string]interface{}{"location": str})},
		{"doStream > streaming-tool-call-arguments-nested > should stream nested partial function call arguments into proper nested JSON", "google-vertex-stream-tool-call-arguments-nested.1", objTool("cookRecipe", map[string]interface{}{"recipe": map[string]interface{}{"type": "object"}})},
	}
	for _, tc := range cases {
		m := vertexFixtureModel(t, tc.fixture)
		chunks := streamAll(t, m, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{tc.tool}})
		snap := googleSnap(t, tc.key)
		if got, want := strings.Join(toolSeq(chunks), ","), strings.Join(snapSeq(snap), ","); got != want {
			t.Fatalf("%s\n got %s\nwant %s", tc.fixture, got, want)
		}
		// Streamed deltas join into the same JSON TS produced, per call.
		var gotDeltas, wantDeltas []string
		var cur strings.Builder
		for _, c := range chunks {
			switch c.Type {
			case provider.ChunkTypeToolInputDelta:
				cur.WriteString(c.Text)
			case provider.ChunkTypeToolInputEnd:
				gotDeltas = append(gotDeltas, cur.String())
				cur.Reset()
			}
		}
		cur.Reset()
		for _, p := range snap.Parts {
			switch p.Type {
			case "tool-input-delta":
				cur.WriteString(p.Delta)
			case "tool-input-end":
				wantDeltas = append(wantDeltas, cur.String())
				cur.Reset()
			}
		}
		if strings.Join(gotDeltas, "|") != strings.Join(wantDeltas, "|") {
			t.Fatalf("%s: joined deltas\n got %q\nwant %q", tc.fixture, gotDeltas, wantDeltas)
		}
	}
}

func TestTSStreamArrayArgumentsMissingTerminal(t *testing.T) {
	m := vertexFixtureModel(t, "google-stream-tool-call-array-arguments-missing-terminal-function-call")
	chunks := streamAll(t, m, &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Tools:           []types.Tool{objTool("writeItems", map[string]interface{}{"operations": map[string]interface{}{"type": "array"}})},
		ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"streamFunctionCallArguments": true}},
	})
	const want = `{"operations":[{"action":"add","description":"Fresh red apple","itemid":"apple_001","price":0.5},{"action":"add","description":"Ripe yellow banana","itemid":"banana_001","price":0.3}]}`
	var deltas strings.Builder
	var calls []*types.ToolCall
	var fin *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputDelta:
			deltas.WriteString(c.Text)
		case provider.ChunkTypeToolCall:
			calls = append(calls, c.ToolCall)
		case provider.ChunkTypeFinish:
			fin = c
		}
	}
	if deltas.String() != want {
		t.Fatalf("deltas = %s", deltas.String())
	}
	got, _ := json.Marshal(calls[0].Arguments)
	var a, b interface{}
	_ = json.Unmarshal(got, &a)
	_ = json.Unmarshal([]byte(want), &b)
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if len(calls) != 1 || calls[0].ToolName != "writeItems" || string(ga) != string(gb) {
		t.Fatalf("calls = %+v", calls)
	}
	if fin == nil || fin.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish = %+v", fin)
	}
}

func TestTSStreamNoArgsToolCallKeepsSignature(t *testing.T) {
	m := vertexFixtureModel(t, "google-stream-no-args-tool-call")
	chunks := streamAll(t, m, &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			objTool("read_theme", map[string]interface{}{}),
			objTool("read_screen", map[string]interface{}{"id": map[string]interface{}{"type": "string"}}),
		},
		ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"streamFunctionCallArguments": true}},
	})
	var names, inputs []string
	var themeMeta map[string]map[string]interface{}
	var startMeta, endMeta string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolCall:
			names = append(names, c.ToolCall.ToolName)
			b, _ := json.Marshal(c.ToolCall.Arguments)
			inputs = append(inputs, string(b))
			if c.ToolCall.ToolName == "read_theme" {
				if err := json.Unmarshal(c.ProviderMetadata, &themeMeta); err != nil {
					t.Fatalf("tool-call metadata %q: %v", c.ProviderMetadata, err)
				}
			}
		case provider.ChunkTypeToolInputStart:
			if c.ToolCall.ToolName == "read_theme" {
				startMeta = string(c.ProviderMetadata)
			}
		case provider.ChunkTypeToolInputEnd:
			if m := string(c.ProviderMetadata); strings.Contains(m, "thoughtSignature") {
				endMeta = m
			}
		}
	}
	if got := strings.Join(names, ","); got != "read_theme,read_screen,read_screen,read_screen" {
		t.Fatalf("calls = %s", got)
	}
	if got := strings.Join(inputs, ","); got != `{},{"id":"A"},{"id":"B"},{"id":"C"}` {
		t.Fatalf("inputs = %s", got)
	}
	sig, _ := themeMeta["vertex"]["thoughtSignature"].(string)
	if len(sig) <= 100 {
		t.Fatalf("read_theme metadata = %+v", themeMeta)
	}
	if themeMeta["googleVertex"]["thoughtSignature"] != sig {
		t.Fatalf("googleVertex key missing: %+v", themeMeta)
	}
	if !strings.Contains(startMeta, sig) {
		t.Fatalf("tool-input-start for the no-args call lacks the signature: %s", startMeta)
	}
	if !strings.Contains(endMeta, sig) {
		t.Fatalf("tool-input-end for the no-args call lacks the signature: %q", endMeta)
	}
}
