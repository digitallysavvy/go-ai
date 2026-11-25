package anthropic

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS 84208104a4 google-vertex-anthropic-provider.test.ts: the native-output
// opt-in is supported for claude-fable-5-2.
func TestR90provaFable52NativeOutput(t *testing.T) {
	if ClaudeFable5_2 != "claude-fable-5-2" || !vertexAnthropicSupportsNativeStructuredOutput(string(ClaudeFable5_2)) {
		t.Fatal("claude-fable-5-2 must support the native-output opt-in")
	}
}

// TS google-vertex-anthropic-provider.ts transformRequestHeaders:
// parseProviderOptions({provider: 'vertex'}) throws InvalidArgumentError
// {argument: 'providerOptions', message: 'invalid vertex provider options'}
// for labels that are not a string record (null included) or vertex options
// that are not an object.
func TestR90provaLabelsParseProviderOptionsError(t *testing.T) {
	for name, vertex := range map[string]interface{}{
		"number value": map[string]interface{}{"labels": map[string]interface{}{"a": 1}},
		"null labels":  map[string]interface{}{"labels": nil},
		"array labels": map[string]interface{}{"labels": []interface{}{"a"}},
		"string":       "x",
	} {
		_, err := transformRequestHeaders(map[string]string{}, map[string]interface{}{"vertex": vertex})
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Field != "providerOptions" || err.Error() != "invalid vertex provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	out, err := transformRequestHeaders(map[string]string{}, map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]interface{}{"team": "a"}}})
	if err != nil || out[LabelsHeader] != "eyJ0ZWFtIjoiYSJ9" {
		t.Errorf("valid labels: %v %v", out, err)
	}
}
