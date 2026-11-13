package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-image.ts keeps only `images` for non-gateway provider entries.
func TestGenerateImage_NonGatewayProviderMetadataKeepsOnlyImages(t *testing.T) {
	t.Parallel()
	model := &testutil.MockImageModel{
		MaxImages: 1,
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			return &types.ImageResult{
				Images: [][]byte{pngBytes},
				ProviderMetadata: map[string]interface{}{
					"mock": map[string]interface{}{
						"images": []interface{}{map[string]interface{}{"a": 1}},
						"extra":  "dropped",
					},
				},
			}, nil
		},
	}
	res, err := GenerateImage(context.Background(), GenerateImageOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	entry := res.ProviderMetadata["mock"].(map[string]interface{})
	if _, ok := entry["extra"]; ok {
		t.Fatalf("extra key kept: %v", entry)
	}
	if _, ok := entry["images"]; !ok {
		t.Fatalf("images missing: %v", entry)
	}
}

// TS step.reasoning keeps empty reasoning parts.
func TestReasoningPartsOfKeepsEmptyParts(t *testing.T) {
	t.Parallel()
	got := reasoningPartsOf([]types.ContentPart{types.ReasoningContent{Text: ""}, types.ReasoningContent{Text: "x"}})
	if len(got) != 2 {
		t.Fatalf("got %d parts", len(got))
	}
}

func TestMergeStreamedToolResultsKeepsArrivalOrder(t *testing.T) {
	t.Parallel()
	streamed := map[string]types.ToolResult{}
	var order []string
	ids := []string{"c9", "a1", "m5", "b2", "z0", "k7", "d4", "q3"}
	for _, id := range ids {
		order = recordStreamedToolResult(streamed, order, types.ToolResult{ToolCallID: id})
	}
	order = recordStreamedToolResult(streamed, order, types.ToolResult{ToolCallID: "a1"})
	for run := 0; run < 20; run++ {
		got := mergeStreamedToolResults(nil, streamed, order)
		if len(got) != len(ids) {
			t.Fatalf("got %d results", len(got))
		}
		for i, id := range ids {
			if got[i].ToolCallID != id {
				t.Fatalf("run %d position %d = %s want %s", run, i, got[i].ToolCallID, id)
			}
		}
	}
}

func r31ObjectModel(captured *[]types.Message, system *string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			*captured = opts.Prompt.Messages
			*system = opts.Prompt.System
			return &types.GenerateResult{Text: `{"name":"x"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

var r31ObjectSchema = schema.NewSimpleJSONSchema(map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
})

// TS standardizePrompt rejects system messages in `messages` unless
// allowSystemInMessages is set; `instructions` is `system`'s alias.
func TestGenerateObject_SystemMessagesNeedOptIn(t *testing.T) {
	t.Parallel()
	var msgs []types.Message
	var system string
	model := r31ObjectModel(&msgs, &system)
	sysMsg := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be nice"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	const tsText = "Invalid prompt: System messages are not allowed in the prompt or messages fields. Use the instructions option instead."
	var sysErr *promptutils.UnsupportedSystemMessageError
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema})
	if !errors.As(err, &sysErr) || err.Error() != tsText {
		t.Fatalf("err = %v", err)
	}
	// GenerateText and StreamText reject the same way (all four entry points agree).
	sres, err := StreamText(context.Background(), StreamTextOptions{Model: model, Messages: sysMsg})
	if err == nil {
		_, err = sres.ReadAll()
	}
	if !errors.As(err, &sysErr) {
		t.Fatalf("StreamText err = %v", err)
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Messages: sysMsg}); !errors.As(err, &sysErr) || err.Error() != tsText {
		t.Fatalf("GenerateText err = %v", err)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema, AllowSystemInMessages: true}); err != nil {
		t.Fatalf("opt-in failed: %v", err)
	}
	instr := "from instructions"
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "hi", System: "old", Instructions: &instr, Schema: r31ObjectSchema}); err != nil {
		t.Fatal(err)
	}
	if system != instr {
		t.Fatalf("system = %q, want %q", system, instr)
	}
	_, err = StreamObject(context.Background(), StreamObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema})
	if !errors.As(err, &sysErr) || err.Error() != tsText {
		t.Fatalf("stream err = %v", err)
	}
}

// TS streamObject takes usage from the finish part only; a usage part followed
// by a finish part with the same totals must not be summed.
func TestStreamObject_UsageIsNotSummedAcrossParts(t *testing.T) {
	t.Parallel()
	in, out, total := int64(3), int64(4), int64(7)
	u := types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total}
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"x"}`},
				{Type: provider.ChunkTypeUsage, Usage: &u},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &u},
			}), nil
		},
	}
	res, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "p", Schema: r31ObjectSchema})
	if err != nil {
		t.Fatal(err)
	}
	if got := *res.Usage.TotalTokens; got != 7 {
		t.Fatalf("total tokens = %d, want 7", got)
	}
}

// TS startBatch sends `n: request.n ?? 1` for image requests.
func TestExperimentalStartBatch_ImageRequestDefaultsNToOne(t *testing.T) {
	t.Parallel()
	var got *int
	mock := &mockBatchV4{providerName: "mock.batch", startFn: func(_ context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
		got = opts.Requests[0].Image.Options.N
		return &provider.BatchV4StartResult{BatchID: "b1"}, nil
	}}
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: mock,
		Requests: []BatchRequest{{Image: &BatchImageRequest{ID: "i1", Model: "m", Prompt: "cat"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != 1 {
		t.Fatalf("N = %v, want 1", got)
	}
}

type r31ReasoningRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	step []string
	end  []string
}

func (r *r31ReasoningRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.step = append(r.step, e.Reasoning)
}

func (r *r31ReasoningRecorder) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.end = append(r.end, e.Reasoning)
}

// TS legacy-open-telemetry.ts joins reasoning parts with "\n" for
// ai.response.reasoning, while reasoningText has no separator.
func TestGenerateText_TelemetryReasoningIsNewlineJoined(t *testing.T) {
	rec := &r31ReasoningRecorder{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "hi",
				Content:      []types.ContentPart{types.ReasoningContent{Text: "a"}, types.ReasoningContent{Text: "b"}, types.TextContent{Text: "hi"}},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "p",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ReasoningText != "ab" {
		t.Fatalf("ReasoningText = %q", res.ReasoningText)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.step) != 1 || rec.step[0] != "a\nb" {
		t.Fatalf("step reasoning = %q", rec.step)
	}
	if len(rec.end) != 1 || rec.end[0] != "a\nb" {
		t.Fatalf("end reasoning = %q", rec.end)
	}
}
