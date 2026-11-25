package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS 2ebc78b654 (ruling 1, cr-r91-gen-to-integrator 1): the legacy agent
// loop downloads a generated file URL with the agent's ExperimentalDownload
// (isUrlSupportedByModel false), not with the default downloader.
func TestR91integAgentGeneratedFileUsesExperimentalDownload(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/gen.png"}}},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	var got []ai.DownloadRequest
	res, err := NewToolLoopAgent(AgentConfig{
		Model: model,
		ExperimentalDownload: func(_ context.Context, reqs []ai.DownloadRequest) ([]*ai.DownloadResult, error) {
			got = append(got, reqs...)
			return []*ai.DownloadResult{{Data: []byte("png"), MediaType: "image/png"}}, nil
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "draw"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://files.example.com/gen.png" || got[0].IsURLSupportedByModel {
		t.Fatalf("download requests = %+v", got)
	}
	var data string
	for _, part := range res.Steps[0].Content {
		if f, ok := part.(types.GeneratedFileContent); ok {
			data = string(f.Data)
		}
	}
	if data != "png" {
		t.Fatalf("step content = %#v", res.Steps[0].Content)
	}
}
