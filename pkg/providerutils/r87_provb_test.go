package providerutils

import "testing"

// The deprecated v0.5 option spellings warn in a fixed order, not in Go map
// iteration order.
func TestR87provbDeprecatedOptionWarningsDeterministic(t *testing.T) {
	opts := map[string]interface{}{"reasoning-effort": "low", "text-verbosity": "high", "text_verbosity": "high"}
	for i := 0; i < 50; i++ {
		w := OpenAICompatibleCommonOptionWarnings(opts)
		if len(w) != 3 || w[0].Setting != "provider option key 'reasoning-effort'" || w[1].Setting != "provider option key 'text-verbosity'" || w[2].Setting != "provider option key 'text_verbosity'" {
			t.Fatalf("warnings = %+v", w)
		}
	}
}
