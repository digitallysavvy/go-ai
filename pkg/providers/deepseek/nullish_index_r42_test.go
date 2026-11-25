package deepseek

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS makes choice.index nullish and only reports choiceIndex when it is
// present; the finish part always carries providerMetadata[name].
func TestStreamOmitsChoiceIndexWhenAbsentButKeepsProviderMetadata(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck
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
	if finish == nil || len(finish.ProviderMetadata) == 0 {
		t.Fatalf("finish = %#v, want providerMetadata", finish)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	ds, ok := meta["deepseek"]
	if !ok {
		t.Fatalf("meta = %v, want deepseek key", meta)
	}
	if _, has := ds["choiceIndex"]; has {
		t.Fatalf("choiceIndex = %v, want absent", ds["choiceIndex"])
	}
}

func TestResponseOmitsChoiceIndexWhenAbsent(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat")
	var resp deepseekResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	res, err := m.convertResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["deepseek"].(map[string]interface{})
	if _, has := meta["choiceIndex"]; has {
		t.Fatalf("choiceIndex = %v, want absent", meta["choiceIndex"])
	}
}
