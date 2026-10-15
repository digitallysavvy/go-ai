package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// End-to-end scenarios over the Converse and ConverseStream wire formats using
// the TS SDK's recorded fixtures (packages/amazon-bedrock/src/__fixtures__,
// copied to testdata/ts).

func bedrockFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ts", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixtureServer serves name.json for /converse and name.chunks.txt (one
// {eventType: payload} object per line, as TS stores it) as an AWS event
// stream for /converse-stream.
func fixtureServer(t *testing.T, name string, capture *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			_ = json.NewDecoder(r.Body).Decode(capture)
		}
		if strings.HasSuffix(r.URL.Path, "/converse-stream") {
			var evs [][2]string
			for _, l := range strings.Split(string(bedrockFixture(t, name+".chunks.txt")), "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				var m map[string]json.RawMessage
				if err := json.Unmarshal([]byte(l), &m); err != nil {
					t.Errorf("bad fixture line %q", l)
					continue
				}
				for k, v := range m {
					evs = append(evs, [2]string{k, string(v)})
				}
			}
			w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
			w.Header().Set("x-amzn-requestid", "req-xyz")
			_, _ = w.Write(buildBedrockEventStreamBody(evs))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-amzn-requestid", "req-xyz")
		_, _ = w.Write(bedrockFixture(t, name+".json"))
	}))
}

func drainChunks(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	defer s.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		out = append(out, c)
	}
}

func seqOf(chunks []*provider.StreamChunk) string {
	var parts []string
	for _, c := range chunks {
		n := string(c.Type)
		if len(parts) > 0 && parts[len(parts)-1] == n && (c.Type == provider.ChunkTypeText || c.Type == provider.ChunkTypeReasoning || c.Type == provider.ChunkTypeToolInputDelta) {
			continue
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ",")
}

func TestFixtureText(t *testing.T) {
	srv := fixtureServer(t, "amazon-bedrock-text", nil)
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	// TS snapshot "doGenerate > text": usage 22 in / 57 out, finish end_turn.
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "end_turn" ||
		*res.Usage.InputTokens != 22 || *res.Usage.OutputTokens != 57 || !strings.HasPrefix(res.Text, "Let me count the \"r\"s") {
		t.Fatalf("generate: %+v", res)
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.ID != "req-xyz" {
		t.Fatalf("response metadata = %+v", res.ResponseMetadata)
	}
	md := res.ProviderMetadata["amazonBedrock"].(map[string]interface{})
	if usage, _ := md["usage"].(map[string]interface{}); usage["cacheWriteInputTokens"] == nil {
		t.Fatalf("provider metadata = %#v", res.ProviderMetadata)
	}
	if _, ok := res.ProviderMetadata["bedrock"]; !ok {
		t.Fatal("providerMetadata must also be available under the bedrock key")
	}

	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainChunks(t, s)
	if got := seqOf(chunks); got != "stream-start,response-metadata,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	if chunks[1].ResponseMetadata.ID != "req-xyz" {
		t.Fatalf("response metadata = %+v", chunks[1].ResponseMetadata)
	}
}

func TestFixtureReasoning(t *testing.T) {
	srv := fixtureServer(t, "amazon-bedrock-reasoning", nil)
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")

	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) < 2 {
		t.Fatalf("content = %#v", res.Content)
	}
	rc, ok := res.Content[0].(types.ReasoningContent)
	if !ok || rc.Signature == "" || rc.Text == "" {
		t.Fatalf("content[0] = %#v, want signed reasoning first", res.Content[0])
	}
	if _, ok := res.Content[1].(types.TextContent); !ok {
		t.Fatalf("content[1] = %#v, want the answer text after the reasoning", res.Content[1])
	}

	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainChunks(t, s)
	// TS snapshot: reasoning-start, deltas, a signature delta, reasoning-end,
	// then the text block with the next index.
	if got := seqOf(chunks); got != "stream-start,response-metadata,reasoning-start,reasoning,reasoning-end,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	signed := false
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning && len(c.ProviderMetadata) > 0 {
			signed = strings.Contains(string(c.ProviderMetadata), `"signature"`) && strings.Contains(string(c.ProviderMetadata), `"bedrock"`)
		}
	}
	if !signed {
		t.Fatal("no signature metadata on a reasoning delta")
	}
}

