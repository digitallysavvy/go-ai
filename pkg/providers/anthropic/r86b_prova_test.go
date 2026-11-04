package anthropic

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TS parseJsonLines (provider-utils response-handler.ts) skips a line only
// when line.trim() is empty and JSON.parses the line as read: a line holding
// only U+FEFF and a space is skipped, U+0085 alone and U+00A0 before the JSON
// value are JSONParseErrors (Node: JSON.parse("\u00a0{}") throws).
func TestR86bprovaAnthropicResultsJSLineTrim(t *testing.T) {
	valid := `{"custom_id":"a","result":{"type":"canceled"}}`
	cases := []struct {
		name  string
		body  string
		items int
		fail  bool
	}{
		{"bom-only line skipped", valid + "\n\ufeff \n", 1, false},
		{"nbsp before value", "\u00a0" + valid + "\n", 0, true},
		{"nel-only line", valid + "\n\u0085\n", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := newAnthropicBatchResultsStream(io.NopCloser(strings.NewReader(tc.body)), nil, "https://x.example/r", 1<<20)
			items := 0
			for {
				item, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					var jp *providererrors.JSONParseError
					if !tc.fail || !errors.As(err, &jp) {
						t.Fatalf("err = %T %v", err, err)
					}
					tc.fail = false
					break
				}
				if item == nil {
					t.Fatal("nil item")
				}
				items++
			}
			if items != tc.items || tc.fail {
				t.Fatalf("items = %d (want %d), missing JSONParseError = %v", items, tc.items, tc.fail)
			}
		})
	}
}
