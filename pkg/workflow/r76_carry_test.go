package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r76carryAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

// r76carryRoundTripModel is r75integToolRoundTripModel with a DoGenerate
// twin: a tool call to "t", then "done".
func r76carryRoundTripModel() *testutil.MockLanguageModel {
	m := r75integToolRoundTripModel()
	var mu sync.Mutex
	n := 0
	m.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}
	return m
}

// R76 item 3: TS OpenTelemetry (packages/otel open-telemetry.ts) under the
// ai.workflowAgent.stream root. onStart routes the root to onGenerateStart
// (one INTERNAL root named after mapOperationName, which has no entry for
// the workflow operation), onStepStart opens "step n" spans under the root,
// onLanguageModelCallStart opens "chat <model>" under the step with the
// root's state.settings (stop sequences included), and onToolExecutionStart
// opens "execute_tool <name>" under the open step span. The delegated
// StreamText opens no invoke_agent root of its own.
func TestR76Carry_WorkflowStreamGenAIOTelSpansUnderRoot(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) { r76carryGenAISpans(t, mode) })
	}
}

func r76carryGenAISpans(t *testing.T, mode string) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: r76carryRoundTripModel(),
		Tools: []types.Tool{r75integTool()},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("g")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "stream" {
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", StopSequences: []string{"STOP"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
	} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", StopSequences: []string{"STOP"}}); err != nil {
		t.Fatal(err)
	}
	ended := rec.Ended()
	byID := map[string]sdktrace.ReadOnlySpan{}
	var roots []sdktrace.ReadOnlySpan
	for _, s := range ended {
		byID[s.SpanContext().SpanID().String()] = s
		if !s.Parent().IsValid() {
			roots = append(roots, s)
		}
	}
	if len(roots) != 1 {
		names := []string{}
		for _, s := range roots {
			names = append(names, s.Name())
		}
		t.Fatalf("root spans = %v, want one workflow root", names)
	}
	root := roots[0]
	if root.Name() != workflowRootOperation+" mock-model" {
		t.Errorf("root span name = %q, want %q", root.Name(), workflowRootOperation+" mock-model")
	}
	if op, _ := r76carryAttr(root, "gen_ai.operation.name"); op != workflowRootOperation {
		t.Errorf("root gen_ai.operation.name = %q, want %q (TS mapOperationName identity)", op, workflowRootOperation)
	}
	parentName := func(s sdktrace.ReadOnlySpan) string {
		if p, ok := byID[s.Parent().SpanID().String()]; ok {
			return p.Name()
		}
		return ""
	}
	counts := map[string]int{}
	for _, s := range ended {
		switch s.Name() {
		case "step 1", "step 2":
			counts["step"]++
			if s.Parent().SpanID() != root.SpanContext().SpanID() {
				t.Errorf("%s parent = %q, want the workflow root", s.Name(), parentName(s))
			}
			if op, _ := r76carryAttr(s, "gen_ai.operation.name"); op != "agent_step" {
				t.Errorf("%s gen_ai.operation.name = %q, want agent_step", s.Name(), op)
			}
		case "chat mock-model":
			counts["chat"]++
			if pn := parentName(s); pn != "step 1" && pn != "step 2" {
				t.Errorf("chat span parent = %q, want a step span", pn)
			}
			if v, ok := r76carryAttr(s, "gen_ai.request.stop_sequences"); !ok || v != `["STOP"]` {
				t.Errorf("chat span stop_sequences = %q (%v), want [STOP] from the root's settings", v, ok)
			}
		case "execute_tool t":
			counts["tool"]++
			if pn := parentName(s); pn != "step 1" {
				t.Errorf("tool span parent = %q, want step 1", pn)
			}
		default:
			if s.SpanContext().SpanID() != root.SpanContext().SpanID() {
				t.Errorf("unexpected span %q", s.Name())
			}
		}
	}
	if counts["step"] != 2 || counts["chat"] != 2 || counts["tool"] != 1 {
		t.Errorf("span counts = %v, want 2 steps, 2 chats, 1 tool", counts)
	}
}

