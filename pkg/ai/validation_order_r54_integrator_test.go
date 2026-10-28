package ai

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS resolves the tool callers (resolveToolCallerConfiguration) before
// prepareRetries in generateText and streamText, so an invalid
// experimental_toolCallers wins over an invalid maxRetries.
func TestR54IntegratorToolCallersValidatedBeforeMaxRetries(t *testing.T) {
	maxRetries := -1
	model := &testutil.MockLanguageModel{}
	tools := []types.Tool{{Name: "a"}}
	callers := ExperimentalToolCallers{"missing": {DirectToolCall}}
	check := func(name string, err error) {
		t.Helper()
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "experimental_toolCallers" {
			t.Fatalf("%s: err = %v, want the experimental_toolCallers error", name, err)
		}
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: tools, ExperimentalToolCallers: callers, MaxRetries: &maxRetries})
	check("GenerateText", err)
	_, err = StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Tools: tools, ExperimentalToolCallers: callers, MaxRetries: &maxRetries})
	check("StreamText", err)
}
