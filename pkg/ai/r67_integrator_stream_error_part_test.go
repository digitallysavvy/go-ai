package ai

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR67IntegratorStreamPromptFailureErrorPartCarriesRawError: a prompt
// conversion failure (here a download callback's gateway error) before the
// first step (failBeforeStream) or before a later step (failNextStep) puts
// the error itself in the error part (TS pipeline and step catches enqueue
// `{ type: 'error', error }`); only OnError gets it through wrapGatewayError.
func TestR67IntegratorStreamPromptFailureErrorPartCarriesRawError(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	const wrapped = "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"
	fileURL := types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/file.txt", MediaType: "text/plain"}
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("step %d", failAt), func(t *testing.T) {
			rawErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
			model := &testutil.MockLanguageModel{
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return SimulateReadableStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "file", Arguments: map[string]interface{}{}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}, 0), nil
				},
			}
			content := []types.ContentPart{types.TextContent{Text: "start"}}
			if failAt == 1 {
				content = append(content, types.FileContent{FileData: fileURL, MediaType: "text/plain"})
			}
			tool := types.Tool{Name: "file", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return types.ToolResultOutput{
					Type:    types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{types.FileContentBlock{FileData: fileURL, MediaType: "text/plain"}},
				}, nil
			}}
			var mu sync.Mutex
			var onErrorErr error
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:    model,
				Messages: []types.Message{{Role: types.RoleUser, Content: content}},
				Tools:    []types.Tool{tool},
				StopWhen: []StopCondition{StepCountIs(3)},
				ExperimentalDownload: func(context.Context, []DownloadRequest) ([]*DownloadResult, error) {
					return nil, rawErr
				},
				OnError: func(_ context.Context, err error) {
					mu.Lock()
					onErrorErr = err
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			stream := result.Stream()
			var errorChunk *provider.StreamChunk
			for {
				c, err := stream.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeError {
					errorChunk = c
				}
			}
			if errorChunk == nil {
				t.Fatal("no error part")
			}
			if errorChunk.Text == wrapped {
				t.Errorf("error part text = %q, want the download error itself", errorChunk.Text)
			}
			mu.Lock()
			defer mu.Unlock()
			if onErrorErr == nil || onErrorErr.Error() != wrapped {
				t.Errorf("OnError error = %v, want the wrapped gateway error", onErrorErr)
			}
		})
	}
}
