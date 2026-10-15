package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// encodeTSStream turns the TS test's JSON event lines ({"<event>": {...}})
// into AWS event-stream frames, as the TS test's mocked decoder does in
// reverse.
func encodeTSStream(lines []string) (string, []byte) {
	var events [][2]string
	for _, l := range lines {
		var ev map[string]json.RawMessage
		if json.Unmarshal([]byte(strings.TrimSpace(l)), &ev) != nil || len(ev) != 1 {
			events = append(events, [2]string{"invalid", l})
			continue
		}
		for k, v := range ev {
			events = append(events, [2]string{k, string(v)})
		}
	}
	return "application/vnd.amazon.eventstream", buildBedrockEventStreamBody(events)
}

func TestTSCasesChat(t *testing.T) {
	tsfixture.StreamBodyEncoder = encodeTSStream
	defer func() { tsfixture.StreamBodyEncoder = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "anthropic.claude-3-haiku-20240307-v1:0"
		}
		m, err := New(Config{APIKey: "k", Region: "us-east-1", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}
