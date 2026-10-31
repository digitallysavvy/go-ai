package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop's start and step-start events carry the same settings as
// TS generateText's: tool order, timeout, reasoning, headers (with the agent
// and ai/<version> user-agent segments), output, provider options and steps.
func TestR64IntegratorLegacyStartEventsCarryEverySetting(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls == 1 {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			}, nil
		}
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	var mu sync.Mutex
	var start ai.OnStartEvent
	var stepStarts []ai.OnStepStartEvent
	timeout := &ai.TimeoutConfig{}
	output := ai.TextOutput()
	reasoning := types.ReasoningLevel("high")
	providerOptions := map[string]interface{}{"p": map[string]interface{}{"k": "v"}}
	_, err := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			},
		}},
		ToolOrder:       []string{"t"},
		Timeout:         timeout,
		Reasoning:       &reasoning,
		Headers:         map[string]string{"x-h": "1"},
		Output:          output,
		ProviderOptions: providerOptions,
		StopWhen:        []ai.StopCondition{ai.IsStepCount(2)},
		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			mu.Lock()
			start = e
			mu.Unlock()
		},
		OnStepStartEvent: func(_ context.Context, e ai.OnStepStartEvent) {
			mu.Lock()
			stepStarts = append(stepStarts, e)
			mu.Unlock()
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(start.ToolOrder) != 1 || start.ToolOrder[0] != "t" {
		t.Fatalf("start ToolOrder = %v, want [t]", start.ToolOrder)
	}
	if start.Timeout != timeout {
		t.Fatalf("start Timeout = %v, want the configured timeout", start.Timeout)
	}
	if start.Reasoning == nil || *start.Reasoning != reasoning {
		t.Fatalf("start Reasoning = %v, want high", start.Reasoning)
	}
	if start.Output == nil {
		t.Fatal("start Output is nil, want the configured output")
	}
	if start.Headers["x-h"] != "1" {
		t.Fatalf("start Headers = %v, want x-h", start.Headers)
	}
	ua := ""
	for k, v := range start.Headers {
		if strings.EqualFold(k, "user-agent") {
			ua = v
		}
	}
	if !strings.Contains(ua, "ai-sdk-agent/tool-loop") || !strings.Contains(ua, "ai/") {
		t.Fatalf("start user-agent = %q, want the agent and ai/<version> segments (TS headersWithUserAgent)", ua)
	}
	if len(stepStarts) != 2 {
		t.Fatalf("step starts = %d, want 2", len(stepStarts))
	}
	s := stepStarts[1]
	if len(s.ToolOrder) != 1 || s.ToolOrder[0] != "t" {
		t.Fatalf("step ToolOrder = %v, want [t]", s.ToolOrder)
	}
	if _, ok := s.ProviderOptions["p"]; !ok {
		t.Fatalf("step ProviderOptions = %v, want the configured options", s.ProviderOptions)
	}
	if len(s.Steps) != 1 {
		t.Fatalf("second step start Steps = %d, want 1", len(s.Steps))
	}
}

// TS stepTimeMs starts just before onLanguageModelCallStart, after prompt
// conversion, so a slow download before the model call is not step time.
func TestR64IntegratorLegacyStepTimeExcludesPromptDownload(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	res, err := NewToolLoopAgent(AgentConfig{
		Model: model,
		ExperimentalDownload: func(context.Context, []ai.DownloadRequest) ([]*ai.DownloadResult, error) {
			time.Sleep(150 * time.Millisecond)
			return []*ai.DownloadResult{{Data: []byte("x")}}, nil
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.test/a.txt", MediaType: "text/plain"},
			MediaType: "text/plain",
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Steps[0].Performance; p.StepTimeMs >= 150 {
		t.Fatalf("StepTimeMs = %d, want it to leave out the 150ms prompt download", p.StepTimeMs)
	}
}
