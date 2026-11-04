package anthropic

import (
	"testing"
)

// TS getBetasFromHeaders trims the header values with String.prototype.trim
// (U+FEFF is trimmed, U+0085 is not) and drops empty ones; the
// providerOptions anthropicBeta entries join the Set as given (no trim, ""
// kept). Expected value from Node:
//
//	getBetasFromHeaders("\ufeffB1 ,\u0085, b2", " ") then
//	new Set([...user, " b3 ", "", "b1"]) joined with ","
func TestR86bprovaBetaTrimAndSet(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k", Headers: map[string]string{"anthropic-beta": "\ufeffB1 ,\u0085, b2"}}), ClaudeSonnet4_6, nil)
	opts := genOpts("hi", map[string]interface{}{
		"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{" b3 ", "", "b1"}},
	})
	opts.Headers = map[string]string{"anthropic-beta": " "}
	if got, want := model.combineBetaHeaders(opts, false), "b1,\u0085,b2, b3 ,"; got != want {
		t.Fatalf("anthropic-beta = %q, want %q", got, want)
	}
}
