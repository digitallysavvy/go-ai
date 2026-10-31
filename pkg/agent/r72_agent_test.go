package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 72 ports of TS packages/ai/src/agent/tool-loop-agent.test.ts.

func r72agentTextModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         text,
				Content:      []types.ContentPart{types.TextContent{Text: text}},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: text},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

type r72agentRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *r72agentRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *r72agentRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// TS 2275 "should rethrow the generate error when onError throws": a panicking
// ErrorHandler (TS notify swallows a throwing callback) must not replace the
// generate error.
func TestR72agentGenerateErrorHandlerPanicKeepsError(t *testing.T) {
	providerErr := errors.New("provider failure")
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, providerErr
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { panic("callback failure") },
	})
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Generate panicked: %v", r)
			}
		}()
		_, err = a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	}()
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v, want the provider error", err)
	}
}

// TS 2254 "should call onError and rethrow when generate fails".
func TestR72agentGenerateCallsErrorHandlerOnceAndReturnsError(t *testing.T) {
	providerErr := errors.New("provider failure")
	var got []error
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, providerErr
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { got = append(got, err) },
	})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || !errors.Is(got[0], providerErr) {
		t.Fatalf("ErrorHandler calls = %v", got)
	}
}

// TS 2294 "should not call onError for a timeout in generate()".
func TestR72agentGenerateTimeoutDoesNotCallErrorHandler(t *testing.T) {
	called := false
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	total := 10 * time.Millisecond
	if _, err := a.Generate(context.Background(), AgentGenerateOptions{
		Prompt:  "Hello, world!",
		Timeout: &ai.TimeoutConfig{Total: &total},
	}); err == nil {
		t.Fatal("expected an error")
	}
	if called {
		t.Fatal("ErrorHandler called for a timeout")
	}
}

// TS 2335 "should not call onError when a provider wraps a timeout error".
func TestR72agentGenerateWrappedTimeoutDoesNotCallErrorHandler(t *testing.T) {
	called := false
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, fmt.Errorf("Gateway request failed: %w", context.DeadlineExceeded)
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	if err == nil || !strings.Contains(err.Error(), "Gateway request failed") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("ErrorHandler called for a wrapped timeout")
	}
}

// TS 2409 "should not call onError when generate() is aborted with a custom reason".
func TestR72agentGenerateCustomAbortCauseDoesNotCallErrorHandler(t *testing.T) {
	called := false
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	userLeft := errors.New("user left")
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				cancel(userLeft)
				return nil, context.Cause(c)
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	_, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "Hello, world!"})
	if err == nil || !strings.Contains(err.Error(), "user left") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("ErrorHandler called for an abort")
	}
}

// TS 2356 / 2381: a provider wrapping an abort or a timeout in stream() does
// not reach onError.
func TestR72agentStreamWrappedAbortAndTimeoutDoNotCallOnError(t *testing.T) {
	t.Run("abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{
				DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					cancel()
					return nil, fmt.Errorf("Gateway request failed: %w", context.Canceled)
				},
			},
			MaxRetries:   r72agentIntPtr(0),
			ErrorHandler: func(ctx context.Context, err error) { called = true },
		})
		result, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		if called {
			t.Fatal("OnError called for a wrapped abort")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{
				DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					<-c.Done()
					return nil, fmt.Errorf("Gateway request failed: %w", context.Cause(c))
				},
			},
			MaxRetries:   r72agentIntPtr(0),
			ErrorHandler: func(ctx context.Context, err error) { called = true },
		})
		total := 10 * time.Millisecond
		result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{
			Prompt:  "Hello, world!",
			Timeout: &ai.TimeoutConfig{Total: &total},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		if called {
			t.Fatal("OnError called for a wrapped timeout")
		}
	})
}

// TS 2231 "should call onError when a stream step fails": a stream with only
// stream-start reports NoOutputGeneratedError once.
func TestR72agentStreamNoOutputCallsOnErrorOnce(t *testing.T) {
	var mu sync.Mutex
	var got []error
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}}), nil
			},
		},
		ErrorHandler: func(ctx context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, err)
		},
	})
	result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err == nil {
		t.Fatal("expected the text to fail")
	}
	mu.Lock()
	defer mu.Unlock()
	var noOutput *ai.NoOutputGeneratedError
	if len(got) != 1 || !errors.As(got[0], &noOutput) {
		t.Fatalf("OnError calls = %v", got)
	}
}

