package parallel

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OVC-01 probe: Go-typed option literals (map[string]bool, the exported
// options struct) are read like their JSON form, both for the request and
// for the unknown-key warnings.
func TestR91toolsParallelTypedOptionLiterals(t *testing.T) {
	yes := true
	for name, options := range map[string]interface{}{
		"map[string]bool": map[string]bool{"includeExcerpts": true, "unused": true},
		"struct":          DecisionModelOptions{IncludeExcerpts: &yes},
	} {
		s, m, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
		opts := r85toolsOptions()
		opts.ProviderOptions = map[string]interface{}{"parallel": options}
		res, err := m.DoDecide(context.Background(), opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s.mu.Lock()
		if s.calls[0].body["include_excerpts"] != true {
			t.Errorf("%s: body = %v", name, s.calls[0].body)
		}
		s.mu.Unlock()
		var want []types.Warning
		if name == "map[string]bool" {
			want = []types.Warning{{Type: "unsupported", Feature: "providerOptions.parallel.unused"}}
		}
		if !reflect.DeepEqual(res.Warnings, want) {
			t.Errorf("%s: warnings = %+v", name, res.Warnings)
		}
	}
}
