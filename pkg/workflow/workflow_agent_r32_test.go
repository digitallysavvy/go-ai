package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func runWorkflow(t *testing.T, w *WorkflowAgent, stream bool, gen WorkflowGenerateOptions, str WorkflowStreamOptions) error {
	t.Helper()
	if stream {
		res, err := w.StreamWithOptions(context.Background(), str)
		if err != nil {
			return err
		}
		return res.ConsumeStream()
	}
	_, err := w.GenerateWithOptions(context.Background(), gen)
	return err
}

func systemMessagesOf(o *provider.GenerateOptions) []types.Message {
	var out []types.Message
	for _, m := range o.Prompt.Messages {
		if m.Role == types.RoleSystem {
			out = append(out, m)
		}
	}
	return out
}

func sysInstructions() []types.Message {
	return []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}, ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": "ephemeral"}}},
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be kind"}}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"x": 1.0}}},
	}
}

// TS instructions may be SystemModelMessage[]; each message keeps its
// providerOptions through WorkflowAgent, including across prepareCall and
// prepareStep hooks that do not touch the instructions.
func TestWorkflowInstructionMessagesKeepProviderOptions(t *testing.T) {
	noop := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) { return o, nil }
	for _, hooks := range []string{"none", "prepareCall", "prepareStep"} {
		for _, perCall := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				name := hooks
				if perCall {
					name += "/percall"
				}
				if stream {
					name += "/stream"
				}
				t.Run(name, func(t *testing.T) {
					model := &wfMockModel{}
					cfg := WorkflowAgent{Model: model}
					if !perCall {
						cfg.Instructions = sysInstructions()
					}
					switch hooks {
					case "prepareCall":
						cfg.PrepareCall = noop
					case "prepareStep":
						cfg.PrepareStep = noop
					}
					w, err := NewWorkflowAgent(cfg)
					if err != nil {
						t.Fatal(err)
					}
					gen, str := WorkflowGenerateOptions{Prompt: "hi"}, WorkflowStreamOptions{Prompt: "hi"}
					if perCall {
						gen.Instructions, str.Instructions = sysInstructions(), sysInstructions()
					}
					if err := runWorkflow(t, w, stream, gen, str); err != nil {
						t.Fatal(err)
					}
					if len(model.opts) == 0 {
						t.Fatal("no model call")
					}
					sys := systemMessagesOf(model.opts[0])
					if len(sys) != 2 {
						t.Fatalf("system messages = %+v, want 2", sys)
					}
					if sys[0].ProviderOptions["anthropic"] == nil || sys[1].ProviderOptions["openai"] == nil {
						t.Fatalf("providerOptions lost: %+v", sys)
					}
				})
			}
		}
	}
}

// A hook that returns different System text replaces the instruction
// messages (TS `instructions` returned by prepareCall / prepareStep).
func TestWorkflowHookSystemReplacesInstructionMessages(t *testing.T) {
	for _, hookName := range []string{"prepareCall", "prepareStep"} {
		for _, stream := range []bool{false, true} {
			name := hookName
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				model := &wfMockModel{}
				hook := func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					if len(o.InstructionMessages) != 2 || len(o.InitialInstructionMessages) != 2 {
						t.Errorf("hook saw %d / %d instruction messages, want 2", len(o.InstructionMessages), len(o.InitialInstructionMessages))
					}
					o.System = "replaced"
					return o, nil
				}
				cfg := WorkflowAgent{Model: model, Instructions: sysInstructions()}
				if hookName == "prepareCall" {
					cfg.PrepareCall = hook
				} else {
					cfg.PrepareStep = hook
				}
				w, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := runWorkflow(t, w, stream, WorkflowGenerateOptions{Prompt: "hi"}, WorkflowStreamOptions{Prompt: "hi"}); err != nil {
					t.Fatal(err)
				}
				if sys := systemMessagesOf(model.opts[0]); len(sys) != 0 || model.opts[0].Prompt.System != "replaced" {
					t.Fatalf("system = %q, system messages = %+v; want only the replacement", model.opts[0].Prompt.System, sys)
				}
			})
		}
	}
}

