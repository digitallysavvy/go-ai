package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamText_StepResponsePopulatesIDModelIDTimestamp verifies that
// StepResult.Response.ID/ModelID/Timestamp are populated from the stream's
// response-metadata chunk, not just Headers (audit: "From P0-3b: stream.go
// per-step StepResult.Response never sets ID/ModelID/Timestamp (only
// Headers)").
func TestStreamText_StepResponsePopulatesIDModelIDTimestamp(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
					ID:        "resp_123",
					ModelID:   "resolved-model",
					Timestamp: ts,
				}},
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	resp := steps[0].Response
	if resp.ID != "resp_123" {
		t.Errorf("Response.ID = %q, want %q", resp.ID, "resp_123")
	}
	if resp.ModelID != "resolved-model" {
		t.Errorf("Response.ModelID = %q, want %q", resp.ModelID, "resolved-model")
	}
	if !resp.Timestamp.Equal(ts) {
		t.Errorf("Response.Timestamp = %v, want %v", resp.Timestamp, ts)
	}

	// Also verify Response() (used across the whole call) reflects the same.
	got := result.Response()
	if got.ID != "resp_123" || got.ModelID != "resolved-model" || !got.Timestamp.Equal(ts) {
		t.Errorf("Response() = %+v, want ID=resp_123 ModelID=resolved-model Timestamp=%v", got, ts)
	}
}

// TS createStepResponse: a streamed step without response metadata reports a
// generated id (the call's generateId) and the step's start time, and a
// partial response-metadata chunk keeps those defaults for the fields it
// leaves out.
func TestStreamText_StepResponseDefaultsLikeTS(t *testing.T) {
	t.Parallel()
	for name, chunks := range map[string][]provider.StreamChunk{
		"no metadata": {
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
		},
		"model id only": {
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ModelID: "resolved"}},
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			model := &testutil.MockLanguageModel{
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream(chunks), nil
				},
			}
			before := time.Now()
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:    model,
				Prompt:   "hi",
				Internal: &InternalOptions{GenerateID: func() string { return "gen-id" }},
			})
			if err != nil {
				t.Fatalf("StreamText() error = %v", err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			resp := result.Steps()[0].Response
			if resp.ID != "gen-id" {
				t.Errorf("Response.ID = %q, want the generated id", resp.ID)
			}
			if resp.Timestamp.Before(before) || resp.Timestamp.After(time.Now()) {
				t.Errorf("Response.Timestamp = %v, want the step start time", resp.Timestamp)
			}
			if got := result.Response(); got.ID != "gen-id" || got.Timestamp.IsZero() {
				t.Errorf("Response() = %+v", got)
			}
		})
	}
}

// TS generateText/generateObject responseData: a provider response that
// reports only some metadata keeps it and gets defaults for the rest
// (id ?? generateId(), timestamp ?? new Date(), modelId ?? model.modelId).
func TestGenerate_PartialResponseMetadataGetsDefaultsLikeTS(t *testing.T) {
	t.Parallel()
	newModel := func(text string) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:             text,
					FinishReason:     types.FinishReasonStop,
					ResponseMetadata: &types.ResponseMetadata{ModelID: "resolved"},
				}, nil
			},
		}
	}
	before := time.Now()
	internal := &InternalOptions{GenerateID: func() string { return "gen-id" }}
	text, err := GenerateText(context.Background(), GenerateTextOptions{Model: newModel("hi"), Prompt: "hi", Internal: internal})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	resp := text.Steps[0].Response
	if resp.ID != "gen-id" || resp.ModelID != "resolved" || resp.Timestamp.Before(before) {
		t.Errorf("GenerateText step response = %+v", resp)
	}
	obj, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: newModel(`{"a":1}`), Prompt: "hi",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})
	if err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}
	if obj.Response.ID == "" || obj.Response.ModelID != "resolved" || obj.Response.Timestamp.IsZero() {
		t.Errorf("GenerateObject response = %+v", obj.Response)
	}
}

// The language-model-call-end event reports the step response id: the
// provider's, a finish chunk's, or else the generated default (TS
// stepResponse.id).
func TestStreamText_ModelCallEndResponseID(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		finish provider.StreamChunk
		want   string
	}{
		"generated":       {provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}, "gen-id"},
		"finish metadata": {provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop", ResponseMetadata: &provider.ResponseMetadata{ID: "resp-finish"}}, "resp-finish"},
	} {
		t.Run(name, func(t *testing.T) {
			model := &testutil.MockLanguageModel{
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "hi"}, tc.finish}), nil
				},
			}
			var got string
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "hi",
				Internal:               &InternalOptions{GenerateID: func() string { return "gen-id" }},
				OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { got = e.ResponseID },
			})
			if err != nil {
				t.Fatalf("StreamText() error = %v", err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("model call end ResponseID = %q, want %q", got, tc.want)
			}
		})
	}
}