// TS model-call-iterator.test.ts "providerMetadata to providerOptions
// mapping": "should preserve providerMetadata for multiple parallel tool
// calls", "preserves OpenAI itemId in tool-call provider metadata",
// "preserves all OpenAI tool-call provider metadata fields" and "preserves
// mixed provider metadata on tool calls". Each client tool call in the next
// step's assistant message carries its own metadata as providerOptions,
// unchanged (toAssistantToolCallContent). Both modes.
func TestR76Carry_ToolCallProviderMetadataMapping(t *testing.T) {
	type call struct {
		id, tool string
		meta     map[string]interface{}
	}
	cases := []struct {
		name  string
		calls []call
	}{
		{"parallel", []call{
			{"call-1", "weatherTool", map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig_weather_123"}}},
			{"call-2", "newsTool", map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig_news_456"}}},
		}},
		{"openai itemId", []call{
			{"call-1", "testTool", map[string]interface{}{"openai": map[string]interface{}{"itemId": "fc_0402bf2d292dd7ed00697a35fb10e0819ab0098545c4d0d7f5"}}},
		}},
		{"all openai fields", []call{
			{"call-1", "testTool", map[string]interface{}{"openai": map[string]interface{}{"itemId": "fc_0402bf2d292dd7ed00697a35fb10e0819ab0098545c4d0d7f5", "someOtherField": "should-be-preserved"}}},
		}},
		{"mixed providers", []call{
			{"call-1", "testTool", map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig_gemini_preserved"}, "openai": map[string]interface{}{"itemId": "fc_should_be_preserved"}}},
		}},
	}
	tool := func(name string) types.Tool {
		return types.Tool{Name: name, Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"result": "success"}, nil
		}}
	}
	for _, tc := range cases {
		var toolCalls []types.ToolCall
		var tools []types.Tool
		seen := map[string]bool{}
		for _, c := range tc.calls {
			toolCalls = append(toolCalls, types.ToolCall{ID: c.id, ToolName: c.tool, Arguments: map[string]interface{}{"query": "test"}, ProviderMetadata: c.meta})
			if !seen[c.tool] {
				seen[c.tool] = true
				tools = append(tools, tool(c.tool))
			}
		}
		for _, mode := range []string{"stream", "generate"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				var prompt []types.Message
				if mode == "stream" {
					var chunks []provider.StreamChunk
					for i := range toolCalls {
						tcCopy := toolCalls[i]
						chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &tcCopy})
					}
					chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
					model, calls := r75carryScripted(chunks)
					agent, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: tools})
					if err != nil {
						t.Fatal(err)
					}
					_ = r75carryRunStream(t, agent)
					if len(*calls) != 2 {
						t.Fatalf("model calls = %d, want 2", len(*calls))
					}
					prompt = (*calls)[1].Prompt.Messages
				} else {
					var gcalls []*provider.GenerateOptions
					gm := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true, DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
						gcalls = append(gcalls, o)
						if len(gcalls) == 1 {
							return &types.GenerateResult{ToolCalls: toolCalls, FinishReason: types.FinishReasonToolCalls}, nil
						}
						return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
					}}
					agent, err := NewWorkflowAgent(WorkflowAgent{Model: gm, Tools: tools})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test"}); err != nil {
						t.Fatal(err)
					}
					if len(gcalls) != 2 {
						t.Fatalf("model calls = %d, want 2", len(gcalls))
					}
					prompt = gcalls[1].Prompt.Messages
				}
				got := map[string]map[string]interface{}{}
				for _, m := range prompt {
					if m.Role != types.RoleAssistant {
						continue
					}
					for _, p := range m.Content {
						if c, ok := p.(types.ToolCallContent); ok {
							got[c.ToolCallID] = c.ProviderOptions
						}
					}
				}
				if len(got) != len(tc.calls) {
					t.Fatalf("assistant tool calls = %v, want %d", got, len(tc.calls))
				}
				for _, c := range tc.calls {
					if !reflect.DeepEqual(got[c.id], c.meta) {
						t.Errorf("%s providerOptions = %v, want %v", c.id, got[c.id], c.meta)
					}
				}
			})
		}
	}
}