// TS 2171 / 2190 / 2207: onAbort fires for an aborted or timed-out stream(),
// not for an aborted generate().
func TestR72agentOnAbort(t *testing.T) {
	blockingStream := func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		<-c.Done()
		return nil, c.Err()
	}
	t.Run("stream abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var calls int
		var mu sync.Mutex
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				cancel()
				return blockingStream(c, opts)
			}},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if len(steps) != 0 {
					t.Errorf("steps = %d, want 0", len(steps))
				}
			},
		})
		result, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Fatalf("AbortHandler calls = %d, want 1", calls)
		}
	})
	t.Run("stream timeout", func(t *testing.T) {
		var calls int
		var mu sync.Mutex
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoStreamFunc: blockingStream},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) {
				mu.Lock()
				defer mu.Unlock()
				calls++
			},
		})
		total := 10 * time.Millisecond
		result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{
			Prompt: "Hello, world!", Timeout: &ai.TimeoutConfig{Total: &total},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Fatalf("AbortHandler calls = %d, want 1", calls)
		}
	})
	t.Run("generate abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				cancel()
				return nil, c.Err()
			}},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) { called = true },
		})
		if _, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "Hello, world!"}); err == nil {
			t.Fatal("expected an error")
		}
		if called {
			t.Fatal("AbortHandler called for generate()")
		}
	})
}

// TS 553-632 "LanguageModelCallOptions forwarding" and 634 "RequestOptions
// forwarding" (headers), for generate and stream: settings reach the model
// call unchanged.
func TestR72agentLanguageModelCallOptionsForwarding(t *testing.T) {
	temp, topP, pres, freq := 0.5, 0.9, 0.3, 0.4
	maxTokens, topK, seed := 100, 40, 42
	stop := []string{"STOP", "END"}
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			var got *provider.GenerateOptions
			model := r72agentTextModel("ok")
			gen, str := model.DoGenerateFunc, model.DoStreamFunc
			model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				got = opts
				return gen(ctx, opts)
			}
			model.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				got = opts
				return str(ctx, opts)
			}
			a := NewToolLoopAgent(AgentConfig{
				Model:            model,
				Temperature:      &temp,
				MaxTokens:        &maxTokens,
				TopP:             &topP,
				TopK:             &topK,
				PresencePenalty:  &pres,
				FrequencyPenalty: &freq,
				StopSequences:    stop,
				Seed:             &seed,
				Headers:          map[string]string{"x-custom": "value"},
			})
			if mode == "generate" {
				if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello"}); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			if got == nil {
				t.Fatal("model not called")
			}
			if got.Temperature == nil || *got.Temperature != temp ||
				got.MaxTokens == nil || *got.MaxTokens != maxTokens ||
				got.TopP == nil || *got.TopP != topP ||
				got.TopK == nil || *got.TopK != topK ||
				got.PresencePenalty == nil || *got.PresencePenalty != pres ||
				got.FrequencyPenalty == nil || *got.FrequencyPenalty != freq ||
				got.Seed == nil || *got.Seed != seed ||
				!reflect.DeepEqual(got.StopSequences, stop) {
				t.Fatalf("call options not forwarded: %+v", got)
			}
			if got.Headers["x-custom"] != "value" {
				t.Fatalf("headers = %v", got.Headers)
			}
		})
	}
}

func r72agentIntPtr(v int) *int { return &v }

// r72agentToolModel calls tool "t" once, then answers with text.
func r72agentToolModel() *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	next := func() int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return calls
	}
	call := types.ToolCall{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{"value": "x"}}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if next() == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{call},
					Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: call.ID, ToolName: call.ToolName, Input: `{"value":"x"}`}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if next() == 1 {
				c := call
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &c},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func r72agentTool() types.Tool {
	return types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result", nil
	}}
}