// prepareCall may return toolCallConcurrency; a per-call option wins over it.
func TestWorkflowPrepareCallToolCallConcurrency(t *testing.T) {
	one, three := 1, 3
	for _, tc := range []struct {
		name    string
		hook    *int
		perCall *int
		want    int32
	}{
		{name: "hook limits", hook: &one, want: 1},
		{name: "per-call wins over hook", hook: &one, perCall: &three, want: 2},
		{name: "no override", want: 2},
	} {
		for _, stream := range []bool{false, true} {
			name := tc.name
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				var running, peak atomic.Int32
				exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					n := running.Add(1)
					for {
						p := peak.Load()
						if n <= p || peak.CompareAndSwap(p, n) {
							break
						}
					}
					time.Sleep(40 * time.Millisecond)
					running.Add(-1)
					return "ok", nil
				}
				w, err := NewWorkflowAgent(WorkflowAgent{
					Model: &twoToolCallModel{},
					Tools: []types.Tool{
						{Name: "first", Parameters: map[string]interface{}{"type": "object"}, Execute: exec},
						{Name: "second", Parameters: map[string]interface{}{"type": "object"}, Execute: exec},
					},
					PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
						if tc.perCall == nil && o.ToolCallConcurrency != nil {
							t.Errorf("hook saw ToolCallConcurrency %d, want nil (unset)", *o.ToolCallConcurrency)
						}
						o.ToolCallConcurrency = tc.hook
						return o, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := runWorkflow(t, w, stream,
					WorkflowGenerateOptions{Prompt: "hi", ToolCallConcurrency: tc.perCall},
					WorkflowStreamOptions{Prompt: "hi", ToolCallConcurrency: tc.perCall}); err != nil {
					t.Fatal(err)
				}
				if got := peak.Load(); got != tc.want {
					t.Fatalf("peak concurrent tool calls = %d, want %d", got, tc.want)
				}
			})
		}
	}
}

// An invalid prepareCall toolCallConcurrency fails the run (TS
// prepareToolCallConcurrency) unless a per-call value overrides it.
func TestWorkflowPrepareCallInvalidToolCallConcurrency(t *testing.T) {
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
			o.ToolCallConcurrency = &zero
			return o, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
	var invalid *providererrors.InvalidArgumentError
	if !asInvalidArg(err, &invalid) {
		t.Fatalf("error = %v, want InvalidArgumentError", err)
	}
}

func asInvalidArg(err error, target **providererrors.InvalidArgumentError) bool {
	for err != nil {
		if e, ok := err.(*providererrors.InvalidArgumentError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// prepareCall may return telemetry settings that apply to the call: the model
// and step spans go to the returned integration, not the agent's.
func TestWorkflowPrepareCallTelemetryOverride(t *testing.T) {
	newSettings := func(t *testing.T) (*telemetry.Options, *tracetest.SpanRecorder) {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
		return &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("r32")})},
		}, rec
	}
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			base, baseRec := newSettings(t)
			override, overrideRec := newSettings(t)
			var sawBase atomic.Bool
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:     &wfMockModel{},
				Telemetry: base,
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					sawBase.Store(o.Telemetry == base)
					o.Telemetry = override
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := runWorkflow(t, w, stream, WorkflowGenerateOptions{Prompt: "hi"}, WorkflowStreamOptions{Prompt: "hi"}); err != nil {
				t.Fatal(err)
			}
			if !sawBase.Load() {
				t.Error("hook should see the call's current telemetry settings")
			}
			if len(overrideRec.Ended()) == 0 {
				t.Fatalf("telemetry returned by PrepareCall recorded no spans (agent telemetry recorded %d)", len(baseRec.Ended()))
			}
		})
	}
}

// PrepareCall runs once per invocation even when the run first resumes tool
// approvals (TS runs prepareCall before the approvals and never again).
func TestWorkflowPrepareCallOnceWithApprovalResume(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var calls, executed atomic.Int32
			var mu sync.Mutex
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: &wfMockModel{},
				Tools: []types.Tool{{
					Name: "getWeather", ToolApproval: true,
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						executed.Add(1)
						return "sunny", nil
					},
				}},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					mu.Lock()
					defer mu.Unlock()
					calls.Add(1)
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			msgs := workflowApprovalMessages("")
			if err := runWorkflow(t, w, stream, WorkflowGenerateOptions{Messages: msgs}, WorkflowStreamOptions{Messages: msgs}); err != nil {
				t.Fatal(err)
			}
			if executed.Load() != 1 {
				t.Errorf("approved tool ran %d times, want 1", executed.Load())
			}
			if calls.Load() != 1 {
				t.Errorf("PrepareCall ran %d times, want 1", calls.Load())
			}
		})
	}
}
