package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateTextPrepareStepCarriesInstructionsAndMessagesForward(t *testing.T) {
	var systems []string
	var messageCounts []int
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			systems = append(systems, opts.Prompt.System)
			messageCounts = append(messageCounts, len(opts.Prompt.Messages))
			if len(systems) == 1 {
				return &types.GenerateResult{
					Text:         "call tool",
					ToolCalls:    []types.ToolCall{{ID: "tc", ToolName: "echo", Arguments: map[string]interface{}{"value": "x"}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	tool := types.Tool{
		Name: "echo",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return input["value"], nil
		},
	}
	override := "override"
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "start"}},
		}},
		System:   "initial",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(2)},
		PrepareStep: func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 0 {
				step.Instructions = &override
				step.Messages = append(step.Messages, types.Message{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "extra"}},
				})
			}
			return step
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(systems) != 2 || systems[0] != "override" || systems[1] != "override" {
		t.Fatalf("systems = %+v, want override carried forward", systems)
	}
	if len(messageCounts) != 2 || messageCounts[0] != 2 || messageCounts[1] <= messageCounts[0] {
		t.Fatalf("messageCounts = %+v, want override then response messages carried forward", messageCounts)
	}
}

func TestGenerateTextIncludeRequestMessagesAndBodyDefaults(t *testing.T) {
	rawReq := map[string]interface{}{"body": true}
	rawResp := map[string]interface{}{"finish_reason": "stop"}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				RawRequest:   rawReq,
				RawResponse:  rawResp,
			}, nil
		},
	}
	defaultResult, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("GenerateText default error = %v", err)
	}
	if defaultResult.Request.Body != nil || defaultResult.Response.Body != nil || len(defaultResult.FinalStep.Request.Messages) != 0 {
		t.Fatalf("default include retained data: request=%+v response=%+v", defaultResult.Request, defaultResult.Response)
	}
	includedResult, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Include: &IncludeOptions{
			RequestBody:     true,
			ResponseBody:    true,
			RequestMessages: true,
		},
	})
	if err != nil {
		t.Fatalf("GenerateText include error = %v", err)
	}
	if includedResult.Request.Body == nil || includedResult.Response.Body == nil || len(includedResult.FinalStep.Request.Messages) != 1 {
		t.Fatalf("include did not retain requested data: request=%+v response=%+v", includedResult.Request, includedResult.Response)
	}
}

