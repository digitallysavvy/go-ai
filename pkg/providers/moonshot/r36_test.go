package moonshot

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS forwards promptCacheKey / safetyIdentifier whenever they are not null,
// including the empty string.
func TestMoonshotForwardsEmptyPromptCacheKeyAndSafetyIdentifier(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "kimi-k2.5")
	body, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"moonshotai": map[string]interface{}{
			"promptCacheKey": "", "safetyIdentifier": "",
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["prompt_cache_key"]; !ok || v != "" {
		t.Errorf("prompt_cache_key = %#v (present %v), want empty string", v, ok)
	}
	if v, ok := body["safety_identifier"]; !ok || v != "" {
		t.Errorf("safety_identifier = %#v (present %v), want empty string", v, ok)
	}
}

// TS keeps toolCallTypes for any non-null tool_calls, even an empty array.
func TestMoonshotResponseKeepsEmptyToolCallTypes(t *testing.T) {
	var resp moonshotResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"x","tool_calls":[]}}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	res, err := NewLanguageModel(New(Config{APIKey: "k"}), "kimi-k2.5").convertResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["moonshotai"].(map[string]json.RawMessage)
	if string(meta["toolCallTypes"]) != "[]" {
		t.Errorf("toolCallTypes = %s, want []", meta["toolCallTypes"])
	}
}

// An error chunk after a finish_reason resets the finish to {error, raw:
// undefined}; the earlier raw reason must not survive.
func TestMoonshotStreamErrorChunkClearsRawFinishReason(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"stop"}]}` + "\n\n" +
		`data: {"error":{"message":"boom","type":"server_error"}}` + "\n\n" +
		"data: [DONE]\n\n"
	s := newMoonshotStream(io.NopCloser(strings.NewReader(sse)), false)
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
	if finish == nil || finish.FinishReason != types.FinishReasonError || finish.RawFinishReason != "" {
		t.Fatalf("finish = %+v, want error with empty raw", finish)
	}
}
