package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r84genSupportedURLModel struct {
	*testutil.MockLanguageModel
	supported map[string][]string
}

func (m *r84genSupportedURLModel) SupportedURLs() map[string][]string { return m.supported }

// TS convertToLanguageModelPrompt: supportedUrls are matched on
// url.toString() and the provider receives the WHATWG-normalized URL.
func TestR84genGenerateTextNormalizesFileURLs(t *testing.T) {
	var got types.FileContent
	model := &r84genSupportedURLModel{
		MockLanguageModel: &testutil.MockLanguageModel{
			DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				got = opts.Prompt.Messages[0].Content[0].(types.FileContent)
				return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
			},
		},
		// Matches only the serialized form (lowercase host, no :443, %20).
		supported: map[string][]string{"image/*": {`^https://example\.test/a%20b\.png$`}},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.FileContent{MediaType: "image/png", URL: "HTTPS://Example.TEST:443/x/../a b.png"}},
		}},
		ExperimentalDownload: func(_ context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			if len(requests) != 1 || !requests[0].IsURLSupportedByModel || requests[0].URL != "https://example.test/a%20b.png" {
				t.Fatalf("download requests = %#v", requests)
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.Type != types.FileDataTypeURL || got.FileData.URL != "https://example.test/a%20b.png" ||
		got.URL != "https://example.test/a%20b.png" || got.FileData.OriginalURL != "HTTPS://Example.TEST:443/x/../a b.png" {
		t.Fatalf("provider file part = %+v", got)
	}
}

// TS new URL(...) TypeError: a URL file part that does not parse fails the
// call before the model is invoked.
func TestR84genGenerateTextInvalidFileURL(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			t.Fatal("model called")
			return nil, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://exa mple.com/x.png"}}},
		}},
	})
	var invalid *promptutils.InvalidURLError
	if !errors.As(err, &invalid) || invalid.URL != "https://exa mple.com/x.png" {
		t.Fatalf("err = %v", err)
	}
}

// TS execute-tool-call.ts aborts only the tool's signal at its deadline: a
// tool that ignores it and still returns has its result, not a timeout error.
func TestR84genToolResultAfterTimeoutKept(t *testing.T) {
	toolTimeout := 10 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "late", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	late := types.Tool{
		Name: "late",
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			<-ctx.Done()
			return "finished anyway", nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go", Tools: []types.Tool{late},
		StopWhen: []StopCondition{StepCountIs(1)},
		Timeout:  &TimeoutConfig{ToolMs: &toolTimeout},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].Error != nil || res.ToolResults[0].Result != "finished anyway" {
		t.Fatalf("tool results = %+v", res.ToolResults)
	}
}

// TS generate-text.test.ts 9256: an invalid structuredOutputText (the empty
// string or a truncated object) is parsed as given, without falling back to the valid
// text content: NoObjectGeneratedError with that text and a JSONParseError.
func TestR84genInvalidStructuredOutputTextNoFallback(t *testing.T) {
	for _, structured := range []string{"", `{"value":`} {
		structured := structured
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Content:              []types.ContentPart{types.TextContent{Text: `{"value":"draft"}`}},
					Text:                 `{"value":"draft"}`,
					StructuredOutputText: &structured,
					FinishReason:         types.FinishReasonStop,
				}, nil
			},
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "prompt",
			Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r31ObjectSchema}),
		})
		var noObject *NoObjectGeneratedError
		var parseErr *providererrors.JSONParseError
		if !errors.As(err, &noObject) || noObject.Text != structured || !errors.As(noObject.Cause, &parseErr) {
			t.Fatalf("structuredOutputText %q: err = %#v", structured, err)
		}
	}
}

// TS convertToLanguageModelPrompt logs the "image" content part deprecation
// (logWarnings without provider or model) once per user message with image
// parts, on every step's prompt conversion.
func TestR84genImagePartDeprecationLoggedPerStep(t *testing.T) {
	var batches [][]types.Warning
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` && o.Provider == "" && o.Model == "" {
			batches = append(batches, o.Warnings)
		}
	})
	defer SetLogWarnings(nil)

	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "look"},
			types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png"},
			types.ImageContent{Image: []byte{4, 5}, MimeType: "image/jpeg"},
		}}},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batches = %+v, want two steps of two warnings", batches)
	}
	w := batches[0][0]
	if w.Type != "deprecated" || w.Message != promptutils.ImageContentPartDeprecation ||
		GetDeprecationCode(w.Setting, "") != "AISDK_DEP_IMAGE_CONTENT_PART" {
		t.Fatalf("warning = %+v", w)
	}
}
