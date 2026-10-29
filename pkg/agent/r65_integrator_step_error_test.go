package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r65RunStepErrors runs the legacy loop and returns the OnStepError events
// it reported and the run's error.
func r65RunStepErrors(t *testing.T, cfg AgentConfig) ([]ai.OnStepErrorEvent, error) {
	t.Helper()
	var mu sync.Mutex
	var events []ai.OnStepErrorEvent
	cfg.OnStepError = func(_ context.Context, e ai.OnStepErrorEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	_, err := NewToolLoopAgent(cfg).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	mu.Lock()
	defer mu.Unlock()
	return append([]ai.OnStepErrorEvent(nil), events...), err
}

// TS generateText notifies onStepError with wrapGatewayError(error): an
// unauthenticated gateway error reaches the callback as the wrapped error
// that says how to authenticate (the original stays reachable).
func TestR65IntegratorLegacyStepErrorWrapsGatewayError(t *testing.T) {
	raw := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	zero := 0
	events, _ := r65RunStepErrors(t, AgentConfig{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, raw
		}},
		MaxRetries: &zero,
	})
	if len(events) != 1 {
		t.Fatalf("step errors = %d, want 1", len(events))
	}
	var auth *gatewayerrors.GatewayAuthenticationError
	if !errors.As(events[0].Error, &auth) || !strings.Contains(events[0].Error.Error(), "AI_GATEWAY_API_KEY") {
		t.Fatalf("step error = %v, want the wrapped gateway error", events[0].Error)
	}
}

// Failures after the model call of a started step end it with OnStepError:
// a tool input callback error, an invalid PrepareCall tool-call concurrency,
// an invalid tool context and a ToModelOutput error (reported as the conversion error itself).
func TestR65IntegratorLegacyStepErrorAfterModelCall(t *testing.T) {
	call := types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}
	inputErr := errors.New("input failed")
	convertErr := errors.New("convert failed")
	baseTool := func() types.Tool {
		return types.Tool{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}
	}

	inputTool := baseTool()
	inputTool.OnInputAvailable = func(context.Context, types.OnInputAvailableOptions) error { return inputErr }
	convertTool := baseTool()
	convertTool.ToModelOutput = func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
		return nil, convertErr
	}
	ctxTool := baseTool()
	ctxTool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"tenant"}})
	zero := 0

	cases := []struct {
		name string
		cfg  AgentConfig
		want func(error) bool
		step int
	}{
		{"input callback", AgentConfig{Tools: []types.Tool{inputTool}}, func(err error) bool { return errors.Is(err, inputErr) }, 0},
		{"prepare call concurrency", AgentConfig{
			Tools: []types.Tool{baseTool()},
			// Step 0's prepared value is validated before the run starts; a
			// later step's override is checked when its tools run.
			PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
				if c.StepNumber == 1 {
					c.ToolCallConcurrency = &zero
				}
				return c
			},
		}, func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "toolCallConcurrency must be >= 1")
		}, 1},
		{"tool context", AgentConfig{Tools: []types.Tool{ctxTool}, ToolsContext: map[string]interface{}{"t": map[string]interface{}{}}}, func(err error) bool { return err != nil }, 0},
		{"to model output", AgentConfig{Tools: []types.Tool{convertTool}}, func(err error) bool { return err == convertErr }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Model = &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
			}}
			tc.cfg.MaxSteps = 3
			events, err := r65RunStepErrors(t, tc.cfg)
			if err == nil {
				t.Fatal("run succeeded, want a failure")
			}
			if len(events) != 1 || !tc.want(events[0].Error) || events[0].StepNumber != tc.step {
				t.Fatalf("err %v; step error events = %+v, want one for step %d with the failure", err, events, tc.step)
			}
		})
	}
}

// TS generateText (which ToolLoopAgent runs) throws wrapGatewayError(error):
// the legacy loop returns the wrapped gateway error too, with the original
// still reachable through errors.As.
func TestR66LegacyLoopReturnsWrappedGatewayError(t *testing.T) {
	raw := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	zero := 0
	cfg := AgentConfig{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, raw
		}},
		MaxRetries: &zero,
	}
	_, genErr := NewToolLoopAgent(cfg).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	_, execErr := NewToolLoopAgent(cfg).Execute(context.Background(), "go")
	for name, err := range map[string]error{"GenerateAgent": genErr, "Execute": execErr} {
		var auth *gatewayerrors.GatewayAuthenticationError
		if !errors.As(err, &auth) || !strings.Contains(err.Error(), "AI_GATEWAY_API_KEY") {
			t.Fatalf("%s error = %v, want the wrapped gateway error", name, err)
		}
	}
}

// Instructions given as system messages replace the text instructions in the
// legacy loop's events, as GenerateText reports them (TS instructions is a
// single value): Instructions is empty next to InstructionMessages.
func TestR66LegacyLoopInstructionMessagesReplaceSystemInEvents(t *testing.T) {
	var mu sync.Mutex
	var got []string
	record := func(label, instructions string, n int) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, label+"="+instructions)
		if n != 1 {
			got = append(got, label+" messages missing")
		}
	}
	cfg := AgentConfig{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		}},
		System:              "text",
		InstructionMessages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "msg"}}}},
		OnLanguageModelCallStart: func(_ context.Context, e ai.LanguageModelCallStartEvent) {
			record("lm", e.Instructions, len(e.InstructionMessages))
		},
	}
	_, err := NewToolLoopAgent(cfg).GenerateAgent(context.Background(), AgentGenerateOptions{
		Prompt: "go",
		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			record("start", e.Instructions, len(e.InstructionMessages))
		},
		OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) {
			record("step", e.Instructions, len(e.InstructionMessages))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "start=,step=,lm="; strings.Join(got, ",") != want {
		t.Fatalf("events = %q, want %q", strings.Join(got, ","), want)
	}
}

// TS generateText checks a response's deferred provider result against the
// call's tools: a SupportsDeferredResults provider tool left out of the
// step's ActiveTools still accepts its result (no "Tool call not found").
func TestR66LegacyLoopDeferredResultCheckedAgainstCallTools(t *testing.T) {
	cfg := AgentConfig{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
				Content: []types.ContentPart{
					types.TextContent{Text: "done"},
					types.ToolResultContent{ToolCallID: "earlier", ToolName: "p", Result: "r"},
				},
			}, nil
		}},
		Tools: []types.Tool{
			{Name: "c", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}},
			{Name: "p", ProviderExecuted: true, SupportsDeferredResults: true, Metadata: map[string]interface{}{"k": "v"}},
		},
		ActiveTools: []string{"c"},
	}
	res, err := NewToolLoopAgent(cfg).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	found := false
	for _, part := range res.Steps[0].Content {
		if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == "earlier" {
			found = true
			if tr.ToolMetadata["k"] != "v" {
				t.Fatalf("deferred result metadata = %v, want the tool's metadata", tr.ToolMetadata)
			}
		}
	}
	if !found {
		t.Fatalf("content = %+v, want the deferred result", res.Steps[0].Content)
	}
}
