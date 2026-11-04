package groq

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

var r87dprovrestPorted = map[string]string{
	tsfixture.PortedKey("should keep reasoning active when deltas include empty tool calls", "events.filter(({ type }) => type.startsWith('reasoning-'))", "toStrictEqual"): "TestR87dprovrestGroqReasoningActiveWithEmptyToolCalls",
}

// Hand ports of the groq-chat-language-model.test.ts assertions the replay
// harness does not evaluate (R87D_TSFIXTURE). Each test replays the
// committed case and checks the TS expression with the TS expected value.

func r87dprovrestGroqModel(t *testing.T) func(base, id string) provider.LanguageModel {
	return func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gemma2-9b-it"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
}

// TS 1210: expect(events.filter(({ type }) => type.startsWith('reasoning-'))).toStrictEqual([...])
func TestR87dprovrestGroqReasoningActiveWithEmptyToolCalls(t *testing.T) {
	run := r87dprovrestReplay(t, "testdata/tscases/chat_asserts.json.gz", "should keep reasoning active when deltas include empty tool calls", 0, nil, r87dprovrestGroqModel(t))
	var got []interface{}
	for _, p := range run.parts {
		if typ, _ := p.(map[string]interface{})["type"].(string); strings.HasPrefix(typ, "reasoning-") {
			got = append(got, p)
		}
	}
	r87dprovrestEqual(t, "reasoning parts", []interface{}{
		map[string]interface{}{"type": "reasoning-start", "id": "reasoning-0"},
		map[string]interface{}{"type": "reasoning-delta", "id": "reasoning-0", "delta": "Think "},
		map[string]interface{}{"type": "reasoning-delta", "id": "reasoning-0", "delta": "more..."},
		map[string]interface{}{"type": "reasoning-end", "id": "reasoning-0"},
	}, got)
}