type r76carryTelemetryRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu           sync.Mutex
	stepContexts []map[string]interface{}
	callEndMeta  []map[string]interface{}
}

func (r *r76carryTelemetryRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	r.stepContexts = append(r.stepContexts, e.RuntimeContext)
	r.mu.Unlock()
	return ctx
}

func (r *r76carryTelemetryRecorder) OnLanguageModelCallEnd(_ context.Context, e telemetry.LanguageModelCallEndEvent) {
	r.mu.Lock()
	r.callEndMeta = append(r.callEndMeta, e.ProviderMetadata)
	r.mu.Unlock()
}

func r76carryRun(t *testing.T, a *WorkflowAgent, mode string) {
	t.Helper()
	if mode == "generate" {
		if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test"}); err != nil {
			t.Fatal(err)
		}
		return
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
}

// r76carryTextModel answers every call with "done" and the given finish
// provider metadata, recording each call's options.
func r76carryTextModel(meta map[string]interface{}) (*testutil.MockLanguageModel, *[]*provider.GenerateOptions) {
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	raw, _ := json.Marshal(meta)
	m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
	m.DoGenerateFunc = func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop, ProviderMetadata: meta}, nil
	}
	m.DoStreamFunc = func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
		finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}
		if meta != nil {
			finish.ProviderMetadata = raw
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, finish}), nil
	}
	return m, &calls
}

// TS model-call-iterator.test.ts "telemetry": "uses runtime context updated
// by prepareStep for model-step telemetry" (the step telemetry sees the
// PrepareStep runtime context, filtered by IncludeRuntimeContext) and "should
// expose provider metadata on model-call end". Both modes.
func TestR76Carry_StepTelemetryRuntimeContextAndModelCallEndMetadata(t *testing.T) {
	meta := map[string]interface{}{"gateway": map[string]interface{}{"generationId": "generation-id"}}
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) {
			model, _ := r76carryTextModel(meta)
			rec := &r76carryTelemetryRecorder{}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:          model,
				RuntimeContext: map[string]interface{}{"requestId": "initial"},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.RuntimeContext = map[string]interface{}{"requestId": "updated", "secret": "private"}
					return o, nil
				},
				Telemetry: &telemetry.Options{
					IsEnabled:             telemetry.Bool(true),
					IncludeRuntimeContext: map[string]bool{"requestId": true},
					Integrations:          []telemetry.TelemetryIntegration{rec},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			r76carryRun(t, a, mode)
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.stepContexts) != 1 || !reflect.DeepEqual(rec.stepContexts[0], map[string]interface{}{"requestId": "updated"}) {
				t.Errorf("step start runtime contexts = %v, want [{requestId: updated}]", rec.stepContexts)
			}
			if len(rec.callEndMeta) != 1 || !reflect.DeepEqual(rec.callEndMeta[0], meta) {
				t.Errorf("model-call end provider metadata = %v, want [%v]", rec.callEndMeta, meta)
			}
		})
	}
}

// TS model-call-iterator.test.ts "activeTools > passes no tools to the model
// when prepareStep returns an empty list". Both modes.
func TestR76Carry_PrepareStepEmptyActiveToolsSendsNoTools(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) {
			model, calls := r76carryTextModel(nil)
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: model,
				Tools: []types.Tool{{Name: "hidden", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.ActiveTools = []string{}
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			r76carryRun(t, a, mode)
			if len(*calls) != 1 {
				t.Fatalf("model calls = %d, want 1", len(*calls))
			}
			if got := (*calls)[0].Tools; len(got) != 0 {
				t.Errorf("model tools = %v, want none", got)
			}
		})
	}
}

