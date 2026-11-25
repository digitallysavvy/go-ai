package errors_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Round 96: providererrors.JSONParseError became an alias of the type the
// internal decoders return. The public API still works the way callers use
// it: construct it, match it with errors.As on *providererrors.JSONParseError
// (also wrapped, and from a real provider call that rejects a __proto__
// payload), read Text / Cause, and IsJSONParseError.
func TestR96integJSONParseErrorPublicAPI(t *testing.T) {
	cause := errors.New("boom")
	err := fmt.Errorf("wrapped: %w", providererrors.NewJSONParseError("{x", cause))
	var target *providererrors.JSONParseError
	if !errors.As(err, &target) || target.Text != "{x" || !errors.Is(target.Cause, cause) {
		t.Fatalf("errors.As = %+v", target)
	}
	if !providererrors.IsJSONParseError(err) {
		t.Fatal("IsJSONParseError = false")
	}
	if want := "JSON parsing failed: Text: {x.\nError message: boom"; target.Error() != want {
		t.Fatalf("Error() = %q, want %q", target.Error(), want)
	}
	literal := &providererrors.JSONParseError{Text: "t"}
	if literal.Error() != "JSON parsing failed: Text: t.\nError message: unknown error" {
		t.Fatalf("literal Error() = %q", literal.Error())
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"__proto__":{"x":1}}`))
	}))
	defer srv.Close()
	model, err := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}}}},
	})
	target = nil
	if !errors.As(err, &target) || !strings.Contains(target.Error(), "Object contains forbidden prototype property") {
		t.Fatalf("provider call err = %v, want a *providererrors.JSONParseError for the __proto__ body", err)
	}
}