func TestGenerateTextRefinesToolInputAndPassesSandboxAndMetadata(t *testing.T) {
	sandbox := struct{ name string }{"sandbox"}
	var receivedInput map[string]interface{}
	var receivedSandbox interface{}
	var receivedMetadata map[string]interface{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{{
					ID:           "tc",
					ToolName:     "lookup",
					Arguments:    map[string]interface{}{"q": "raw"},
					ToolMetadata: map[string]interface{}{"trace": "tool"},
				}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	tool := types.Tool{
		Name: "lookup",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			receivedInput = input
			receivedSandbox = options.ExperimentalSandbox
			receivedMetadata = options.ToolMetadata
			return "ok", nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:               model,
		Prompt:              "hi",
		Tools:               []types.Tool{tool},
		StopWhen:            []StopCondition{StepCountIs(1)},
		ExperimentalSandbox: sandbox,
		ExperimentalRefineToolInput: map[string]ToolInputRefiner{
			"lookup": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
				return map[string]interface{}{"q": "refined"}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if receivedInput["q"] != "refined" {
		t.Fatalf("received input = %+v, want refined", receivedInput)
	}
	if receivedSandbox != sandbox {
		t.Fatalf("sandbox = %+v, want %+v", receivedSandbox, sandbox)
	}
	if receivedMetadata["trace"] != "tool" {
		t.Fatalf("metadata = %+v, want tool metadata", receivedMetadata)
	}
}

func TestGenerateTextDownloadsToolResultFileURLsBeforeNextStep(t *testing.T) {
	var call int
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			call++
			if call == 1 {
				return &types.GenerateResult{
					ToolCalls: []types.ToolCall{{
						ID:        "tc",
						ToolName:  "file",
						Arguments: map[string]interface{}{},
					}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			var fileBlock types.FileContentBlock
			for _, message := range opts.Prompt.Messages {
				if message.Role != types.RoleTool {
					continue
				}
				for _, part := range message.Content {
					toolResult, ok := part.(types.ToolResultContent)
					if !ok || toolResult.Output == nil {
						continue
					}
					for _, block := range toolResult.Output.Content {
						if file, ok := block.(types.FileContentBlock); ok {
							fileBlock = file
						}
					}
				}
			}
			if string(fileBlock.Data) != "downloaded" || fileBlock.URL != "" || fileBlock.FileData.Type != types.FileDataTypeData {
				t.Fatalf("tool result file was not downloaded before next step: %+v", fileBlock)
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	tool := types.Tool{
		Name: "file",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return types.ToolResultOutput{
				Type: types.ToolResultOutputContent,
				Content: []types.ToolResultContentBlock{
					types.FileContentBlock{
						FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/file.txt", MediaType: "text/plain"},
						MediaType: "text/plain",
					},
				},
			}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "start"}},
		}},
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(2)},
		ExperimentalDownload: func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			if len(requests) != 1 || requests[0].URL != "https://example.test/file.txt" {
				t.Fatalf("download requests = %#v", requests)
			}
			return []*DownloadResult{{Data: []byte("downloaded")}}, nil
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestStreamTextResultStepsFinalStepAndResponseMessages(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Include: &IncludeOptions{
			RequestMessages: true,
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(result.Steps()) != 1 || result.FinalStep().Text != "hello" {
		t.Fatalf("unexpected stream steps/finalStep: %+v", result.Steps())
	}
	if len(result.ResponseMessages()) != 1 {
		t.Fatalf("response messages = %+v, want one assistant message", result.ResponseMessages())
	}
	if len(result.FinalStep().Request.Messages) != 1 {
		t.Fatalf("request messages were not included: %+v", result.FinalStep().Request)
	}
}

func TestStreamTextIncludeRawChunksRequestsProviderRawChunks(t *testing.T) {
	var includeRawChunks bool
	done := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			includeRawChunks = opts.IncludeRawChunks
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"provider": "chunk"}},
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var sawRaw bool
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Include: &IncludeOptions{RawChunks: includeBoolPtr(true)},
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeRaw {
				sawRaw = true
			}
		},
		OnFinish: func(result *StreamTextResult) {
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if !includeRawChunks {
		t.Fatal("Include.RawChunks was not forwarded to provider options")
	}
	if !sawRaw {
		t.Fatal("raw chunk was not forwarded to OnChunk")
	}
}

func TestStreamTextDeprecatedIncludeRawChunksFallbackWithInclude(t *testing.T) {
	var includeRawChunks bool
	done := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			includeRawChunks = opts.IncludeRawChunks
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"provider": "chunk"}},
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var sawRaw bool
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:            model,
		Prompt:           "hi",
		Include:          &IncludeOptions{RequestMessages: true},
		IncludeRawChunks: true,
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeRaw {
				sawRaw = true
			}
		},
		OnFinish: func(result *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if !includeRawChunks {
		t.Fatal("deprecated IncludeRawChunks fallback was not applied when Include.RawChunks was unset")
	}
	if !sawRaw {
		t.Fatal("raw chunk was not forwarded when deprecated fallback enabled it")
	}
}

func TestStreamTextIncludeRawChunksOverridesDeprecatedFallback(t *testing.T) {
	var includeRawChunks bool
	done := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			includeRawChunks = opts.IncludeRawChunks
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"provider": "chunk"}},
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var sawRaw bool
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:            model,
		Prompt:           "hi",
		Include:          &IncludeOptions{RawChunks: includeBoolPtr(false)},
		IncludeRawChunks: true,
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeRaw {
				sawRaw = true
			}
		},
		OnFinish: func(result *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if includeRawChunks {
		t.Fatal("Include.RawChunks=false did not override deprecated IncludeRawChunks=true")
	}
	if sawRaw {
		t.Fatal("raw chunk was forwarded when Include.RawChunks=false")
	}
}