// TS "should call both constructor and method <callback> in correct order"
// (onStart 1403/1646, onStepStart 1797/2009, onToolExecutionStart 2904/3110,
// onToolExecutionEnd 3331/3548, onStepFinish 2579/2731, onEnd 3714/3863),
// plus TS mergeCallbacks (Promise.allSettled): a throwing constructor
// callback does not stop the method callback.
func TestR72agentMergedCallbacksOrderAndIsolation(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		for _, settingsPanics := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/panic=%v", mode, settingsPanics), func(t *testing.T) {
				rec := &r72agentRecorder{}
				settings := func(name string) {
					rec.add("settings:" + name)
					if settingsPanics {
						panic("settings " + name)
					}
				}
				a := NewToolLoopAgent(AgentConfig{
					Model:                r72agentToolModel(),
					Tools:                []types.Tool{r72agentTool()},
					OnStart:              func(context.Context, ai.OnStartEvent) { settings("start") },
					OnStepStartEvent:     func(context.Context, ai.OnStepStartEvent) { settings("stepStart") },
					OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) { settings("toolStart") },
					OnToolExecutionEnd:   func(context.Context, ai.OnToolCallFinishEvent) { settings("toolEnd") },
					OnStepEndEvent:       func(context.Context, ai.OnStepFinishEvent) { settings("stepEnd") },
					OnEndEvent:           func(context.Context, ai.OnFinishEvent) { settings("end") },
				})
				opts := AgentGenerateOptions{
					Prompt:               "Hello",
					OnStart:              func(context.Context, ai.OnStartEvent) { rec.add("method:start") },
					OnStepStart:          func(context.Context, ai.OnStepStartEvent) { rec.add("method:stepStart") },
					OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) { rec.add("method:toolStart") },
					OnToolExecutionEnd:   func(context.Context, ai.OnToolCallFinishEvent) { rec.add("method:toolEnd") },
					OnStepEnd:            func(context.Context, ai.OnStepFinishEvent) { rec.add("method:stepEnd") },
					OnEnd:                func(context.Context, ai.OnFinishEvent) { rec.add("method:end") },
				}
				if mode == "generate" {
					res, err := a.Generate(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					if res.Text != "done" {
						t.Fatalf("text = %q", res.Text)
					}
				} else {
					r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := r.ReadAll(); err != nil {
						t.Fatal(err)
					}
				}
				want := []string{
					"settings:start", "method:start",
					"settings:stepStart", "method:stepStart",
					"settings:toolStart", "method:toolStart",
					"settings:toolEnd", "method:toolEnd",
					"settings:stepEnd", "method:stepEnd",
					"settings:stepStart", "method:stepStart",
					"settings:stepEnd", "method:stepEnd",
					"settings:end", "method:end",
				}
				if got := rec.get(); !reflect.DeepEqual(got, want) {
					t.Fatalf("events =\n%v\nwant\n%v", got, want)
				}
			})
		}
	}
}

// TS 2433 counterpart for the merged stream error handlers: a panicking
// config ErrorHandler still lets the per-call OnError run.
func TestR72agentStreamMergedOnErrorIsolation(t *testing.T) {
	rec := &r72agentRecorder{}
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, errors.New("boom")
			},
		},
		MaxRetries: r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) {
			rec.add("config")
			panic("config handler")
		},
	})
	r, err := a.Stream(context.Background(), AgentStreamOptions{
		AgentGenerateOptions: AgentGenerateOptions{Prompt: "hi"},
		OnError:              func(ctx context.Context, err error) { rec.add("call") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.ReadAll()
	if got := rec.get(); !reflect.DeepEqual(got, []string{"config", "call"}) {
		t.Fatalf("events = %v", got)
	}
}

func r72agentUsageModel(inner *testutil.MockLanguageModel) *testutil.MockLanguageModel {
	in, out := int64(3), int64(10)
	gen, str := inner.DoGenerateFunc, inner.DoStreamFunc
	inner.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
		r, err := gen(ctx, opts)
		if r != nil {
			r.Usage = types.Usage{InputTokens: &in, OutputTokens: &out}
		}
		return r, err
	}
	inner.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		s, err := str(ctx, opts)
		if err != nil {
			return nil, err
		}
		var chunks []provider.StreamChunk
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeFinish {
				c.Usage = &types.Usage{InputTokens: &in, OutputTokens: &out}
			}
			chunks = append(chunks, *c)
		}
		return testutil.NewMockTextStream(chunks), nil
	}
	return inner
}