// An explicitly empty ActiveTools (agent, call, PrepareCall) sends no tools
// in both modes, as TS filterActiveTools with `activeTools: []`.
func TestR76Carry_EmptyActiveToolsEverywhereSendsNoTools(t *testing.T) {
	hidden := []types.Tool{{Name: "hidden", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}}
	type setup struct {
		name  string
		agent func(m provider.LanguageModel) WorkflowAgent
		call  []string
	}
	setups := []setup{
		{"agent", func(m provider.LanguageModel) WorkflowAgent {
			return WorkflowAgent{Model: m, Tools: hidden, ActiveTools: []string{}}
		}, nil},
		{"call", func(m provider.LanguageModel) WorkflowAgent { return WorkflowAgent{Model: m, Tools: hidden} }, []string{}},
		{"prepareCall", func(m provider.LanguageModel) WorkflowAgent {
			return WorkflowAgent{Model: m, Tools: hidden, PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.ActiveTools = []string{}
				return o, nil
			}}
		}, nil},
	}
	for _, s := range setups {
		for _, mode := range []string{"stream", "generate"} {
			t.Run(s.name+"/"+mode, func(t *testing.T) {
				model, calls := r76carryTextModel(nil)
				a, err := NewWorkflowAgent(s.agent(model))
				if err != nil {
					t.Fatal(err)
				}
				if mode == "generate" {
					_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test", ActiveTools: s.call})
				} else {
					var st *WorkflowStreamResult
					if st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test", ActiveTools: s.call}); err == nil {
						_, err = st.ReadAll()
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(*calls) != 1 || len((*calls)[0].Tools) != 0 {
					t.Errorf("model calls = %d, tools = %v, want 1 call without tools", len(*calls), (*calls)[0].Tools)
				}
			})
		}
	}
}

func r76carryF(v float64) *float64 { return &v }

// TS model-call-iterator.test.ts "generation settings > merges defined
// prepareStep overrides": a setting the hook leaves undefined keeps the
// current value (mergePrepareStepGenerationSettings copies defined keys
// only). Go's nil is TS undefined. Both modes.
func TestR76Carry_PrepareStepMergesDefinedSettingsOnly(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) {
			model, calls := r76carryTextModel(nil)
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Temperature: r76carryF(0.2), TopP: r76carryF(0.5),
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.Temperature = nil
					o.TopP = r76carryF(0.9)
					n := 256
					o.MaxTokens = &n
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			r76carryRun(t, a, mode)
			c := (*calls)[0]
			if c.Temperature == nil || *c.Temperature != 0.2 || c.TopP == nil || *c.TopP != 0.9 || c.MaxTokens == nil || *c.MaxTokens != 256 {
				t.Errorf("model settings temperature=%v topP=%v maxTokens=%v, want 0.2, 0.9, 256", c.Temperature, c.TopP, c.MaxTokens)
			}
		})
	}
}

