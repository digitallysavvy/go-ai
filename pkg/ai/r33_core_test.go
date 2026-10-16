package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS standardizePrompt throws InvalidPromptError when neither or both of
// prompt and messages are given, and when messages are empty.
func TestPromptInputIsStandardized(t *testing.T) {
	t.Parallel()
	called := false
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			called = true
			return &types.GenerateResult{Text: `{"name":"x"}`, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			called = true
			return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
		},
	}
	user := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}
	cases := []struct {
		name     string
		prompt   string
		messages []types.Message
		want     string
	}{
		{"neither", "", nil, "Invalid prompt: prompt or messages must be defined"},
		{"both", "hi", user, "Invalid prompt: prompt and messages cannot be defined at the same time"},
		{"empty messages", "", []types.Message{}, "Invalid prompt: messages must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := func(entry string, err error) {
				t.Helper()
				var invalid *providererrors.InvalidPromptError
				if !errors.As(err, &invalid) || err.Error() != tc.want {
					t.Fatalf("%s err = %v, want InvalidPromptError %q", entry, err, tc.want)
				}
			}
			_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: tc.prompt, Messages: tc.messages})
			check("GenerateText", err)
			res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: tc.prompt, Messages: tc.messages})
			if err == nil {
				_, err = res.ReadAll()
			}
			check("StreamText", err)
			_, err = GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: tc.prompt, Messages: tc.messages, Schema: r31ObjectSchema})
			check("GenerateObject", err)
			if called {
				t.Fatal("the model was called for an invalid prompt")
			}
		})
	}
}

// A provider that reports no embedding usage (TS NaN tokens) leaves the root
// span's ai.usage.tokens unset instead of recording 0.
func TestTelemetryUsageFromEmbeddingUsage(t *testing.T) {
	t.Parallel()
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{}); u.TotalTokens != nil {
		t.Fatalf("no usage -> %d, want unset", *u.TotalTokens)
	}
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{Tokens: 7, TotalTokens: 7}); u.TotalTokens == nil || *u.TotalTokens != 7 {
		t.Fatalf("usage 7 -> %v", u.TotalTokens)
	}
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{TotalTokens: 3}); u.TotalTokens == nil || *u.TotalTokens != 3 {
		t.Fatalf("legacy TotalTokens-only usage -> %v", u.TotalTokens)
	}
}

// TS generateText rethrows the output parse error unchanged (no wrapper text).
func TestGenerateText_OutputParseErrorIsReturnedAsIs(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "not json", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hi",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r31ObjectSchema}),
	})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %T %v, want NoObjectGeneratedError", err, err)
	}
	if strings.HasPrefix(err.Error(), "output parsing failed") {
		t.Fatalf("err text has a wrapper prefix: %v", err)
	}
}
