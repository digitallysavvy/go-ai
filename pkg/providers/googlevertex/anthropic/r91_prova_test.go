package anthropic

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Typed-option ruling (probe): Vertex Anthropic labels and the Anthropic
// call options given as Go-typed literals (map[string]string,
// map[string]map[string]string, int64) send the same request as the
// map[string]interface{} form.
func TestR91provaTypedOptionLiterals(t *testing.T) {
	for name, po := range map[string]map[string]interface{}{
		"map[string]string labels": {"vertex": map[string]interface{}{"labels": map[string]string{"team": "a"}}},
		"nested typed map":         {"vertex": map[string]map[string]string{"labels": {"team": "a"}}},
		"plain":                    {"vertex": map[string]interface{}{"labels": map[string]interface{}{"team": "a"}}},
	} {
		m, cap := newLabelsModel(t, nil)
		po["anthropic"] = map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}}
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: po}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := base64.StdEncoding.DecodeString(cap.headers[0].Get(LabelsHeader))
		if string(got) != `{"team":"a"}` {
			t.Errorf("%s: labels header = %q", name, got)
		}
		if want := `"budget_tokens":2048`; !strings.Contains(cap.bodies[0], want) {
			t.Errorf("%s: body = %s", name, cap.bodies[0])
		}
	}
}