// TS model-call-iterator.ts prepareStep: every key the hook leaves undefined
// keeps the current value (mergePrepareStepGenerationSettings for the
// generation settings; `!== undefined` checks for runtimeContext,
// toolsContext, activeTools and toolChoice). A Go hook that clears a field
// to nil leaves the model call's value as it was. Both modes.
func TestR76Carry_PrepareStepNilFieldsKeepCurrent(t *testing.T) {
	clear := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
		o.Temperature, o.TopP, o.TopK, o.MaxTokens = nil, nil, nil, nil
		o.PresencePenalty, o.FrequencyPenalty, o.StopSequences, o.Seed = nil, nil, nil, nil
		o.Headers, o.Reasoning, o.ProviderOptions = nil, nil, nil
		o.RuntimeContext, o.ActiveTools, o.Messages = nil, nil, nil
		return o, nil
	}
	for _, hookKind := range []string{"prepareStep", "prepareCall"} {
		for _, mode := range []string{"stream", "generate"} {
			t.Run(hookKind+"/"+mode, func(t *testing.T) {
				model, calls := r76carryTextModel(nil)
				topK, maxTokens, seed := 4, 100, 7
				reasoning := types.ReasoningLevel("high")
				cfg := WorkflowAgent{
					Model: model,
					Tools: []types.Tool{
						{Name: "a", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
						{Name: "b", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
					},
					ActiveTools: []string{"a"},
					Temperature: r76carryF(0.2), TopP: r76carryF(0.5), TopK: &topK, MaxTokens: &maxTokens,
					PresencePenalty: r76carryF(0.1), FrequencyPenalty: r76carryF(0.3), StopSequences: []string{"STOP"}, Seed: &seed,
					Headers: map[string]string{"x-test": "1"}, Reasoning: &reasoning,
					ProviderOptions: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}},
					RuntimeContext:  map[string]interface{}{"requestId": "r1"},
				}
				if hookKind == "prepareStep" {
					cfg.PrepareStep = clear
				} else {
					cfg.PrepareCall = clear
				}
				a, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				r76carryRun(t, a, mode)
				c := (*calls)[0]
				var problems []string
				check := func(name string, ok bool) {
					if !ok {
						problems = append(problems, name)
					}
				}
				check("temperature", c.Temperature != nil && *c.Temperature == 0.2)
				check("topP", c.TopP != nil && *c.TopP == 0.5)
				check("topK", c.TopK != nil && *c.TopK == 4)
				check("maxTokens", c.MaxTokens != nil && *c.MaxTokens == 100)
				check("presencePenalty", c.PresencePenalty != nil && *c.PresencePenalty == 0.1)
				check("frequencyPenalty", c.FrequencyPenalty != nil && *c.FrequencyPenalty == 0.3)
				check("stopSequences", reflect.DeepEqual(c.StopSequences, []string{"STOP"}))
				check("seed", c.Seed != nil && *c.Seed == 7)
				check("headers", c.Headers["x-test"] == "1")
				check("reasoning", c.Reasoning != nil && *c.Reasoning == reasoning)
				check("providerOptions", reflect.DeepEqual(c.ProviderOptions, map[string]interface{}{"mock": map[string]interface{}{"k": "v"}}))
				check("runtimeContext", reflect.DeepEqual(c.RuntimeContext, map[string]interface{}{"requestId": "r1"}))
				check("activeTools", len(c.Tools) == 1 && c.Tools[0].Name == "a")
				check("messages", len(c.Prompt.Messages) == 1)
				if len(problems) > 0 {
					t.Errorf("a nil %s field dropped the current value of: %v", hookKind, problems)
				}
			})
		}
	}
}

// TS model-call-iterator.test.ts "conversation prompt > passes initial
// instructions and messages to prepareStep" and "runtimeContext and
// toolsContext > applies current contexts to the reconstructed step". Both
// modes.
func TestR76Carry_PrepareStepInitialInputsAndStepContexts(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) {
			model, _ := r76carryTextModel(nil)
			var seen []LanguageModelCallOptions
			runtimeContext := map[string]interface{}{"tenantId": "tenant_123"}
			toolsContext := map[string]interface{}{"weather": map[string]interface{}{"unit": "celsius"}}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Instructions: "initial instructions",
				RuntimeContext: runtimeContext, ToolsContext: toolsContext,
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					seen = append(seen, o)
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "initial message"}}}}
			var steps []types.StepResult
			if mode == "generate" {
				res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: msgs})
				if err != nil {
					t.Fatal(err)
				}
				steps = res.Steps
			} else {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
				if err != nil {
					t.Fatal(err)
				}
				steps = st.Steps()
			}
			if len(seen) != 1 || seen[0].InitialInstructions != "initial instructions" || len(seen[0].InitialMessages) != 1 {
				t.Fatalf("PrepareStep inputs = %+v, want initial instructions and the one initial message", seen)
			}
			if tc, ok := seen[0].InitialMessages[0].Content[0].(types.TextContent); !ok || tc.Text != "initial message" {
				t.Errorf("InitialMessages = %+v", seen[0].InitialMessages)
			}
			if len(steps) != 1 || steps[0].StepNumber != 0 || !reflect.DeepEqual(steps[0].RuntimeContext, runtimeContext) || !reflect.DeepEqual(steps[0].ToolsContext, toolsContext) {
				t.Errorf("step = %+v, want step 0 with the runtime and tools contexts", steps)
			}
		})
	}
}