// TS "should pass correct event information" for onStart (1488/1673),
// onStepStart (1882/2036), onToolExecutionStart (2936/3144),
// onToolExecutionEnd (3363/3582), onStepFinish (2604/2758) and onEnd
// (3739/3890), in generate and stream. onEnd usage is the total (TS
// generate-text.ts 1731 usage: totalUsage).
func TestR72agentCallbackEventInformation(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var start ai.OnStartEvent
			var stepStarts []ai.OnStepStartEvent
			var toolStart ai.OnToolCallStartEvent
			var toolEnd ai.OnToolCallFinishEvent
			var stepEnds []ai.OnStepFinishEvent
			var end ai.OnFinishEvent
			n := 0
			temp, maxTokens := 0.7, 500
			instr := "You are a helpful assistant"
			a := NewToolLoopAgent(AgentConfig{
				Model:          r72agentUsageModel(r72agentToolModel()),
				Tools:          []types.Tool{r72agentTool()},
				Instructions:   &instr,
				Temperature:    &temp,
				MaxTokens:      &maxTokens,
				RuntimeContext: map[string]interface{}{"userId": "test-user"},
				Internal: &ai.InternalOptions{GenerateCallID: func() string {
					mu.Lock()
					defer mu.Unlock()
					id := fmt.Sprintf("call-%d", n)
					n++
					return id
				}},
			})
			opts := AgentGenerateOptions{
				Prompt:  "test",
				OnStart: func(_ context.Context, e ai.OnStartEvent) { start = e },
				OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) {
					mu.Lock()
					defer mu.Unlock()
					stepStarts = append(stepStarts, e)
				},
				OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) { toolStart = e },
				OnToolExecutionEnd:   func(_ context.Context, e ai.OnToolCallFinishEvent) { toolEnd = e },
				OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
					mu.Lock()
					defer mu.Unlock()
					stepEnds = append(stepEnds, e)
				},
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) { end = e },
			}
			if mode == "generate" {
				if _, err := a.Generate(context.Background(), opts); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			userMsg := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
			rc := map[string]interface{}{"userId": "test-user"}

			if start.Provider != "mock" || start.ModelID != "mock-model" || start.Instructions != instr ||
				start.Temperature == nil || *start.Temperature != 0.7 || start.MaxTokens == nil || *start.MaxTokens != 500 ||
				!reflect.DeepEqual(start.Messages, userMsg) || !reflect.DeepEqual(start.RuntimeContext, rc) {
				t.Errorf("onStart = provider %q model %q instr %q temp %v max %v messages %+v rc %v",
					start.Provider, start.ModelID, start.Instructions, start.Temperature, start.MaxTokens, start.Messages, start.RuntimeContext)
			}
			if len(stepStarts) != 2 {
				t.Fatalf("step starts = %d", len(stepStarts))
			}
			s0 := stepStarts[0]
			if s0.StepNumber != 0 || len(s0.Steps) != 0 || s0.Instructions != instr || len(s0.Messages) != 1 ||
				s0.Provider != "mock" || s0.ModelID != "mock-model" || !reflect.DeepEqual(s0.RuntimeContext, rc) {
				t.Errorf("first onStepStart = %+v", s0)
			}
			if stepStarts[1].StepNumber != 1 || len(stepStarts[1].Steps) != 1 {
				t.Errorf("second onStepStart step %d steps %d", stepStarts[1].StepNumber, len(stepStarts[1].Steps))
			}
			wantInput := map[string]interface{}{"value": "x"}
			if toolStart.CallID != "call-0" || !reflect.DeepEqual(toolStart.Messages, userMsg) ||
				toolStart.ToolCall.ID != "call-1" || toolStart.ToolCall.ToolName != "t" ||
				!reflect.DeepEqual(toolStart.ToolCall.Arguments, wantInput) || toolStart.ToolContext != nil {
				t.Errorf("onToolExecutionStart = %+v", toolStart)
			}
			out := toolEnd.ToolOutput
			if toolEnd.CallID != "call-0" || !reflect.DeepEqual(toolEnd.Messages, userMsg) ||
				toolEnd.ToolCall.ID != "call-1" || out.ToolCallID != "call-1" || out.ToolName != "t" ||
				out.Result != "result" || out.Dynamic || out.Error != nil || !reflect.DeepEqual(out.Input, wantInput) {
				t.Errorf("onToolExecutionEnd = %+v", toolEnd)
			}
			if len(stepEnds) != 2 {
				t.Fatalf("step ends = %d", len(stepEnds))
			}
			se := stepEnds[1]
			if se.FinishReason != types.FinishReasonStop || se.StepNumber != 1 || se.Text != "done" ||
				se.Usage.InputTokens == nil || *se.Usage.InputTokens != 3 || se.Usage.OutputTokens == nil || *se.Usage.OutputTokens != 10 {
				t.Errorf("onStepEnd = %+v", se)
			}
			if end.Text != "done" || end.FinishReason != types.FinishReasonStop || len(end.Steps) != 2 ||
				end.Usage.InputTokens == nil || *end.Usage.InputTokens != 6 || end.FinalStep.Usage.InputTokens == nil || *end.FinalStep.Usage.InputTokens != 3 || end.TotalUsage.InputTokens == nil || *end.TotalUsage.InputTokens != 6 {
				t.Errorf("onEnd = text %q finish %q steps %d usage %d total %d", end.Text, end.FinishReason, len(end.Steps), *end.Usage.InputTokens, *end.TotalUsage.InputTokens)
			}
		})
	}
}