type supportedURLMockModel struct {
	*testutil.MockLanguageModel
	supported map[string][]string
}

func (m *supportedURLMockModel) SupportedURLs() map[string][]string {
	return m.supported
}

func TestGenerateTextSkipsDownloadForModelSupportedUserFileURL(t *testing.T) {
	model := &supportedURLMockModel{
		MockLanguageModel: &testutil.MockLanguageModel{
			DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				file, ok := opts.Prompt.Messages[0].Content[0].(types.FileContent)
				if !ok {
					t.Fatalf("prompt content = %T, want FileContent", opts.Prompt.Messages[0].Content[0])
				}
				if file.FileData.Type != types.FileDataTypeURL || file.FileData.URL == "" {
					t.Fatalf("supported URL was downloaded: %+v", file)
				}
				return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
			},
		},
		supported: map[string][]string{"image/*": []string{`^https://example\.test/.+`}},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				MediaType: "image/png",
				FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/cat.png", MediaType: "image/png"},
			}},
		}},
		ExperimentalDownload: func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			if len(requests) != 1 || !requests[0].IsURLSupportedByModel {
				t.Fatalf("download requests = %#v, want supported URL plan", requests)
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestStreamTextPrepareStepModelOverrideUpdatesStepAndFinishMetadata(t *testing.T) {
	firstModel := &testutil.MockLanguageModel{
		ProviderName: "mock",
		ModelName:    "first",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "tc", ToolName: "echo", Arguments: map[string]interface{}{"value": "x"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
	secondModel := &testutil.MockLanguageModel{
		ProviderName: "mock",
		ModelName:    "second",
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var finishedStepModels []string
	var finalModel string
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: firstModel,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "start"}},
		}},
		Tools: []types.Tool{{
			Name: "echo",
			Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
				return input["value"], nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(2)},
		PrepareStep: func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 1 {
				step.Model = secondModel
			}
			return step
		},
		OnStepFinishEvent: func(ctx context.Context, e OnStepFinishEvent) {
			finishedStepModels = append(finishedStepModels, e.ModelID)
		},
		OnFinishEvent: func(ctx context.Context, e OnFinishEvent) {
			finalModel = e.ModelID
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_ = result.Text()
	steps := result.Steps()
	if len(finishedStepModels) != 2 || finishedStepModels[0] != "first" || finishedStepModels[1] != "second" {
		t.Fatalf("finishedStepModels = %#v, want first then second step models", finishedStepModels)
	}
	if len(steps) != 2 || steps[0].Model.ModelID != "first" || steps[1].Model.ModelID != "second" {
		t.Fatalf("steps = %#v, want first then second model metadata", steps)
	}
	if finalModel != "second" {
		t.Fatalf("finalModel = %q, want second", finalModel)
	}
}

func TestStreamTextSuppressesRawChunksWhenIncludeRawChunksFalse(t *testing.T) {
	done := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"provider": "chunk"}},
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var sawRaw bool
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Include: &IncludeOptions{RawChunks: includeBoolPtr(false)},
		OnChunk: func(chunk provider.StreamChunk) {
			if chunk.Type == provider.ChunkTypeRaw {
				sawRaw = true
			}
		},
		OnFinish: func(result *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if sawRaw {
		t.Fatal("raw chunk was forwarded when Include.RawChunks=false")
	}
}