// r76carryToolCallModel streams one call to testTool with input, then "done"
// (TS workflow-agent-compat.test.ts createToolCallStreamMockModel).
func r76carryToolCallModel(input map[string]interface{}) (*testutil.MockLanguageModel, *[]*provider.GenerateOptions) {
	return r75carryScripted([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: input}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
}

func r76carryValueTool(dynamic bool, fail bool) types.Tool {
	toolType := ""
	if dynamic {
		toolType = types.ToolTypeDynamic
	}
	return types.Tool{
		Name:       "testTool",
		Type:       toolType,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			if fail {
				return nil, errors.New("tool failed")
			}
			return fmt.Sprintf("%v-result", in["value"]), nil
		},
	}
}

func r76carryResultIDs(rs []types.ToolResult) string {
	var out []string
	for _, r := range rs {
		out = append(out, fmt.Sprintf("%s:%s:%v:dyn=%v", r.ToolCallID, r.ToolName, r.Result, r.Dynamic))
	}
	return strings.Join(out, ",")
}

// TS workflow-agent-compat.test.ts "completed step tool results": "exposes
// static tool results to step consumers" (PrepareStep's previous steps, the
// step end callback and the result), "makes tool outputs available to stop
// conditions", "classifies dynamic tool results on the completed step" and
// "exposes local tool failures as errors without successful results".
func TestR76Carry_CompletedStepToolResults(t *testing.T) {
	in := map[string]interface{}{"value": "test"}
	t.Run("static", func(t *testing.T) {
		model, _ := r76carryToolCallModel(in)
		var prepared, ended []string
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model, Tools: []types.Tool{r76carryValueTool(false, false)},
			PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				if o.StepNumber > 0 {
					prepared = append(prepared, r76carryResultIDs(o.PreviousSteps[0].StaticToolResults))
				}
				return o, nil
			},
			OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
				ended = append(ended, r76carryResultIDs(e.StaticToolResults))
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		steps := r75carryRunStream(t, a)
		want := "call-1:testTool:test-result:dyn=false"
		if strings.Join(prepared, "|") != want || len(ended) == 0 || ended[0] != want {
			t.Errorf("prepareStep = %v, onStepEnd = %v, want %s", prepared, ended, want)
		}
		if r76carryResultIDs(steps[0].ToolResults) != want || r76carryResultIDs(steps[0].StaticToolResults) != want || len(steps[0].DynamicToolResults) != 0 {
			t.Errorf("step results = %s / %s / %s", r76carryResultIDs(steps[0].ToolResults), r76carryResultIDs(steps[0].StaticToolResults), r76carryResultIDs(steps[0].DynamicToolResults))
		}
		if !reflect.DeepEqual(steps[0].ToolResults[0].Input, in) {
			t.Errorf("tool result input = %v, want %v", steps[0].ToolResults[0].Input, in)
		}
	})
	t.Run("stop condition", func(t *testing.T) {
		model, calls := r76carryToolCallModel(in)
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model, Tools: []types.Tool{r76carryValueTool(false, false)},
			StopWhen: []ai.StopCondition{func(s ai.StopConditionState) string {
				last := s.Steps[len(s.Steps)-1]
				for _, r := range last.ToolResults {
					if r.Result == "test-result" {
						return "found"
					}
				}
				return ""
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		steps := r75carryRunStream(t, a)
		if len(steps) != 1 || len(*calls) != 1 || steps[0].ToolResults[0].Result != "test-result" {
			t.Errorf("steps = %d, calls = %d, want 1 step with test-result", len(steps), len(*calls))
		}
	})
	t.Run("dynamic", func(t *testing.T) {
		model, _ := r76carryToolCallModel(in)
		a, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{r76carryValueTool(true, false)}})
		if err != nil {
			t.Fatal(err)
		}
		steps := r75carryRunStream(t, a)
		if len(steps[0].StaticToolResults) != 0 || r76carryResultIDs(steps[0].DynamicToolResults) != "call-1:testTool:test-result:dyn=true" {
			t.Errorf("static = %s, dynamic = %s", r76carryResultIDs(steps[0].StaticToolResults), r76carryResultIDs(steps[0].DynamicToolResults))
		}
	})
	t.Run("local failure", func(t *testing.T) {
		model, _ := r76carryToolCallModel(in)
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model, Tools: []types.Tool{r76carryValueTool(false, true)},
			StopWhen: []ai.StopCondition{func(s ai.StopConditionState) string {
				for _, p := range s.Steps[len(s.Steps)-1].Content {
					if p.ContentType() == "tool-error" {
						return "error"
					}
				}
				return ""
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		steps := r75carryRunStream(t, a)
		if len(steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(steps))
		}
		var te *types.ToolErrorContent
		for _, p := range steps[0].Content {
			if c, ok := p.(types.ToolErrorContent); ok {
				te = &c
			}
		}
		if te == nil || te.ToolCallID != "call-1" || te.ToolName != "testTool" || !reflect.DeepEqual(te.Input, in) || te.ErrorText != "Error: tool failed" {
			t.Errorf("tool error = %+v, want call-1 testTool with Error: tool failed", te)
		}
		if len(steps[0].ToolResults) != 0 || len(steps[0].StaticToolResults) != 0 || len(steps[0].DynamicToolResults) != 0 {
			t.Errorf("results = %d/%d/%d, want none", len(steps[0].ToolResults), len(steps[0].StaticToolResults), len(steps[0].DynamicToolResults))
		}
	})
}

