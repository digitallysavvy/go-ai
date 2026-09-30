package ai

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Regression tests for the tool-caller message persistence bug: TS
// generate-text.ts / stream-text.ts build each step's `messagesForNextStep`
// from that step's own `stepMessages` (the messages *after*
// appendToolCallerMessages ran), so a local tool caller's
// PrepareModelMessage announcement persists into later steps' history
// instead of being dropped after one step. Ports the shape of TS's
// `expect(second.prompt.slice(0, first.prompt.length)).toEqual(first.prompt)`
// assertion from code-mode/src/tool-search.test.ts (also ported directly in
// pkg/codemode/tool_search_integration_test.go).

func TestGenerateText_ToolCallers_AnnouncementPersistsAcrossSteps(t *testing.T) {
	t.Parallel()
	var prompts [][]types.Message
	callNum := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			prompts = append(prompts, opts.Prompt.Messages)
			callNum++
			return toolCallResponse(callID(callNum), "code_mode", nil), nil
		},
	}

	announcement := "Available caller tools: getInventory."
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(2)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", func([]types.Tool) *string { return &announcement }),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": "sku-1", "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("model called %d times, want 2", len(prompts))
	}

	first, second := prompts[0], prompts[1]
	assertAnnouncementMessage(t, first, announcement)

	// The second step's prompt must be an exact prefix-extension of the
	// first step's (mirrors TS generate-text.test.ts / the ported
	// pkg/codemode assertion): the announcement appended on step 1 stays in
	// its original position rather than being dropped from history and
	// re-appended at the tail of step 2's recomputed prompt.
	if len(second) < len(first) {
		t.Fatalf("second-step prompt has %d messages, want at least %d (first-step length)", len(second), len(first))
	}
	if !reflect.DeepEqual(second[:len(first)], first) {
		t.Fatalf("second-step prompt prefix = %+v, want %+v (first-step prompt)", second[:len(first)], first)
	}
}