func TestStreamTextPrepareStepToolOverrideAppliesToExecutionAndCallbacks(t *testing.T) {
	done := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "tc", ToolName: "prepared", Arguments: map[string]interface{}{"value": "raw"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
	var executed bool
	var callbackStep int = -1
	preparedTool := types.Tool{
		Name: "prepared",
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			if input["value"] != "refined" {
				t.Fatalf("tool input = %+v, want refined", input)
			}
			return "ok", nil
		},
	}
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "hi",
		StopWhen: []StopCondition{StepCountIs(1)},
		PrepareStep: func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions {
			step.Tools = []types.Tool{preparedTool}
			return step
		},
		ExperimentalRefineToolInput: map[string]ToolInputRefiner{
			"prepared": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
				if opts.Tool == nil || opts.Tool.Name != "prepared" {
					t.Fatalf("refiner tool = %+v, want prepared", opts.Tool)
				}
				return map[string]interface{}{"value": "refined"}, nil
			},
		},
		OnToolExecutionStart: func(ctx context.Context, e OnToolCallStartEvent) {
			callbackStep = e.StepNumber
		},
		OnFinish: func(result *StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if !executed {
		t.Fatal("prepared tool was not executed")
	}
	if callbackStep != 0 {
		t.Fatalf("callback step = %d, want 0", callbackStep)
	}
}

func TestGenerateTextPrepareStepModelControlsDownloadDecision(t *testing.T) {
	baseModel := &testutil.MockLanguageModel{}
	supportedModel := &supportedURLMockModel{
		MockLanguageModel: &testutil.MockLanguageModel{
			DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				file := opts.Prompt.Messages[0].Content[0].(types.FileContent)
				if file.FileData.Type != types.FileDataTypeURL {
					t.Fatalf("prepareStep model supported URL was downloaded: %+v", file)
				}
				return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
			},
		},
		supported: map[string][]string{"image/*": []string{`^https://example\.test/.+`}},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: baseModel,
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				MediaType: "image/png",
				FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/cat.png", MediaType: "image/png"},
			}},
		}},
		PrepareStep: func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions {
			step.Model = supportedModel
			return step
		},
		ExperimentalDownload: func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			if len(requests) != 1 || !requests[0].IsURLSupportedByModel {
				t.Fatalf("download requests = %#v, want supported URL after prepareStep model override", requests)
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextForwardsHeadersAndInternalIDs(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.Headers["x-test"] != "generate" {
				t.Fatalf("headers = %#v, want x-test generate", opts.Headers)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var startCallID string
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:   model,
		Prompt:  "hi",
		Headers: map[string]string{"x-test": "generate"},
		Internal: &InternalOptions{
			GenerateID:     func() string { return "response-id" },
			GenerateCallID: func() string { return "call-id" },
		},
		OnStart: func(ctx context.Context, e OnStartEvent) {
			startCallID = e.CallID
			if e.Headers["x-test"] != "generate" {
				t.Fatalf("start headers = %#v", e.Headers)
			}
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if startCallID != "call-id" {
		t.Fatalf("start call id = %q, want call-id", startCallID)
	}
	if result.Response.ID != "response-id" {
		t.Fatalf("response id = %q, want response-id", result.Response.ID)
	}
}

func TestStreamTextForwardsHeadersAndInternalCallID(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if opts.Headers["x-test"] != "stream" {
				t.Fatalf("headers = %#v, want x-test stream", opts.Headers)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var startCallID string
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Headers: map[string]string{"x-test": "stream"},
		Internal: &InternalOptions{
			GenerateCallID: func() string { return "stream-call-id" },
		},
		OnStart: func(ctx context.Context, e OnStartEvent) {
			startCallID = e.CallID
			if e.Headers["x-test"] != "stream" {
				t.Fatalf("start headers = %#v", e.Headers)
			}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_ = result.Steps()
	if startCallID != "stream-call-id" {
		t.Fatalf("start call id = %q, want stream-call-id", startCallID)
	}
}