func TestFixtureToolCallAndNoArgs(t *testing.T) {
	srv := fixtureServer(t, "amazon-bedrock-tool-call.1", nil)
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 {
		t.Fatalf("generate: %+v err=%v", res, err)
	}
	s, _ := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if got := seqOf(drainChunks(t, s)); got != "stream-start,response-metadata,tool-input-start,tool-input-delta,tool-input-end,tool-call,finish" {
		t.Fatalf("sequence = %s", got)
	}
	srv.Close()

	srv = fixtureServer(t, "amazon-bedrock-tool-no-args", nil)
	defer srv.Close()
	m = newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	s, _ = m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	for _, c := range drainChunks(t, s) {
		if c.Type == provider.ChunkTypeToolCall {
			if b, _ := json.Marshal(c.ToolCall.Arguments); string(b) != "{}" {
				t.Fatalf("tool-call arguments = %s, want {}", b)
			}
		}
	}
	res, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "updateIssueList" {
		t.Fatalf("generate no-args: %+v err=%v", res, err)
	}
	// TS snapshot: text first, then the tool call.
	if len(res.Content) != 2 {
		t.Fatalf("content = %#v", res.Content)
	}
	if _, ok := res.Content[0].(types.TextContent); !ok {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
}

// TS "should surface guardrail intervention as a content-filter finish reason
// with trace metadata".
func TestFixtureGuardrailIntervened(t *testing.T) {
	var body map[string]interface{}
	srv := fixtureServer(t, "amazon-bedrock-guard-content-intervened", &body)
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "Tell me something restricted.", ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"guardContent": true, "guardContentQualifiers": []interface{}{"query"}}}},
		}}}},
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"guardrailConfig": map[string]interface{}{"guardrailIdentifier": "1abcd2ef34gh", "guardrailVersion": "1", "trace": "enabled"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonContentFilter || res.RawFinishReason != "guardrail_intervened" {
		t.Fatalf("finish = %v %q", res.FinishReason, res.RawFinishReason)
	}
	if res.Text != "Sorry, the model cannot answer this question." {
		t.Fatalf("text = %q", res.Text)
	}
	md := res.ProviderMetadata["bedrock"].(map[string]interface{})
	if md["trace"] == nil {
		t.Fatalf("trace missing: %#v", md)
	}
	gc, _ := body["guardrailConfig"].(map[string]interface{})
	if gc["guardrailIdentifier"] != "1abcd2ef34gh" {
		t.Fatalf("guardrailConfig = %#v", body["guardrailConfig"])
	}
}

// TS "should handle JSON response format in streaming": the json tool's input
// becomes the text, the finish reason is stop and the metadata flags it.
func TestFixtureJSONResponseTool(t *testing.T) {
	srv := fixtureServer(t, "amazon-bedrock-json-tool.1", nil)
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Generate JSON"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"},
		}},
	}
	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainChunks(t, s)
	if got := seqOf(chunks); got != "stream-start,response-metadata,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	var text strings.Builder
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeText {
			text.WriteString(c.Text)
		}
	}
	fin := chunks[len(chunks)-1]
	if text.String() != `{"value":"test"}` || fin.FinishReason != types.FinishReasonStop || fin.RawFinishReason != "tool_use" {
		t.Fatalf("text=%q finish=%v/%q", text.String(), fin.FinishReason, fin.RawFinishReason)
	}
	if !strings.Contains(string(fin.ProviderMetadata), `"isJsonResponseFromTool":true`) {
		t.Fatalf("finish metadata = %s", fin.ProviderMetadata)
	}

	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonStop || !strings.Contains(res.Text, `"elements"`) || len(res.ToolCalls) != 0 {
		t.Fatalf("generate: %+v", res)
	}
}

func TestConverseHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		body      string
		retryable bool
	}{
		{400, `{"message":"The provided model identifier is invalid."}`, false},
		{429, `{"message":"Too many requests, please wait before trying again."}`, true},
		{503, `{"message":"Service unavailable"}`, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-3-haiku-20240307-v1:0")
		for name, call := range map[string]func() error{
			"generate": func() error {
				_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				return err
			},
			"stream": func() error {
				_, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				return err
			},
		} {
			err := call()
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.StatusCode != tc.status || pe.IsRetryable() != tc.retryable || !strings.Contains(pe.Message, "wait before trying again") && tc.status == 429 {
				t.Errorf("%d %s: err = %#v", tc.status, name, err)
			}
		}
		srv.Close()
	}
}

// Reasoning must come first when the turn is replayed, followed by the text
// and the tool call -- the API rejects a thinking block after other content.
func TestGenerateContentOrderIsResponseOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[
			{"reasoningContent":{"reasoningText":{"text":"hmm","signature":"SIG"}}},
			{"text":"Let me check."},
			{"toolUse":{"toolUseId":"","name":"weather","input":{"city":"SF"}}}
		]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
	}))
	defer srv.Close()
	m := newHTTPTestBedrockModel(t, srv, "anthropic.claude-sonnet-4-5-20250929-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	if _, ok := res.Content[0].(types.ReasoningContent); !ok {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
	if tx, ok := res.Content[1].(types.TextContent); !ok || tx.Text != "Let me check." {
		t.Fatalf("content[1] = %#v", res.Content[1])
	}
	call, ok := res.Content[2].(types.ToolCallContent)
	if !ok || call.ToolCallID == "" || call.ToolName != "weather" {
		t.Fatalf("content[2] = %#v (empty toolUseId must be replaced by a generated id)", res.Content[2])
	}
	if res.ToolCalls[0].ID != call.ToolCallID {
		t.Fatalf("tool call id %q != content id %q", res.ToolCalls[0].ID, call.ToolCallID)
	}
}
