package xai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TestR77ProvbTSCasesUsage replays convert-xai-responses-usage.test.ts
// through convertXAIResponsesUsage (the TS wire usage decoded as the
// Responses stream and response bodies decode it).
func TestR77ProvbTSCasesUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		var u responses.ResponsesAPIUsage
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertXAIResponsesUsage(&u)), nil, nil
	}, nil)
}
