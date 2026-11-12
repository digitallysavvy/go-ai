package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r91integStreamFileModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/a.png"}}},
			{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &types.ReasoningFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/r.png"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}), nil
	}}
}

// TS 2ebc78b654 (stream-language-model-call.ts passes download to
// resolveGeneratedFileData; loop-r91-stream item 2): StreamText downloads
// generated and reasoning file URLs with ExperimentalDownload.
func TestR91integStreamGeneratedFileUsesExperimentalDownload(t *testing.T) {
	t.Run("custom download", func(t *testing.T) {
		var got []promptutils.DownloadRequest
		res, err := StreamText(context.Background(), StreamTextOptions{Model: r91integStreamFileModel(), Prompt: "hi",
			ExperimentalDownload: func(_ context.Context, reqs []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				got = append(got, reqs...)
				return []*promptutils.DownloadResult{{Data: []byte("png"), MediaType: "image/png"}}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].URL != "https://files.example.com/a.png" || got[1].URL != "https://files.example.com/r.png" || got[0].IsURLSupportedByModel || got[1].IsURLSupportedByModel {
			t.Fatalf("download requests = %+v", got)
		}
		if files := res.Files(); len(files) != 1 || string(files[0].Data) != "png" {
			t.Fatalf("files = %+v", files)
		}
	})
	t.Run("nil result", func(t *testing.T) {
		res, err := StreamText(context.Background(), StreamTextOptions{Model: r91integStreamFileModel(), Prompt: "hi",
			ExperimentalDownload: func(context.Context, []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				return []*promptutils.DownloadResult{nil}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = res.ReadAll()
		err = res.Err()
		var dlErr *providererrors.DownloadError
		if !errors.As(err, &dlErr) || err.Error() != "Generated file URL https://files.example.com/a.png must be downloaded, but the download function returned null" {
			t.Fatalf("err = %v", err)
		}
	})
}