// TS workflow-agent-compat.test.ts "completed step tool results > discovers
// and executes deferred tools": a toolSearch call loads the deferred tool for
// the next steps, and both steps expose their tool results.
func TestR76Carry_DiscoversAndExecutesDeferredTools(t *testing.T) {
	model, calls := r75carryScripted(
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "search-call", ToolName: ai.ToolSearchDefaultName, Arguments: map[string]interface{}{"query": "weather forecast"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		},
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "weather-call", ToolName: "weather", Arguments: map[string]interface{}{"city": "London"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		},
	)
	a, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{
		ai.ToolSearch(),
		{
			Name: "weather", DeferLoading: true, Description: "Get the weather forecast for a city.",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
			Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"city": in["city"], "forecast": "rain"}, nil
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	steps := r75carryRunStream(t, a)
	var toolNames []string
	for _, c := range *calls {
		var names []string
		for _, tl := range c.Tools {
			names = append(names, tl.Name)
		}
		toolNames = append(toolNames, strings.Join(names, ","))
	}
	if want := []string{"toolSearch", "toolSearch,weather", "toolSearch,weather"}; !reflect.DeepEqual(toolNames, want) {
		t.Errorf("model tools per call = %v, want %v", toolNames, want)
	}
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	same := func(v interface{}, want string) bool {
		b, _ := json.Marshal(v)
		var g, w interface{}
		_ = json.Unmarshal(b, &g)
		_ = json.Unmarshal([]byte(want), &w)
		return reflect.DeepEqual(g, w)
	}
	if r := steps[0].ToolResults[0]; r.ToolCallID != "search-call" || !same(r.Result, `{"tools":[{"name":"weather","description":"Get the weather forecast for a city."}]}`) {
		t.Errorf("search result = %s %v", r.ToolCallID, r.Result)
	}
	if r := steps[1].ToolResults[0]; r.ToolCallID != "weather-call" || !same(r.Result, `{"city":"London","forecast":"rain"}`) {
		t.Errorf("weather result = %s %v", r.ToolCallID, r.Result)
	}
}
