package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ported call-site checks (not TS tests — the TS tests live in
// generate-text.test.ts / stream-text.test.ts / generate-object.test.ts /
// stream-object.test.ts under "should log warnings" describe blocks): each
// of GenerateText, StreamText, GenerateObject and StreamObject calls
// LogWarnings once per model call with that call's provider/model warnings,
// matching TS's logWarnings placement in each of those files. The global
// logger is process state, so these tests are not parallel (see
// log_warnings_test.go).

func TestGenerateText_LogsModelWarnings(t *testing.T) {
	buf := setupLogWarnings(t)
	warning := types.Warning{Type: "other", Message: "generate-text warning"}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop, Warnings: []types.Warning{warning}}, nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi"}); err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if lines := logLines(buf); len(lines) != 2 || !strings.Contains(lines[1], "generate-text warning") {
		t.Fatalf("expected a logged warning, got lines=%v", lines)
	}
}

func TestGenerateText_WarnsOnStreamingOnlyTimeoutSetting(t *testing.T) {
	buf := setupLogWarnings(t)
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	perChunk := 5000 * time.Millisecond
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:   model,
		Prompt:  "hi",
		Timeout: &TimeoutConfig{PerChunk: &perChunk},
	}); err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	lines := logLines(buf)
	if len(lines) < 2 || !strings.Contains(lines[1], "chunkMs") {
		t.Fatalf("expected an unsupported timeout.chunkMs warning, got lines=%v", lines)
	}
}

func TestStreamText_LogsModelWarnings(t *testing.T) {
	buf := setupLogWarnings(t)
	warning := types.Warning{Type: "other", Message: "stream-text warning"}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{warning}},
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if lines := logLines(buf); len(lines) != 2 || !strings.Contains(lines[1], "stream-text warning") {
		t.Fatalf("expected a logged warning, got lines=%v", lines)
	}
}

func TestGenerateObject_LogsModelWarnings(t *testing.T) {
	buf := setupLogWarnings(t)
	warning := types.Warning{Type: "other", Message: "generate-object warning"}
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"value":"a"}`,
				FinishReason: types.FinishReasonStop,
				Warnings:     []types.Warning{warning},
			}, nil
		},
	}
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
	})
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "hi", Schema: sch}); err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}
	if lines := logLines(buf); len(lines) != 2 || !strings.Contains(lines[1], "generate-object warning") {
		t.Fatalf("expected a logged warning, got lines=%v", lines)
	}
}

func TestStreamObject_LogsModelWarnings(t *testing.T) {
	buf := setupLogWarnings(t)
	warning := types.Warning{Type: "other", Message: "stream-object warning"}
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{warning}},
				{Type: provider.ChunkTypeText, Text: `{"value":"a"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
	})
	if _, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "hi", Schema: sch}); err != nil {
		t.Fatalf("StreamObject() error = %v", err)
	}
	if lines := logLines(buf); len(lines) != 2 || !strings.Contains(lines[1], "stream-object warning") {
		t.Fatalf("expected a logged warning, got lines=%v", lines)
	}
}
