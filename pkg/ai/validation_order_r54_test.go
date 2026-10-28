package ai

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText/streamText prepare toolCallConcurrency before
// prepareRetries, so with both invalid the toolCallConcurrency error wins.
func TestGenerateAndStreamTextReportToolCallConcurrencyBeforeMaxRetries(t *testing.T) {
	maxRetries, concurrency, streamRetries := -1, 0, -1
	model := &testutil.MockLanguageModel{}
	check := func(name string, err error) {
		t.Helper()
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Field != "toolCallConcurrency" {
			t.Fatalf("%s: err = %v, want the toolCallConcurrency error", name, err)
		}
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries, ToolCallConcurrency: &concurrency})
	check("GenerateText", err)
	_, err = StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries, StreamRetries: &streamRetries, ToolCallConcurrency: &concurrency})
	check("StreamText", err)
}
