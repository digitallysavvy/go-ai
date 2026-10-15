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
)

func rawUsageFromMetadata(t *testing.T, md json.RawMessage) map[string]interface{} {
	t.Helper()
	var m map[string]map[string]interface{}
	if err := json.Unmarshal(md, &m); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	u, _ := m["anthropic"]["usage"].(map[string]interface{})
	return u
}

// TS passes response.usage through unchanged as Usage.raw and
// providerMetadata.anthropic.usage, including fields the SDK does not model.
func TestGenerateRawUsagePassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-5",
			"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":4,"output_tokens":3,"cache_creation_input_tokens":0,"service_tier":"standard",
			"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"server_tool_use":{"web_search_requests":1}}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model, _ := p.LanguageModel(ClaudeSonnet4_5)
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Raw["service_tier"] != "standard" {
		t.Fatalf("Usage.Raw = %#v", res.Usage.Raw)
	}
	if _, ok := res.Usage.Raw["cache_creation"]; !ok {
		t.Fatalf("Usage.Raw missing cache_creation: %#v", res.Usage.Raw)
	}
	if _, ok := res.Usage.Raw["cache_creation_input_tokens"]; !ok {
		t.Fatalf("explicit zero field dropped: %#v", res.Usage.Raw)
	}
	md, _ := json.Marshal(res.ProviderMetadata)
	u := rawUsageFromMetadata(t, md)
	if u["service_tier"] != "standard" || u["server_tool_use"] == nil {
		t.Fatalf("metadata usage = %#v", u)
	}
}

func TestStreamRawUsageMergesStartAndDelta(t *testing.T) {
	body := io.NopCloser(strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":10,\"service_tier\":\"standard\",\"cache_creation\":{\"ephemeral_5m_input_tokens\":2}}}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5,\"server_tool_use\":{\"web_search_requests\":2}}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	))
	stream := newAnthropicStream(body, false)
	var finish *provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish")
	}
	for _, raw := range []map[string]interface{}{finish.Usage.Raw, rawUsageFromMetadata(t, finish.ProviderMetadata)} {
		if raw["service_tier"] != "standard" || raw["server_tool_use"] == nil || raw["cache_creation"] == nil || raw["input_tokens"] == nil {
			t.Fatalf("raw usage = %#v", raw)
		}
	}
}
