package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 73 (gen): ToolErrorContent.Err (owner ruling 2026-10-31, option c)
// and generate-text.test.ts ports.

type r73genToolErr struct{ code int }

func (e *r73genToolErr) Error() string { return "tool failed" }

func r73genFailingTool(name string) types.Tool {
	return types.Tool{
		Name:       name,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, &r73genToolErr{code: 42}
		},
	}
}

func r73genToolErrors(content []types.ContentPart) []types.ToolErrorContent {
	var out []types.ToolErrorContent
	for _, p := range content {
		if te, ok := p.(types.ToolErrorContent); ok {
			out = append(out, te)
		}
	}
	return out
}

// TS tool-error part: error is the thrown value. Go keeps the text in Error
// and the typed error in Err; the JSON is unchanged.
func TestR73Gen_GenerateTextToolErrorContentKeepsTypedErr(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r73genFailingTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tes := r73genToolErrors(res.Steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", res.Steps[0].Content)
	}
	te := tes[0]
	var target *r73genToolErr
	if !errors.As(te.Err, &target) || target.code != 42 {
		t.Fatalf("Err = %#v, want the tool's *r73genToolErr", te.Err)
	}
	if te.Error != "tool failed" || te.ErrorText != "Error: tool failed" {
		t.Fatalf("Error = %#v, ErrorText = %q", te.Error, te.ErrorText)
	}
	b, err := json.Marshal(te)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"Err"`) || strings.Contains(string(b), `"err"`) {
		t.Fatalf("JSON = %s, Err must not be serialized", b)
	}
	// The result's content (TS steps.flatMap(step => step.content)) holds the
	// same typed error.
	if got := r73genToolErrors(res.Content); len(got) != 1 || got[0].Err != te.Err {
		t.Fatalf("result content tool errors = %#v", got)
	}
}

// The SDK's own errors (invalid input, unknown tool) are typed too.
func TestR73Gen_GenerateTextInvalidInputToolErrorErr(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": 1}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tes := r73genToolErrors(res.Steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", res.Steps[0].Content)
	}
	var target *InvalidToolInputError
	if !errors.As(tes[0].Err, &target) || target.ToolName != "tool1" {
		t.Fatalf("Err = %#v, want *InvalidToolInputError", tes[0].Err)
	}
	if tes[0].Error != target.Error() {
		t.Fatalf("Error = %#v, want %q", tes[0].Error, target.Error())
	}
}

// StreamText step content: local tool failures go through
// toolResultsToContentParts and keep the typed error.
func TestR73Gen_StreamTextToolErrorContentKeepsTypedErr(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "test-input",
		Tools:    []types.Tool{r73genFailingTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	steps := res.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	tes := r73genToolErrors(steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", steps[0].Content)
	}
	var target *r73genToolErr
	if !errors.As(tes[0].Err, &target) || target.code != 42 {
		t.Fatalf("Err = %#v, want the tool's *r73genToolErr", tes[0].Err)
	}
}

// toolResultContentFromToolResult: a provider-executed failure keeps a Go
// error, but a structured provider error value leaves Err nil.
func TestR73Gen_ProviderToolErrorErr(t *testing.T) {
	goErr := &r73genToolErr{code: 7}
	p := toolResultContentFromToolResult(types.ToolResult{ToolCallID: "c", ToolName: "web_search", Error: goErr, ProviderExecuted: true})
	te, ok := p.(types.ToolErrorContent)
	if !ok || te.Err != goErr || te.Error != "tool failed" {
		t.Fatalf("part = %#v, want Err = the provider's Go error", p)
	}
	structured := map[string]interface{}{"errorCode": "unavailable"}
	p = toolResultContentFromToolResult(types.ToolResult{ToolCallID: "c", ToolName: "web_search", Error: errors.New("x"), Result: structured, ProviderExecuted: true})
	te, ok = p.(types.ToolErrorContent)
	if !ok || te.Err != nil {
		t.Fatalf("part = %#v, want Err nil for a structured provider error", p)
	}
	// providerToolResultFromError carries a provider part's Err to the
	// tool result.
	tr := providerToolResultFromError(types.ToolErrorContent{ToolCallID: "c", Error: "tool failed", Err: goErr})
	if tr.Error != goErr {
		t.Fatalf("tool result error = %#v, want the part's Err", tr.Error)
	}
}

// TS StepResult.reasoning = content.filter(reasoning | reasoning-file): the
// reasoning file parts are StepResult.ReasoningFiles() in GenerateText and
// StreamText (carry-in item 2).
func TestR73Gen_StepReasoningFiles(t *testing.T) {
	f := types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1, 2}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, &types.GenerateResult{
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "think"},
				f,
				types.TextContent{Text: "done"},
			},
			Text: "done", FinishReason: types.FinishReasonStop,
		}),
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.FinalStep.ReasoningFiles()
	if len(got) != 1 || got[0].MediaType != "image/png" || string(got[0].Data) != "\x01\x02" {
		t.Fatalf("GenerateText ReasoningFiles = %#v", got)
	}
	if len(res.FinalStep.Reasoning) != 1 || res.FinalStep.Reasoning[0].Text != "think" {
		t.Fatalf("Reasoning = %#v", res.FinalStep.Reasoning)
	}

	sres, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				fc := f
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeReasoning, Reasoning: "think"},
					{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &fc},
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		},
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sres.ReadAll(); err != nil {
		t.Fatal(err)
	}
	got = sres.FinalStep().ReasoningFiles()
	if len(got) != 1 || got[0].MediaType != "image/png" {
		t.Fatalf("StreamText ReasoningFiles = %#v", got)
	}
	// Pointer parts count too; other parts do not.
	step := types.StepResult{Content: []types.ContentPart{&f, (*types.ReasoningFileContent)(nil), types.FileContent{MediaType: "image/png"}}}
	if got := step.ReasoningFiles(); len(got) != 1 {
		t.Fatalf("pointer ReasoningFiles = %#v", got)
	}
	if got := (types.StepResult{}).ReasoningFiles(); got != nil {
		t.Fatalf("empty ReasoningFiles = %#v, want nil", got)
	}
}
