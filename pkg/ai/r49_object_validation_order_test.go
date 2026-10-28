package ai

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateObject and streamObject run validateObjectGenerationInput before
// prepareRetries, so invalid output settings are reported ahead of an invalid
// maxRetries.
func TestR49ObjectInputValidatedBeforeMaxRetries(t *testing.T) {
	negative := -1
	model := &testutil.MockLanguageModel{}
	check := func(name string, err error) {
		t.Helper()
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "enumValues" {
			t.Fatalf("%s: err = %v, want the enumValues InvalidArgumentError", name, err)
		}
	}
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "p", OutputMode: ObjectModeEnum, MaxRetries: &negative})
	check("GenerateObject", err)
	_, err = StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "p", OutputMode: ObjectModeEnum, MaxRetries: &negative})
	check("StreamObject", err)
}