func TestStreamText_ToolCallers_AnnouncementPersistsAcrossSteps(t *testing.T) {
	t.Parallel()
	// The announcement text changes on every step (like code-mode's
	// "conversation" discovery catalog growing as tools are discovered), so
	// AppendToolCallerMessages's same-text dedup never masks the bug: a
	// stable, unchanging announcement would be deduped against itself on
	// every step regardless of whether it was actually persisted, so it
	// would not distinguish correct persistence from a step that just
	// recomputes it fresh each time. The bug only surfaces at step 3: step
	// 2's own prompt is always correct (it is built from the freshly
	// prepared nextMessages, sent directly to the model), but persisting
	// that value into currentMessages for step 3 is exactly what the fix
	// under test restores.
	var mu sync.Mutex
	callNum := 0
	announcementFor := func(n int) string {
		return fmt.Sprintf("Available caller tools (v%d): getInventory.", n)
	}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			callNum++
			n := callNum
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: callID(n), ToolName: "code_mode", Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	announceCalls := 0
	done := make(chan struct{})
	maxSteps := 3
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "Check inventory.",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{
			localCallerTool("code_mode", func([]types.Tool) *string {
				mu.Lock()
				announceCalls++
				n := announceCalls
				mu.Unlock()
				text := announcementFor(n)
				return &text
			}),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": "sku-1", "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
		OnFinish: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(model.StreamCalls) != 3 {
		t.Fatalf("model called %d times, want 3", len(model.StreamCalls))
	}
	step1 := model.StreamCalls[0].Prompt.Messages
	step2 := model.StreamCalls[1].Prompt.Messages
	step3 := model.StreamCalls[2].Prompt.Messages
	assertAnnouncementMessage(t, step1, announcementFor(1))
	assertAnnouncementMessage(t, step2, announcementFor(2))

	// Each step's prompt must be an exact prefix-extension of the previous
	// step's (mirrors TS generate-text.test.ts / the ported pkg/codemode
	// assertion). In particular step 3 must still contain step 2's own
	// announcement (v2): without the fix, currentMessages carries forward
	// the pre-announcement value, so step 2's v2 announcement — although
	// correctly sent to the model for step 2 itself — is dropped from the
	// history used to build step 3's prompt.
	if len(step2) < len(step1) {
		t.Fatalf("step2 prompt has %d messages, want at least %d (step1 length)", len(step2), len(step1))
	}
	if !reflect.DeepEqual(step2[:len(step1)], step1) {
		t.Fatalf("step2 prompt prefix = %+v, want %+v (step1 prompt)", step2[:len(step1)], step1)
	}
	if len(step3) < len(step2) {
		t.Fatalf("step3 prompt has %d messages, want at least %d (step2 length)", len(step3), len(step2))
	}
	if !reflect.DeepEqual(step3[:len(step2)], step2) {
		t.Fatalf("step3 prompt prefix = %+v, want %+v (step2 prompt)", step3[:len(step2)], step2)
	}
}

func assertAnnouncementMessage(t *testing.T, messages []types.Message, announcement string) {
	t.Helper()
	for _, m := range messages {
		if m.Role != types.RoleUser || len(m.Content) != 1 {
			continue
		}
		if tc, ok := m.Content[0].(types.TextContent); ok && tc.Text == announcement {
			return
		}
	}
	t.Fatalf("expected announcement message %q in %+v", announcement, messages)
}

func callID(n int) string {
	return fmt.Sprintf("call-%d", n)
}

// TestGenerateText_ToolCallers_PrepareStepInitialMessagesStaysRaw is a
// regression test for a second bug in the same area: TS generate-text.ts
// keeps `initialMessages = initialPrompt.messages` fixed for the whole call
// and passes that raw, un-announced value to every prepareStep invocation's
// `initialMessages` field (distinct from `messages`, which is that step's
// own announcement-inclusive `stepMessages`). GenerateText must do the same:
// PrepareStepOptions.InitialMessages must never pick up a tool caller's
// PrepareModelMessage announcement from an earlier step, even though that
// announcement correctly persists into Messages/history.
func TestGenerateText_ToolCallers_PrepareStepInitialMessagesStaysRaw(t *testing.T) {
	t.Parallel()
	callNum := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callNum++
			return toolCallResponse(callID(callNum), "code_mode", nil), nil
		},
	}

	announcement := "Available caller tools: getInventory."
	var initialMessagesByStep [][]types.Message
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(2)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			localCallerTool("code_mode", func([]types.Tool) *string { return &announcement }),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": "sku-1", "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			initialMessagesByStep = append(initialMessagesByStep, step.InitialMessages)
			return step
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(initialMessagesByStep) != 2 {
		t.Fatalf("PrepareStep called %d times, want 2", len(initialMessagesByStep))
	}

	want := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Check inventory."}}}}
	for i, got := range initialMessagesByStep {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d InitialMessages = %+v, want %+v (raw prompt, no announcement)", i, got, want)
		}
	}
}

// TestStreamText_ToolCallers_PrepareStepInitialMessagesStaysRaw is the
// StreamText analogue of TestGenerateText_ToolCallers_PrepareStepInitialMessagesStaysRaw.
func TestStreamText_ToolCallers_PrepareStepInitialMessagesStaysRaw(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	callNum := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			callNum++
			n := callNum
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: callID(n), ToolName: "code_mode", Arguments: map[string]interface{}{},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	announcement := "Available caller tools: getInventory."
	var initialMessagesByStep [][]types.Message
	done := make(chan struct{})
	maxSteps := 3
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "Check inventory.",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{
			localCallerTool("code_mode", func([]types.Tool) *string { return &announcement }),
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"sku": "sku-1", "availableUnits": 42}, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{
			"getInventory": {"code_mode"},
		},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			initialMessagesByStep = append(initialMessagesByStep, step.InitialMessages)
			mu.Unlock()
			return step
		},
		OnFinish: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(initialMessagesByStep) != 3 {
		t.Fatalf("PrepareStep called %d times, want 3", len(initialMessagesByStep))
	}

	want := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Check inventory."}}}}
	for i, got := range initialMessagesByStep {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d InitialMessages = %+v, want %+v (raw prompt, no announcement) — step 3 picking up an earlier step's announcement means r.cbMessages (step 0's post-announcement messages) leaked into InitialMessages instead of a separate raw snapshot", i, got, want)
		}
	}
}
