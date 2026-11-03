package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r82provbStreamParts streams body from a test server through an
// OpenAI-compatible chat model and returns the chunks read.
func r82provbStreamParts(t *testing.T, body string) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider"}).ChatModel("m")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		out = append(out, c)
	}
	return out
}

// TS openai-compatible pendingToolCalls keeps a present empty id (`id ??
// null`), so the later "real" id is not adopted and the call gets a
// generated id; and a never-named pending call throws in flush() before
// toolCallTracker.flush(), so the named call is not finalized and no
// finish follows.
func TestR82provbCompatPendingIDAndFlushThrow(t *testing.T) {
	parts := r82provbStreamParts(t, `data: {"id":"x","choices":[{"delta":{"tool_calls":[{"index":0,"id":"","function":{"arguments":"{"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"real","function":{"name":"t","arguments":"}"}}]},"finish_reason":"tool_calls"}]}

`)
	var call *types.ToolCall
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolCall {
			call = p.ToolCall
		}
	}
	if call == nil || call.ID == "real" || call.ID == "" || call.RawArguments != "{}" {
		t.Fatalf("tool call = %+v, want a generated id (not \"real\") and {}", call)
	}

	parts = r82provbStreamParts(t, `data: {"id":"x","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"arguments":"{}"}},{"index":1,"id":"c1","function":{"name":"t","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

`)
	var types_ []provider.ChunkType
	for _, p := range parts {
		types_ = append(types_, p.Type)
		if p.Type == provider.ChunkTypeToolCall || p.Type == provider.ChunkTypeFinish {
			t.Fatalf("unexpected %s after a never-named pending call: %v", p.Type, types_)
		}
	}
	if last := parts[len(parts)-1]; last.Type != provider.ChunkTypeError || !strings.Contains(last.Text, "function.name") {
		t.Fatalf("last part = %+v, want the function.name error", last)
	}
}

// r82provbTestExtractor is the TS "metadata extraction" testMetadataExtractor.
type r82provbTestExtractor struct{}

func (r82provbTestExtractor) ExtractMetadata(_ context.Context, body interface{}) (map[string]interface{}, error) {
	m, ok := body.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	v, ok := m["test_field"]
	if !ok {
		return nil, nil
	}
	return map[string]interface{}{"test-provider": map[string]interface{}{"value": v}}, nil
}

func (r82provbTestExtractor) CreateStreamExtractor() StreamMetadataExtractor {
	return &r82provbTestStreamExtractor{}
}

type r82provbTestStreamExtractor struct{ value interface{} }

func (e *r82provbTestStreamExtractor) ProcessChunk(chunk interface{}) {
	m, ok := chunk.(map[string]interface{})
	if !ok {
		return
	}
	choices, ok := m["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return
	}
	c, _ := choices[0].(map[string]interface{})
	if v, ok := m["test_field"]; ok && c["finish_reason"] == "stop" {
		e.value = v
	}
}

func (e *r82provbTestStreamExtractor) BuildMetadata() map[string]interface{} {
	if s, _ := e.value.(string); s != "" {
		return map[string]interface{}{"test-provider": map[string]interface{}{"value": s}}
	}
	return nil
}

// TS openai-compatible "metadata extraction" > "should process metadata
// from complete response" / "from streaming response".
func TestR82provbCompatMetadataExtractor(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(body, "data:") {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider", MetadataExtractor: r82provbTestExtractor{}}).ChatModel("gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"test-provider":{"value":"test_value"}}`

	body = `{"id":"chatcmpl-123","object":"chat.completion","created":1711115037,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant"},"finish_reason":"stop"}],"test_field":"test_value"}`
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(res.ProviderMetadata); string(got) != want {
		t.Fatalf("generate providerMetadata = %s, want %s", got, want)
	}

	body = "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"finish_reason\":\"stop\"}],\"test_field\":\"test_value\"}\n\ndata: [DONE]\n\n"
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || string(finish.ProviderMetadata) != want {
		t.Fatalf("finish = %+v, want providerMetadata %s", finish, want)
	}

	// Accepted/rejected prediction tokens stay on the extracted entry for
	// the provider key (TS sets them on providerMetadata[key] after the
	// spread).
	got := mergeExtractedMetadata(map[string]interface{}{"p": map[string]interface{}{"acceptedPredictionTokens": 1}}, "p",
		map[string]interface{}{"p": map[string]interface{}{"x": 2}, "q": 3})
	if b, _ := json.Marshal(got); string(b) != `{"p":{"acceptedPredictionTokens":1,"x":2},"q":3}` {
		t.Fatalf("merge = %s", b)
	}
}

// TS mapComputerCallInput: `actions ?? (action != null ? [action] : [])` --
// a present empty actions list is not replaced by the singular action.
func TestR82provbComputerCallEmptyActionsKept(t *testing.T) {
	for _, tc := range []struct{ wire, want string }{
		{`{"type":"computer_call","id":"c","call_id":"k","status":"completed","action":{"type":"wait"},"actions":[]}`, `{"actions":[],"pendingSafetyChecks":[],"status":"completed"}`},
		{`{"type":"computer_call","id":"c","call_id":"k","status":"completed","action":{"type":"wait"},"actions":null}`, `{"actions":[{"type":"wait"}],"pendingSafetyChecks":[],"status":"completed"}`},
		{`{"type":"computer_call","id":"c","call_id":"k","status":"completed","action":{"type":"wait"}}`, `{"actions":[{"type":"wait"}],"pendingSafetyChecks":[],"status":"completed"}`},
	} {
		var item responses.ComputerCall
		if err := json.Unmarshal([]byte(tc.wire), &item); err != nil {
			t.Fatal(err)
		}
		if _, raw := computerCallArguments(item); raw != tc.want {
			t.Fatalf("%s: input = %s, want %s", tc.wire, raw, tc.want)
		}
	}
}