// r72agentCapture wraps the model so each call's options are recorded.
func r72agentCapture(model *testutil.MockLanguageModel) (*testutil.MockLanguageModel, func() *provider.GenerateOptions) {
	var mu sync.Mutex
	var last *provider.GenerateOptions
	gen, str := model.DoGenerateFunc, model.DoStreamFunc
	model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		last = opts
		mu.Unlock()
		return gen(ctx, opts)
	}
	model.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		last = opts
		mu.Unlock()
		return str(ctx, opts)
	}
	return model, func() *provider.GenerateOptions {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func r72agentRun(t *testing.T, a *ToolLoopAgent, mode string, opts AgentGenerateOptions) (*ai.GenerateTextResult, error) {
	t.Helper()
	if mode == "generate" {
		return a.Generate(context.Background(), opts)
	}
	r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
	if err != nil {
		return nil, err
	}
	_, err = r.ReadAll()
	return nil, err
}

// TS 79/804 "should use prepareCall", 165/872 "should forward toolOrder",
// 152 "should preserve a caller-provided user-agent", 191/899 "should pass
// sandbox to prepareCall", 233/978 and 248/995 allowSystemInMessages, 266
// rejection, 288/962 "should pass settings timeout".
func TestR72agentPrepareCallAndSettingsForwarding(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode+"/prepareCall providerOptions", func(t *testing.T) {
			model, last := r72agentCapture(r72agentTextModel("reply"))
			a := NewToolLoopAgent(AgentConfig{
				Model: model,
				PrepareCall: func(ctx context.Context, c PrepareCallConfig) PrepareCallConfig {
					opts, _ := c.CallOptions.(map[string]interface{})
					c.ProviderOptions = map[string]interface{}{"test": map[string]interface{}{"value": opts["value"]}}
					return c
				},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "Hello, world!", CallOptions: map[string]interface{}{"value": "test"}}); err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"test": map[string]interface{}{"value": "test"}}
			if !reflect.DeepEqual(last().ProviderOptions, want) {
				t.Fatalf("providerOptions = %v", last().ProviderOptions)
			}
		})
		t.Run(mode+"/toolOrder", func(t *testing.T) {
			model, last := r72agentCapture(r72agentTextModel("reply"))
			obj := map[string]interface{}{"type": "object"}
			a := NewToolLoopAgent(AgentConfig{
				Model:     model,
				Tools:     []types.Tool{{Name: "zebra", Parameters: obj}, {Name: "alpha", Parameters: obj}, {Name: "middle", Parameters: obj}},
				ToolOrder: []string{"middle"},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "Hello, world!"}); err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, tl := range last().Tools {
				names = append(names, tl.Name)
			}
			if !reflect.DeepEqual(names, []string{"middle", "alpha", "zebra"}) {
				t.Fatalf("tools = %v", names)
			}
		})
		t.Run(mode+"/user-agent", func(t *testing.T) {
			model, last := r72agentCapture(r72agentTextModel("reply"))
			a := NewToolLoopAgent(AgentConfig{Model: model, Headers: map[string]string{"user-agent": "my-app/1.0"}})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "Hello, world!"}); err != nil {
				t.Fatal(err)
			}
			ua := last().Headers["user-agent"]
			if !strings.Contains(ua, "my-app/1.0") || !strings.Contains(ua, "ai-sdk-agent/tool-loop") {
				t.Fatalf("user-agent = %q", ua)
			}
		})
		t.Run(mode+"/sandbox to prepareCall", func(t *testing.T) {
			sandbox := &struct{ name string }{"test sandbox"}
			var recorded interface{}
			a := NewToolLoopAgent(AgentConfig{
				Model: r72agentTextModel("reply"),
				PrepareCall: func(ctx context.Context, c PrepareCallConfig) PrepareCallConfig {
					recorded = c.ExperimentalSandbox
					return c
				},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "Hello, world!", ExperimentalSandbox: sandbox}); err != nil {
				t.Fatal(err)
			}
			if recorded != sandbox {
				t.Fatalf("sandbox = %v", recorded)
			}
		})
		sys := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "SYSTEM INSTRUCTIONS"}}}}
		for _, via := range []string{"settings", "prepareCall"} {
			t.Run(mode+"/allowSystemInMessages via "+via, func(t *testing.T) {
				model, last := r72agentCapture(r72agentTextModel("reply"))
				cfg := AgentConfig{Model: model}
				if via == "settings" {
					cfg.AllowSystemInMessages = true
				} else {
					cfg.PrepareCall = func(ctx context.Context, c PrepareCallConfig) PrepareCallConfig {
						c.AllowSystemInMessages = true
						return c
					}
				}
				if _, err := r72agentRun(t, NewToolLoopAgent(cfg), mode, AgentGenerateOptions{Messages: sys}); err != nil {
					t.Fatal(err)
				}
				msgs := last().Prompt.Messages
				if len(msgs) != 1 || msgs[0].Role != types.RoleSystem {
					t.Fatalf("prompt = %+v", last().Prompt)
				}
			})
		}
		t.Run(mode+"/settings timeout", func(t *testing.T) {
			var hasDeadline bool
			model := r72agentTextModel("reply")
			gen, str := model.DoGenerateFunc, model.DoStreamFunc
			model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				_, hasDeadline = ctx.Deadline()
				return gen(ctx, opts)
			}
			model.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				_, hasDeadline = ctx.Deadline()
				return str(ctx, opts)
			}
			total := 5 * time.Second
			a := NewToolLoopAgent(AgentConfig{Model: model, Timeout: &ai.TimeoutConfig{Total: &total}})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "Hello, world!"}); err != nil {
				t.Fatal(err)
			}
			if !hasDeadline {
				t.Fatal("settings timeout not applied to the model call")
			}
		})
	}
	t.Run("generate/reject system messages", func(t *testing.T) {
		a := NewToolLoopAgent(AgentConfig{Model: r72agentTextModel("reply")})
		_, err := a.Generate(context.Background(), AgentGenerateOptions{Messages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "SYSTEM INSTRUCTIONS"}}}}})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "system messages are not allowed") {
			t.Fatalf("err = %v", err)
		}
	})
}

