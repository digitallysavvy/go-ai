package fireworks

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TS fireworksErrorStructure: errorToMessage is the string error or
// error.message, an empty string included.
func TestR87provbErrorEmptyMessageKept(t *testing.T) {
	for _, body := range []string{`{"error":""}`, `{"error":{"message":"","code":7}}`} {
		err := parseFireworksProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "" {
			t.Fatalf("%s: err = %#v, want a ProviderError with an empty message", body, err)
		}
	}
	if err := parseFireworksProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"code":7}}`)}); err != nil {
		t.Fatalf("missing message: err = %#v, want nil", err)
	}
}
