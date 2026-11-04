package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS add01abd2c (openai-compatible-chat-language-model.test.ts "should pass
// raw verbosity through when textVerbosity is not set" / "should prefer
// textVerbosity over raw verbosity").
func TestR87provbfireworksRawVerbosityPassthrough(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "m")
	for _, tc := range []struct {
		opts map[string]interface{}
		want string
	}{
		{map[string]interface{}{"verbosity": "low"}, "low"},
		{map[string]interface{}{"verbosity": "low", "textVerbosity": "high"}, "high"},
	} {
		body, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"fireworks": tc.opts}}, false)
		if body["verbosity"] != tc.want {
			t.Errorf("%v: verbosity = %v, want %q", tc.opts, body["verbosity"], tc.want)
		}
	}
}