// TS 648 "should forward include to generateText" (requestMessages) and 1128
// "should forward include to streamText" (rawChunks).
func TestR72agentIncludeForwarding(t *testing.T) {
	t.Run("generate requestMessages", func(t *testing.T) {
		a := NewToolLoopAgent(AgentConfig{Model: r72agentTextModel("reply"), Include: &ai.IncludeOptions{RequestMessages: true}})
		res, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "test"})
		if err != nil {
			t.Fatal(err)
		}
		want := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
		if !reflect.DeepEqual(res.FinalStep.Request.Messages, want) {
			t.Fatalf("request messages = %+v", res.FinalStep.Request.Messages)
		}
	})
	t.Run("stream rawChunks", func(t *testing.T) {
		model, last := r72agentCapture(r72agentTextModel("reply"))
		yes := true
		a := NewToolLoopAgent(AgentConfig{Model: model, Include: &ai.IncludeOptions{RawChunks: &yes}})
		if _, err := r72agentRun(t, a, "stream", AgentGenerateOptions{Prompt: "Hello, world!"}); err != nil {
			t.Fatal(err)
		}
		if !last().IncludeRawChunks {
			t.Fatal("includeRawChunks not forwarded")
		}
	})
}

// TS 4544 / 4587 callOptionsSchema: invalid options fail before the model
// call with "Type validation failed for options"; valid ones pass.
func TestR72agentCallOptionsSchema(t *testing.T) {
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"topic": map[string]interface{}{"type": "string", "enum": []interface{}{"legal", "medical"}}},
		"required":   []interface{}{"topic"},
	})
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			called := false
			model := r72agentTextModel("reply")
			gen, str := model.DoGenerateFunc, model.DoStreamFunc
			model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				called = true
				return gen(ctx, opts)
			}
			model.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				called = true
				return str(ctx, opts)
			}
			a := NewToolLoopAgent(AgentConfig{Model: model, CallOptionsSchema: sch})
			_, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "hi", CallOptions: map[string]interface{}{"topic": "evil"}})
			if err == nil || !strings.Contains(err.Error(), "Type validation failed for options") {
				t.Fatalf("err = %v", err)
			}
			if called {
				t.Fatal("model called despite invalid options")
			}
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "hi", CallOptions: map[string]interface{}{"topic": "legal"}}); err != nil {
				t.Fatalf("valid options: %v", err)
			}
		})
	}
}

