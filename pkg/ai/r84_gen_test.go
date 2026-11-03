package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
