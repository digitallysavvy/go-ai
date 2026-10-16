package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
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

// TS reports a doStream failure as the error itself, not wrapped in text.
func TestStreamText_StartFailureIsReturnedAsIs(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, boom
		},
	}
	maxRetries := 0
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &maxRetries})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

// TS wrapGatewayError: generateObject turns an unauthenticated AI Gateway error
// into the "Unauthenticated request to AI Gateway" error; the gateway error
// stays reachable for Go callers.
func TestGenerateObject_WrapsGatewayAuthenticationError(t *testing.T) {
	authErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, authErr
		},
	}
	for _, tc := range []struct{ env, want string }{
		{"", "\u001b[1m\u001b[31mUnauthenticated request to AI Gateway.\u001b[0m\n\nTo authenticate, set the \u001b[33mAI_GATEWAY_API_KEY\u001b[0m environment variable with your API key.\n\nAlternatively, you can use a provider module instead of the AI Gateway.\n\nLearn more: \u001b[34mhttps://ai-sdk.dev/unauthenticated-ai-gateway\u001b[0m\n\n"},
		{"production", "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"},
	} {
		t.Setenv("NODE_ENV", tc.env)
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "hi", Schema: r31ObjectSchema})
		if err == nil || err.Error() != tc.want {
			t.Fatalf("NODE_ENV=%q err = %q, want %q", tc.env, err, tc.want)
		}
		var auth *gatewayerrors.GatewayAuthenticationError
		if !errors.As(err, &auth) {
			t.Fatal("the gateway authentication error is no longer reachable")
		}
	}
	// other errors pass through unchanged
	boom := errors.New("boom")
	if got := wrapGatewayError(boom); got != boom {
		t.Fatalf("wrapGatewayError(other) = %v", got)
	}
}

// TS generateText and streamText wrap an AI Gateway authentication error
// (wrapGatewayError) like generateObject; the original stays reachable.
func TestTextCallsWrapGatewayAuthenticationError(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	const want = "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"
	authErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	zero := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, authErr
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, authErr
		},
	}
	var auth *gatewayerrors.GatewayAuthenticationError
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", MaxRetries: &zero})
	if err == nil || err.Error() != want || !errors.As(err, &auth) {
		t.Fatalf("GenerateText err = %v", err)
	}
	var onErr error
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxRetries: &zero,
		OnError: func(_ context.Context, e error) { onErr = e }})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil || err.Error() != want || !errors.As(err, &auth) {
		t.Fatalf("StreamText err = %v", err)
	}
	if onErr == nil || onErr.Error() != want {
		t.Fatalf("StreamText OnError = %v", onErr)
	}
}