type r72agentIntegration struct {
	telemetry.NoopTelemetryIntegration
	prefix string
	rec    *r72agentRecorder
	panics bool
	rcs    *[]interface{}
}

func (i *r72agentIntegration) note(name string, rc map[string]interface{}) {
	i.rec.add(i.prefix + name)
	if i.rcs != nil && rc != nil {
		i.rec.mu.Lock()
		*i.rcs = append(*i.rcs, rc)
		i.rec.mu.Unlock()
	}
	if i.panics {
		panic("integration error")
	}
}

func (i *r72agentIntegration) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	i.note("onStart", e.RuntimeContext)
	return ctx
}

func (i *r72agentIntegration) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	i.note("onStepStart", nil)
	return ctx
}

func (i *r72agentIntegration) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	i.note("onToolExecutionStart", nil)
	return ctx
}

func (i *r72agentIntegration) OnToolExecutionEnd(ctx context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	i.note("onToolExecutionEnd", nil)
}

func (i *r72agentIntegration) OnStepEnd(ctx context.Context, e telemetry.TelemetryStepEndEvent) {
	i.note("onStepEnd", e.RuntimeContext)
}

func (i *r72agentIntegration) OnEnd(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	i.note("onEnd", e.RuntimeContext)
}

// TS 3925-4540 "telemetry integrations" for generate and stream: per-call
// listeners see every lifecycle event; listeners run alongside the agent
// callbacks (callbacks first for step end and end; start telemetry first is
// the closed r71 known difference); a panicking listener does not break the
// run; includeRuntimeContext filters the telemetry runtime context only.
func TestR72agentTelemetryIntegrations(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode+"/lifecycle", func(t *testing.T) {
			rec := &r72agentRecorder{}
			a := NewToolLoopAgent(AgentConfig{
				Model:     r72agentToolModel(),
				Tools:     []types.Tool{r72agentTool()},
				Telemetry: &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r72agentIntegration{rec: rec}}},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "test"}); err != nil {
				t.Fatal(err)
			}
			want := []string{"onStart", "onStepStart", "onToolExecutionStart", "onToolExecutionEnd", "onStepEnd", "onStepStart", "onStepEnd", "onEnd"}
			if got := rec.get(); !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %v", got)
			}
		})
		t.Run(mode+"/alongside callbacks", func(t *testing.T) {
			rec := &r72agentRecorder{}
			a := NewToolLoopAgent(AgentConfig{
				Model:             r72agentTextModel("Hello!"),
				OnStart:           func(context.Context, ai.OnStartEvent) { rec.add("agent-onStart") },
				OnStepFinishEvent: func(context.Context, ai.OnStepFinishEvent) { rec.add("agent-onStepFinish") },
				OnFinishEvent:     func(context.Context, ai.OnFinishEvent) { rec.add("agent-onFinish") },
				Telemetry:         &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r72agentIntegration{rec: rec, prefix: "integration-"}}},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "test"}); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"integration-onStart", "agent-onStart",
				"integration-onStepStart",
				"agent-onStepFinish", "integration-onStepEnd",
				"agent-onFinish", "integration-onEnd",
			}
			if got := rec.get(); !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %v", got)
			}
		})
		t.Run(mode+"/listener panics", func(t *testing.T) {
			rec := &r72agentRecorder{}
			a := NewToolLoopAgent(AgentConfig{
				Model:     r72agentTextModel("Hello!"),
				Telemetry: &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r72agentIntegration{rec: rec, panics: true}}},
			})
			if mode == "generate" {
				res, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "test"})
				if err != nil || res.Text != "Hello!" {
					t.Fatalf("res=%v err=%v", res, err)
				}
				return
			}
			r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "test"}})
			if err != nil {
				t.Fatal(err)
			}
			text, err := r.ReadAll()
			if err != nil || text != "Hello!" {
				t.Fatalf("text=%q err=%v", text, err)
			}
		})
		t.Run(mode+"/includeRuntimeContext", func(t *testing.T) {
			rec := &r72agentRecorder{}
			var telemetryRCs []interface{}
			var callbackRCs []interface{}
			full := map[string]interface{}{"userId": "user-123", "requestId": "request-123"}
			note := func(rc interface{}) {
				rec.mu.Lock()
				defer rec.mu.Unlock()
				callbackRCs = append(callbackRCs, rc)
			}
			a := NewToolLoopAgent(AgentConfig{
				Model:             r72agentTextModel("Hello!"),
				RuntimeContext:    full,
				OnStart:           func(_ context.Context, e ai.OnStartEvent) { note(e.RuntimeContext) },
				OnStepFinishEvent: func(_ context.Context, e ai.OnStepFinishEvent) { note(e.RuntimeContext) },
				OnFinishEvent:     func(_ context.Context, e ai.OnFinishEvent) { note(e.RuntimeContext) },
				Telemetry: &ai.TelemetrySettings{
					IncludeRuntimeContext: map[string]bool{"requestId": true},
					Integrations:          []telemetry.TelemetryIntegration{&r72agentIntegration{rec: rec, rcs: &telemetryRCs}},
				},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "test"}); err != nil {
				t.Fatal(err)
			}
			filtered := map[string]interface{}{"requestId": "request-123"}
			if !reflect.DeepEqual(callbackRCs, []interface{}{full, full, full}) {
				t.Errorf("callback runtime contexts = %v", callbackRCs)
			}
			if !reflect.DeepEqual(telemetryRCs, []interface{}{filtered, filtered, filtered}) {
				t.Errorf("telemetry runtime contexts = %v", telemetryRCs)
			}
		})
	}
	t.Run("generate/global", func(t *testing.T) {
		rec := &r72agentRecorder{}
		telemetry.RegisterTelemetryIntegration(&r72agentIntegration{rec: rec, prefix: "global-"})
		defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})
		a := NewToolLoopAgent(AgentConfig{Model: r72agentTextModel("Hello!")})
		if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "test"}); err != nil {
			t.Fatal(err)
		}
		want := []string{"global-onStart", "global-onStepStart", "global-onStepEnd", "global-onEnd"}
		if got := rec.get(); !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %v", got)
		}
	})
}

// TS tool-loop-agent-steering.test.ts: a message steered during the first
// model call is delivered at step 1 and the run continues past a stop finish
// (generate with the default stop condition, stream, createAgentUIStream).
func TestR72agentSteeringDeliveredAtNextStep(t *testing.T) {
	for _, mode := range []string{"generate", "stream", "ui"} {
		t.Run(mode, func(t *testing.T) {
			steering := ai.NewSteering()
			var mu sync.Mutex
			var handle *ai.SteerHandle
			call := 0
			first := func() bool {
				mu.Lock()
				defer mu.Unlock()
				call++
				if call == 1 {
					handle = steering.Steer("Also tell me about Tokyo.")
					return true
				}
				return false
			}
			model := &testutil.MockLanguageModel{
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					text := "Tokyo is rainy."
					if first() {
						text = "It is sunny in Paris."
					}
					return &types.GenerateResult{Text: text, Content: []types.ContentPart{types.TextContent{Text: text}}, FinishReason: types.FinishReasonStop}, nil
				},
				DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					text := "Tokyo is rainy."
					if first() {
						text = "Sunny."
					}
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: text},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
					}), nil
				},
			}
			a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{r72agentTool()}})
			opts := AgentGenerateOptions{Prompt: "What is the weather in Paris?", ExperimentalSteering: steering}
			switch mode {
			case "generate":
				res, err := a.Generate(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				if res.Text != "Tokyo is rainy." {
					t.Fatalf("text = %q", res.Text)
				}
			case "stream":
				r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.ReadAll(); err != nil {
					t.Fatal(err)
				}
				if got := r.Text(); got != "Tokyo is rainy." {
					t.Fatalf("text = %q", got)
				}
			case "ui":
				chunks, errs, err := CreateAgentUIStreamFromUIMessages(context.Background(), a, CreateAgentUIStreamFromUIMessagesOptions{
					UIMessages:   `[{"id":"message-1","role":"user","parts":[{"type":"text","text":"What is the weather in Paris?"}]}]`,
					AgentOptions: AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{ExperimentalSteering: steering}},
				})
				if err != nil {
					t.Fatal(err)
				}
				for range chunks {
				}
				for e := range errs {
					if e != nil {
						t.Fatal(e)
					}
				}
				mu.Lock()
				n := call
				mu.Unlock()
				if n != 2 {
					t.Fatalf("doStream calls = %d", n)
				}
			}
			mu.Lock()
			h := handle
			mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			o, err := h.Wait(ctx)
			if err != nil || !o.Delivered || o.StepNumber != 1 {
				t.Fatalf("outcome = %+v err = %v", o, err)
			}
		})
	}
}
